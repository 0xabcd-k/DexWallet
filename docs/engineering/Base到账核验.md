# Base 到账核验

[项目入口](../README.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md) · [Solana 核验](Solana到账核验.md)

## 当前范围

2026-11-19 接通 Base 主网/Sepolia 固定目录下的普通 ETH 外层转账及 USDC Transfer 日志核验、持久去重和提交异常恢复。真实只读验收使用两笔已有 Base Sepolia 交易，没有签名、广播、充值或支付动作。主网未实测。

平台代付 USDC 时，外层交易发送人是付费方，实际出资人来自 USDC 日志。本批不根据 tx.from 猜测充值用户，也不把调用成功等同于代币到账。此前 MetaMask 智能账户 ETH 代付属于内部调用，外层 value=0。第五批已新增显式内部 CALL 核验及持久化；后续通过 Pocket 公共 Base Sepolia 节点取得完整历史轨迹，真实核验及隔离持久化通过。公共节点仍存在偶发空响应、历史调用错误和连接异常，不能把一次成功当作生产可用性验收。不能仅信任外层成功、授权事件或调用参数。

与 Solana 一致，本批仅记录转账事件事实。它不证明产品订单归属，也不保证整笔交易的净实收；自动入账仍需不可变订单、平台控制的收款目标、允许的交易形态及唯一事件分配。链上签名广播、平台收费、失败 Gas 回收和用户页面没有开放。

## 内部入口与事件身份

`chainreceipt.BaseRequest` 包含 `network/hash/assetId/recipient/atoms`，USDC 另需 `logIndex`，普通 ETH 省略位置；内部 ETH 另需 `callIndex`，两个位置参数互斥。网络只能是 `base-mainnet` 或 `base-sepolia`，资产来自既有固定目录；地址及哈希验证长度和十六进制后以小写保存，数量为正的规范十进制 uint256 最小单位整数，不接受浮点或前导零。

1. `chainassets.ReadBaseEvidence` 校验配置网络 chain ID，依次取得交易、回执、finalized 区块头和付款高度的当前规范区块；请求不能带入任意 RPC URL。
2. 交易与回执的哈希、块哈希、高度、交易位置、发送人、目标和类型必须一致。付款高度不高于 finalized 高度；规范区块哈希须匹配，且该位置的交易哈希必须存在。相同高度的 finalized 头也必须匹配块哈希。
3. 普通 ETH 只接受空 calldata、正 value 的成功外层转账。USDC 必须来自完整白名单合约地址，topic0 为标准 Transfer，两个地址 topic 和数量 data 都按精确 ABI 宽度解析；源不得为零地址或收款人，金额和收款人须与预期完全一致。
4. 日志使用 RPC 自身的 `logIndex`，不是回执日志数组下标。每条日志须具有一致的交易/区块定位、唯一索引及 `removed=false`。本次真实 USDC 回执有两条日志，转账在数组第 2 项，实际 logIndex 是 5。
5. 只接受普通 EVM 交易类型 0/1/2/4；外层合约创建、存入类型、未知形态不作为成功事件；内部 ETH 必须按下节显式选择并验证完整调用轨迹。

`BaseService.Record` 必须自己读取并核验，不接受客户端构造的 Receipt。`Find` 只恢复已保存事件；入口在 `ServiceContext.BaseReceipts`，没有公开 HTTP API。共享存储的 `signature/slot/instruction` 历史字段在 Base 分别表示交易哈希、区块高度和事件位置，明确由 `event.kind` 区分：

| 类型 | 位置 | 唯一事件身份 |
| --- | --- | --- |
| `solana_instruction` | 顶层指令索引 | 网络 + 签名 + 类型 + 位置 |
| `evm_value` | 固定 0 | 网络 + 交易哈希 + 类型 + 0 |
| `evm_log` | RPC logIndex | 网络 + 交易哈希 + 类型 + 日志位置 |
| `evm_call` | 完整调用树的先序索引，根为 0 但不可选 | 网络 + 交易哈希 + 类型 + 调用位置 |

因此同一交易的外层 value 和第 0 条日志不会碰撞。第一次保存原始证据；后续观察只比较稳定交易、回执和费用事实，finalized 头前进不会制造冲突。金额、日志或其他保留事实变化则返回 `ErrConflict`，不覆盖原记录。

协议依据：[Ethereum JSON-RPC](https://ethereum.org/developers/docs/apis/json-rpc/)、[ERC-20 Transfer](https://eips.ethereum.org/EIPS/eip-20)。核验依赖部署所信任的节点，不独立验证共识或重放 EVM；恶意 RPC 能伪造结果，因此网络配置是服务端信任边界。

## 智能账户内部 ETH（第五批）

`ReadBaseCallEvidence` 只从同一配置节点取得 `debug_traceTransaction(hash, {tracer: callTracer, tracerConfig: {onlyTopCall: false}})`，不接受客户端上传的轨迹。根调用的 from/to/value/input/gas 必须匹配交易，gasUsed 必须匹配成功回执；原有链 ID、finalized、规范块成员关系和三项费用校验仍全部执行。

索引按完整调用树先序遍历：根为 0，随后逐子树遍历；失败、STATICCALL、DELEGATECALL 等帧也计数，不筛选后重新编号。只接受根以下的正 value `CALL`，源账户使用该调用的实际执行账户；DELEGATECALL/CALLCODE 传递的 value 上下文不作为资产转移。选中帧及全部上层必须没有执行错误；上层已回滚即使子调用显示成功，也返回 `ErrExecutionFailed`。STATICCALL 子树不能产生本批到账事件。失败 CREATE 若省略目标，不推测地址，其后代仍全部标记失败。

验证会遍历整棵树，检查执行账户关系和字段，校验根与回执。轨迹依赖所信任节点完整返回，服务自身不重放 EVM，也不能用一笔 CALL 事件保证整笔交易净实收：收款合约仍可能在其他调用中转出，自动入账前须另行限制收款目标和订单支付形态。

business 19 在交易表新增 `call_trace_fingerprint` 与 `call_trace_json`。首次完整轨迹与事件在同一短事务写入；已存在 USDC 等记录时，只补充独立轨迹列，不覆盖原 evidence_json 和旧交易指纹。所有内部事件共用一份轨迹指纹，包含未选中分支的标准调用字段；另一分支变化同样拒绝，不能为不同事件保存互相矛盾的轨迹。后续普通 ETH/USDC 查询继续复用旧交易事实，费用仍只保存一次。已有轨迹或 evm_call 事件时，19 → 18 回退保护拒绝删证据。

协议与字段依据：[Geth callTracer 文档](https://geth.ethereum.org/docs/developers/evm-tracing/built-in-tracers)、[Geth 原生调用追踪实现](https://github.com/ethereum/go-ethereum/blob/master/eth/tracers/native/call.go)、[执行层 debug_traceTransaction](https://ethereum.github.io/execution-apis/next/api/methods/debug_traceTransaction/)。

## 网络费用与未知结果

`feeAtoms = executionFeeAtoms + l1FeeAtoms + operatorFeeAtoms`，均为 wei。其中执行费为回执 `gasUsed × effectiveGasPrice`，L1 数据费取回执 `l1Fee`。Operator 费调用固定 GasPriceOracle `0x420000000000000000000000000000000000000f` 的 `getOperatorFee(gasUsed)`，使用回执块哈希和 `requireCanonical=true` 读取该区块的规则；不使用当前费率，也不因回执省略字段就默认为零。所需 RPC 需支持该历史区块的 `eth_call`。

参考 [Base 费用组成](https://docs.base.org/specifications/transactions/network-fees)、[OP Stack Operator 费与回执](https://specs.optimism.io/protocol/isthmus/exec-engine.html#operator-fee)、[GasPriceOracle 实现](https://github.com/ethereum-optimism/optimism/blob/develop/packages/contracts-bedrock/src/L2/GasPriceOracle.sol)。本批记录观察到的实际网络费；用户最终扣费、费用资产与承担规则仍以 [逐项审核清单](../design/链上资金费用逐项审核.md) 为准。

交易或回执不存在、尚未 finalized 返回 `ErrUnconfirmed`。最终确认的失败返回 `ErrExecutionFailed`；错目标/金额/资产、回执不一致、费用或定位缺项返回 `ErrInvalidEvidence`；未覆盖形态返回 `ErrUnsupported`。节点错误单独返回原因，不伪装为零余额、零费用或已退款。数据库提交结果不明时查询同一事件，未能查明则返回 `ErrOutcomeUnknown`；不得换事件或重新付款。

## 数据迁移与配置

**当前代码的持久记录入口均要求 business 19**，包括 Solana；[TON 接入](TON到账核验.md) 将地址列扩宽。迁移 `000017_base_receipts` 在原两张事实表增加事件类型、块哈希及三项费用，金额字段扩为 78 位字符串；16 的历史 Solana 事件默认归入 `solana_instruction`，原始证据和指纹不变。没有新增依赖或第三套链上表。

升级前已有 Solana 记录可在 16/17 往返并继续幂等复用；已有 Base 记录或无法用 16 表结构表达的数据时，回退明确失败。不得强制清掉 guard 或 dirty 标记来丢弃证据，需按迁移故障与归档流程处理。生成 model、手写锁查询、共享短事务及资金表隔离保持原职责。

复用 `DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL` / `DEXWALLET_CHAIN_BASE_MAINNET_RPC_URL` 等既有 [节点配置](链上资产查询.md)。默认未配置即关闭。节点调用沿用 HTTP 超时、2 MiB 响应上限、禁止重定向和不自动重试；普通 Base 核验需要六次只读 RPC；内部 ETH 增加一次 `debug_traceTransaction`。完整过程受同一截止时间约束，节点追踪超时为 15 秒；缺少历史状态、方法不开放、RPC 错误或超限均直接报错，不降级为外层成功。

## 工具与复查

在 `backend/` 执行，默认只读：

```bash
DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL=https://sepolia.base.org \
go run ./tools/chain_receipts --network base-sepolia \
  --signature 0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17 \
  --log-index 5 --asset base-sepolia:usdc \
  --recipient 0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA --atoms 10000 --timeout 30s
```

Base 沿用 `--signature` 参数传交易哈希，USDC 使用 `--log-index`，普通 ETH 省略位置，内部 ETH 使用 `--call-index`；Solana 继续使用 `--instruction`，混用会拒绝。显式 `--record` 才使用 `DEXWALLET_MYSQL_DSN` 写入事实表，要求业务迁移版本至少 19 且 dirty=false；不加余额、不发送业务通知。

普通回归为 `go test ./logic/chainreceipt ./logic/chainassets`，使用 fake、httptest 和 `backend/logic/chainreceipt/testdata/base-*.json` 公开快照。数据库回归使用 testenv 的 `TestBaseReceiptUpgradeAndPersistence` 和既有 Solana 持久化测试，不连接生产目标。

独立真实查询：

```bash
DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL=https://sepolia.base.org \
DEXWALLET_TEST_BASE_RECEIPTS_LIVE=1 \
go test -tags=integration ./logic/chainreceipt -run '^TestLiveBaseHistoricalReceipts$' -count=1 -v
```

可选 `DEXWALLET_TEST_BASE_RECEIPTS_EVIDENCE` 指向已存在目录的绝对路径，保存两笔成功核验的公开请求、结果和证据；第三笔内部 ETH 仍检查未提供 `callIndex` 时明确返回不支持；显式内部模式使用下列独立入口。完整结果及未开放范围见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。


内部 ETH 真实验收单独启用。本次通过的公开节点和一次性进程参数如下，不包含密钥：

```bash
GODEBUG=http2client=0 \
DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL=https://base-testnet.api.pocket.network \
DEXWALLET_TEST_BASE_CALL_RECEIPTS_LIVE=1 \
go test -tags=integration ./logic/chainreceipt -run '^TestLiveBaseInternalETHReceipt$' -count=1 -v
```

该入口只查询此前 `0x1c3509c224c12725106e47b57e5f5627934edf229ae51fb42861173fb36c2852`，从完整轨迹找出唯一匹配的内部 CALL 位置，再做正式核验并可保存公开证据。真实轨迹共 29 帧，转账位置为 callIndex=16、子调用路径 `[13,0]`；离线构造样例的 callIndex=6 仍只属于合成交易。正式节点需要支持历史 `callTracer`、历史 Operator 费及普通查询。当前实链状态和各公共节点结果见 [第五批及补验记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)；不要求用户重新付款或购买测试币。


此次真实事件的数量是 1000000000000 wei（0.000001 ETH），源为 `0xecd14f7586f5053cecae148ae26309c2277d86a8`，收款人和外层付费人为 `0xa59c992aa84657df4ebff6f04e11a1632f6db1fa`。区块 53792630 已 finalized；L2 1935996000000 + L1 9267 + Operator 0 = 1935996009267 wei。真实快照保存为 `backend/logic/chainreceipt/testdata/base-internal-eth.json`，与合成的 `base-internal-eth-fixture.json` 分开。

本机默认 HTTP 客户端访问曾遇到空证据与 unexpected EOF；显式 `GODEBUG=http2client=0` 后取得完整采集及实时测试成功，但同设置也出现过上游 eth_call -32603 和查不到交易的响应，因此尚不能将问题完全归因于 HTTP/2。该开关仅用于本次验收进程，含义见 [Go HTTP 文档](https://pkg.go.dev/net/http#hdr-HTTP_2)，没有更改项目默认传输设置或加入自动重试。CLI 的一次独立复查保留 `ErrUnconfirmed`，不把空响应记作失败、退款或到账。后续部署仍需验证节点稳定性；本批没有主网、广播或新付款。
