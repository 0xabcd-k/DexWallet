# Telegram / Walt 用 USDT 支付手续费：2026-11-04

[阶段讨论](01讨论记录.md) · [此前连接与双通道方案](21TON候选钱包能力与覆盖汇总.md) · [阶段目录](../README.md)

## 目标与当前结论

验证 `telegram-wallet` 对应的 Walt 自托管入口能否在项目 TON Connect 钱包直付请求中，使用 USDT 支付手续费并保留订单 memo。钱包内手动转账、项目发起的请求、测试币机制和正式 USDT 分别记录；当前尚无本入口的 USDT 付费实转证据。本轮不接入正式充值或账本，也不使用主网资金。

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

用户反馈当前 Walt 中没有找到测试网选项。已查到下述官方历史设置深链，下一步先检查实际客户端是否仍显示网络切换；确认选择 Testnet 后才建立新连接会话。若没有网络选项，记录当前入口受限，不能继续假设用户已经切网，也不靠重复更换公网链接解决。

[TON Connect 官方钱包规范](https://github.com/ton-blockchain/ton-connect/blob/main/guides/wallet-guidelines.md#networks)要求付款请求网络与钱包网络一致；连接绑定的账户也不随钱包中浏览的账户自动变化。因此不使用主网连接尝试测试网付款，不修改地址前缀伪装网络，也不自动改为主网小额实转。

## 测试网设置入口核对

2026-11-04 重新核对官方资料，纠正此前只给出「Version & Network」的不足：

- 2026-11-02 更新的 [Walt 帮助](https://help.walt.io/article/1032-welcome-to-defi-account)说明该菜单可以选择 W5/v4R2，配图位于 DeFi 设置区域；它没有演示 Testnet 开关。合约版本与网络是两个不同的选择，不能从菜单名称推断测试网一定可选。
- 找到 [TON Dev News 于 2025-01-13 转发的钱包团队公告](https://t.me/tondev_news/174)，公告宣布当时的 TON Space 支持测试网，并提供 [设置直达链接](https://t.me/wallet/start?startapp=tonspace_settings)。已下载并查看随附演示：`Select network` 行可以从 Mainnet 选择 Testnet，与 `TON Space Version` 分开。
- 该链接的当前公开启动页仍标识为 Walt (ex. Wallet)，但本机没有登录用户的 Telegram，尚未验证链接在当前客户端中是否仍打开相同设置、是否显示网络选择。历史公告与新版帮助不能替代这一步，也不能证明 USDT 测试币手续费服务可用。

供用户核对的操作：在 Telegram 中打开上述设置链接，查找 `Select network` / `Network` /「选择网络」，若有则选择 Testnet；若只有 W5/v4R2 版本或根本没有网络选项，保留该现象，不要求用户退出账户、导出助记词或改用主网资金。当前只检查设置，不发起付款。公开资料原件及 SHA-256 保存在本机忽略目录 `official-settings-research/`，位于本记录下述证据根目录内。

## 后续实测判断标准

- 首先获得真实测试网连接，核对 W5、网络和可用代币；不能把主网地址前缀换成测试前缀就当作切网。
- 确认钱包实际接受的手续费代币，再准备少量对应测试资产；自建或社区测试币只证明其自身路径，不冒充 Tether 正式 USDT。
- 项目构造带唯一付款标识的 Jetton 转账，固定测试网络、测试收款方和小额本金；用户在手机钱包确认费用币种。若钱包要求原生币或不提供测试代币付费，应记录拒绝条件，不自动改成其他付费模式后宣称通过。
- 核对实际本金到账、memo、钱包扣款及手续费接收方，追踪完整接收结果和最终确认。只有钱包显示选项而未完成转账，标记为预览证据；不能据此认定实际网络费由 USDT 结算。

连接与原始账户数据保存在本机 `.tools/payment-feasibility/telegram-usdt-gas-2026-11-04/`，不入库。公开证据只保留必要能力、错误、合约类型和公共接口结果；记录不会发布会话存储或密钥。

[公开预检查证据](attachments/telegram-usdt-gas-2026-11-04/preflight.json)包含 W5 识别结果、公共手续费配置、测试网代币状态及显式测试网连接拒绝。[连接对照证据](attachments/telegram-usdt-gas-2026-11-04/connection-network-comparison.json)保存本次主网响应的必要能力、离线比对结果和 SDK 断开状态，不包含账户标识或会话链接。USDT 支付手续费仍未实转验证，本轮没有发起付款或扣费。
