# Base 到账核验

[项目入口](../README.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md) · [Solana 核验](Solana到账核验.md)

## 当前范围

2026-11-19 接通 Base 主网/Sepolia 固定目录下的普通 ETH 外层转账及 USDC Transfer 日志核验、持久去重和提交异常恢复。真实只读验收使用两笔已有 Base Sepolia 交易，没有签名、广播、充值或支付动作。主网未实测。

平台代付 USDC 时，外层交易发送人是付费方，实际出资人来自 USDC 日志。本批不根据 tx.from 猜测充值用户，也不把调用成功等同于代币到账。此前 MetaMask 智能账户 ETH 代付属于内部调用，外层 value=0；本批明确返回不支持。其后续核验须接入可靠的内部执行证据，不能仅信任外层成功、授权事件或调用参数。

与 Solana 一致，本批仅记录转账事件事实。它不证明产品订单归属，也不保证整笔交易的净实收；自动入账仍需不可变订单、平台控制的收款目标、允许的交易形态及唯一事件分配。链上签名广播、平台收费、失败 Gas 回收和用户页面没有开放。

## 内部入口与事件身份

`chainreceipt.BaseRequest` 包含 `network/hash/assetId/recipient/atoms`，USDC 另需 `logIndex`，普通 ETH 必须省略。网络只能是 `base-mainnet` 或 `base-sepolia`，资产来自既有固定目录；地址及哈希验证长度和十六进制后以小写保存，数量为正的规范十进制 uint256 最小单位整数，不接受浮点或前导零。

1. `chainassets.ReadBaseEvidence` 校验配置网络 chain ID，依次取得交易、回执、finalized 区块头和付款高度的当前规范区块；请求不能带入任意 RPC URL。
2. 交易与回执的哈希、块哈希、高度、交易位置、发送人、目标和类型必须一致。付款高度不高于 finalized 高度；规范区块哈希须匹配，且该位置的交易哈希必须存在。相同高度的 finalized 头也必须匹配块哈希。
3. 普通 ETH 只接受空 calldata、正 value 的成功外层转账。USDC 必须来自完整白名单合约地址，topic0 为标准 Transfer，两个地址 topic 和数量 data 都按精确 ABI 宽度解析；源不得为零地址或收款人，金额和收款人须与预期完全一致。
4. 日志使用 RPC 自身的 `logIndex`，不是回执日志数组下标。每条日志须具有一致的交易/区块定位、唯一索引及 `removed=false`。本次真实 USDC 回执有两条日志，转账在数组第 2 项，实际 logIndex 是 5。
5. 只接受普通 EVM 交易类型 0/1/2/4；合约创建、存入类型、内部 ETH、未知形态不作为本批成功事件。

`BaseService.Record` 必须自己读取并核验，不接受客户端构造的 Receipt。`Find` 只恢复已保存事件；入口在 `ServiceContext.BaseReceipts`，没有公开 HTTP API。共享存储的 `signature/slot/instruction` 历史字段在 Base 分别表示交易哈希、区块高度和事件位置，明确由 `event.kind` 区分：

| 类型 | 位置 | 唯一事件身份 |
| --- | --- | --- |
| `solana_instruction` | 顶层指令索引 | 网络 + 签名 + 类型 + 位置 |
| `evm_value` | 固定 0 | 网络 + 交易哈希 + 类型 + 0 |
| `evm_log` | RPC logIndex | 网络 + 交易哈希 + 类型 + 日志位置 |

因此同一交易的外层 value 和第 0 条日志不会碰撞。第一次保存原始证据；后续观察只比较稳定交易、回执和费用事实，finalized 头前进不会制造冲突。金额、日志或其他保留事实变化则返回 `ErrConflict`，不覆盖原记录。

协议依据：[Ethereum JSON-RPC](https://ethereum.org/developers/docs/apis/json-rpc/)、[ERC-20 Transfer](https://eips.ethereum.org/EIPS/eip-20)。核验依赖部署所信任的节点，不独立验证共识或重放 EVM；恶意 RPC 能伪造结果，因此网络配置是服务端信任边界。

## 网络费用与未知结果

`feeAtoms = executionFeeAtoms + l1FeeAtoms + operatorFeeAtoms`，均为 wei。其中执行费为回执 `gasUsed × effectiveGasPrice`，L1 数据费取回执 `l1Fee`。Operator 费调用固定 GasPriceOracle `0x420000000000000000000000000000000000000f` 的 `getOperatorFee(gasUsed)`，使用回执块哈希和 `requireCanonical=true` 读取该区块的规则；不使用当前费率，也不因回执省略字段就默认为零。所需 RPC 需支持该历史区块的 `eth_call`。

参考 [Base 费用组成](https://docs.base.org/specifications/transactions/network-fees)、[OP Stack Operator 费与回执](https://specs.optimism.io/protocol/isthmus/exec-engine.html#operator-fee)、[GasPriceOracle 实现](https://github.com/ethereum-optimism/optimism/blob/develop/packages/contracts-bedrock/src/L2/GasPriceOracle.sol)。本批记录观察到的实际网络费；用户最终扣费、费用资产与承担规则仍以 [逐项审核清单](../design/链上资金费用逐项审核.md) 为准。

交易或回执不存在、尚未 finalized 返回 `ErrUnconfirmed`。最终确认的失败返回 `ErrExecutionFailed`；错目标/金额/资产、回执不一致、费用或定位缺项返回 `ErrInvalidEvidence`；未覆盖形态返回 `ErrUnsupported`。节点错误单独返回原因，不伪装为零余额、零费用或已退款。数据库提交结果不明时查询同一事件，未能查明则返回 `ErrOutcomeUnknown`；不得换事件或重新付款。

## 数据迁移与配置

**当前代码的持久记录入口均要求 business 17**，包括 Solana。迁移 `000017_base_receipts` 在原两张事实表增加事件类型、块哈希及三项费用，金额字段扩为 78 位字符串；16 的历史 Solana 事件默认归入 `solana_instruction`，原始证据和指纹不变。没有新增依赖或第三套链上表。

升级前已有 Solana 记录可在 16/17 往返并继续幂等复用；已有 Base 记录或无法用 16 表结构表达的数据时，回退明确失败。不得强制清掉 guard 或 dirty 标记来丢弃证据，需按迁移故障与归档流程处理。生成 model、手写锁查询、共享短事务及资金表隔离保持原职责。

复用 `DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL` / `DEXWALLET_CHAIN_BASE_MAINNET_RPC_URL` 等既有 [节点配置](链上资产查询.md)。默认未配置即关闭。节点调用沿用 HTTP 超时、2 MiB 响应上限、禁止重定向和不自动重试；Base 完整核验通常需要六次只读 RPC，整个过程受同一截止时间约束。

## 工具与复查

在 `backend/` 执行，默认只读：

```bash
DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL=https://sepolia.base.org \
go run ./tools/chain_receipts --network base-sepolia \
  --signature 0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17 \
  --log-index 5 --asset base-sepolia:usdc \
  --recipient 0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA --atoms 10000 --timeout 30s
```

Base 沿用 `--signature` 参数传交易哈希，USDC 使用 `--log-index`，普通 ETH 省略位置；Solana 继续使用 `--instruction`，混用会拒绝。显式 `--record` 才使用 `DEXWALLET_MYSQL_DSN` 写入事实表，要求业务迁移版本至少 17 且 dirty=false；不加余额、不发送业务通知。

普通回归为 `go test ./logic/chainreceipt ./logic/chainassets`，使用 fake、httptest 和 `backend/logic/chainreceipt/testdata/base-*.json` 公开快照。数据库回归使用 testenv 的 `TestBaseReceiptUpgradeAndPersistence` 和既有 Solana 持久化测试，不连接生产目标。

独立真实查询：

```bash
DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL=https://sepolia.base.org \
DEXWALLET_TEST_BASE_RECEIPTS_LIVE=1 \
go test -tags=integration ./logic/chainreceipt -run '^TestLiveBaseHistoricalReceipts$' -count=1 -v
```

可选 `DEXWALLET_TEST_BASE_RECEIPTS_EVIDENCE` 指向已存在目录的绝对路径，保存两笔成功核验的公开请求、结果和证据；第三笔内部 ETH 只检查明确返回不支持。完整结果及未开放范围见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
