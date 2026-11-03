# Solana USDT：代币身份、代付机制与冻结验证

[阶段讨论](01讨论记录.md) · [Solana USDC 实转](07SolanaUSDC代付与账户费用验证.md) · [阶段目录](../README.md)

## 结论与边界

2026-11-03（UTC）完成 **Solflare 2.41.1 + 自建 Devnet SPL 测试币** 的两笔真实签名、平台代付和 finalized 到账。测试币使用与主网 USDT 相同的原版 Token Program 和 6 位精度；本次只读查询还核对了两个网络该程序的实际代码哈希相同。用户只签名，后端校验原消息后补签、模拟和广播，用户 SOL 未减少。

**本轮是 Solana USDT 所需转账机制的验证，不是 Tether 官方测试 USDT 或主网 USDT 实转。** 本次未找到可核实的 Tether 官方 Devnet USDT mint / 水龙头，因而自行创建了独立测试 mint，明确标为 `USDT-FIXTURE`，没有使用官方名称或图标伪装发行来源。程序相同支持技术可行性判断，不能替代正式 mint 的钱包预览、发行方冻结状态、主网网络环境与费用验收。

| 场景 | 用户支付测试币 | 用户 SOL 变化 | 平台网络费 | 平台收款 ATA 存储押金 | 结果 |
| --- | ---: | ---: | ---: | ---: | --- |
| 首次收款，创建 ATA | 0.01 | 0 | 0.00001 SOL | 0.00148844 SOL | finalized |
| 再次收款，复用 ATA | 0.01 | 0 | 0.00001 SOL | 0 | finalized |
| 来源账户被冻结 | 无支付 | 0 | 无支付网络费 | 0 | 准备阶段 HTTP 400，未生成付款签名 |

首次实际支出 0.00149844 SOL，第二次 0.00001 SOL；测试币从用户 20 → 19.99 → 19.98，收款方 0 → 0.01 → 0.02。两笔即时重放都返回 `AlreadyProcessed`，之后实读余额与单笔回执一致。ATA 存储押金仍不等于网络手续费，未改变[逐项费用审核](../../../链上资金费用逐项审核.md)的待审核规则。

## 身份与程序核对

[Tether 官方协议表](https://tether.to/en/supported-protocols/)列出的 Solana USDT mint 为 `Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB`。通过主网 RPC **只读**确认：owner 为原版 Token Program，已初始化，精度 6，mintAuthority 与 freezeAuthority 均为 `Q6XprfkF8RQQKoQVG33xT88H7wi8Uk1B1CC7YAs69Gi`。保留冻结权限意味着转账仍须检查账户冻结状态。

同一个地址在 Devnet 上实际是 **System Program 的普通 SOL 账户，space 0，并非 mint**。仅复制主网地址、看到地址存在、或按 USDT 名称搜索，都不能证明测试资产正确。开发时固定“网络 + mint + Token Program”，不只校验符号。

[身份与程序证据](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-identity.json)保存主网 mint、Devnet 同地址、测试 mint 与账户读取结果。两个网络的 Token Program 均为 `TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA`，ProgramData 地址均为 `3gvYRKWyXRR9xKWe1ZjPhLY5ZJRN7KDB4rFZFGoJfFk2`。去掉升级加载器的 45 字节 ProgramData 元数据后，108600 字节程序区的 SHA-256 均为：

```text
8190d3f7ceb6cb7a7a8d8924bff89f9f611e15ce1f806f2b6237f3311a98f697
```

主网部署 slot 为 419472000，Devnet 为 451008000。该哈希比较针对本次读取时的程序区，不承诺之后升级仍相同，也不把 mint 状态、发行方权限或元数据视为相同。

| 对象 | 固定值 |
| --- | --- |
| 测试网络 | Solana Devnet，`https://api.devnet.solana.com` |
| 自建测试 mint | `2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5` |
| 测试资产标识 / 精度 | `USDT-FIXTURE` / 6 位 |
| 测试发行与冻结权限 | 平台专用测试账户；不是 Tether |
| 用户 | `9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf` |
| 平台 | `DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN` |
| 收款 owner | `7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5` |
| 用户 ATA | `G3fPnGibrCdEUDURrYFxk8Y1KfUmNWGPFsobTCRRbAdt` |
| 收款 ATA | `DT3EUVqAxQ4ZL6bvo419FiA9vpd4efKsn6Htmt4SLE5J` |

按 Solana [创建 mint](https://solana.com/docs/tokens/basics/create-mint)流程，平台先创建测试 mint、用户 ATA 并发行 20 枚，仅用于本次机制验证。准备交易实际支出 2565240 lamports：mint 存储资金 1066800、用户 ATA 存储资金 1488440、网络费 10000，见[准备回执](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-setup-admin.json)。这些是测试夹具准备费用，不计作用户付款手续费。

## 真实签名、到账与重复处理

两笔复用既有 Solana 交易结构：平台为 fee payer，先执行幂等 ATA 创建，再执行 `TransferChecked`，用户作为转出授权人签名。服务器固定 mint、两端 ATA、owner、整数数量、精度、平台付款人和 blockhash，比较完整消息字节并验签，随后核对有效高度、估算网络费、平台补签和模拟。可广播字节在发送前落盘，不因 RPC 失败自动生成替代付款。

| 项目 | 首次 ATA | 已有 ATA |
| --- | --- | --- |
| 广播时间（UTC） | 15:56:01 | 15:57:22 |
| 确认 slot | 518267731 | 518268071 |
| reference | `101d4342-c53d-47c2-a72f-8162fafb8ef6` | `78b15c8d-9760-4325-b07b-ca8c11b5e166` |
| 完整回执 | [首次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-public-result.json) | [再次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-existing-ata-public-result.json) |
| 钱包与即时重放 | [首次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-wallet-pay.json) | [再次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-existing-ata-wallet-pay.json) |
| 重放后余额核对 | [首次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-balance-audit.json) | [再次](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-existing-ata-balance-audit.json) |

[首次链上交易](https://explorer.solana.com/tx/4589sfUuPKo6gsG9dqppgxtCgiFwkePZg4doFPdQp2rKgbE4BjEm7mmGoCUAkAgQeWbJhQ4guBc3xbYTeTNP8nK5?cluster=devnet) · [再次链上交易](https://explorer.solana.com/tx/5MBm7KQaoiaPEoirRbupQiKDqyJeASf1qQKLTSh5XEK7Qd9Bz43GAvQ4hxtq7pGWXToUCvY58YbCybucXoxpxm2u?cluster=devnet)

两笔钱包返回消息摘要与后端原消息相同，`meta.err` 为 null；token 回执按账户索引、mint、owner、programId、decimals 和原子整数核对。用户 SOL 始终为 4999000 lamports，收款 owner 始终为 1001000；平台从准备及解冻后的 4989901320 → 4988402880 → 4988392880 lamports。第二笔后，用户与收款 ATA 各保留 1488440 lamports。

两笔重复 `/submit` 均返回原签名和 `reusedSubmission: true`。即时 `/replay` 重发原字节均返回 HTTP 400 / `AlreadyProcessed`，没有把拒绝包装成接受。交易 finalized 后分别读取相关账户，读取 slot 不早于付款回执；余额等于单次执行结果，没有重复转账、扣网络费或创建 ATA。

## 冻结、拒绝与钱包限制

Solana [冻结账户说明](https://solana.com/docs/tokens/basics/freeze-account)明确冻结可阻止转出及接收。本轮由测试发行权限真实冻结用户 ATA，再请求 `/prepare`：后端返回“Token account is frozen or uninitialized”，服务器状态仍为 null，用户、平台、收款 owner 余额均未变。之后解冻，再执行上述两笔成功支付。冻结和解冻管理交易各花费 5000 lamports，与被拒绝的付款费用分开。

[冻结管理交易](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-freeze-admin.json) · [准备阶段拒绝与余额](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-frozen-rejection.json) · [解冻管理交易](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-thaw-admin.json)

此外，Solflare [真实拒绝](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-wallet-reject.json)返回 `Transaction cancelled`，订单停在 prepared，没有用户或平台签名。首次尝试拒绝流程时，[RPC TLS 超时](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-wallet-reject-rpc-timeout.json)发生在 prepare，未创建付款状态；确认无签名后重试才得到真实钱包拒绝。超时原因未证明，不把人工顺序重试当作生产可靠性方案。

[首次截图](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-new-ata-sign.png)和[再次截图](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-fixture-existing-ata-sign.png)仍显示 Solflare 安全预览服务错误。沿用钱包原有的信任本机测试站点选项，只批准固定账户、固定 mint 和 0.01 测试币，没有修改钱包或伪造风控响应。**没有验证正式 USDT 的正常金额 / 资产展示，也没有验证用户 SOL 严格为零的界面行为。** 实际冻结验证针对签名前已冻结的来源账户；签名后再冻结的竞态与上链失败费用另需验证。

## 脚本与复现

无新增依赖，沿用 Node 24.22.1、Solflare 2.41.1 原版扩展及专用 profile。公共 SPL 逻辑移至 `solana-token-core.mjs`；`solana-usdc-core.mjs` 保留原 USDC 调用入口。固定资产定义只允许 Circle Devnet USDC 和明确标记的自建测试币，HTTP 调用方不能指定任意 mint。`solana-usdc-wallet.mjs` 与 `solana-usdc-audit.mjs` 保留历史文件名，现可按服务配置验证两类固定测试资产。

以下命令在仓库根、Node 24.22.1 下执行。当前两笔与管理交易均已完成，**不能清空状态重跑支付，也不能把现有 ATA 当作首次账户**。以后新增付款先核对旧终态，再明确安排独立测试；本轮未建立生产重试或恢复服务。

```sh
# 离线行为检查；不连接真实网络
node --test scripts/payment_probe/solana-live.test.mjs scripts/payment_probe/solana-usdc.test.mjs scripts/payment_probe/solana-token.test.mjs

# 默认只读核对；主网只查资产与程序，绝不发送主网交易
node scripts/payment_probe/solana-usdt-fixture.mjs --inspect

# 本轮准备顺序，已执行：--setup → --freeze → 验证 prepare 拒绝 → --thaw
# 管理命令保留各自签名字节；再次运行只检查原交易，不补发或增发
# 不应以重跑管理命令来获取历史余额，历史快照使用已保留附件

# 首次场景；另开终端运行钱包驱动 / 查询
node scripts/payment_probe/solana-live-server.mjs --usdt-fixture
node scripts/payment_probe/solana-usdc-wallet.mjs --reject
node scripts/payment_probe/solana-usdc-wallet.mjs --pay
node scripts/payment_probe/solana-live-check.mjs
node scripts/payment_probe/solana-usdc-audit.mjs

# 停止上一个服务后，再运行已有 ATA 场景
node scripts/payment_probe/solana-live-server.mjs --usdt-fixture --existing-ata
node scripts/payment_probe/solana-usdc-wallet.mjs --pay
node scripts/payment_probe/solana-live-check.mjs
node scripts/payment_probe/solana-usdc-audit.mjs
```

余额核对脚本要在当前交易 finalized 后、下一笔改变余额前执行。两笔订单使用 `solana-usdt-fixture-new-ata-state.json`、`solana-usdt-fixture-existing-ata-state.json`，没有覆盖此前 SOL / USDC 状态。签名字节、测试密钥和解锁密码仅在 `.tools/payment-feasibility/`；支付服务只读平台密钥，钱包驱动只读解锁密码，用户签名来自原版扩展。新 mint 密钥文件为 `solana-usdt-fixture-mint-key.json`，不入库。

23 项离线测试通过，覆盖原 SOL / USDC 行为、资产串用、金额 / mint / 目标 / 精度篡改、来源或收款账户冻结、Token-2022 程序混用及余额不足；原三笔 SOL / USDC 存档回执也通过提取后的公共逻辑核对。[HTTP 冒烟](attachments/solana-usdt-fixture-2026-11-03/solana-usdt-http-checks.json)确认已签订单不能重建、外部 Origin 被拒绝、密钥路径 404。附件逐项校验值见 [SHA256SUMS](attachments/solana-usdt-fixture-2026-11-03/SHA256SUMS)。最终语法、文档链接、差异与凭据检查均通过；未重跑无关后端或前端业务测试，临时服务和浏览器已关闭。

## 后续开发参考

本轮支持“Solana USDT 可采用用户签名、平台作为 fee payer 补签执行”的机制判断，但验收表应继续标注“主网身份只读 + 自建测试币机制实转”，不可写成官方 USDT 实转通过。Solflare 正常预览、真实资产识别、用户零 SOL 与其他钱包仍需独立验收。

四链九组合现均已有某种验证依据，证据等级与限制不同：Base ETH 的 13.55 兼容问题、TRON USDT 全资源由本平台承担和钱包精度、TON 新 W5 初始化与中继余款，以及各链正式资产和业务入账都未因此完成。建议下一轮优先回到 TRON USDT，补齐同一笔支付的 Energy 与 Bandwidth 均由项目平台提供的证据，再按已记录问题推进；不据此提前开放充值或改变费用承担决议。
