# TRON 到账核验

[项目入口](../README.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md) · [Base 核验](Base到账核验.md)

## 当前范围

2026-11-19 接通 TRON 主网/Nile 固定目录下的普通 TRX 转账、TRC-20 USDT Transfer 日志核验、持久去重和提交异常恢复。真实只读验收使用三笔已有 Nile 交易；主网未实测。数据库继续使用 business 17，没有新增迁移、依赖或公开 API。

本批仅记录转账事实，充值订单归属、平台净实收、唯一事件分配及美元入账由后续业务接入。签名广播、监听任务、资源委托和用户付款入口仍未开放；逐场景费用规则继续按原审核清单处理。

## 内部入口和核验依据

`chainreceipt.TRONRequest` 包含 `network/hash/assetId/recipient/atoms`。`hash` 是不带 `0x` 的 64 位十六进制 txID，保存为小写；目标为校验通过的 Base58Check 地址。TRX 数量使用正的 int64 sun，USDT 数量使用正的 uint256 最小单位整数，输入均为规范十进制字符串。USDT 必须提供 `logIndex`，TRX 必须省略。

1. `chainassets.ReadTRONEvidence` 仅访问服务端配置的 Solidity 节点接口，先匹配创世块 ID，再读取固化交易、回执、当前固化块及包含交易的区块。请求使用 `visible=false`，不接受客户端提供节点地址。
2. 交易 txID、原始交易字节 SHA-256、回执 ID、区块高度/时间、固化高度和交易在区块中的唯一出现必须一致。目标交易只接受单个已知合约类型，执行成功后才解析转账。节点返回 `{}` 表示暂未找到；超时、RPC 错误、畸形内容独立报错。
3. 普通 TRX 只接受顶层 `TransferContract` 的发送人、目标和 amount。内部 TRX、TRC-10 及未知合约类型不在当前核验范围。
4. USDT 只接受成功的 `TriggerSmartContract` 中，由目录内指定合约发出的 `Transfer(address,address,uint256)`。合约地址、topic、地址填充、目标和精确数量必须一致；铸币和自转拒绝。事实源是 Transfer 日志，不以调用参数或合约返回值代替到账。
5. `logIndex` 是 **原始 `info.log` 数组位置**，不先过滤其他事件、不沿用 Base 的日志自身 `logIndex`。同一交易的两条 Transfer 是两个事件；交易费用只存一次。

节点是配置的可信数据来源。本批验证 Solidity 返回的交易与回执一致性，不运行轻客户端、不验证共识签名或独立重放 VM；原始交易哈希核对也不构成对任意恶意 RPC 所返回 JSON 的完整密码学证明。

创世块固定值如下；主网与 Nile 使用相同地址前缀，因此不能仅靠地址区分网络。值来自本轮官方公共节点的 block 0 查询，并保留于公开快照和验证附件。

| 网络 | 创世块 ID |
| --- | --- |
| `tron-mainnet` | `00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc` |
| `tron-nile` | `0000000000000000d698d4192c56cb6be724a558448e2684802de4d6cd8690dc` |

Solidity 确认语义与区块扫描依据见 [官方回执接口](https://developers.tron.network/reference/gettransactioninfobyid-1)、[交易所/钱包接入说明](https://developers.tron.network/docs/exchangewallet-integrate-with-the-tron-network)。日志地址为不含 `41` 的 20 字节十六进制，恢复地址及事件 ABI 依据见 [官方事件说明](https://developers.tron.network/docs/event)。

## 资产、费用与资源

主网 USDT 合约固定为 `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t`。Nile 的测试币必须显式配置，资产 ID 包含完整合约，例如 `tron-nile:usdt:TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf`；官方水龙头币和自建测试币是不同资产，不能替换主网 USDT。

- `Transaction.FeeAtoms` 是 `info.fee` 的实际燃烧总额，单位 sun；`FeePayer` 保存交易 owner，即发生燃烧时的扣费账户。
- Energy、Bandwidth、origin Energy、Energy/Net 燃烧费及惩罚 Energy 保留在原始回执和不可变事实指纹中。总燃烧费允许包含开户等其他链上收费，不简单等于 EnergyFee + NetFee。
- Proto3 省略的数值字段按协议解释为 0；显式 null、负数、小数、字符串数值和 int64 溢出拒绝。协议字段定义见 [TRON 官方 protobuf](https://github.com/tronprotocol/protocol/blob/master/core/Tron.proto)。
- 燃烧为零不能证明项目平台承担了资源成本；`origin_energy_usage` 是合约方提供的资源，交易 owner 也不等于资源委托者。历史资源来源仍须结合当时委托/账户资源证据，不能仅凭本次回执给用户扣平台代付 Gas。

普通 TRX 样本使用 266 Bandwidth。官方测试 USDT 使用 29,650 Energy，全部来自合约方，Bandwidth 为 345。自建测试币使用 14,650 Energy，origin Energy 为 0、Bandwidth 为 345；原实验的完整委托证据见 [TRON 完整资源代付验证](../design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/18TRON完整资源代付验证.md)。三笔当前回执的燃烧总额均为 0。

## 持久化与恢复

内部 `TRONService.Record` 先读取并核验链上事实，再复用原有短事务。交易按 `(network, signature)` 去重，事件按 `(transaction_id, event_kind, instruction_index)` 去重：TRX 为 `tron_transfer:0`，USDT 为 `tron_log:<原始日志数组位置>`。共享字段 `instruction_index` 在 TRON 中承担事件位置含义。

同一交易的完整解析日志、资源费用、目标交易与区块 ID 纳入指纹，另一条日志或资源事实变化也会报冲突；当前固化头前进不产生冲突、不覆盖首次原始证据。提交应答丢失查询已记录事实恢复，找不到时保留结果未知。`Find` 仅查询已存事件，不推断订单归属。

既有 business 17 回退保护会拒绝丢弃 TRON 的区块 ID 或事件类型。没有写入用户账户、账本、占用、转账订单或 outbox。

## 运行与验证

默认只读查询一笔已有 Nile TRX 交易，工作目录 `backend/`：

```bash
export DEXWALLET_CHAIN_TRON_NILE_RPC_URL=https://nile.trongrid.io
go run ./tools/chain_receipts \
  --network tron-nile \
  --signature c08fc0d615c4dc8546ce592e2d3bdbe711c282c68b437c18549395ac1979ac3e \
  --asset tron-nile:trx \
  --recipient TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH \
  --atoms 1000 --timeout 30s
```

USDT 另配置 `DEXWALLET_CHAIN_TRON_NILE_TEST_USDT_CONTRACT`，使用对应完整资产 ID 和 `--log-index 0`。`--instruction` 只用于 Solana。显式 `--record` 才使用 `DEXWALLET_MYSQL_DSN` 写库，并要求 clean business ≥17；此开关仍不执行产品入账。配置文件和秘密留在本机，不提交仓库。

确定性测试使用 `backend/logic/chainreceipt/testdata/tron-*.json`，不连接真实节点。真实历史查询为独立入口：

```bash
DEXWALLET_TEST_TRON_RECEIPTS_LIVE=1 \
DEXWALLET_CHAIN_TRON_NILE_RPC_URL=https://nile.trongrid.io \
go test -tags=integration ./logic/chainreceipt \
  -run '^TestLiveTRONHistoricalReceipts$' -count=1 -v
```

可设置 `DEXWALLET_TEST_TRON_RECEIPTS_EVIDENCE` 为本机已有输出目录，保存新的公开快照；正常检查不会调用此入口。隔离数据库测试为 `TestTRONReceiptPersistenceAndRecovery`，使用现有 testenv 专用配置。全部实际结果、交易与来源摘要见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。下一批继续 TON 原生币和 Jetton 到账核验。
