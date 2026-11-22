# TON 测试网到账监听与断点补扫

阶段 5.3 的后台监听已接通 TON Testnet 的原生 TON 和配置的测试 USDT。业务照常开收款请求，后台据此找到需要观察的钱包、发现最终确认的转账并匹配请求。用户无需回填交易哈希。当前结果用于内部确认收款归属，美元入账和商户结算由后续业务处理。[收款请求与归属规则](链上收款归属.md)

## 从地址找到付款

1. 后台从已签发请求中取出每个收款 owner 和资产组合，以最早请求的生效时间作为观察起点。原生 TON 扫描 owner 的交易。USDT 首次扫描前从已配置的 Jetton master 推导收款人的 Jetton wallet，并保存这个地址。
2. 从该账户的第一笔交易开始，使用 TON Center v3 按逻辑时间（LT）升序分页，每次最多取 8 笔。每页先校验节点测试网标识，逐条检查账户、最终确认状态、前一笔的 LT 和哈希。首笔必须连接到账户历史的起点。分页有缺口时保留原游标，待索引恢复后重查。空页会在后续轮询中重查。[TON Center 交易查询](https://docs.ton.org/api/v3/blockchain-data/get-transactions)
3. 已确认的连续前缀可推进游标。收录时间达到观察起点的交易进入待核验队列。游标和候选交易在同一数据库事务中提交，多个实例读到同一页也只会保存一次。即使请求已经过期，仍会补扫停机期间的历史，归属按实际链上收录时间裁定。
4. 后台另行读取候选交易的证据，从原生消息或 Jetton 载荷取得实际金额，再通过原到账核验器检查收款交易、发送消息、区块和资产。已保存过的到账直接用持久化事实恢复归属。首次匹配、重试和手工查证共用事件唯一约束。[TON 到账核验](TON到账核验.md)

原生收款地址还可能收到 Jetton 的通知，这条消息有自己的交易 ID；USDT 金额取自 Jetton wallet 上的真实代币转账。RPC 故障时，单个候选保留重试时间，其他候选仍可处理。已核实的多付、少付、重复支付或备注不符会留下到账事实和未归属原因，供人工核查。

## 启用及查询

使用 business 33，在本地或开发环境的 `dev` 模式下启用。API 与后台共用服务进程；默认关闭监听。需要启用 Redis 并配置 `DEXWALLET_CHAIN_TON_TESTNET_RPC_URL`。测试 USDT 还需设置 `DEXWALLET_CHAIN_TON_TESTNET_TEST_USDT_CONTRACT`，沿用签发请求时目录中的资产 ID。公网节点可能需要数次间隔查询才能核实一笔 Jetton 交易，以下示例给单次 RPC 观察留出 20 秒。

```yaml
ChainAssets:
  Timeout: 20s
TONReceiptListener:
  PollInterval: 5s
  OperationTimeout: 25s
  RetryInterval: 1m
```

每个实例以 `PollInterval` 间隔交替扫描一页和处理一个候选。单次操作预算不超过服务退出等待时间，候选重试间隔大于单次操作预算。停机时取消当前查询，重启后沿用数据库进度。没有签发过收款请求的地址不会启动观察。节点需要保留目标账户的完整历史，历史缺失或代币配置变更会记录错误并等待恢复。

可在专用库只读查询最新游标和待处理记录。

```sql
SELECT a.owner, c.asset_id, c.scan_account, c.last_lt, c.last_hash, c.next_poll
FROM ton_receive_cursor c JOIN chain_receive_address a ON a.id = c.address_id
ORDER BY c.id;

SELECT c.asset_id, p.tx_lt, p.tx_hash, p.status, p.attempts, p.next_try, p.detail
FROM ton_receive_candidate p JOIN ton_receive_cursor c ON c.id = p.cursor_id
ORDER BY p.id DESC LIMIT 50;
```

| 队列状态 | 含义 |
| --- | --- |
| `pending` | 等待核验或重试。持续失败时按交易哈希、资产和日志核查节点或证据。 |
| `matched` | 已核实且归给一个请求，可用原请求键查询。 |
| `unmatched` | 到账已保存，付款条件、时间、标识或唯一性检查未通过。 |
| `ignored` | 已确认的执行失败、非收款消息或不支持的转账形态。 |

当前持续观察已分配的地址和资产。首轮从头校验账户历史，对交易量大的老地址需要多次轮询。存在游标时迁移会拒绝降至 business 32，保留补扫进度和待核验记录。Solana 已接通[自动到账监听](Solana到账监听.md)。Base Sepolia 已接通[ETH 与 USDC 自动监听](Base到账监听.md)。TRON Nile 已接通[TRX 与测试 USDT 自动监听](TRON到账监听.md)。
