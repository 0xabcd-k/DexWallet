# Base Sepolia USDC：真实钱包授权与平台代付

[阶段讨论](01讨论记录.md) · [前期协议与暂停记录](05支付可行性验证.md) · [Solana USDC 实转](07SolanaUSDC代付与账户费用验证.md)

2026-11-02 用户取得测试 ETH 后，明确恢复 Base 首版需求并继续验证。本轮已完成 **MetaMask 13.55.0.0 + Base Sepolia 官方 USDC 的真实授权转账**：用户 ETH 为零，只在钱包签署 EIP-712 授权；后端按固定订单验签、模拟并估费，再由独立平台账户支付 ETH 费用、广播和核对到账。

最终状态为 **finalized**，`2026-11-02T15:10:09.932Z` 完成最终核验；[完整公开结果](attachments/base-live-2026-11-02/base-live-public-result.json) 中 finalized 高度已覆盖支付区块。Base 的 ETH、USDC 已恢复首版开发与验收范围，但本轮只验证 USDC 的 EIP-3009 路线，不能据此宣布原生 ETH 代付、所有 MetaMask 账户类型或其他钱包通过。没有接入业务数据库订单、充值入账或生产通道。

## 实转结果

| 项目 | 结果 |
| --- | --- |
| 网络 / 合约 | Base Sepolia，chain ID `84532`；USDC `0x036CbD53842c5426634e7929541eC2318f3dCF7e` |
| 用户 / 实际签名钱包 | `0xecd14F7586f5053CECae148Ae26309C2277D86A8`；MetaMask 扩展导入的普通 EOA |
| 平台付款人兼收款人 | `0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA` |
| 本机 reference | `93ec7037-898c-4382-87e7-19b4737f5b14` |
| 支付数量 | 10,000 最小单位，即 0.01 USDC；精度 6 |
| 用户 USDC | 20 → 19.99 |
| 平台 USDC | 0 → 0.01 |
| 用户 ETH | 0 → 0，全程未补充或扣除用户 ETH |
| 平台 ETH | 0.001 → 0.000999382791999804 |
| L2 执行费用 | 617,208,000,000 wei |
| L1 数据费用 | 196 wei |
| Operator fee | 0 wei |
| 平台实际总费用 | 617,208,000,196 wei = **0.000000617208000196 ETH** |
| 广播时间 / 所在区块 | `2026-11-02T14:43:09.080Z` / `53543602` |

[支付交易](https://sepolia.basescan.org/tx/0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17) 的哈希为 `0xdb945055aa01454df203b52e16cecf1548e6c826627da9aaba98f908db3a3d17`。回执 status=1，调用人是平台，目标是官方 USDC 合约，原生 value=0；实际 calldata 与后端保存的订单授权一致。已核对唯一的 `Transfer`、匹配用户与 nonce 的 `AuthorizationUsed`，以及合约 `authorizationState=true`。这三类证据与资产变化一起使用，不只凭交易哈希入账。

来源依据：[Circle USDC 地址表](https://developers.circle.com/stablecoins/usdc-contract-addresses)、[EIP-3009](https://eips.ethereum.org/EIPS/eip-3009)。首次 SDK 探针已经核对域与 typehash；本轮再次通过 RPC 核对 name=USDC、version=2、decimals=6 与 DOMAIN_SEPARATOR。

## 测试 ETH 的网络与跨网准备

用户领取的 1 ETH 实际到达 **Ethereum Sepolia** 的平台地址，Base Sepolia 的同地址起初仍为零；[只读核对](attachments/base-live-2026-11-02/base-funding-ethereum-sepolia.json) 记录 chain ID `11155111`。不需要购买主网 ETH。

按 [Base 官方合约清单](https://docs.base.org/specifications/reference/base-contracts) 与锁定 viem 的 Base Sepolia 配置，核对 Ethereum Sepolia 上的 OptimismPortal 为 `0x49f53e41452C74589E85cA1677426Ba426459e85`。使用该 Portal 的 `depositTransaction`，将 **0.001 测试 ETH** 存入 Base Sepolia 的同一平台地址；其余测试 ETH 留在 Ethereum Sepolia。调用前校验两端 chain ID、目标代码、余额、模拟和费用上限，广播前保存签名字节及哈希，不自动重复存入。

| 证据 | 结果 |
| --- | --- |
| Ethereum Sepolia 存入 | [L1 交易](https://sepolia.etherscan.io/tx/0x61e72c0bebb4e2b095b328a909fff4d35f3b49475a12bff597ddc242b2541bbd)，成功 |
| Base Sepolia 对应到账 | [L2 存入交易](https://sepolia.basescan.org/tx/0x76074004a00f8e459f3e3a3e36dbdab9f645bb1ded81a68928d66ffb9d080c5d)，成功 |
| 到账关联 | 从指定 Portal 的 TransactionDeposited 日志，经 viem `getL2TransactionHashes` 得到 L2 哈希，再核对 L2 回执与余额 |
| 到账与费用 | 平台 Base Sepolia ETH 从 0 到 0.001；L1 本次费用 2,192,677 wei，单独记录，不混入 USDC 支付费 |

[完整资金准备记录](attachments/base-live-2026-11-02/base-funding-deposit-public.json) 保留两端回执。跨网过程有等待，L1 成功时不能直接声称 Base 已可支付。这只是专用测试账户备资，不将跨链桥加入产品功能，也不证明用户原生 ETH 支付由平台代付已实现。Portal 调用参考 [viem depositTransaction](https://viem.sh/op-stack/actions/depositTransaction)。

## 钱包与真实签名

- 官方 Chrome Web Store 的 MetaMask 原始包已校验 CRX3 开发者签名及 ZIP，版本 `13.55.0.0`；[校验记录](attachments/base-live-2026-11-02/metamask-verification.json) 保留包大小、官方 ID 和散列。没有修改钱包代码。
- 采用独立 Playwright Chromium profile，Node 24.22.1、Playwright 1.64.1、Chromium 156.0.8078.4。专用用户私钥只在初始化时通过原版钱包导入界面导入；钱包驱动只读解锁密码，服务只读平台密钥。测试账户资料都在忽略目录，未进入仓库。
- 解包 manifest 无固定 key，测试运行 ID 为 `bdbmjkjloodjopbpjjmmoihedfomicgg`，不同于商店 ID；可信来源依据为 CRX3 验签，不能仅比较运行时 ID。
- Headless Chromium 没有自动弹出钱包确认窗口。本轮在标签页打开原扩展 `notification.html`，操作原有 Connect、网络确认和签名按钮。没有伪造 provider 响应；自动弹窗及手机回跳仍需各自验证。[连接](attachments/base-live-2026-11-02/base-metamask-connect.png) · [添加网络](attachments/base-live-2026-11-02/base-metamask-add-network.png) · [返回账户](attachments/base-live-2026-11-02/base-metamask-connected.json)。
- 首次连接后添加自定义测试网络，签名返回 [4100 权限错误](attachments/base-live-2026-11-02/base-metamask-initial-sign-error.json)。单纯再次 connect 或刷新页面未解决；通过原生 `wallet_revokePermissions` 撤销本站授权，再由真实 Connect 界面重授后签名成功。该观察限定本轮版本与会话，不把所有 4100 都归因于同一原因；探针提供显式 Renew wallet permissions 操作。
- 实际使用 `eth_signTypedData_v4` 签署 TransferWithAuthorization，字段包括 from、to、整数 value、有效期与随机 nonce；[付款运行记录](attachments/base-live-2026-11-02/base-metamask-wallet-pay.json) 和 [签名截图](attachments/base-live-2026-11-02/base-metamask-wallet-pay.png) 已保留。钱包显示的是原始 `Value: 10000`，没有自动格式化成 0.01 USDC；产品付款框仍须明确展示币种、数量、收款对象及费用，不能依赖钱包替代报价展示。

签名方法依据 [MetaMask 官方文档](https://docs.metamask.io/metamask-connect/evm/guides/sign-data/)。本轮验证的是软件 EOA，硬件钱包、智能账户、移动版和其他钱包品牌不能直接继承结论。

## 后端执行与重放约束

服务端先保存固定订单、chain ID、USDC 合约域、转出与收款账户、0.01 USDC、30 分钟授权有效期及随机 nonce，前端只交回 reference 和签名；后端使用保存的订单验签，不接受前端重新定义金额或地址。有效期按链上区块时间验证。签名不是付款，只有后端再次检查余额、授权未用、模拟成功和估费满足条件后才承担费用。

平台调用 USDC `transferWithAuthorization`；它签署的是独立 EIP-1559 交易，不是修改用户已签普通转账的 Gas payer。签名广播前保存平台 nonce、原始签名字节和确定的 tx hash，RPC 结果未知时只允许检查或显式重发同一字节，不自动产生新订单、新授权或新交易 nonce。

估费使用实际 `eth_estimateGas`、区块 base fee 与 priority fee、GasPriceOracle 的 `getL1FeeUpperBound` 和 `getOperatorFee`，并加入测试预算余量；本次预算为 1,384,053,000,730 wei，低于探针单次 0.0001 测试 ETH 上限。实际总费用按回执 L2、L1 费用及回执区块的 operator fee 汇总，再与平台余额差额核对，符合 [Base 费用组成](https://docs.base.org/specifications/transactions/network-fees)。估算与测试上限不是未来实际费率承诺，也不改变待审核的业务费用承担政策。

| 场景 | 实际观察 |
| --- | --- |
| 用户取消签名 | [真实取消记录](attachments/base-live-2026-11-02/base-metamask-wallet-reject.json) 返回 4001；订单保持 prepared，没有平台交易哈希；[截图](attachments/base-live-2026-11-02/base-metamask-reject.png) |
| 重复业务提交 | 返回同一 tx hash，`reusedSubmission=true`，无第二笔平台签名 |
| 同字节即时重播 | RPC 返回 `nonce too low: next nonce 2, tx nonce 1`；原交易成功，余额仅变化一次 |
| 再次使用同一 USDC 授权 | `eth_call` 明确返回 `FiatTokenV2: authorization is used or canceled`，没有实际广播第二笔交易或支付失败 Gas |
| 服务重启 | [重启后审计](attachments/base-live-2026-11-02/base-restart-audit.json) 返回原哈希和 reusedSubmission，余额保持单次执行结果，不重新产生支付 |

最终审计将重复授权调用固定在原回执区块：当时有效期尚未结束，因此能区分“nonce 已使用”和“只是过期”。当前实际余额另按最新区块读取并与单次回执结果比较。测试账户在此期间没有其他收支；这一精确余额断言用于隔离验证，不能替代生产交易事件归属。

EIP-3009 授权可由其他广播者提交到固定目标；正式业务还须保存业务单与授权 nonce 的关系，并从授权消费和实际 Transfer 找到执行事实，不能只等待我方某次广播的 tx hash。未知广播结果恢复、取消授权、报价失效、并发消费等生产流程仍需独立实现。

## 复现与检查

以下为独立探针，未接入普通 common/browser 检查。使用 Node 24.22.1，在仓库根执行；服务与钱包脚本使用不同终端。当前付款与备资已执行，**保留原状态，不能删除状态后直接重跑付款或存入**。

```sh
# 离线行为检查：签名/订单/回执/零 ETH 与完整费用
node --test scripts/payment_probe/base-live.test.mjs

# 已有备资只查询；--send 只供确认无旧存入记录的新一轮专用测试
node scripts/payment_probe/base-fund.mjs --check

node scripts/payment_probe/base-live-server.mjs
# 独立 profile 须已通过原钱包 UI 导入指定测试账户
node scripts/payment_probe/base-live-wallet.mjs --connect
# 仅在观察到上述权限问题时，由原钱包 UI 重授本站权限
node scripts/payment_probe/base-live-wallet.mjs --connect --renew-permissions
# 以下两个动作仅供尚未签名付款的新一轮，不用于重做当前已支付单
node scripts/payment_probe/base-live-wallet.mjs --reject
node scripts/payment_probe/base-live-wallet.mjs --pay
# 只查询与审计，要求 finalized 后才导出最终成功文件
node scripts/payment_probe/base-live-check.mjs
```

`base-live-state.json` 与 `base-funding-deposit-state.json` 各自保存签名字节，0600；对外导出剔除签名和原始待广播交易字段。独立 profile、解锁配置、平台及用户专用测试密钥仍只在 `.tools/payment-feasibility/`，不会提交。公开结果中的已执行交易 input 为链上公开 calldata，可用于复核授权签名，不能与私钥混淆。

10 项离线检查已通过，覆盖正确签名及金额、目标、nonce、有效期、chain ID、合约被篡改的拒绝，错误签名人/业务 reference、时间边界，以及真实事件和余额/完整费用核对。六项 [HTTP 冒烟](attachments/base-live-2026-11-02/base-http-smoke.json) 验证配置、密钥路径 404、外部 Origin、错误账户、已签名订单禁止覆盖及错误 reference。最终 finalized、授权防重放、重启恢复、余额及完整费用核对通过；JS 语法、文档链接、归档完整性与差异检查通过。测试服务、独立浏览器及 REPL 已关闭，状态和本机密钥保留。未修改业务代码或依赖版本，未重跑无关业务集成测试。

最终确认查询曾出现 [新旧高度不一致](attachments/base-live-2026-11-02/base-finality-observations.json)：一次直接 RPC 读到 finalized 高度 53,545,673，紧接着服务查询得到 53,543,524，仍低于付款区块 53,543,602，最终导出因此明确拒绝。之后只读重查得到覆盖付款区块的 finalized 高度并通过核验，没有重复支付。该现象的 RPC 节点/缓存根因未确认；后续正式通道需按已有确认与回滚规则处理查询差异，不能把一次旧响应当作重新付款依据。
