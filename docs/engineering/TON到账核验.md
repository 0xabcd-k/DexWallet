# TON 到账核验

[项目入口](../README.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md) · [TRON 核验](TRON到账核验.md)

## 当前范围

2026-11-19 接通 TON 主网/测试网固定目录下的原生币与 USDT（Jetton）到账核验、持久去重和提交异常恢复。真实只读验收复查三笔已有测试网收款：0.01 TON、1 枚测试 USDT 和未初始化账户收到的 0.05 TON。主网转账未实测。

TON 批次以 business 18 扩宽地址列；当前记录入口统一要求 **business 19**，见 [Base 内部轨迹](Base到账核验.md)。沿用现有 tonutils-go 1.15.5，不新增 SDK、资金 API 或页面。本批仍是转账事实存储，订单绑定、唯一分配、美元入账、签名广播及完整支付链路费用结算由后续业务接入。

## 定位与确认

`chainreceipt.TONRequest` 包含 `network/hash/assetId/recipient/atoms`。

- `hash` 是 **收款账户处理入站消息的交易哈希**，支持 64 位 hex 或标准 Base64，保存为小写 hex。它与付款钱包交易、外部消息哈希不同；以后发送通道须追踪到对应收款交易再调用本入口。
- `recipient` 是最终 owner，可用 raw 或带校验的友好地址；统一存 raw，主网拒绝 test-only 地址。数量为正的规范十进制最小单位整数，遵循 TON `VarUInteger 16` 的 120 位范围。
- 每笔收款交易只有一个入站消息。事件类型为 `ton_incoming` 或 `ton_jetton`，位置固定 0；不接收客户端指定指令或日志位置。

证据来自配置的 TON Center 兼容节点：V2 `getMasterchainInfo`、V3 `transactions`、`adjacentTransactions?direction=in`、`blocks`，Jetton 再调用白名单 master 的 `get_wallet_address`。先匹配 zero-state；交易须为 finalized、明确非模拟，分片块须与交易一致并锚定对应主链区块，LT 落在块内且主链头已覆盖。发送交易必须成功，其唯一对应 out message 与收款 in message 的哈希、地址、数量、BoC body 等事实一致，且 LT 顺序成立。

主网 zero-state 为 `F6OpKZKqvqeFp6CQmFomXNMfMj2EnaUSOXN+Mh+wVWk=`，测试网为 `gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=`，来自 [官方主网配置](https://ton.org/global-config.json) 与 [测试网配置](https://ton.org/testnet-global.config.json)。交易 finality 与查询语义依据 [TON Center 交易接口](https://docs.ton.org/api/v3/blockchain-data/get-transactions)、[finality 模型](https://docs.ton.org/api/streaming/overview)。

这是可信 RPC/索引器提供的事实核验，不是轻客户端共识证明；没有独立验证整笔交易 BoC、区块签名或 VM 重放。空列表/尚未 finalized 保持未确认，RPC 错误和畸形证据明确报错，不能据此退款或重新付款。

## 原生币与 Jetton

**原生币**只接受空 body 或 opcode 0 的 UTF-8 文本备注，无额外货币、收款侧 StateInit 或转出动作；credit 必须等于入站消息金额。一般收款要求计算与 action 成功。对于 non-bounce 消息，`no_state` 且账户为 uninit、或 `no_gas` 且账户为 active，若存在对应 credit、无 bounce/action/转出且未销毁，也可记录到账。其他 VM 失败拒绝，不能把所有 `aborted=true` 统一解释为未到账。该区别依据 [官方支付处理说明](https://github.com/ton-community/ton-docs/blob/main/docs/v3/guidelines/dapps/asset-processing/payments-processing.mdx)，并由本轮真实 no-state 交易复查覆盖；no-gas 分支使用确定性派生样本测试。

**Jetton**以成功执行 `internal_transfer` 的收款代币钱包交易为事件，不把随后通知交易当作同一笔代币到账。必须满足：

1. 目标 owner 在白名单 master 的 `get_wallet_address` 返回值与实际收款代币钱包地址相同，拒绝同名假币和自报 master 的钱包。
2. 使用 BoC 解析 internal_transfer 的数量、原 owner、query_id 和附带 TON；不信任 API 的 `decoded` 文本。
3. 发送代币钱包交易成功，其入站 transfer 的调用 owner、目标 owner、数量、query_id 与 forward TON 和实际 internal_transfer 对应。
4. 收款代币钱包执行成功，消息非 bounced，数量与预期一致，拒绝自转。原生币执行预算不当作 Jetton 数量。

TEP-74 的字段及消息定义见 [官方标准](https://github.com/ton-blockchain/TEPs/blob/master/text/0074-jettons-standard.md)，白名单及从 master 派生钱包的安全依据见 [官方 Jetton 支付说明](https://docs.ton.org/applications/payments/jettons)。本批使用其中的指定合约 internal_transfer 形态，不声称兼容任意自定义 Jetton 协议。

V3 get-method 返回**当前** master 的钱包映射，不能声称这是历史状态证明；若当前映射与历史接收地址不一致，本入口拒绝并留待人工核对/历史状态方案，不能自动改订单归属。真实测试的 master 为 `kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy`，资产 ID 包含完整 master；它是测试币，不能替代主网 USDT。此前付款 trace 中 1 nanoton 通知跳过计算的现象见 [历史验证](../design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/16TON测试USDT代付与余款验证.md)，不影响已成功的代币钱包收款事实。

## 费用、存储与兼容

`FeeAtoms` 仅记录**本笔收款交易**的 `total_fees + 自己发出消息的剩余 fwd_fee`，单位 nanoton；`FeePayer` 为该收款账户（Jetton 场景是代币钱包）。它不代表整条支付 trace 的网络费，也不说明谁提供附带预算。历史 0.01 TON 收款自身费用为 36,401 nanoton，测试 USDT 收款自身费用为 738,377 + 66,313 + 44,446 = 849,136 nanoton。平台预算、余款、合约保留资金和用户应扣费用仍须分别处理。

`Slot` 保存主链确认高度，`BlockHash` 保存对应分片块 root hash；交易 LT、消息哈希、解析出的执行状态和费用进入事实指纹，完整原始响应首次保留在证据中。事件 source/recipient account 是消息两侧账户，Jetton 的 owner 另列。主链头前进不改变指纹；修改收款费用、消息或已保存事实会报冲突。提交应答丢失时复用已查回事件，查不到保留结果未知，不产生资金重发。

business 18 将 fee_payer、program_id、mint 和双方 account/owner 列从 44 扩至 68 字节，容纳 `0:` 与 `-1:` raw 地址；完整 DDL 与增量迁移一致，生成器运行后无需变更 Go 字段类型。升级、无 TON 数据时回退再升级均保留前三链记录及原始证据；已有长地址时回退保护拒绝截断。部署遵循 [迁移说明](契约生成与迁移.md)，工具不自动迁移。

## 使用与验证

工作目录 `backend/`，默认只读，不需要钱包密钥或数据库：

```bash
export DEXWALLET_CHAIN_TON_TESTNET_RPC_URL=https://testnet.toncenter.com
go run ./tools/chain_receipts \
  --network ton-testnet \
  --signature 53d778fb27fe4ed339ba2514b5f3938964dcbc48b6b88a66fdea326cf5e25e98 \
  --asset ton-testnet:ton \
  --recipient 0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp \
  --atoms 10000000 --timeout 30s
```

测试 USDT 另配置 `DEXWALLET_CHAIN_TON_TESTNET_TEST_USDT_CONTRACT`，资产 ID 使用 `ton-testnet:usdt:<配置的完整 master>`。显式 `--record` 才使用 `DEXWALLET_MYSQL_DSN` 写入事实表，并要求 clean business ≥19；不会给用户加余额。`--instruction`、`--log-index` 均不用于 TON。

无 API key 的公共 TON Center 限制 1 RPS，本轮实际遇到过 429；单次证据观察在各响应完成后间隔 1.1 秒，超时/取消仍生效，没有自动重试。并发观察可能继续遇到配额限制，正式接入需配置相应 `DEXWALLET_CHAIN_TON_*_API_KEY` 和节点配额。服务默认查询预算 3 秒不足以完成无 key 的多次读取，本内部核验应配置 `ChainAssets.Timeout` 为 10–30 秒；独立工具建议上述 30 秒。配额来源见 [官方接口说明](https://docs.ton.org/api/v3/blockchain-data/get-transactions)。

普通单元测试使用 `backend/logic/chainreceipt/testdata/ton-*.json`，不联网。独立真实只读入口：

```bash
DEXWALLET_TEST_TON_RECEIPTS_LIVE=1 \
DEXWALLET_CHAIN_TON_TESTNET_RPC_URL=https://testnet.toncenter.com \
go test -tags=integration ./logic/chainreceipt \
  -run '^TestLiveTONHistoricalReceipts$' -count=1 -v
```

`DEXWALLET_TEST_TON_RECEIPTS_EVIDENCE` 可设为本机已有输出目录。隔离数据库用 `TestTONReceiptMigrationPersistenceAndRecovery`，同时回归原三链事实存储；详细结果见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。Base 内部 ETH 历史核验已补齐；已建立 [TON 平台出款执行基础](TON平台出款执行.md)，继续接入真实签名/广播、发送到收款关联、监听与订单分配。
