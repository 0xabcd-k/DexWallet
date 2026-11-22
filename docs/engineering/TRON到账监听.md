# TRON Nile 自动到账监听与断点补扫

后台从已固化的区块中发现转入已签发收款地址的 TRX 和测试 USDT。付款人无需上报 txID。到账归属继续核对请求里的付款人、资产、金额和有效期，成功后可按请求键查询实收事件。美元余额入账属于后续充值结算。[收款请求与归属](链上收款归属.md)

| 资产 | 付款依据 | 事件位置 |
| --- | --- | --- |
| TRX | 成功的顶层 `TransferContract`，金额单位 sun | `tron_transfer/0` |
| 已配置的 Nile 测试 USDT | 成功的 `TriggerSmartContract` 交易中，由指定代币合约发出的标准 Transfer 日志，金额使用合约最小单位整数 | `tron_log` 和原始交易回执中的日志数组下标 |

使用服务端配置节点的 `walletsolidity` 接口，核对 Nile 创世块、固化高度、父块哈希和收录时间。后台逐块读取交易及整块回执，按 txID 一一对应。遗漏、重复或来自其他高度的回执会中止本次扫描。空回执数组需要与同高度的已固化空块对应。对涉及已签发地址的付款，复用到账核验器校验原始交易字节的哈希、执行结果、实际收款人、日志、金额和资源费用。[TRON 官方区块回执接口](https://developers.tron.network/reference/gettransactioninfobyblocknum-1) · [TRON 到账核验](TRON到账核验.md)

## 恢复与归属

- TRX 和测试 USDT 按完整资产 ID 分开保存高度和区块哈希。测试 USDT 的 ID 包含合约地址。更换测试合约后，新合约使用自己的扫描进度，恢复旧合约配置时沿用其原进度。
- 同一资产的一个区块内，已验证的实收、符合条件的请求归属和扫描进度在同一 MySQL 事务中提交。任何一项写入失败会整块回滚。进程重启或提交应答不明时，重新读取已提交的位置。多个实例读到同一块时，只有成功推进原游标的实例写入。
- 已签发地址同资产的短款、过期款和重复付款保留实收，订单最多关联一个事件。订单有效期按毫秒区块时间换算为 Unix 秒判断。后台通过整块回执补查，无需用户重新付款。

## 启用及查询

使用已迁移至 business 36 的业务库、local 或 dev 环境及 dev 模式，启用 Redis。设置 `DEXWALLET_CHAIN_TRON_NILE_RPC_URL`。如需监听测试 USDT，同时设置 `DEXWALLET_CHAIN_TRON_NILE_TEST_USDT_CONTRACT`。监听默认关闭。以下锚点用于重放本次 TRX 样例。

```yaml
ChainAssets:
  Timeout: 18s
TRONReceiptListener:
  AnchorHeight: 72384387
  PollInterval: 5s
  OperationTimeout: 25s
```

某资产有已签发请求后才启动扫描。首次锚点须是已固化区块，且其时间严格早于该资产最早请求的生效秒，从下一块开始补扫。已保存游标时以数据库位置为准。节点故障、数据无法核验或写库失败后，按轮询间隔重试。存在游标后，迁移会拒绝降至 business 35。

以下查询显示补扫位置，以及已签发地址的实收及其实际归属。付款有实收而 `matched_request` 为空时，可按交易与日志位置复核。

```sql
SELECT asset_id, height, block_hash FROM tron_receive_cursor;

SELECT t.slot AS height, t.signature AS tx_id, e.event_kind, e.instruction_index,
       e.asset_id, e.amount_atoms, e.recipient_owner, r.request_key AS matched_request
FROM chain_transfer_event e
JOIN chain_observed_transaction t ON t.id = e.transaction_id
JOIN chain_receive_address a ON a.network = t.network AND a.owner = e.recipient_owner
LEFT JOIN chain_receive_request r ON r.matched_event_id = e.id
WHERE t.network = 'tron-nile'
ORDER BY t.slot DESC, e.id DESC LIMIT 50;
```

部署验收还需比较数据库高度与节点固化高度，观察持续扫描速度、归档覆盖和限流。客户端单次响应上限为 2 MiB，整块数据超过上限时该资产的游标留在前一块。本次验收覆盖测试网短区间历史补扫。[交易样例与验证命令](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)
