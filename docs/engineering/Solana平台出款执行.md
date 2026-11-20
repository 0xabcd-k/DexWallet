# Solana 平台出款执行

5.3 第十批接通 Devnet 原生 SOL 平台出款，第十一批接通 Devnet USDC 和项目自建的测试 USDT 出款。专用测试账户同时支付本金和网络费。内部调用入口是 `backend/logic/chainsend.NewSolana`，显式运维入口是 `backend/tools/solana_payout_devnet`。第十二批接通 Devnet 已签名出款的自动恢复。订单结算与生产签名仍待接入。已有 SOL、USDC、测试 USDT 的收款能力见 [Solana 到账核验](Solana到账核验.md)。

## 原生 SOL 出款

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

## SPL 代币出款

调用 `PrepareToken(ctx, key, recipient, assetID, atoms, createATA)`，随后沿用 `Bind` 和 `Step`。recipient 是收款钱包地址，atoms 是规范十进制的正整数，当前两个资产均为六位精度。Devnet USDC 由固定资产目录识别，项目测试 USDT 使用节点配置白名单，资产 ID 包含完整 mint。出款地址同时持有代币并提供 SOL。服务按付款人、收款人、mint 和原版 Token Program 推导两端的关联代币账户（ATA）。[关联账户规范](https://solana.com/docs/tokens/basics/create-token-account)

- **已有账户**：`createATA=false`，只签一条 `TransferChecked`。收款账户缺失时准备阶段报错。
- **允许开户**：`createATA=true`，签名覆盖 `CreateIdempotent` 和随后的 `TransferChecked`。两条指令属于同一交易。签名后若其他人先开好了账户，幂等创建可以继续，核算以该交易真实垫付的 SOL 为准。[代币转账规范](https://solana.com/docs/tokens/basics/transfer-tokens)

准备时在同一次节点查询中检查两端账户的程序、owner、mint、精度、冻结状态及出资账户可用数量。重复准备读取原意图，变更资产、金额、目标或开户权限会返回冲突。签名前后按固定字段重建消息，重启继续使用原区块哈希和原签名。

确认时逐项核对账户表、签名者和可写权限、指令、mint、最终执行状态及两端代币余额差。开户指令还须与原付款人、目标 ATA、收款钱包及程序一致。`receipt.transaction.feeAtoms` 为本次网络费，`intent.AtaFundingLamports` 为目标 ATA 实际增加且由付款人提供的 SOL。付款人 SOL 减少量须等于这两项之和。开户垫资成为目标账户的存储余额。两项分别保存，用户收费规则沿用[逐项审核清单](../design/链上资金费用逐项审核.md)。

business 24 为既有出款表增加资产、mint、代币数量、开户权限和实际垫资字段。原生 SOL 历史记录保留原消息摘要，已有代币意图时禁止回退到 23。本机工具支持逐步执行，第十二批新增下文的自动恢复。

测试工具沿用上一节的 `prepare/bind/step/status/evidence/cleanup`。SPL policy 使用 `Key/Sender/Recipient/AssetID/TokenAtoms/CreateATA`。测试 USDT 需显式配置 `DEXWALLET_CHAIN_SOLANA_DEVNET_TEST_USDT_CONTRACT`，与资产 ID 中的 mint 一致。本机独立签名样例为 `scripts/payment_probe/solana-spl-payout-sign.mjs`，它以锁定的 SDK 核对本次四个样例的固定授权。在 `probe -action prepare` 成功后、bind 前运行：

```bash
node scripts/payment_probe/solana-spl-payout-sign.mjs "$work/prepared.json" .tools/payment-feasibility/solana-test-keys.json "$work/detached.sig"
```

历史实转可在隔离数据库中离线复验：

```bash
cd backend
go run ./tools/testenv run go test -race -tags=integration ./tests/integration -run 'TestSolana(Payout|TokenPayout)' -count=1
```

四笔实际数量、费用、交易签名和原始 RPC 证据见[第十一批验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。两笔新建账户交易完成后，对应 ATA 已存在，重做开户成本实验需要另选尚未开户的测试收款账户。

## 第十二批　已签名出款自动恢复

调度器按配置的 Devnet 平台付款地址领取 signed 和 submitted 出款。签名保存在原出款行，签名提交后即使进程退出，下一次扫描也能发现。自动恢复沿用原签名、消息、区块哈希和最后有效高度。

| 查到的情况 | 处理 |
| --- | --- |
| 已有本地实收证据或链上 finalized 交易 | 核对原授权，保存实收。在同一次出款行更新中写入 confirmed、实际开户垫资，清除租约。 |
| 交易与签名状态均明确为空 | 仅 signed 且区块哈希仍有效时可广播原交易。submitted 或已过有效高度时继续观察。 |
| 节点暂时不可达、签名可见但尚无最终交易、广播回执不明 | 保留原单，间隔后再次查询。 |
| 签名或证据与授权冲突、已核实链上执行失败、耗尽尝试次数 | 转入 review，记录原因，等待运维核对。资金结算和退款由后续业务流程决定。 |

领取使用短事务，每次领取增加代次并记录租约。节点查询期间释放数据库锁。租约过期后其他进程可接管，旧进程的排期写回和新广播尝试均受代次、租约校验约束。已核实的实收证据可以结束原付款。调度次数与原交易广播次数分别保存。

### 启用和运维

先停止使用 business 24 的 Solana 出款写入进程，迁移到 business 25 并启动新版服务。迁移将历史 confirmed 行归为已完成，已有 signed、submitted 行在启用扫描后可领取。升级检查会拒绝发送状态与恢复确认状态不一致的写入。默认配置关闭扫描。启用时在 local/dev 环境的 dev 模式配置文件中加入以下内容，并设置 `DEXWALLET_CHAIN_SOLANA_DEVNET_RPC_URL`。

```yaml
SolanaRecovery:
  Sender: "替换为本环境 Devnet 平台付款地址"
```

扫描只需公钥。项目自建测试 USDT 仍通过 `DEXWALLET_CHAIN_SOLANA_DEVNET_TEST_USDT_CONTRACT` 指定 mint。不同测试付款地址分别配置对应的调度实例。默认一个 worker，空闲轮询 5 秒，单次操作上限 30 秒，租约 60 秒，重试间隔 30 秒，最多领取 60 次。操作超时须在服务排空时间内，租约须长于操作超时。

在 backend 目录运行下列命令。运维工具沿用原目录名 `ton_recovery`，通过 `-network solana-devnet` 选择 Solana。列表提供原签名、资产、最小单位金额、有效高度、错误及调度状态。

```bash
go run ./tools/ton_recovery -network solana-devnet -f /本机配置/solana.yaml -action list -state review
go run ./tools/ton_recovery -network solana-devnet -f /本机配置/solana.yaml -action retry -key '<原请求键>' -reason '已核对原签名和节点状态'
go run ./tools/ton_recovery -network solana-devnet -f /本机配置/solana.yaml -action run-once
```

retry 会记录当前系统用户和原因，并为同一签名恢复有限次数的检查。服务关闭时停止领取新任务，等待进行中的查询写回。已签名记录及尝试历史的降级保护见[契约生成与迁移](契约生成与迁移.md)。
