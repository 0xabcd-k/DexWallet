# Solana 到账核验

[项目入口](../README.md) · [阶段清单](工程阶段与工作清单.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md)

## 当前能力与边界

2026-11-16 完成 5.3 首批：服务端读取固定 Solana 网络的交易、核验一条顶层转账指令，并按事件身份保存不可覆盖的链上事实。新增 business 16。没有公开 HTTP 接口或页面变化，5.2 最终付款按钮仍禁用。

核验结果证明可信节点报告的转账事实，不代表某产品用户拥有该笔充值，也不是整笔交易的净收入证明。一个成功交易可以包含多笔转入和转出；业务入账还要结合不可变订单、平台控制的收款账户、授权交易形态及唯一事件分配。5.5/6.5 必须在订单结算事务内落实这些约束，不能把本工具的成功输出直接转换成美元余额。

本批没有平台签名、广播、订单状态机、监听游标、后台重试或自动入账；没有收取手续费。失败交易也不写成功转账事件，故本表不是完整的失败 Gas 账单。费用承担仍遵循 [逐项审核清单](../design/链上资金费用逐项审核.md)，不能把本批实施解释为待审核规则已批准。

2026-11-19 [Base 核验](Base到账核验.md) 扩展共享事实表至 business 17；Solana 的输入和核验规则保持，事件增加 `kind=solana_instruction`，升级后原证据和指纹继续复用。

## 输入及核验规则

内部 `chainreceipt.Request` 包含 `network/signature/instruction/assetId/recipient/atoms`。`instruction` 从 0 开始，只指向顶层指令；`recipient` 是接收钱包 owner，SPL 场景不是 token account；`atoms` 为正的规范十进制最小单位整数，最大为 u64，不接受浮点数或带前导零的字符串。

1. 网络与资产必须来自固定目录；Devnet 测试 USDT 的 ID 包含完整 mint，主网资产不能由配置替换。只用服务器配置的 RPC，并核对 genesis hash。
2. 使用 `getTransaction` 的 `jsonParsed/finalized/maxSupportedTransactionVersion=0`，另查 `getSignatureStatuses` 并启用历史搜索。第一签名须与请求一致，交易和状态 slot 一致，状态为 finalized，两处执行结果均成功。
3. 账户表不得重复，付款账户及签名数量须一致。SOL 只接受 System Program 的 `transfer`，核对源账户签名、源/目标可写及精确 lamports。
4. SPL 只接受原版 Token Program 的 `transferChecked`，核对完整 mint、精度、原子数量、签名 authority 与源/目标账户。源 owner 取交易前 token balances，目标 owner 取交易后 token balances，通过原交易账户索引关联；不用当前账户查询替代历史归属。
5. 预期收款 owner 和数量必须完全相等，自转不作为到账。本批不支持 CPI、Token-2022、SPL 普通 `transfer`、多签 authority 或历史 owner 不可取得的形态，返回不支持或无效证据。

`getTransaction=null` 或尚未 finalized 返回 `ErrUnconfirmed`：既不能入账，也不能据此退款或重发。最终确认的失败返回 `ErrExecutionFailed`；响应不完整、金额/资产/收款人不符返回 `ErrInvalidEvidence`，未覆盖的交易形态返回 `ErrUnsupported`，节点错误保留独立原因。

协议依据：[getTransaction](https://solana.com/docs/rpc/http/gettransaction)、[交易 JSON 结构](https://solana.com/docs/rpc/json-structures)、[历史签名状态](https://solana.com/docs/rpc/http/getsignaturestatuses)。这是基于部署所信任 RPC 的核验，不是独立重放共识或密码学证明；恶意 RPC 可以伪造响应，因此不能接受客户端提交的 RPC 地址或自制 Receipt。

## 持久化、并发与恢复

`chain_observed_transaction` 按 `(network, signature)` 唯一，记录 slot、fee payer、网络费、指纹和首次原始 RPC 结果；`chain_transfer_event` 按 `(transaction_id, instruction_index)` 唯一，记录资产、程序、mint、精度、源与目标账户/owner、数量。数字自增 ID 只供内部关联，对外事件身份仍为网络、签名和指令位置。

网络费取 `meta.fee`，单位为 lamports，只存一次；代币账户开户 rent 不包含在该字段中，更不能据此直接确定用户应付的平台费用。一笔交易的多个事件不得重复累加同一笔网络费。

RPC 在 SQL 事务前完成，随后先锁定唯一交易记录，再核对事实和指令事件。重复请求返回 `reused=true`；已保存事实与新观察冲突返回 `ErrConflict`，不覆盖历史。指纹对保留的解析字段进行精确 JSON 归一化和 SHA-256，覆盖 slot、版本、签名、账户、顶层指令及 token balances；不包含状态查询时刻，JSON 对象字段顺序无影响。它用于检测观察冲突，不冒充原始签名字节的交易哈希。

数据库提交结果不明时，只查询原事件：存在且一致则恢复成功，否则返回 `ErrOutcomeUnknown`，由调用方保留同一事件身份后续核查。父记录和事件在同一数据库事务提交，不写用户账户、凭证、订单或 outbox。

DDL 在 `backend/manifest/sql/ddl/chainreceipt/`，迁移为 `000016_chain_receipts`。model 由 goctl 生成，锁定查询为手写扩展；业务位于 `backend/logic/chainreceipt`，复用 `chainassets` HTTP 客户端，在 `ServiceContext.ChainReceipts` 组装。回退到 15 只允许空表；已有证据时迁移明确失败，须先制定归档/恢复方案，不能强行丢弃记录。失败回退可能留下 guard 表及迁移 dirty 状态，应按迁移故障流程处理。

## 运维工具与验证

默认只读，不需要数据库或密钥。在 `backend/` 执行以下历史 SOL 样例：

```bash
DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL=https://api.devnet.solana.com \
go run ./tools/chain_receipts \
  --network solana-devnet \
  --signature 3uechSmrGT1Axu3zhxVj6URgbMgoLXLyzSd1rS2EnJ88LpRRfMkB8ahZ4Csh13o6VzFmiyoAnHmmoMQ2yYxv5FJn \
  --instruction 0 --asset solana-devnet:sol \
  --recipient 7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5 --atoms 1000
```

显式 `--record` 才使用 `DEXWALLET_MYSQL_DSN` 写入两张事实表，要求 business ≥17 且 dirty=false；输出 receipt/recorded/reused。默认总 RPC 超时 10 秒，可用 `--timeout` 设置，最大 30 秒，另预留 5 秒给数据库；服务内默认沿用 `ChainAssets.Timeout`。配置和 HTTP 响应限制沿用 [链上资产查询](链上资产查询.md)，内部工具不暴露公开用户请求入口。

普通 `go test ./logic/chainreceipt ./logic/chainassets` 使用 fake/本机 httptest 和公开历史快照，不连接真实链。隔离 MySQL 行为在 `TestChainReceiptSchemaAndPersistence`，使用 `tools/testenv`，不得指向生产库。

真实只读复查独立运行，须显式配置 Devnet URL 和测试 mint：

```bash
export DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL=https://api.devnet.solana.com
export DEXWALLET_CHAIN_SOLANA_DEVNET_TEST_USDT_CONTRACT=2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5
DEXWALLET_TEST_CHAIN_RECEIPTS_LIVE=1 \
go test -tags=integration ./logic/chainreceipt -run '^TestLiveHistoricalReceipts$' -count=1 -v
```

可选 `DEXWALLET_TEST_CHAIN_RECEIPTS_EVIDENCE` 为已存在目录的绝对路径，保存公开请求、结果和证据。三个快照位于 `backend/logic/chainreceipt/testdata/`，测试 USDT 为自建替代币，不是 Tether 正式发行资产。真实节点、离线回归和隔离数据库结果见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
