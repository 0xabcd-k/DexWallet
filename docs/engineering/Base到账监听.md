# Base Sepolia 到账监听与断点补扫

后台按区块发现一次性收款地址收到的 ETH 和 USDC，逐笔核验后关联已签发的请求。付款人无需上报交易哈希。当前用于测试网，归属成功后可查询付款证据。美元余额入账属于后续充值结算。[收款条件与归属](链上收款归属.md)

## 三种付款如何识别

| 付款 | 从最终确认区块发现候选 | 保存的事件位置 |
| --- | --- | --- |
| 普通 ETH | 成功交易的根调用，空 calldata、正 value，按接收地址识别 | `evm_value`，位置 0 |
| 合约内部 ETH | 遍历整笔交易的调用树，取成功、未被上层回滚的正 value `CALL`。忽略静态上下文与自转 | `evm_call`，按完整树先序编号，根为 0。失败分支也占位置 |
| USDC | 检查已配置 USDC 合约的 ERC-20 Transfer 日志，读取实际付款地址、收款地址和整数数量 | `evm_log`，使用链上 `logIndex`，可能与回执内的数组下标不同 |

两种资产各自保存最后成功扫描的高度和区块哈希。每次推进一块，检查 finalized 高度、父哈希、交易列表与整个区块的回执。ETH 还需按区块哈希读取完整的调用轨迹。节点拒绝这一方法时，ETH 停在前一块，USDC 可以继续。允许的交易类型、内部调用形态和实际费用沿用[Base 到账核验规则](Base到账核验.md)。协议字段见 [Ethereum 区块回执](https://ethereum.github.io/execution-apis/next/api/methods/eth_getBlockReceipts/) 和 [Geth callTracer](https://geth.ethereum.org/docs/developers/evm-tracing/built-in-tracers)。

游标与该区块的候选事件同事务提交。核验后台按独立的事件位置重读交易，核对其金额、目标、区块哈希、高度和时间，再按已有流程保存实收、核对请求中的付款人、资产、金额和有效期。中途重启可复用已保存的交易证据。两笔付款竞争同一请求时只有一笔归属成功，其余实收保留待核查。

## 启用与排查

运行环境为 local 或 dev，服务处于 dev 模式，业务库已迁至 business 35，Redis 已启用。设置 `DEXWALLET_CHAIN_BASE_SEPOLIA_RPC_URL`，选用支持历史区块、交易回执、历史费用查询的可信节点。ETH 还需要 `debug_traceBlockByHash` 和 `debug_traceTransaction` 两项 callTracer 能力。区块证据和交易核验均使用配置节点并核对 chain ID 84532。

监听默认关闭。配置中的 `AnchorHeight` 是已经确认且时间**严格早于最早收款请求生效秒**的区块。Base 的多个连续区块可能共享同一秒，填写时应检查区块时间。下面的锚点适用于本次 USDC 历史样例。

```yaml
ChainAssets:
  Timeout: 18s
BaseReceiptListener:
  AnchorHeight: 53543600
  PollInterval: 5s
  OperationTimeout: 25s
  RetryInterval: 1m
```

某资产首次签发请求后才启用该资产的游标。启动时发现锚点太晚会记录原因，可修正配置后重启。游标一旦建立，后台始终从数据库保存的位置继续。节点限流、区块响应超出链客户端 2 MiB 上限或区块内容不完整时保留进度。可用下列只读查询对照节点的 finalized 高度，检查扫描差距和待核验原因。

```sql
SELECT asset_id, height, block_hash FROM base_receive_cursor;

SELECT r.request_key, c.height, c.tx_hash, c.event_kind, c.event_index,
       c.amount_atoms, c.status, c.attempts, c.next_try, c.detail
FROM base_receive_candidate c
JOIN chain_receive_request r ON r.id = c.request_id
ORDER BY c.id DESC LIMIT 50;
```

`pending` 表示等待核验或下次重试，`matched` 表示已归属，`unmatched` 表示实收已保存且与请求条件不符或请求已有付款。存在游标后回退至 business 34 会被拒绝。本次覆盖真实历史短区间补扫；持续追上出块速度、节点归档深度及限流配额仍需在部署环境验收。[可复查的交易与测试命令](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)
