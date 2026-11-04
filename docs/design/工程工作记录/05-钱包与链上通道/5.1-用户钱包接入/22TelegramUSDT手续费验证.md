# Telegram / Walt 用 USDT 支付手续费：2026-11-04

[阶段讨论](01讨论记录.md) · [此前连接与双通道方案](21TON候选钱包能力与覆盖汇总.md) · [阶段目录](../README.md)

## 目标与当前结论

后续产品范围调整：2026-11-04 用户决定 Mini App 仅保留任务与中心化钱包，用户链上操作改由 App 承接。因此下述 Walt 实验保留为技术参考，其受阻状态不再构成 Mini App 发布前置；是否在 App 中继续选择该钱包，按实际 App 接入能力另行验证。见 [客户端分工决议](../../../../decisions/决议与待落实事项.md#2026-11-04-mini-app-与-app-客户端分工调整)。

验证 `telegram-wallet` 对应的 Walt 自托管入口能否在项目 TON Connect 钱包直付请求中，使用 USDT 支付手续费并保留订单 memo。钱包内手动转账、项目发起的请求、测试币机制和正式 USDT 分别记录；当前尚无本入口的 USDT 付费实转证据。本轮不接入正式充值或账本，也不使用主网资金。

当前状态：**测试网验证受阻，USDT 手续费实转未验证**。用户按历史官方设置链接核对后仍反馈“没有网络选项”；结合显式测试网请求被拒绝、默认连接返回主网，本轮没有可用的 Walt 测试网连接。结论限于当前客户端入口和本次会话，不推广为所有版本均不支持。临时连接服务已停止，无付款、签名或广播。

用户已确认“支持平台付的优先平台付，不支持的就前端付”，正式业务、界面、费用先付方和验收目标已同步；该决定不自动使未验证的直付通道通过。

## 已检查事实

1. [Walt 官方交易说明](https://help.walt.io/article/666-transactions)与[账户版本说明](https://help.walt.io/article/1032-welcome-to-defi-account)明确 W5 支持用 USDT 等受支持代币支付转账费用。它们描述钱包功能，不单独证明项目请求、测试网或任意测试币受支持。
2. 使用上次实际连接的 `walletStateInit` 在本机离线校验地址，并比对锁定的 `@ton/ton 16.3.0` 标准合约：地址吻合，code hash 为 `20834b7b72b112147e1b2fb457b84e74d1a30f04f737d4f62a668e9552d2b72f`，匹配 W5R1、不匹配 v4R2。这只是连接返回的合约类型识别，不是支付完成、账户所有权证明或当前链上状态核验。公开记录不含用户地址、公钥或 StateInit。
3. 用户此前回复设置中可以选择 Testnet，后续澄清当前 Walt 中没有找到该入口，因此不把先前回复作为当前客户端已经切网的证据。此前为测试启动独立临时连接服务，固定 manifest 名称 `DexWallet USDT gas testnet`，SDK 5.0.0、官方 Walt bridge，连接阶段只请求 ton_addr，无支付或签名。公网 manifest 返回 200，连接入口返回 302；服务设置四小时上限。
4. 使用 `setConnectionNetwork('-3')` 时，钱包实际返回 `BadRequestError`，正文为 `Network is not supported`。用户看到 connect fail，服务仍正常；不是简单把公网链接故障当成钱包不支持。该结果仅证明显式测试网连接请求被拒绝。随后准备省略连接请求中的 network 做对照，先检查实际返回网络；主网响应不得进入付款，所有测试付款仍须固定 `network: '-3'`。
5. 只读查询 TonAPI 主网和测试网的 `/v2/gasless/config` 均返回 200，列出的 13 个手续费代币地址集合相同，包含正式主网 USDT 地址，不包含此前实验的社区测试币。随后查询 testnet.toncenter：主网 USDT 同地址在测试网为 uninitialized、没有合约代码；社区测试币 master 为 active。这证明不能把测试网配置返回成功或主网地址出现，当成测试 USDT 可用。也没有证据证明 Walt 当前使用同一手续费服务，不能把 TonAPI 的结果冒充 Walt 拒绝结果。
6. 用户完成新入口连接后，2026-11-04 14:12:44（Asia/Shanghai）收到与当前会话匹配的真实响应。省略连接 network 后，返回 `account.chain: '-239'`，仍为主网；离线核对为同一主网 W5R1 地址与公钥。设备仍只声明 SendTransaction / SignData，没有 SignMessage。探针自动断开，随后实际检查 SDK `connected === false`；没有请求付款、签名或广播。本轮连接未获得测试网账户，不能继续测试网 USDT 付费，也不能据此断言该钱包在任何入口都不支持测试网。

## 本轮连接对照与下一步

| 请求方式 | 钱包实际响应 | 当前结论 |
| --- | --- | --- |
| 显式指定连接网络 `-3` | `BadRequestError: Network is not supported` | 当前显式测试网请求被拒绝 |
| 省略连接 network | 成功返回主网 `-239`；探针自动断开 | 新入口已收回调，但未建立测试网连接 |
| 历史官方设置直达链接 | 用户反馈“没有网络选项” | 当前客户端无法按该路径切换；这是用户反馈，不是本机 UI 截图证据 |

用户已完成上述设置核对，不再重复要求寻找同一开关或更换公网链接。本轮追加检索没有找到可核实的新官方测试入口；停止仅用于这项核对的 `dexwallet-telegram-usdt-testnet.service`，实际状态为 inactive/dead、Result=success。保留本机证据与全部既有链币验证，未移除 Walt 候选，也未将钱包直付记为通过。

后续恢复条件为：取得官方当前可用的测试入口及受支持的测试手续费代币，或另行获得主网小额验证授权。后一种情形须先准备具体收款方、金额、订单 memo 与钱包展示的费用，再由用户确认预算；测试环境授权不包含主网花费。本轮不要求用户购买或转入资产。使用其他钱包继续验证通用直付、memo 与到账核验可以独立开展，但结果不能替代 Walt 客户端兼容验收。

[TON Connect 官方钱包规范](https://github.com/ton-blockchain/ton-connect/blob/main/guides/wallet-guidelines.md#networks)要求付款请求网络与钱包网络一致；连接绑定的账户也不随钱包中浏览的账户自动变化。因此不使用主网连接尝试测试网付款，不修改地址前缀伪装网络，也不自动改为主网小额实转。

## 测试网设置入口核对

2026-11-04 重新核对官方资料，纠正此前只给出「Version & Network」的不足：

- 2026-11-02 更新的 [Walt 帮助](https://help.walt.io/article/1032-welcome-to-defi-account)说明该菜单可以选择 W5/v4R2，配图位于 DeFi 设置区域；它没有演示 Testnet 开关。合约版本与网络是两个不同的选择，不能从菜单名称推断测试网一定可选。
- 找到 [TON Dev News 于 2025-01-13 转发的钱包团队公告](https://t.me/tondev_news/174)，公告宣布当时的 TON Space 支持测试网，并提供 [设置直达链接](https://t.me/wallet/start?startapp=tonspace_settings)。已下载并查看随附演示：`Select network` 行可以从 Mainnet 选择 Testnet，与 `TON Space Version` 分开。
- 该链接的当前公开启动页仍标识为 Walt (ex. Wallet)。随后用户核对反馈“没有网络选项”；本机没有登录用户的 Telegram，因此没有直接检查其 UI。历史公告不能据此当作当前客户端仍有开关的证据，也不能证明 USDT 测试币手续费服务可用。

已完成的用户核对范围是该链接下的 `Select network` / `Network` /「选择网络」选项。用户反馈未找到，当前无需继续操作。公开资料原件及 SHA-256 保存在本机忽略目录 `official-settings-research/`，位于本记录下述证据根目录内；最新用户反馈及服务关闭结果保存在同目录根下的 `testnet-ui-blocker-private.json`。

## 后续实测判断标准

- 首先获得真实测试网连接，核对 W5、网络和可用代币；不能把主网地址前缀换成测试前缀就当作切网。
- 确认钱包实际接受的手续费代币，再准备少量对应测试资产；自建或社区测试币只证明其自身路径，不冒充 Tether 正式 USDT。
- 项目构造带唯一付款标识的 Jetton 转账，固定测试网络、测试收款方和小额本金；用户在手机钱包确认费用币种。若钱包要求原生币或不提供测试代币付费，应记录拒绝条件，不自动改成其他付费模式后宣称通过。
- 核对实际本金到账、memo、钱包扣款及手续费接收方，追踪完整接收结果和最终确认。只有钱包显示选项而未完成转账，标记为预览证据；不能据此认定实际网络费由 USDT 结算。

连接与原始账户数据保存在本机 `.tools/payment-feasibility/telegram-usdt-gas-2026-11-04/`，不入库。公开证据只保留必要能力、错误、合约类型和公共接口结果；记录不会发布会话存储或密钥。

[公开预检查证据](attachments/telegram-usdt-gas-2026-11-04/preflight.json)包含 W5 识别结果、公共手续费配置、测试网代币状态及显式测试网连接拒绝。[连接对照证据](attachments/telegram-usdt-gas-2026-11-04/connection-network-comparison.json)保存本次主网响应的必要能力、离线比对结果和 SDK 断开状态，不包含账户标识或会话链接。USDT 支付手续费仍未实转验证，本轮没有发起付款或扣费。
