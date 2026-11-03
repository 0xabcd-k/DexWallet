# Solana USDC 代付与代币账户费用：2026-11-02

[前一轮 SOL 实转](06Solana钱包代付实转验证.md) · [四链验证基线](05支付可行性验证.md) · [阶段讨论](01讨论记录.md)

## 结论

**Solflare 扩展 2.41.1 + Solana Devnet + Circle 官方测试 USDC 已完成两笔真实支付**：用户钱包仅签名，后端校验完整消息，平台补签、模拟、广播，最后核对 finalized 回执和实际代币余额。

| 场景 | 用户支付 | 用户 SOL 变化 | 平台网络费 | 平台创建代币账户的存储押金 | 结果 |
| --- | ---: | ---: | ---: | ---: | --- |
| 收款方首次接收该 USDC，尚无 ATA | 0.01 USDC | 0 | 0.00001 SOL | 0.00148844 SOL | finalized，收款 0.01 USDC |
| 同一收款方再次收款，复用 ATA | 0.01 USDC | 0 | 0.00001 SOL | 0 | finalized，再收款 0.01 USDC |

首次由平台实际支出 0.00149844 SOL；第二次仅支出 0.00001 SOL。**ATA 存储押金与网络手续费应分开展示和核对，不能把它们都当作每笔必收的 Gas。** 这些是测试网当次实际数值，不是生产报价或费用承担决议；[费用逐项审核](../../../链上资金费用逐项审核.md) 仍按用户审核进度执行。

本轮还通过真实钱包拒绝签名、重复业务提交、同字节即时重放及重放后余额核对。未实现业务数据库订单或账本入账，也不把此结果推广到主网 USDC、USDT、其他钱包或移动端。用户账户本来仍有少量 SOL，本轮证明其 SOL 未被扣费，**尚未验证用户 SOL 严格为零时的钱包界面行为**。

Solflare 在线安全预览仍显示服务器错误，按钱包原有的信任本机站点选项批准测试请求；没有修改钱包程序或伪造风控响应。正常预览和生产用户体验仍待验证。

## 资产、账户与资金准备

根据 [Circle 官方地址表](https://developers.circle.com/stablecoins/usdc-contract-addresses) 选定 Devnet USDC mint，并通过真实 RPC 核对其所属程序、初始化状态与精度。没有自行发行同名代币代替 USDC。

| 项目 | 本轮固定值 |
| --- | --- |
| 网络 | Solana Devnet，`https://api.devnet.solana.com` |
| USDC mint | `4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU` |
| Token Program | `TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA`，原版 SPL Token |
| 精度与单次金额 | 6 位；10,000 个最小单位 = 0.01 USDC |
| 用户 | `9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf` |
| 平台 fee payer / ATA payer | `DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN` |
| 收款钱包 | `7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5` |
| 用户 USDC ATA | `78j9szZm4GTNojYZG2WKKc1BGZJX1wJQnf537Tsq2Tu5` |
| 收款 USDC ATA | `J7PRVxqgPXaXQ9dvZuetPR6UdXFrbEKo7rSzfqS8Yr4o` |

[首次链上读取](attachments/solana-usdc-2026-11-02/solana-usdc-initial.json) 确认两端 USDC ATA 都不存在，平台已有足够 Devnet SOL。通过 [Circle 免费水龙头](https://faucet.circle.com/) 请求 20 USDC 时，网站要求人工验证；[提示记录](attachments/solana-usdc-2026-11-02/usdc-faucet-attempt.json) 保留，未绕过验证或改用新地址规避限制。用户协助领取后，核对用户 ATA 实收 20 USDC，收款方 ATA 仍不存在。

[水龙头到账及资产快照](attachments/solana-usdc-2026-11-02/solana-usdc-funded.json) · [领取交易](https://explorer.solana.com/tx/466D6RaqNSVPPrWgeAfGzjC7pu1ZtkVF9KdQJUMju1qSxGYEoZYq8dVhQJyH3aqSzG1nUe2pwLbD5jDiTeaRiKb9?cluster=devnet)。领取交易只证明测试资产准备，不作为用户钱包支付签名的证据。

## 两笔支付证据

| 项目 | 首次 ATA | 已有 ATA |
| --- | --- | --- |
| 本机测试 reference | `d8578cc7-bba5-4e0c-95f8-6f65d4e858b0` | `7e2984de-567b-4da4-b225-a7a55d88a34d` |
| 广播时间（UTC） | 2026-11-02 10:39:01 | 2026-11-02 10:50:32 |
| 最终确认 slot | `517827592` | `517830459` |
| 完整回执 | [首次回执](attachments/solana-usdc-2026-11-02/solana-usdc-new-ata-public-result.json) | [再次回执](attachments/solana-usdc-2026-11-02/solana-usdc-existing-ata-public-result.json) |
| 钱包签名及重放 | [首次运行](attachments/solana-usdc-2026-11-02/solana-usdc-new-ata-wallet-pay.json) | [再次运行](attachments/solana-usdc-2026-11-02/solana-usdc-existing-ata-wallet-pay.json) |
| 重放后余额 | [首次核对](attachments/solana-usdc-2026-11-02/solana-usdc-new-ata-balance-audit.json) | [再次核对](attachments/solana-usdc-2026-11-02/solana-usdc-existing-ata-balance-audit.json) |

[首次支付浏览器](https://explorer.solana.com/tx/4KyfkNqosDFzAd4sAQwmGEtqEmmGZmp7hHFs8BNz5Za5TpuL9rREueodng2L1wNvpJgzmdhBdSkk2BQFxpqLf7pf?cluster=devnet) · [再次支付浏览器](https://explorer.solana.com/tx/3Wbx2643dTmDTrYQhSbbcXTV27KZVNW2cGUMppCp24U1Uv5PMKU3NiHPjCFyq3K9N9LB15NvDvixqxMTbXbuePcz?cluster=devnet)

两笔 `meta.err` 都为 null，钱包返回的消息摘要均与后端构造值一致：

```text
首次 ATA 330a56d3641a49da50df1fc015e28246032a11d94b2505f650b2db6e3822d5b3
已有 ATA becf9c719520d908c152e17bfd9c2b006ecea8efb777cbb3165e0a65886cc0a9
```

用户 USDC 从 20 → 19.99 → 19.98，收款方从 0 → 0.01 → 0.02。用户 SOL 始终为 4,999,000 lamports；平台 SOL 从 4,993,985,000 → 4,992,486,560 → 4,992,476,560 lamports。收款 ATA 在第一笔获得 1,488,440 lamports 存储押金，第二笔保持不变。

核对使用 `preTokenBalances` / `postTokenBalances` 的整数 amount，并匹配账户索引、mint、owner、programId 和 decimals；不会用 SOL 余额变化推断 USDC 到账。每次即时重放完成且交易 finalized 后，再读取五个相关账户，使用 `minContextSlot` 避免读到早于回执的快照；代币与 SOL 余额均等于单次回执的结果。

## 交易构造与费用含义

两笔都由服务端先确定 fee payer、收款 owner、mint、整数数量、Token Program、ATA 地址和 blockhash，构造同一类原子交易：

1. `CreateAssociatedTokenAccountIdempotent`：平台作为创建付款人，收款钱包作为代币账户 owner；账户已存在时不再次创建。
2. `TransferChecked`：用户作为转出授权人，核对 USDC mint、6 位精度及 10,000 最小单位。

选择依据为 Solana 官方 [ATA 创建说明](https://solana.com/docs/tokens/basics/create-token-account) 和 [TransferChecked 说明](https://solana.com/docs/tokens/basics/transfer-tokens)。本轮使用 `getMinimumBalanceForRentExemption(165)` 取得 1,488,440 lamports，真实开户支出与查询一致；没有将常见示例的 2,039,280 lamports 写死为收费值。探针的 3,000,000 lamports 存储押金上限只是测试支出保护，不是产品费率。

存储押金保留在 ATA 中，属于账户存储资金。官方关闭账户流程可以将剩余 lamports 转给关闭指令指定目标，**不会自动返给最初垫付的平台**；本轮未关闭账户、未验证退款，也未决定未来由谁最终承担此项资金。该区别应进入后续费用审核和账务设计。

签名前检查官方 mint、账户 authority、数量充足、账户未冻结及 ATA 场景正确；补签前复用原有完整消息字节比较和用户验签。平台签名后先模拟再广播，原签名和交易 ID 在广播前保存。链上成功后核对：用户 SOL 未变、USDC 等额转出和到账、平台只支付网络费与实际 ATA 存储资金。

## 拒绝、重复与失败记录

- **真实钱包拒绝**：[拒绝记录](attachments/solana-usdc-2026-11-02/solana-usdc-new-ata-wallet-reject.json) 返回 `Transaction cancelled`；后端保留 prepared 状态，没有用户签名或平台签名，不进入广播。
- **没有 USDC**：[领取前预检查](attachments/solana-usdc-2026-11-02/usdc-preflight-before-faucet.json) 返回 HTTP 400，明确要求准备官方测试 USDC，没有把缺失 ATA 伪装成普通成功。
- **首次支付即时重放**：提交后约 0.6 秒重发相同签名字节，RPC 接受并返回原交易签名；最终只发生一次 USDC 转移和一次平台扣费。
- **再次支付即时重放**：RPC 明确返回 `AlreadyProcessed`，探针保留 HTTP 400 和原错误；原交易仍 finalized，余额未再次变化。这与上一轮 SOL 的 `BlockhashNotFound` 是不同证据。
- **两笔重复业务提交**：均返回原签名和 `reusedSubmission: true`；没有创建新的付款消息。
- **RPC TLS 故障**：[诊断汇总](attachments/solana-usdc-2026-11-02/rpc-errors.json) 保留一次首次场景、四次已有账户场景的准备失败。检查时均无可广播签名，才允许重新请求钱包流程；失败未伪装为支付成功。独立只读 RPC 当时可恢复，直连仍超时。最后一次在浏览器初始化后等待 15 秒再准备交易成功，具体网络根因未证实，不能把这个等待当作生产修复或可靠性保证。

[首次钱包截图](attachments/solana-usdc-2026-11-02/solana-usdc-new-ata-sign.png) 与 [再次截图](attachments/solana-usdc-2026-11-02/solana-usdc-existing-ata-sign.png) 均保留在线安全预览错误。本轮批准的仅是固定 localhost 测试站点、固定账户、固定 0.01 USDC；没有声称钱包金额预览或在线风控验证通过。

## 复现与状态保留

沿用上一轮已核验的原始 Solflare 扩展、独立 profile 和专用测试账户，Node 24.22.1 / Playwright 1.64.1 / Chromium 156.0.8078.4。依赖版本未变，无新增业务依赖。脚本只连接 Devnet，读取平台签名文件；钱包驱动只读取本机解锁密码，通过原版扩展界面操作，不读取用户私钥或直接调用 SDK 用户签名。

本轮新增 `solana-usdc-core.mjs`、`solana-usdc-wallet.mjs`、`solana-usdc-audit.mjs` 和行为测试；既有 SOL 服务增加明确的 USDC 两种测试模式，复用相同的签名验证、落盘、广播和查询代码。未修改生产 API、产品界面或数据库。

以下命令在仓库根执行，先切换到 Node 24.22.1；服务与钱包驱动使用不同终端。当前两笔均已完成，**不能直接清空状态重跑付款**；新一轮需要先核对旧交易终态和测试账户现状。现有收款 ATA 已创建，不能再把它当作首次场景。

```sh
# 离线行为测试：9 项 SOL + 11 项 USDC
node --test scripts/payment_probe/solana-live.test.mjs scripts/payment_probe/solana-usdc.test.mjs

# 场景一：要求收款 ATA 不存在，状态名 solana-usdc-new-ata
node scripts/payment_probe/solana-live-server.mjs --usdc
# 在另一终端：--connect 默认不付款；--reject 验证取消；--pay 才签名、提交并即时重放
node scripts/payment_probe/solana-usdc-wallet.mjs --connect
node scripts/payment_probe/solana-usdc-wallet.mjs --reject
node scripts/payment_probe/solana-usdc-wallet.mjs --pay
node scripts/payment_probe/solana-live-check.mjs
node scripts/payment_probe/solana-usdc-audit.mjs

# 停止上一个服务后启动场景二：要求 ATA 已存在，状态名 solana-usdc-existing-ata
node scripts/payment_probe/solana-live-server.mjs --usdc --existing-ata
node scripts/payment_probe/solana-usdc-wallet.mjs --pay
node scripts/payment_probe/solana-live-check.mjs
node scripts/payment_probe/solana-usdc-audit.mjs
```

`solana-usdc-audit.mjs` 必须在该笔 finalized 后、其他付款改变余额之前执行，否则会明确报告快照已变化；它不是长期业务余额不变的承诺。`solana-live-check.mjs --replay` 仍可显式重发同一字节，区分接受、AlreadyProcessed 和 BlockhashNotFound，不自动换 blockhash。关闭浏览器和服务不会删除证据。

本机 `.tools/payment-feasibility/solana-usdc-new-ata-state.json` 与 `solana-usdc-existing-ata-state.json` 分开保存两笔订单及签名字节，0600；上一轮 `solana-live-state.json` 未被覆盖。解锁配置、测试私钥和完整 profile 仍在忽略目录，不入库。附件中的钱包 `userSigned` 字段是已剔除原始交易字节的服务端公开结果对象，不是私钥或完整可广播交易。

## 验证范围与下一步

20 项离线行为检查通过，包含 USDC 数量、mint、精度、目标 ATA、ATA 付款人和收款 owner 被篡改时拒绝，缺币、冻结、错误 authority 和租金超限时拒绝，以及真实 token 变化与费用核对。两种真实场景、拒绝签名、重复提交与即时重放证据见上文。

HTTP 冒烟确认 USDC 场景标识正确、已签名订单不能被重新创建、密钥路径 404、外部 Origin 被拒绝；默认 SOL 模式与上一笔最终回执检查通过。探针 JS/Python 语法、文档链接、差异检查通过；27 个本轮文件未发现本机已知测试私钥、助记词或密码，两笔链上签名均与保留记录匹配。bigint-buffer 沿用纯 JS 回退提示，未影响这些检查。测试服务、浏览器与 REPL 均已关闭，本机钱包资料和证据保留。未重跑无关业务集成测试。

当前已有 Solflare 扩展的 SOL 与 USDC 测试网实转依据。此前建议继续 Base Sepolia USDC；用户同日曾因测试币获取门槛暂停，随后取得测试 ETH 并明确恢复 Base 需求及验证，见 [范围决议](../../../../decisions/决议与待落实事项.md)。后续按恢复后的四链九组合范围推进。Solana USDT、其他钱包、用户零 SOL、完整报价体验、未知广播结果恢复及业务账本入账仍各有独立验收，不能用本轮成功替代。

后续 [Solana USDT 代付机制验证](17SolanaUSDT代付机制验证.md) 复用此 SPL 支付逻辑并增加固定自建测试资产；旧 USDC 调用入口、命令及存档保留，提取后的公共逻辑已核对本轮两笔历史回执。该测试资产不属于 Circle 或 Tether 官方发行。
