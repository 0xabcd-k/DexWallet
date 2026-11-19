# Solana 平台出款执行

5.3 第十批接通 Devnet 原生 SOL 平台出款。专用测试账户同时支付本金和网络费。内部调用入口是 `backend/logic/chainsend.NewSolana`，显式运维入口是 `backend/tools/solana_payout_devnet`。SPL 代币出款、自动扫描、订单结算与生产签名属于后续批次。已有 SOL、USDC、测试 USDT 的收款能力见 [Solana 到账核验](Solana到账核验.md)。

## 一笔出款如何执行

1. 调用方以稳定的业务请求键、目标地址和正整数 lamports 调用 `Prepare`。执行器向配置的 Devnet RPC 校验 genesis，再取得 confirmed 区块哈希及最后有效区块高度。它保存付款地址、金额、区块信息和待签消息的 SHA-256。同键重试读取原请求，同键改金额或目标返回冲突。
2. 签名进程用独立的付款授权核对目标和金额。测试签名脚本使用 Solana SDK 重建一条 legacy System Program transfer，并核对消息摘要。平台账户是唯一签名者。Go 执行器的 `Bind` 验证原消息的 Ed25519 签名并提交数据库。第一签名就是交易 ID，可在广播前持久保存。[Solana 交易结构](https://solana.com/docs/core/transactions/transaction-structure)
3. `Step` 根据原签名、原区块哈希、付款人、收款人、金额和单条指令核验到账。它优先查看数据库已保存的链上证据，再查询可信节点的 finalized 交易与历史签名状态。两个查询均明确返回空、出款处于 signed、当前区块高度仍在有效期内时，才保存一次广播尝试，并提交原交易。RPC 开启预检，关闭节点自动重试，响应须匹配原签名。[sendTransaction](https://solana.com/docs/rpc/http/sendtransaction) · [getLatestBlockhash](https://solana.com/docs/rpc/http/getlatestblockhash)
4. 节点接收后状态进入 submitted。链上最终确认且到账事件保存后进入 confirmed。收款证据和出款状态分两次短写入，若在中间退出，下一次按原请求重新核验已保存的证据，复用同一到账事件。重复运行 confirmed 请求直接读取持久结果。

| 操作中断位置 | 按原 key 恢复 |
| --- | --- |
| 准备或签名落库时断线 | 查询原记录，重复 Prepare 或提交同一签名 |
| 广播前后超时 | 保留 signed、签名和尝试次数，执行 Step 查询原签名。仅在上述条件全部满足时重放原字节 |
| 节点已见签名、尚未最终确认 | 继续观察原签名 |
| 超过最后有效高度 | 保留原交易继续查证。重新授权付款需要业务侧另外核查原结果 |
| 节点报告最终执行失败或证据不匹配 | 返回明确错误，由操作人员核查原请求和节点证据 |

两个独立出款若具有同一付款人、目标、金额和区块哈希，会产生相同的待签消息。数据库对消息摘要设唯一约束，第二个请求返回冲突。该请求尚未占用记录，可以在后续区块使用自己的原请求键重新准备。已占用记录的请求始终沿用首次保存的区块哈希。

## 运行一次测试

准备本机隔离测试环境，加载 [专用 MySQL 配置](开发与测试环境.md)。下例使用仓库已有的免费 Devnet 测试账户。外部测试签名脚本只允许已固定的样例付款。每轮使用同一份 run 引用、授权和签名文件，跨进程执行各动作。当前样例已完成，历史验证可使用文末的测试直接离线重放。

从仓库根目录构建，以下 `$work` 指向本机保存本次样例的目录。`policy.json` 包含 Key、Sender、Recipient 和十进制字符串 Lamports，示例值见 [真实测试快照](../../backend/logic/chainreceipt/testdata/sol-payout.json) 的 intent 字段，其中 RequestKey 对应 Key。

```bash
(cd backend && go build -o "$work/solana_payout" ./tools/solana_payout_devnet)
probe() { "$work/solana_payout" -state "$work/run.json" -policy "$work/policy.json" "$@"; }
probe -action prepare > "$work/prepared.json"
node scripts/payment_probe/solana-payout-sign.mjs "$work/prepared.json" .tools/payment-feasibility/solana-sponsor-only.json "$work/detached.sig"
probe -action bind -signature-file "$work/detached.sig"
probe -action step
probe -action status
```

`step` 可跨进程重复执行。观察到 confirmed 后，用 `probe -action evidence` 导出意图、实收和两份原始 RPC 结果，再执行 `probe -action cleanup` 清理该专用测试库。签名文件只保留在本机，未过期的签名足以广播该笔固定付款。

business 23 新增独立的 `solana_payout` 表，额度以规范的 u64 十进制保存。22 → 23 保留既有 TON 发送记录。若已存在任何 Solana 意图，23 → 22 会拒绝删除它。当前确认结果供内部链上执行使用，用户提现、美元账本与费用扣取仍待业务阶段接入。

离线回归使用公开的已消费签名，结合专用数据库覆盖重启、并发和过期边界。从 backend 执行 `go run ./tools/testenv run go test -race -tags=integration ./tests/integration -run '^TestSolanaPayout' -count=1`。真实结果和验证条件见 [第十批验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
