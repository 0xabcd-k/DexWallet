# TronLink 精度诊断与平台 Energy 委托验证

[阶段目录](../README.md) · [前次 USDT 实转](12TronUSDT实转与Energy来源.md)

日期：2026-11-03。沿用原版 TronLink 4.11.2、独立 profile、Nile 测试账户与端口 18795。本次只推进提前技术验证，不改正式业务、费用承担规则或主网资产。

## 结论及证据边界

1. **平台 Energy 委托已完成一次真实消耗验证**：平台质押 1 测试 TRX 并把 Energy 委托给用户，原版钱包签名、后端广播的小合约调用消耗 16 Energy，全部来自用户收到的平台资源；合约部署者承担量与燃烧 TRX 支付的 Energy 均为零，交易已固化。
2. 这是无资产操作的小合约，**不是新增一笔 USDT 付款，也不证明 USDT 实际规模的资源池已足够**。本次 Bandwidth 由平台预先补入 0.278 测试 TRX 后燃烧支付，不能写成“用户全程零 TRX、两种资源均委托”。此前 TRX 的委托带宽证据、USDT 实转证据分别保留。
3. **USDT 金额展示仍未通过**：已经观察到代币搜索 API 的具体鉴权错误；尚未解决服务端为何拒绝本测试客户端，也没有修复钱包显示。不能据此认定所有 TronLink 安装方式或主网均有同样问题。

## USDT 精度问题：定位到可复现的失败点

链上合约仍是 Nile 官方测试 USDT `TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf`；`getcontract` 中有 `decimals`、`symbol`、`balanceOf`、`transfer`，上一轮独立 RPC 已读得 `USDT / 6`。本次原钱包预览依然把 10,000 最小单位显示为 `10,000 undefined`，并提示按 0 位精度计算。没有签署这些诊断用 USDT 请求，均通过原生 Reject 拒绝，没有再次广播 USDT。

按实际浏览器请求检查，发现两层问题：

| 检查 | 实际结果 | 可得结论 |
| --- | --- | --- |
| 将 `niletest.tronlink.org` 放入代理 bypass | 浏览器请求长时间未完成；独立 curl 直连在 8 秒连接超时 | 当前机器不能依赖该域名直连；没有及时收到失败事件不等于请求成功 |
| 云 API 走本机代理、Nile RPC 继续直连 | `/api/wallet/v2/search`、`assetList`、`/api/web/v1/tronweb` 返回 HTTP 200，JSON 为 `code: 4000, message: Authentication failed., data: null` | 仅记录 HTTP 4xx/5xx 会漏掉实际应用错误；代理可达不等于代币信息可用 |
| 调整网络路径后重新打开原生 USDT 签名框 | 精度警告仍存在，搜索接口仍鉴权失败 | 网络调整未修复展示问题 |
| 原生 Custom Token 输入正确合约 | `This is not a token contract. The custom token cannot be added.` | 这是钱包提示；不能据此推翻链上 ABI、余额和已成功转账的事实 |
| 原生 Settings 尝试关闭 Token List Sync、开启 Basic Functionality | Confirm 后状态仍分别为 `true`、`false`，未持久生效 | 没有获得“纯链上本地代币列表”对照结果，不声称该模式也必然有精度问题 |

TronLink 官方说明：关闭 Token List Sync 后可通过本地列表管理代币；未检测到精度时会默认按 0 处理。该说明提供了诊断方向，并不能证明本次 API 鉴权错误的深层原因。[官方手动添加代币说明](https://support.tronlink.org/hc/en-us/articles/52300630497945-Manually-Add-Tokens-to-TronLink-What-You-Need-to-Know)

未修改扩展源码、注入假 API 返回值或直接改钱包存储；未复制鉴权请求头。扩展版本、来源及校验值沿用 [TRX 验证](11TronLinkTRX资源代付实转验证.md)。`tron-live-wallet.mjs` 已改为让云 API 走代理，并记录上述 HTTP 200 内的鉴权错误；这是一项诊断改进，不是钱包问题已解决。

诊断证据：[直连请求记录](attachments/tron-energy-2026-11-03/wallet-network-direct.json)、[API 鉴权错误及预览](attachments/tron-energy-2026-11-03/wallet-api-auth-errors.json)、[设置尝试](attachments/tron-energy-2026-11-03/wallet-settings-attempt.json)、[代理路径下的精度警告](attachments/tron-energy-2026-11-03/wallet-precision-proxy.png)、[手动添加失败](attachments/tron-energy-2026-11-03/wallet-custom-token-invalid.png)。

## 平台 Energy 实际消耗

官方 Nile USDT 的 `consume_user_resource_percent=0`，上一笔的 29,650 Energy 全部由其部署者承担；即使给用户委托 Energy，再调用它仍可能不消耗平台的资源。因此单独部署 `NileEnergyProbe`，设置调用者承担比例 100%，隔离资源来源。TRON 的调用者可使用收到的委托 Energy，部署者分担则由合约设置决定；这两种来源应分别核对。[TRON 资源付费与分担规则](https://developers.tron.network/docs/paying-for-resources)

| 项目 | 值 |
| --- | --- |
| 网络 / chain ID | Nile / `0xcd8690dc` |
| 用户 | `TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio` |
| 平台 | `TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH` |
| 测试合约 | `TK9pCdpqppcJinuCky3TEpYgodj7ksvbXa` |
| 合约设置 | `consume_user_resource_percent=100`、`origin_energy_limit=1` |
| 运行代码 | `600160005260206000f3`，手工组装的 10 字节 TVM 代码，返回整数 1，无存储写入、外部调用或资产转移 |
| 创建代码 | `600a600c600039600a6000f3` 加运行代码；复制并返回后面的 10 字节，不依赖编译器 |
| 平台新增质押 / 委托 | 1 测试 TRX 的 Energy 份额，用户获得 73 Energy 上限 |
| 调用 | `probe()`，零附带 TRX，`fee_limit=1,000,000 sun` 为上限而非预估费用 |
| 预演 / 钱包预估 / 实际消耗 | 均为 16 Energy |

部署后从链上读取运行代码并核对其与本地固定字节一致。后端校验调用的网络、唯一合约、方法、付款账户、零附带金额、费用上限、有效期、原始字节、txID 及真实用户签名。原钱包完成一次 Reject、一次 Sign，签名后独立查询确认钱包没有广播，之后才由后端提交。

Energy 来源证据包括：用户无自有质押；用户 `account_resource.acquired_delegated_frozenV2_balance_for_energy=1,000,000`；平台到用户的委托记录同额；用户 EnergyUsed 从 0 增为 16；交易回执 `energy_usage=energy_usage_total=16`，`origin_energy_usage=0`、`energy_fee=0`。不能仅凭回执的调用者消耗就认定资源由平台提供。

| 步骤 | 交易哈希 | 实际燃烧费用 |
| --- | --- | --- |
| 部署测试合约 | `8ae3e97e1eaa731ef086fbb7c4d5cd7ed1f9c1c3021dd3f19d64b38b50012c3c` | 0.2021 TRX（2,021 Energy）；另用 346 免费带宽 |
| 质押 1 TRX 获取 Energy | `92eeab81ef484e25b529aa7a60f73b3a904114b8cc010a16cb17b23a14cc0322` | 0.252 TRX 带宽费；质押本金另计 |
| 委托 Energy 给用户 | `3b20bf5e6bf0b5bb54fd90bafdd307cec27b8993619d1c0058de2b0cabb0c430` | 0.277 TRX 带宽费 |
| 平台补入 0.278 TRX 带宽预算 | `3753f3069de83a7f2fda348877ddf978a0050e5825b9e7d04d1b7d8078002da1` | 0.267 TRX 转账带宽费；0.278 为转入用户的预算 |
| 原钱包签名后的测试调用 | `1f93c494a52ed122fcfd262129335bfd08d39ab9e5ebcda46d03729d48e27454` | 0.278 TRX 带宽费；Energy 使用平台委托额度，无 Energy 燃烧费 |

调用在区块 **72,387,562** 成功，solidity 节点已固化确认，返回 32 字节整数 1。用户余额从平台补资后的 0.278 TRX 归零，平台调用前后余额不变。准备及调用总燃烧费用 **1.2761 测试 TRX**；另有 **1 测试 TRX 质押**。平台余额 **14.8559 TRX**，连同此前 980 TRX 带宽质押，共保留 **981 测试 TRX 质押**，不当作手续费。USDT 余额仍为用户 999.99、平台 0.01。

采用带宽补资是本轮隔离实验的明确限制：原用户剩余免费带宽与委托带宽分别不足以独立支付 278 带宽，不能合并两个额度支付。补资量由实际单签 protobuf 长度加结果预留、再乘链上 `getTransactionFee=1,000 sun` 得到，并核对真实燃烧额。**不将此临时做法写入产品默认支付方案；USDT 全资源委托、资源不足与并发竞争仍未验收。**

链上完整证据：[固化回执、资源前后与准备交易](attachments/tron-energy-2026-11-03/tron-energy-public-result.json)、[USDT 余额复查](attachments/tron-energy-2026-11-03/tron-energy-token-balances.json)。钱包证据：[签名返回与拒绝](attachments/tron-energy-2026-11-03/energy-wallet-result.json)、[原生签名页面](attachments/tron-energy-2026-11-03/energy-wallet-sign.png)、[原生 Metadata](attachments/tron-energy-2026-11-03/energy-wallet-metadata.png)。

## 复现与后续开发

脚本位于 `scripts/payment_probe/`，使用既有 Node 24.22.1、TronWeb 6.5.1、Playwright 1.64.1，无新增依赖。独立 `tron-energy-state.json` 不覆盖原 TRX/USDT 状态。平台准备脚本只读平台密钥；用户签名只能从原版 TronLink 返回，私钥、解锁密码和未消费签名不入库。

```sh
export PATH=/home/kelizhang/.nvm/versions/node/v24.22.1/bin:$PATH
node --test scripts/payment_probe/tron-live.test.mjs scripts/payment_probe/tron-usdt.test.mjs scripts/payment_probe/tron-energy.test.mjs
# 当前已有完整消费记录，复查只运行：
node scripts/payment_probe/tron-energy-live.mjs --audit
```

原执行顺序为 `--setup` → `--prepare` → `--fund-bandwidth` → `tron-live-wallet.mjs --sign energy` → `tron-energy-live.mjs --submit` → `--audit`。不要删除现有订单/签名重跑；新实验应保留本次消费证据并准备独立状态。脚本先保存签名及 txID，广播未知结果按同一 txID 查询；已确认的步骤不重复转账。真实网络调用不纳入普通检查。

本次离线检查覆盖原 TRX/USDT 及 Energy 探针共 27 个用例，包括签名与期限、篡改调用、错误资源来源、部署者代付冒充平台委托、Energy 燃烧和实际带宽费用核对。真实检查为原钱包拒绝/签名、后端提交、固化复核和 USDT 余额不变。

后续正式接入仍需：解决原钱包金额展示；按实际 USDT 交易和动态链上参数准备足量资源；验证资源不足、资源被其他交易消耗和并发条件；接通业务订单、账本入账及恢复。16 Energy 的小调用不用于制定 USDT 费率或资源池容量。
