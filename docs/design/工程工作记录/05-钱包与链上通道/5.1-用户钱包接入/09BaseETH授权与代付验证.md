# Base Sepolia ETH：MetaMask 高级权限与首次启用

[阶段讨论](01讨论记录.md) · [前期协议基线](05支付可行性验证.md) · [已完成的 Base USDC 实转](08BaseUSDC授权与代付实转验证.md)

2026-11-03（北京时间，原始证据使用 UTC）继续验证原生 ETH。**本轮只通过能力查询、真实拒绝及零 ETH 账户升级；高级权限最终返回 4100，尚未完成项目后端代付 ETH。** 没有向用户补测试 ETH，没有项目平台广播的付款，没有业务订单或入账。Base ETH 仍在首版需求中，不因失败移除，也不加入已验证通过的组合。

## 真实钱包结果

沿用官方 MetaMask 扩展 `13.55.0.0`、独立 Chromium profile 和导入账户 `Imported Account 1`。原包来源及 CRX3 校验见 [USDC 记录](08BaseUSDC授权与代付实转验证.md)。没有修改扩展、伪造 provider 返回或用本地用户私钥代替钱包签名。仍通过原扩展的 `notification.html` 操作确认界面，自动弹窗和手机端不在本次结论内。

`wallet_getSupportedExecutionPermissions` 实际返回 `native-token-allowance`，支持 Base Sepolia `0x14a34` 及 `expiry`、`redeemer`、`payee` 三种规则。能力声明只代表可以尝试请求，不代表后续一定能授权或执行。

本次请求固定总额度 **0.000001 测试 ETH**（`1,000,000,000,000 wei`），不按周期恢复额度；禁止用户调整，开始时间为请求时刻，到期时间为一小时后。执行账户与允许收款人均固定为专用平台地址。`to` 是获得权限的执行账户；`payee` 才是限制资金目标的规则，两者不能混为一个字段。

| 验证项 | 实际观察 |
| --- | --- |
| 取消授权 | 原钱包返回 `4001: Permission request denied at confirmation screen`，没有授予权限 |
| 显式传 `from` | 能显示额度及目标；点击 Grant 后返回 4100 |
| 撤销本站连接后重新连接 | 当前链与账户正确，但同一请求仍返回 4100 |
| 省略 `from`，由钱包界面选择同一账户 | 出现智能账户升级提示；确认后升级实际上链，随后权限请求仍返回 4100 |
| 已升级账户再次授权 | 仍返回 `The requested account and/or method has not been authorized by the user.` |
| 同一站点的普通 EIP-712 签名对照 | 不含付款含义的 `AccountScopeProbe` 正常进入原签名确认界面；主动取消返回 4001 |
| 查询已授予的高级权限 | `wallet_getGrantedExecutionPermissions` 返回空数组；没有可交给后端执行的 permission context |

[完整发现记录](attachments/base-eth-2026-11-03/base-eth-discovery.json) 保留请求、能力返回、逐次错误、普通签名对照及升级交易。[额度授权界面](attachments/base-eth-2026-11-03/base-eth-permission-preview.png)、[升级确认界面](attachments/base-eth-2026-11-03/base-eth-account-upgrade.png)、[升级后再次请求](attachments/base-eth-2026-11-03/base-eth-upgraded-permission-preview.png)、[普通签名对照](attachments/base-eth-2026-11-03/base-eth-normal-signature-scope.png) 可直接复核。

因此故障不等于缺少测试币，也不能笼统归因于“网站未连接”。本轮没有确定是该版本、导入账户、内置权限 Snap、网络配置还是会话环境的兼容问题。错误栈指向钱包账户权限校验，但未取得足以定位根因的运行时数据；不能把推测写成修复结论。未绕过该校验，也未尝试无约束委托。

## 首次升级与费用

钱包的 Account update 界面明确显示费用由 MetaMask 支付。链上核对证明用户签署了限定 Base Sepolia 的 EIP-7702 授权，由第三方中继账户提交 type `0x4` 交易，用户账户获得智能账户代码。这是账户启用，**不是本项目平台代付的 ETH 付款**。

| 项目 | 结果 |
| --- | --- |
| 用户地址 | `0xecd14F7586f5053CECae148Ae26309C2277D86A8` |
| 项目平台地址 | `0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA` |
| 本次启用的交易付款人 | `0xb01caea8c6c47bbf4f4b4c5080ca642043359c2e`；不是用户或项目平台 |
| 交易目标 | MetaMask DelegationManager `0xdb9B1e94B5b69Df7e401DDbedE43491141047dB3`；原生 value 为零 |
| 账户实现 | `0x63c0c19a282a1B52b07dD5a65b58948A07DAE32B`，与 Smart Accounts Kit 2.0.0 的部署表一致 |
| 用户 ETH / nonce | `0 → 0`；账户 nonce `0 → 1` |
| 用户代码 | `0x → 0xef010063c0c19a282a1b52b07dd5a65b58948a07dae32b` |
| 项目平台 ETH / nonce | 均不变；ETH 为 `0.000999382791999804`，nonce 为 `2` |
| L2 执行费 / L1 数据费 / Operator fee | `1,209,672,201,612 / 501 / 0 wei` |
| 本次启用总费用 | `1,209,672,202,113 wei = 0.000001209672202113 ETH`，由上述中继账户支付 |
| 区块 / UTC 时间 | `53572582` / `2026-11-02T16:19:45Z` |

[升级交易](https://sepolia.basescan.org/tx/0x782055191fab42713dd2f4e67ceb83640ed1d2f84b197bf6a7af650004862a04) status=1；[钱包活动记录](attachments/base-eth-2026-11-03/base-eth-upgrade-confirmed.png) 显示 Confirmed 和 Paid by MetaMask。[只读链上审计](attachments/base-eth-2026-11-03/base-eth-upgrade-public-result.json) 保留确认高度、完整交易与回执、EIP-7702 签名恢复结果、前后区块账户代码与余额、完整费用。审计分别输出 `activationStatus` 与始终为 `not_verified` 的 `paymentStatus`，不能用启用交易冒充付款。

最终只读审计的 finalized 高度为 `53578168`，已覆盖启用区块；因此**账户启用已 finalized，应用付款仍未验证通过**。[此前查询](attachments/base-eth-2026-11-03/base-eth-upgrade-initial-check.json) 还只有 confirmed，保留两份记录，不覆盖等待过程，也没有再次发起启用交易。

费用由回执 L2、L1 项和回执区块的 GasPriceOracle `getOperatorFee` 汇总。只对用户及专用平台做前后余额/nonce 断言；第三方中继可能同时服务其他交易，不用它的整块余额差额替代本笔费用。首次启用的补贴是本次钱包实际行为，不承诺所有网络、钱包版本或以后每次都免费，不改变项目中待审核的业务费用承担规则。

此前 USDC 实转时该地址还是普通 EOA；现在本机同一测试地址已升级。历史 USDC 证据保持有效，但之后再测试时应重新记录账户类型，不能继续描述为未升级 EOA。

## 后续开发依据与未完成项

1. **把启用与付款分开记录。** 高级权限调用最后返回错误，仍可能已经产生成功的启用交易。本次即如此。重试前先读取账户代码、nonce、权限与链上状态，不能把整个调用失败当成“链上什么都没发生”。
2. **保留用户额度和目标限制。** 本次钱包能展示总额度、执行者、收款人及到期设置。拿到成功响应后，后端仍须验证实际返回的账户、chain ID、DelegationManager、签名及 caveats，而非相信前端提交的摘要或请求原值。
3. **应用付款必须由自己的平台执行。** 官方 ERC-7710 路线允许获授权的 EOA 经 DelegationManager 执行；这一步需要可用的 permission context。本轮没有获得，因此未实现或宣称平台付款、到账、防重复执行和费用预算已通过。
4. **兼容表保留“待解决”。** 下一步可针对全新普通助记词账户与钱包版本做对照，或继续验证其他 Base 智能钱包。未经真实授权和实转，不自动启用该支付选项；也不把本次错误扩大为所有 MetaMask 或全部 Base ETH 均不可行。

目前不需要用户再领币或提供密钥。现有测试资金足够，阻塞发生在钱包授权阶段。

## 复现与验证

两个新脚本均为隔离探针，使用 Node 24.22.1 与已锁定的 Playwright 1.64.1、viem 2.57.14，不新增业务依赖。权限协议按官方文档和 Smart Accounts Kit 2.0.0 的映射实现为 JSON-RPC；SDK 仅用于本轮源码/部署表核对，未加入最终依赖。

```sh
# 默认只连接并读取账户、能力及已授予权限；专用钱包 profile 需已初始化。
node scripts/payment_probe/base-eth-wallet.mjs --inspect

# 经原钱包界面取消请求，要求 4001，并核对用户/平台 ETH 不变。
node scripts/payment_probe/base-eth-wallet.mjs --reject

# 仅专用测试账户：尝试授权，必要时会通过原钱包界面启用智能账户。
# 即使拿到授权也只保存到本机，不提交付款；钱包拒绝/错误退出码为 2。
node scripts/payment_probe/base-eth-wallet.mjs --grant-probe

# 固定升级交易的只读核验；没有发送交易或签署用户授权的代码。
node scripts/payment_probe/base-eth-check.mjs
```

钱包探针绑定 `127.0.0.1:18794`，不读取用户私钥或平台私钥，只读取独立 profile 的解锁密码。每次运行使用新文件名保存证据；未来若成功返回未消费权限，完整 context 单独写入 0600 的 `*-private-grant.json`，不得归档到仓库，也不得删除已有权限记录后盲目重试。存在已授予权限时探针拒绝新授权请求。

复现脚本曾遇到钱包先渲染 Grant 按钮、稍后才加载账户/额度的情况，现先等待实际额度出现再核验界面；一次只读 RPC 连接被重置发生在钱包请求前，重新运行即可，不因此建立自动付款重试。所有原始运行记录保留本机。正式业务、手机回跳、硬件钱包、其他账户类型及主网均未验收。

最终脚本实跑：[取消授权](attachments/base-eth-2026-11-03/base-eth-wallet-reject.json) 返回 4001，脚本退出 0；[重启钱包后再次授权](attachments/base-eth-2026-11-03/base-eth-wallet-grant-probe.json) 仍返回 4100，脚本按未通过退出 2。两者均完成用户和平台 ETH、平台 nonce 不变的断言。JS 语法、文档链接与历史归档检查通过；恢复原锁文件后 `npm ci --ignore-scripts --offline --no-audit --no-fund` 成功，已有 Base USDC 的 10 项离线行为检查通过。没有新增依赖或改变原 USDC 付款状态；本轮浏览器、探针服务及临时 REPL 均已关闭。

依据：[MetaMask 高级权限概念](https://docs.metamask.io/smart-accounts-kit/concepts/advanced-permissions/)、[原生币额度权限](https://docs.metamask.io/smart-accounts-kit/guides/advanced-permissions/use-permissions/native-token/)、[EOA 代为执行指南](https://docs.metamask.io/smart-accounts-kit/guides/advanced-permissions/execute-on-metamask-users-behalf/)、[支持网络](https://docs.metamask.io/smart-accounts-kit/get-started/supported-networks/)、[ERC-7715](https://eips.ethereum.org/EIPS/eip-7715)。这些资料证明候选机制及接口含义，不替代上述真实失败结果。
