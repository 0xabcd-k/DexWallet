# TronLink / Nile TRX：平台委托带宽与后端广播

[阶段讨论](01讨论记录.md) · [前期协议基线](05支付可行性验证.md) · [Base ETH 实转](10BaseETH故障定位与代付实转.md)

2026-11-03 用户要求继续验证下一项，并协助领取 Nile 测试 TRX。**TronLink 4.11.2 已完成真实钱包仅签名、后端校验及广播、平台委托带宽、固化确认与余额核对。用户只支付 0.001 TRX 本金，付款消耗平台提供的 266 带宽，没有燃烧用户 TRX，也没有使用用户免费带宽。** 本结论限定本次浏览器扩展、Nile 和原生 TRX；TRC-20 USDT、手机钱包、正式订单及账本入账尚未通过本轮验证。

## 实转与资源证据

| 项目 | 实际结果 |
| --- | --- |
| 网络 | TRON Nile，JSON-RPC chain ID `0xcd8690dc`（3448148188） |
| 钱包 / SDK | TronLink 4.11.2 / TronWeb 6.5.1 |
| 用户地址 | `TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio`，普通单签账户，无自身质押 |
| 平台及收款地址 | `TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH` |
| 本金 | `1000 sun = 0.001 TRX` |
| 付款区块 | `72384389`，UTC `2026-11-03 07:19:12` |
| 确认 | Solidity 节点可查交易与回执，`contractRet=SUCCESS`，已固化 |
| 用户 TRX | `1000 → 0 sun` |
| 平台可用 TRX | `17,131,000 → 17,132,000 sun`，只增加收到的本金 |
| 付款消耗 | 回执 `net_usage=266`；`fee`、`net_fee`、Energy 消耗均为零（零值字段省略） |
| 用户免费带宽 | `freeNetUsed` 前后均为零，免费额度为 600 |
| 用户委托带宽 | 付款前 `NetLimit=315`、`NetUsed=0`；核对时 `NetUsed=265`，与回执差 1 来自额度随时间恢复 |
| 资源提供者 | 平台委托 500 TRX 质押份额对应的 Bandwidth，链上委托双方及数量均已核对 |

[付款交易](https://nile.tronscan.org/#/transaction/c08fc0d615c4dc8546ce592e2d3bdbe711c282c68b437c18549395ac1979ac3e) 的完整 txID 为 `c08fc0d615c4dc8546ce592e2d3bdbe711c282c68b437c18549395ac1979ac3e`。[实转 JSON](attachments/tron-trx-2026-11-03/tron-live-public-result.json) 保存准备交易、用户付款、签名校验、余额和资源快照；[最终只读核对](attachments/tron-trx-2026-11-03/tron-final-chain-check.json) 保存 Solidity 高度、交易、回执与本地订单引用。

链上收款地址已经激活。本轮付款不包含收款方首次激活费用；平台预先激活测试用户产生的费用另列如下。没有将免费带宽误判成平台代付：用户没有自身质押，获得的质押份额与平台委托记录一致，并且实际变化发生在 `NetUsed`，不是 `freeNetUsed`。

## 平台准备费用及委托额度问题

官网水龙头需要人机验证，本机自动化没有领到；用户完成后，平台收到 **1000 测试 TRX**，见 [到账读取](attachments/tron-trx-2026-11-03/tron-faucet-received.json)。没有购买或使用主网资产。

| 准备步骤 | 交易 ID | 实际燃烧 TRX | 其他资源 / 资金变化 |
| --- | --- | --- | --- |
| 激活用户并补入 0.001 TRX 本金 | `c57fbd3259ea2cb98f93cb4772c4a858f6678132f6e88dfe87bebf05b5e2e038` | 1.1 | 1 TRX 激活费及 0.1 TRX 带宽固定费用，由平台承担 |
| 初次质押 700 TRX 带宽 | `838e0a57bbc8570f734acb26996ab290f1cebe70428fcf6af749a005230054d3` | 0 | 消耗平台免费带宽 252；质押不是手续费 |
| 补质押 280 TRX | `21738c62770a33cce983cbf12ac22d6bd3de176a10ef5cdbafe033e874964af2` | 1.768 | 512 字节测试 memo：0.768 带宽费 + 1 memo 费，保留平台质押带宽供下一步使用 |
| 委托 500 TRX 对应带宽给用户 | `5ccf788801687ae47e381d91f7e2c9159d78407b74ca91262b04026f163222a2` | 0 | 消耗平台质押带宽 277；没有向用户转入 500 TRX |

准备步骤合计燃烧 **2.868 TRX**；用户付款另耗 266 委托带宽，未再燃烧 TRX。本金回到平台后，平台剩余 **17.132 可用 TRX + 980 质押 TRX**，合计与原 1000 减去准备费一致。500 TRX 委托份额仍属平台质押资产，其余 480 TRX 份额保留在平台；本轮未撤销委托、解质押或提取，后续测试可继续核对使用。Nile 当时 `getUnfreezeDelayDays=1`，不能据此推断主网等待期。

首次质押后虽然平台没有消耗质押带宽，但 `getcandelegatedmaxsize` 仅返回 `251565764 sun`，不足最初计划委托的 600 TRX。探针在签署/广播委托前停止；这不是用户付款失败。节点计算可委托量时还会为委托交易预留消耗，不能用“已质押金额”直接当“现在能委托的金额”。对应 [官方委托说明](https://developers.tron.network/docs/delegation) 与 [节点 Wallet 的额度计算](https://github.com/tronprotocol/java-tron/blob/develop/framework/src/main/java/org/tron/core/Wallet.java) 可供后续实现参考。

本次小资源池补质押时特意给**平台准备交易**添加 512 字节 memo，使整笔带宽需求超出现有质押和免费额度，从平台 TRX 支付带宽费及 memo 费，避免补质押交易先消耗后续需要委托的带宽。补足后改为委托 500 TRX 份额，用户可用 315 带宽，覆盖估算 266 加探针余量 32。该做法仅是此次测试备资调整，生产资源池应预留自身操作消耗；**不是要求每笔付款加 memo 或额外花费 1.768 TRX**。用户实际付款不带 memo。

带宽扣费顺序和每种额度须独立覆盖整笔交易的规则见 [官方资源扣费说明](https://developers.tron.network/docs/paying-for-resources)。测试时链参数快照另存 [JSON](attachments/tron-trx-2026-11-03/tron-chain-parameters.json)，包含带宽单价、免费额度、memo 费及激活费；不要把此次数字写成长期固定主网费率。

## 真实钱包连接、签名与拒绝

从 [TronLink 官网下载入口](https://www.tronlink.org/dlDetails/) 指向的 Chrome 扩展 `ibnejdfjmmkpcnlpebklmnkoeoihofec` 获取 CRX，校验开发者签名及 ZIP 后原样加载。版本 4.11.2、manifest V3，文件 11,633,098 字节，SHA-256 为 `e03d0d11f4520ecd5f98d5d67ed062ed609aeef85921d840baa904c4b0b0f191`，见 [安装包校验](attachments/tron-trx-2026-11-03/tronlink-verification.json)。独立 profile 与密钥文件只在本机忽略目录。

1. 通过原钱包界面导入专用测试用户，访问本机 `http://127.0.0.1:18795/`。
2. `window.tron.request({method:'eth_requestAccounts'})` 自动弹出原连接窗口，返回用户 Base58 地址；再请求 `wallet_switchEthereumChain`，参数 `[{chainId:'0xcd8690dc'}]`，原窗口确认 Mainnet → Nile。
3. 后端按 Nile 区块引用构造单一 `TransferContract`，固定双方、1000 sun 和十分钟有效期，保存意图及 txID。
4. 前端调用 `window.tron.tronWeb.trx.sign(unsigned)`，原签名页展示 Nile、完整地址、0.001 TRX 和预计 266 带宽。
5. 先点击 Reject，返回字符串 **`Confirmation declined by user`**；随后对同一内容点击 Sign，得到一个用户签名。两个步骤之后都检查该交易尚未上链，再交给后端广播。

证据：[连接页面](attachments/tron-trx-2026-11-03/tronlink-connect-preview.png)、[切网页面](attachments/tron-trx-2026-11-03/tronlink-nile-switch.png)、[连接结果](attachments/tron-trx-2026-11-03/tronlink-connection-result.json)、[拒绝页面](attachments/tron-trx-2026-11-03/tronlink-trx-reject-preview.png)、[签名页面](attachments/tron-trx-2026-11-03/tronlink-trx-sign-preview.png)、[无签名的钱包结果摘要](attachments/tron-trx-2026-11-03/tronlink-trx-wallet-result.json)。真实执行期间通过 Playwright 控制原扩展；没有修改扩展、伪造注入对象或在后端使用用户私钥签署付款。

采用的连接、切链及普通交易签名入口见 [TronLink 官方接口文档](https://docs.tronlink.org/plugin-wallet/active-requests/)。本轮有效经验：

- 签名拒绝返回普通字符串，不能只识别连接接口的 `4001`。
- `window.tron.request({method:'eth_chainId'})` 本轮不支持；读取实际注入 TronWeb 的 FullNode / SolidityNode host，并独立核对固定 Nile RPC chain ID。TRON 交易本身没有 EVM 风格的 chain ID 字段，本次也没有证明签名能以 chain ID 做跨网络隔离。
- 首页资产接口曾显示 `No Internet Connection`，而注入 TronWeb 的 Nile 余额查询正常，签名和实转也成功。首页第三方接口故障不能直接认定节点不可用。
- 初始化密码输入应使用 `pressSequentially`；本次 `.fill()` 后输入值被页面重置。不要在密码、私钥或助记词页面输出 accessibility 快照。调试初期旧的未充值 SDK 用户密钥曾进入界面诊断输出，已在任何充值/签名前弃用；实转使用新生成的上述 TDHZ 用户。旧 TNmT 用户只保留历史 SDK 基线，不再用于后续资金验证。
- TronLink 返回的签名末尾恢复字节包含大写 `1B`，节点返回小写 `1b`；回执核对按字节比较，仍拒绝任何签名字节变化。
- 节点错误响应采用 protobuf 默认值省略，重复交易返回 `code=DUP_TRANSACTION_ERROR`，可能没有 `result:false` 字段。成功广播仍要求明确 `result:true` 或已知重复码，不能把空响应当成功。

## 后端校验、追踪及边界

后端仅加载平台专用密钥处理激活、质押、委托。用户付款只接受钱包返回的签名，核对单一合约、owner 权限、发送人、收款人、金额、期限、区块引用、签名恢复地址，以及 JSON / protobuf / txID 一致性。篡改任意支付字段、追加合约或更换签名者都会在广播前拒绝。广播前核对用户本金、没有自身质押、平台委托双方及份额、可用带宽至少 298，临广播前再次检查资源。

签名交易及 txID 在广播前原子保存，文件锁拒绝并发运行；不确定结果沿原 txID 查询，禁止自动换交易。后端通过 `walletsolidity/gettransactionbyid` 和 `gettransactioninfobyid` 核对成功、签名、转账内容、资源回执及余额。重复提交原始已成功交易，节点返回 `DUP_TRANSACTION_ERROR`，双方余额不变；实际结果保存在实转 JSON 的 `replay`。这证明该笔链上交易没有重复执行，不代表正式业务订单幂等已经实现。

用户付款没有 memo。本地意图在签名前已关联 txID，链上确认后按该映射核对双方及本金，因此本次后端发起的付款不依赖 memo 寻单。自行向平台任意转账的充值、补扫与业务账本入账仍是其他待实现路径。

**TRON 这条路径是平台提供账户资源，再由后端广播；普通 TRX 转账没有独立 fee payer。** 资源委托没有为某笔订单原子锁定额度；同一用户从其他客户端交易也可能提前消耗带宽。平台复查不能消除广播至执行间的竞争，资源不足时可能改用用户免费额度，仍不足则燃烧用户 TRX。当前只证明资源充足时用户不另付费，不能直接承诺所有情况下用户绝不扣费。尚未做真实并发抢占、资源耗尽、失败扣费和委托回收验证；后续正式开放前要把这些限制与产品费用承诺一并确定。本轮没有修改待逐条审核的费用承担规则。

## 复现入口和本轮检查

使用 Node 24.22.1、锁定 TronWeb 6.5.1 / Playwright 1.64.1，不新增依赖。脚本位于 `scripts/payment_probe/`，不加入正式后端或默认 CI。

本机 `.tools/payment-feasibility/` 保存 `tronlink-extension`、`tronlink-profile`、`tronlink-secrets.json`（仅密码）、`tron-live-addresses.json`（公开地址）、`tron-sponsor-only.json`（平台私钥 JSON 字符串）、`tron-live-state.json` 和钱包返回的 `tron-live-wallet-signed.json`。真实用户新密钥单独保存在 `tron-live-wallet-key.json`，仅用于原钱包初始化，后端和钱包探针均不读取它；所有私密材料不入库。

```sh
# 先在原钱包界面初始化专用 profile、连接上述本机 origin 并切换 Nile。
node scripts/payment_probe/tron-live-wallet.mjs --inspect

# 新验证环境的顺序；已有状态不得删除后盲目重跑。
node scripts/payment_probe/tron-live.mjs --setup
node scripts/payment_probe/tron-live.mjs --prepare
node scripts/payment_probe/tron-live-wallet.mjs --sign
node scripts/payment_probe/tron-live.mjs --submit
node scripts/payment_probe/tron-live.mjs --replay
node scripts/payment_probe/tron-live.mjs --audit

node --test scripts/payment_probe/tron-live.test.mjs
python3 scripts/check_docs.py
git diff --check
```

`--prepare` 拒绝覆盖已有订单，`--sign` 拒绝为已经提交的付款再次请求签名；`--setup` 复用已确认准备步骤，`--submit` 复用已保存交易，`--audit` 只读链上并复用首次付款后的资源快照（带宽会持续恢复，不能拿数小时后的额度代替原消费证据）。若未消费交易已过期，先核对链上及本地状态，再另行准备新测试，不能隐式重签。

本轮连接、拒绝、签名通过交互式 Playwright 执行，相关调用固化为 `tron-live-wallet.mjs`；封装后实际运行 `--inspect`，重启钱包、解锁及 Nile 地址验证通过，见 [重启读取](attachments/tron-trx-2026-11-03/tronlink-restart-inspect.json)。没有为了测试封装再请求一次已消费付款签名。后端 `--submit`、`--replay`、`--audit` 均实际执行成功。

10 项离线行为测试通过，覆盖金额/地址/raw/txID/权限/额外合约篡改、签名者和期限、资源不足、实际扣费及免费额度误用，包含真实遇到的签名大小写差异；测试签名用未充值公开夹具，与钱包实转证据分开。JS 语法、文档与差异检查通过。钱包扩展、浏览器 profile 和密钥不提交；本轮保留的签名交易均已上链消费。下一资产应独立验证 TRON USDT 的合约、Energy 估算及委托，不能沿用本次“仅 Bandwidth”的费用结论。
