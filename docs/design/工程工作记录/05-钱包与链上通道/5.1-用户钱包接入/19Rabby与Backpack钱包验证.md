# Rabby 与 Backpack：真实钱包兼容验证

[阶段讨论](01讨论记录.md) · [候选范围](05支付可行性验证.md) · [阶段目录](../README.md)

## 本轮结果

2026-11-03（UTC）继续验证候选钱包，完成 Rabby 0.94.14 的 Base Sepolia USDC，以及 Backpack 0.10.219 的 Solana Devnet SOL、Circle USDC、自建 USDT 测试替身四笔付款。全部最终确认，钱包只签名，后端验证原意图后使用平台账户支付网络费并广播。没有接入正式产品或账本，也没有将这些桌面扩展结果推广到同品牌移动端。

| 钱包 / 测试资产 | 用户本金 | 用户网络费 | 平台网络费 | 结果 |
| --- | ---: | ---: | ---: | --- |
| Rabby / Base Sepolia USDC | 0.01 USDC | 0 ETH；用户余额始终 0 | 0.000000547560000199 ETH | finalized |
| Backpack / Devnet SOL | 0.000001 SOL | 0 | 0.00001 SOL | finalized |
| Backpack / Circle Devnet USDC | 0.01 USDC | 0 SOL | 0.00001 SOL | finalized，复用 ATA |
| Backpack / Devnet USDT-FIXTURE | 0.01 自建测试币 | 0 SOL | 0.00001 SOL | finalized，复用 ATA |

USDT-FIXTURE 沿用 [Solana USDT 机制验证](17SolanaUSDT代付机制验证.md)的独立 mint；不是 Tether 发行资产。三笔 Solana 交易的用户余额由 4999000 → 4998000 lamports，之后两笔代币付款均不再扣 SOL。两种代币本轮分别从用户 19.98 → 19.97，收款方 0.02 → 0.03；没有新增 ATA 押金。

## 官方客户端与原生界面

安装源分别来自 [Rabby 官网](https://rabby.io/)及 [Backpack 官网](https://backpack.app/)指向的 Chrome 商店链接。由 Google CRX 服务下载，用仓库 `verify_crx.py` 校验 CRX3 开发者签名及预期扩展 ID，再加载未修改内容。完整版本、原包大小及 SHA-256 见[包记录](attachments/rabby-backpack-2026-11-03/packages.json)。原 manifest 没有 key，解包加载的运行时 ID 与商店 ID 不同，已如实记录，没有写回 manifest 或替换钱包 provider。

| 客户端 | 商店 ID | 原 CRX SHA-256 |
| --- | --- | --- |
| Rabby 0.94.14 | `acmacodkjbdgmoleebolmdjonilkdbch` | `819badd93963634b83d1701869054e51e0b26cd6602ba2eefc2d1306cd3de084` |
| Backpack 0.10.219 | `aflkmfhebedbjioipglgcbcmnbpgliof` | `dfe0a35ffab357a25309840c9065fb6c050dc3050b5c4d02051b26a2efad3301` |

测试账户经原钱包导入界面加入独立 profile；私钥和解锁密码仅保留本机忽略目录。实际付款服务只读取平台密钥，用户签名由原钱包产生。

Rabby 通过私钥导入专用 Base 用户，原生连接窗口显示本机测试站点未收录、知名度低；核对固定 `127.0.0.1:18793` 后使用原有选项继续。未添加 Base Sepolia 时，`wallet_switchEthereumChain` 返回 `-32603` 和固定链 ID 的 `Unrecognized chain ID`，而非 MetaMask 常见的 4902。探针仅对 Rabby 这一明确错误进入添加网络流程，没有吞掉其他 RPC 错误。原生添加页核对 chain ID 84532、ETH、`https://base-sepolia-rpc.publicnode.com` 和 Base Sepolia 浏览器，见[截图](attachments/rabby-backpack-2026-11-03/rabby-add-base-sepolia.png)。

Backpack 的原生路径：导入钱包 → Solana → Private key → 设置密码；Settings → Preferences → Developer Mode 开启，再进入 Settings → Solana → RPC Connection → Custom，将 RPC 设置为 `https://api.devnet.solana.com`。本机无头 Chromium 自动弹出窗口不稳定，直接打开钱包原有 `popout.html` 页可呈现待批准请求；不是伪造批准或修改消息。此前在普通 popup 中刷新 / 关闭会使连接返回 `Plugin Closed`。地址恢复后，provider 返回用户 `9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf`，实际提供 `signTransaction`。

## Rabby USDC 与 ETH 的区别

Rabby 一次真实取消返回 4001 / `User rejected the request`，未生成付款授权；随后原生窗口显示 `TransferWithAuthorization` 的完整 typed data。核对 Base Sepolia chain ID、Circle 合约、from、to、value=10000、nonce 及有效时间后，依次点击 Sign、Confirm。后端恢复签名者，验证订单和授权域，再由平台模拟及执行。测试 UI 显示 `Unknown Signature Type` 和原始 JSON，未验证常规用户友好的 USDC 金额预览，见[拒绝截图](attachments/rabby-backpack-2026-11-03/rabby-usdc-reject.png)、[签名截图](attachments/rabby-backpack-2026-11-03/rabby-usdc-sign.png)。

交易：[Base Sepolia 回执](https://sepolia.basescan.org/tx/0xeab4ed6eca3ba2be87d597e8696426c78734cc93496f21060d76efe190fe5fb4)。平台费用包含 L2 执行 547560000000 wei、L1 数据费 199 wei、operator fee 0；USDC 仅移动一次，用户 ETH 保持零。最终区块覆盖回执区块；已消费 nonce 再模拟被合约拒绝为 `authorization is used or canceled`。原字节重发和重复业务提交均保留同一 txHash，未创建第二笔付款。

证据：[最终回执和余额审计](attachments/rabby-backpack-2026-11-03/base-rabby-usdc-public-result.json) · [钱包行为和能力查询](attachments/rabby-backpack-2026-11-03/rabby-usdc-wallet.json) · [重放](attachments/rabby-backpack-2026-11-03/base-rabby-usdc-replay.json)。第一次准备请求曾遇到 RPC 连接重置，未产生授权；重试准备后进入上述真实签名。

同一已连接用户下，Rabby 的 `wallet_getSupportedExecutionPermissions` 和 `wallet_getCapabilities` 均实际返回 **-32601，没有对应 handler**。因此本版本不能直接复用现有 MetaMask Advanced Permissions 的原生 ETH 代付入口；这不等于证明所有 Rabby 账户、未来版本或其他智能账户方案都不可行。没有广播普通 ETH 转账去冒充独立 gas payer 的支持，也没有执行账户升级。

## Backpack 三种资产与预览限制

SOL 首先真实拒绝，返回 `Approval Denied`；随后三笔分别经过 `signTransaction`，原消息字节未变，用户签名有效，平台签名仍为空。后端验证 blockhash 有效高度、实际估费后补签、模拟和广播；钱包没有自行提交交易。

| 资产 | 交易 | 最终证据 |
| --- | --- | --- |
| SOL | [浏览器](https://explorer.solana.com/tx/62BDTcRNuzyT1EZg8fb9QYRBXr45TEaQTXa7jDok8eNbN2JWikGcBC4T7qXdLQSnF4ToqaTgZp68Pf5vDALGvMkU?cluster=devnet) | [回执](attachments/rabby-backpack-2026-11-03/backpack-solana-live-public-result.json)、[钱包](attachments/rabby-backpack-2026-11-03/backpack-sol-wallet.json) |
| USDC | [浏览器](https://explorer.solana.com/tx/5YqN1vKnbxPC4LR5YPWEzSXAhJcPa1fNfSZUpiNM1hm25HBBMTjzjW8KmkicRuy5Bx5D8WWt8K1mUfgNMbFHNnrF?cluster=devnet) | [回执](attachments/rabby-backpack-2026-11-03/backpack-solana-usdc-existing-ata-public-result.json)、[钱包](attachments/rabby-backpack-2026-11-03/backpack-usdc-wallet.json)、[余额](attachments/rabby-backpack-2026-11-03/backpack-solana-usdc-existing-ata-balance-audit.json) |
| USDT-FIXTURE | [浏览器](https://explorer.solana.com/tx/QtzrB3Etn3S1cbnenMGBkic6vtFo6VvVfSsr4V3w5UHHfs6bhZBa7ZqCyDFbxuqnNYkhc5mguQpvNhUGTU2epHH?cluster=devnet) | [回执](attachments/rabby-backpack-2026-11-03/backpack-solana-usdt-fixture-existing-ata-public-result.json)、[钱包](attachments/rabby-backpack-2026-11-03/backpack-usdt-wallet.json)、[余额](attachments/rabby-backpack-2026-11-03/backpack-solana-usdt-fixture-existing-ata-balance-audit.json) |

三笔重复 `/submit` 都返回同一签名及 `reusedSubmission`。延迟到 blockhash 过期后原字节重放返回 `BlockhashNotFound`，前后余额不变；不能将过期拒绝称为节点接受重放后去重。[SOL 重放](attachments/rabby-backpack-2026-11-03/backpack-solana-live-replay.json)、[USDC 重放](attachments/rabby-backpack-2026-11-03/backpack-solana-usdc-existing-ata-replay.json)、[测试替身重放](attachments/rabby-backpack-2026-11-03/backpack-solana-usdt-fixture-existing-ata-replay.json)保留实际结果。

钱包在 Custom RPC 下显示 `Simulation not supported`；代币名称显示 `Unknown`，0.01 数量可见，极小 SOL 本金被界面舍入为 0。平台 0.00001 SOL 网络费在另一账户行显示。见 [SOL 预览](attachments/rabby-backpack-2026-11-03/backpack-sol-sign.png)、[USDC 预览](attachments/rabby-backpack-2026-11-03/backpack-usdc-sign.png)、[替身预览](attachments/rabby-backpack-2026-11-03/backpack-usdt-sign.png)。这些限制在固定测试意图和后端完整校验下记录，**不能作为正式支付预览已通过的结论**。页面文本提取还包含隐藏的风险提示节点，实际可见状态须结合截图，不以 `innerText` 出现某句文案就判断风险提示已经展示。

## 后续复用

探针新增固定钱包参数，按钱包分开保存状态，避免覆盖已经完成的其他钱包订单。没有新增依赖或通用任意币转账入口；支持名单只是本次脚本的固定候选，不是产品白名单。

```sh
# 每次只启动对应端口的一个服务；旧订单已经完成，不删除状态重跑
node scripts/payment_probe/base-live-server.mjs --wallet=rabby
node scripts/payment_probe/base-live-check.mjs

node scripts/payment_probe/solana-live-server.mjs --wallet=backpack
# 停止上一服务后，可分别查询下列已完成场景
node scripts/payment_probe/solana-live-server.mjs --wallet=backpack --usdc --existing-ata
node scripts/payment_probe/solana-live-server.mjs --wallet=backpack --usdt-fixture --existing-ata
node scripts/payment_probe/solana-live-check.mjs
node scripts/payment_probe/solana-usdc-audit.mjs
```

钱包操作可通过上述原生 UI 路径复查；DApp 中选 Backpack、连接、仅签名、平台提交依次分开。已有订单禁止再次准备，不能为演示而清空终态。余额审计须紧随对应付款、在其他付款改变账户前执行；历史余额使用本页留存附件。通用 Solana、代币及 Base USDC 离线行为测试共 33 项通过，仍保留篡改、错误签名者、资产身份、冻结及手续费核对。
