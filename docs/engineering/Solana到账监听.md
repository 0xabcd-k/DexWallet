# Solana Devnet 到账监听与断点补扫

阶段 5.3 的内部监听已接通 SOL、USDC 和配置了 mint 的测试 USDT。业务在付款前签发一次性地址收款请求，后台从最终确认区块发现相关交易，再按顶层转账指令核实实收和归属。用户无需提交签名。支持的指令、交易版本、金额和归属条件分别见[到账核验](Solana到账核验.md)及[收款请求与到账归属](链上收款归属.md)。

## 发现、核验与恢复

- 扫描器向已核对 Devnet 创世哈希的节点查询 finalized 区块，每批最多 12 个已出块的 slot。逐块比较父 slot 和父哈希，正常跳过未出块的 slot。链路断开、节点限流或缺少必要的区块数据时，从最后成功提交的位置重试。[区块列表](https://solana.com/docs/rpc/http/getblockswithlimit) · [区块查询](https://solana.com/docs/rpc/http/getblock)
- 使用区块的账户摘要确定候选。原生币按钱包地址发现，SPL 还根据交易执行后的代币余额记录查找 owner，因此收款钱包无需出现在交易指令的地址列表中。查询允许同一区块含 v1 交易；现有收款核验范围是 legacy 和 v0 的 System Program `transfer`、原版 Token Program `transferChecked` 顶层指令。
- 一个请求在一笔交易中只生成一个待核验记录。区块游标和该块全部候选在同一事务内提交。重启后沿用数据库中已保存的 slot 和哈希；多实例读到同一块时只会提交一次。
- 核验器读取完整交易和最终确认状态，对照区块的 slot、收录时间以及各条指令的资产、数量、来源和接收 owner。实际付款可能位于开户指令之后。一笔交易可以有多条付款指令，每条按原始指令序号单独保存、匹配，并分别计数。若第一笔到账证据已保存，重启后可从该证据继续处理其余指令。
- `matched` 表示至少一条实收归属于请求；同时查看 `unmatched_events` 可发现多付的其他指令。`unmatched` 表示已核实实收但均未归属，`ignored` 表示相关交易中没有符合该请求资产和收款人的受支持付款。`pending` 在 RPC 或持久化失败后继续重试。匹配过期依据链上收录时间；后台补扫延迟不改变请求条件。

## 启用及诊断

使用 business 34，服务处于 local 或 dev 环境及 dev 模式。监听默认关闭。启用 Redis，设置 `DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL`，使用测试 USDT 时也设置原请求资产 ID 对应的 `DEXWALLET_CHAIN_SOLANA_DEVNET_TEST_USDT_CONTRACT`。

首次启用时，选择**早于最早已签发 Solana 请求生效时间**的已最终确认区块，将其 slot 填入 `AnchorSlot`。该区块作为起点，从它的下一块开始扫描。若尚未签发任何请求，可使用启用时已最终确认的区块。锚点太晚时扫描器报告错误并等待调整，未创建游标前可修改配置后重启。已有游标时更改配置不会快进或回退。以下 slot 仅用于重现本次 SOL 历史测试。

```yaml
ChainAssets:
  Timeout: 15s
SolanaReceiptListener:
  AnchorSlot: 517805171
  PollInterval: 5s
  OperationTimeout: 20s
  RetryInterval: 1m
```

发现和核验分别运行。节点限流时 API 可继续服务。补扫需要节点提供游标之后的区块和相应历史交易，单次 RPC 响应沿用链查询客户端的 2 MiB 上限。若同一区块的账户摘要持续超限，游标会停在前一块，日志会记录读取失败的 slot。首次启动应选择足够早且接近首笔请求的区块，避免扫描无关历史。可将持久化的 `last_slot` 和节点的 finalized slot 比较，观察扫描是否追得上出块。本次验收覆盖短区间历史补扫，持续高流量吞吐仍待验证。

```sql
SELECT last_slot, last_hash FROM solana_receive_cursor WHERE id = 1;

SELECT r.request_key, c.signature, c.slot, c.status, c.matched_events,
       c.unmatched_events, c.attempts, c.next_try, c.detail
FROM solana_receive_candidate c
JOIN chain_receive_request r ON r.id = c.request_id
ORDER BY c.id DESC LIMIT 50;
```

出现游标后降至 business 33 会被拒绝。当前只保留收款事实和归属；美元入账由后续充值结算流程处理。真实测试交易与隔离数据库回放见[阶段验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
