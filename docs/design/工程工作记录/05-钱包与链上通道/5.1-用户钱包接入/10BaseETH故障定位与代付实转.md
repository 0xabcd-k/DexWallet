# Base ETH：4100 根因与平台代付实转

[阶段讨论](01讨论记录.md) · [首次启用与原始失败](09BaseETH授权与代付验证.md) · [Base USDC 实转](08BaseUSDC授权与代付实转验证.md)

2026-11-03 继续处理用户要求的「验证 ETH 代付，排查失败原因」。**已定位当前 MetaMask 13.55 环境中的权限范围不匹配；官方 13.53 独立对照环境成功完成真实钱包授权，以及项目平台广播并承担全部费用的 Base Sepolia ETH 实转，付款已 finalized。** 本次只验证测试网络和指定钱包环境，不能据此开放所有 MetaMask 版本或宣称正式充值入账完成。

## 失败原因

13.55 的 `wallet_requestExecutionPermissions` 由内置 `npm:@metamask/gator-permissions-snap` 发起委托签名。通过 Chrome 调试器只读观察原扩展，在 `eth_signTypedData_v4` 的账户校验位置捕获到：

| 校验数据 | 实际值 |
| --- | --- |
| 请求来源 | `npm:@metamask/gator-permissions-snap`，不是本站 HTTP origin |
| 签名账户 | `0xecd14f7586f5053cecae148ae26309c2277d86a8` |
| 本次网络范围 | `eip155:84532`，即 Base Sepolia |
| 该范围允许的账户 | `[]` |
| Gator 的 `wallet:eip155` 权限 | 已包含上述用户账户 |
| Gator 的 `eip155:84532` 权限 | `accounts: []` |

**钱包级权限中存在用户账户，但签名校验只查询当前网络范围，所以取得空列表并返回 4100。** 本站重新连接只能调整本站权限，未解决 Gator 自己的网络级权限；新增一个未充值的测试账户再切回原账户，也仍返回 4100。这不是缺少测试 ETH，也不是付款合约执行失败：错误发生在生成付款授权之前。

[运行时证据](attachments/base-eth-2026-11-03/base-eth-scope-diagnostic.json) 保留请求、错误、实际 origin、scope 和授权范围。调试期间没有修改权限状态、替换签名返回、关闭检查或修改扩展代码；取证后移除断点并关闭调试连接。一次停在断点过久引起的授权等待超时不计为新的协议失败。

官方 [PR #47200](https://github.com/MetaMask/metamask-extension/pull/47200) 于 2026-10-22 合并，改变了按网络范围查询允许账户的路径。实际发行包中，13.55 的 `getPermittedAccounts(origin, scope)` 使用 `getEthAccountsForScope`；13.53 的同名方法使用不带 scope 的 `getEthAccounts`。前者没有从网络范围回退到 `wallet:eip155`。这段代码差异与本次运行时结果一致；该 PR 的存在不等于上游已确认本次特定 Gator 问题。

## 官方版本对照

从 [官方 v13.53.0 Release](https://github.com/MetaMask/metamask-extension/releases/tag/v13.53.0) 下载 Chrome ZIP，校验 GitHub asset digest、ZIP 完整性后原样加载，建立独立 profile；原 13.55 profile 保留。安装包大小 `25,123,247` 字节，SHA-256 为 `57a341a6fbb136cacbf812ddeb97a7757d39e4acb08197e49712fad44ed77ad0`，[校验记录](attachments/base-eth-2026-11-03/metamask-13.53-verification.json) 已归档。

两份发行包内置 Gator 均为 **2.5.0**，预装资源 `569f64cb9a2e6bd7bbde.json` 的 SHA-256 同为 `1d6e9ad5bb604866258024fdb31f192dc1e46c492f450b75835c0401122fad59`。因此本次不是换了另一套权限 Snap。对照使用同一测试用户、平台地址、Base RPC 和额度规则；用户账户已在前一轮链上升级，本次没有再次启用账户。

13.53 原界面依次显示 Grant 和第二次委托签名 Confirm，均核对账户、Base Sepolia、金额、执行者、收款人及到期时间后点击。随后钱包返回一条有效权限，`context` 长度 1,824 字节、`dependencies` 为空。后端恢复 EIP-712 签名，确认为用户地址签署；没有用本地用户私钥代替这一签名。

证据：[额度确认](attachments/base-eth-2026-11-03/base-eth-13.53-permission-preview.png)、[签名确认](attachments/base-eth-2026-11-03/base-eth-13.53-signature-preview.png)、[无签名的授权摘要](attachments/base-eth-2026-11-03/base-eth-13.53-grant-summary.json)。未消费的签名及完整权限只写本机 0600 文件。第二次确认界面是有效授权路径中的必要一步，原探针已补充处理。

13.53 是用于定位问题的隔离版本对照，**不是要求生产用户降级钱包的解决方案**。13.55 在本次环境中的问题仍存在；尚未证明所有 13.55 profile 都会失败，也未完成其他版本或手机端验证。

## 实际支付与费用

| 项目 | 结果 |
| --- | --- |
| 网络与资产 | Base Sepolia，chain ID `84532`，原生 ETH |
| 用户 | `0xecd14F7586f5053CECae148Ae26309C2277D86A8`，已启用 EIP-7702 智能账户 |
| 平台执行者及收款地址 | `0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA` |
| 付款额 | `1,000,000,000,000 wei = 0.000001 ETH` |
| 授权期限 | UTC `2026-11-03 03:38:43` 至 `04:38:43` |
| 外层交易 | 平台签署 type 2，发送到 DelegationManager，外层 `value=0` |
| 内层执行 | 用户智能账户向平台转出上述本金，调用数据为空 |
| 用户 ETH | 付款前恰有 `0.000001`，付款后 `0`；用户 nonce 仍为 `1` |
| 平台 ETH | `0.000998256461997927 → 0.000997320465988660`；差额包含收款和 gas |
| L2 / L1 / Operator 费用 | `1,935,996,000,000 / 9,267 / 0 wei` |
| 付款总费用 | **`1,935,996,009,267 wei = 0.000001935996009267 ETH`，全部由平台承担** |
| 付款区块 | `53792630`，UTC `2026-11-03 04:33:14` |

[付款交易](https://sepolia.basescan.org/tx/0x1c3509c224c12725106e47b57e5f5627934edf229ae51fb42861173fb36c2852) status=1。本次实际费用高于小额测试本金，而用户只持有本金仍成功付款，证明用户没有另付网络费。

测试准备时，平台另向用户补入恰好上述本金：[备资交易](https://sepolia.basescan.org/tx/0xdc7f376ee9736fa371a93e90c8c4b3c33c12cc3c4720b1781ad2bd4c135a76a8)，区块 `53792342`。备资费用为 `126,330,001,877 wei`，不混入付款费用，也不计为一笔用户充值成功。前一轮由 MetaMask 中继支付的账户启用费用仍单列在 09 记录中。

[完整实转证据](attachments/base-eth-2026-11-03/base-eth-live-public-result.json) 包含两笔交易、回执、精确余额、估算、事件与模拟错误。后端核对实际链、交易发送人/目标/value/calldata、`RedeemedDelegation` 事件中的完整委托，以及交易前后区块的账户余额和 nonce。平台余额变化满足「收入本金减去 L2、L1、Operator 总费用」；没有用 `gasUsed × effectiveGasPrice` 冒充全部 Base 费用。

付款已达到 **finalized**，只读查询的最终确认高度已覆盖付款区块；具体高度见最终 JSON 的 `finalizedBlock`。[初次确认记录](attachments/base-eth-2026-11-03/base-eth-live-initial-check.json) 当时仅为 confirmed，单独保留，不用后来的最终结果覆盖等待过程。

## 后端校验及反例

平台在使用自己的 gas 前校验实际返回的权限，不能只信任前端摘要。本次探针限定固定测试账户和金额，核对链、DelegationManager、账户、期限、签名以及全部六个 caveat；拒绝额外依赖、父级委托、缺失或不同的约束。

1. `NativeTokenPeriodTransferEnforcer`：额度为本金，周期长度为 uint256 最大值，开始时间固定；在一小时授权窗口内不恢复额度。
2. `ExactCalldataEnforcer`：只允许空调用数据。
3. `TimestampEnforcer`：到期时间与请求一致。
4. `RedeemerEnforcer`：只允许平台执行。
5. `AllowedTargetsEnforcer`：只允许平台收款地址。
6. `NonceEnforcer`：本次撤销 nonce 为零；它不是外层交易 nonce，也不能单独证明只执行一次。

部署地址和 terms 编码依据官方 `@metamask/delegation-deployments@2.0.0`、`delegation-core@3.0.0` 与 `delegation-abis@2.0.0` 核对。SDK 压缩包仅供本机源码比对，没有加入业务或探针依赖。执行路线参考 [MetaMask 官方 EOA 代为执行指南](https://docs.metamask.io/smart-accounts-kit/guides/advanced-permissions/execute-on-metamask-users-behalf/)。

以下均通过真实 RPC 的 `eth_call`，未广播失败交易、未花费反例 gas：

| 场景 | 结果 |
| --- | --- |
| 授权内的原始付款 | 模拟成功 |
| 多付 1 wei | `NativeTokenPeriodTransferEnforcer:transfer-amount-exceeded` |
| 改成另一收款地址 | `AllowedTargetsEnforcer:target-address-not-allowed` |
| 换执行账户 | DelegationManager `InvalidDelegate` |
| 添加非空 calldata | `ExactCalldataEnforcer:invalid-calldata` |
| 成功付款后重复消费同一额度 | 在原付款区块模拟，返回 `transfer-amount-exceeded` |

最后一项在付款区块执行，当时授权尚未到期，因此不能把过期、余额不足或 RPC 网络错误误报为防重复成功。本次付款一次用完总额度；**总额度授权本身允许分次消费，不能取代正式业务订单幂等**。正式产品还需将报价、订单和授权关联，按业务需要选择授权限制。

广播前估算包含 L2 gas 上限、L1 数据费上界与 Operator 费，付款预算 `4,308,403,076,426 wei`，小于固定测试上限 `0.0001 ETH`。估算不是锁定未来费率；回执后仍核对实际费用。原始交易、哈希和 nonce 在广播前原子保存；重复执行命令复用已有交易，未知结果也不会自动换 nonce。文件锁拒绝并发探针操作，锁冲突未发出另一笔交易。

## 复现入口与交付范围

使用 Node 24.22.1 和原锁定依赖，不新增依赖。钱包 profile 必须在原界面初始化、导入专用测试账户并连接 Base Sepolia；密钥、密码和本机 profile 不入库。对照版路径分别为 `.tools/payment-feasibility/metamask-13.53-extension`、`metamask-13.53-profile`，解锁配置为 `metamask-13.53-secrets.json`。13.55 使用原路径。

```sh
# 默认 13.55；显式版本参数才使用隔离对照环境。
node scripts/payment_probe/base-eth-wallet.mjs --inspect 13.53.0.0
node scripts/payment_probe/base-eth-wallet.mjs --grant-probe 13.53.0.0

# 本次实转探针的输入是固定文件名；重复 prepare 会拒绝覆盖已有状态。
node scripts/payment_probe/base-eth-live.mjs --prepare
node scripts/payment_probe/base-eth-live.mjs --fund
node scripts/payment_probe/base-eth-live.mjs --submit
node scripts/payment_probe/base-eth-live.mjs --audit

node --test scripts/payment_probe/base-eth-core.test.mjs
python3 scripts/check_docs.py
git diff --check
```

本次实际通过原界面取得授权后，完整返回存入本机 `base-eth-13.53-private-grant.json`，请求存入 `base-eth-13.53-request.json`；后端只从 `base-sponsor-only.json` 读取平台密钥。钱包通用探针会生成带时间戳的新文件名，不自动覆盖上述实转输入；若以后重新验证，应先检查现有授权和交易状态，不能删除已消费记录盲目重跑。现有总额度已用完，`--submit` 不会再建新付款，`--audit` 是只读链上检查。

新增 13 项离线行为测试验证约束、签名与期限拒绝路径，全部通过；原 Base USDC 的 10 项测试也通过。本地测试私钥是公开、未充值的固定夹具，与真实钱包证据分开。JS 语法、文档和差异检查通过。付款后再次执行 `--submit` 返回原交易哈希和成功状态，未新增付款；[重启钱包后的只读检查](attachments/base-eth-2026-11-03/base-eth-13.53-inspect.json) 正常查询到一条已授予权限。探针没有实现订单、账本入账、正式 UI、撤销页面或通用钱包兼容层。

后续开发可沿用「钱包签署受限委托 → 后端验证和模拟 → 平台广播 → 按交易与事件核对」路径；接入时必须检测实际钱包能力和授权结果。当前 13.55 环境应保持不可用提示，待权限修复或其他已实测钱包通过后再开放；不通过忽略钱包权限检查来修复。
