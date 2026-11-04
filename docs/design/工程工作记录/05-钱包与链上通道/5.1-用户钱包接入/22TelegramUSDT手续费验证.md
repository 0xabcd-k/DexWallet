# Telegram / Walt 用 USDT 支付手续费：2026-11-04

[阶段讨论](01讨论记录.md) · [此前连接与双通道方案](21TON候选钱包能力与覆盖汇总.md) · [阶段目录](../README.md)

## 目标与当前结论

验证 `telegram-wallet` 对应的 Walt 自托管入口能否在项目 TON Connect 钱包直付请求中，使用 USDT 支付手续费并保留订单 memo。钱包内手动转账、项目发起的请求、测试币机制和正式 USDT 分别记录；当前尚无本入口的 USDT 付费实转证据。本轮不接入正式充值或账本，也不使用主网资金。

用户已确认“支持平台付的优先平台付，不支持的就前端付”，正式业务、界面、费用先付方和验收目标已同步；该决定不自动使未验证的直付通道通过。

## 已检查事实

1. [Walt 官方交易说明](https://help.walt.io/article/666-transactions)与[账户版本说明](https://help.walt.io/article/1032-welcome-to-defi-account)明确 W5 支持用 USDT 等受支持代币支付转账费用。它们描述钱包功能，不单独证明项目请求、测试网或任意测试币受支持。
2. 使用上次实际连接的 `walletStateInit` 在本机离线校验地址，并比对锁定的 `@ton/ton 16.3.0` 标准合约：地址吻合，code hash 为 `20834b7b72b112147e1b2fb457b84e74d1a30f04f737d4f62a668e9552d2b72f`，匹配 W5R1、不匹配 v4R2。这只是连接返回的合约类型识别，不是支付完成、账户所有权证明或当前链上状态核验。公开记录不含用户地址、公钥或 StateInit。
3. 用户确认设置中可以选择 Testnet。为此启动独立临时连接服务，固定 manifest 名称 `DexWallet USDT gas testnet`，SDK 5.0.0、官方 Walt bridge，连接阶段只请求 ton_addr，无支付或签名。公网 manifest 返回 200，连接入口返回 302；服务设置四小时上限。
4. 使用 `setConnectionNetwork('-3')` 时，钱包实际返回 `BadRequestError`，正文为 `Network is not supported`。用户看到 connect fail，服务仍正常；不是简单把公网链接故障当成钱包不支持。该结果仅证明显式测试网连接请求被拒绝。随后准备省略连接请求中的 network 做对照，先检查实际返回网络；主网响应不得进入付款，所有测试付款仍须固定 `network: '-3'`。
5. 只读查询 TonAPI 主网和测试网的 `/v2/gasless/config` 均返回 200，列出的 13 个手续费代币地址集合相同，包含正式主网 USDT 地址，不包含此前实验的社区测试币。随后查询 testnet.toncenter：主网 USDT 同地址在测试网为 uninitialized、没有合约代码；社区测试币 master 为 active。这证明不能把测试网配置返回成功或主网地址出现，当成测试 USDT 可用。也没有证据证明 Walt 当前使用同一手续费服务，不能把 TonAPI 的结果冒充 Walt 拒绝结果。
6. 用户完成新入口连接后，2026-11-04 14:12:44（Asia/Shanghai）收到与当前会话匹配的真实响应。省略连接 network 后，返回 `account.chain: '-239'`，仍为主网；离线核对为同一主网 W5R1 地址与公钥。设备仍只声明 SendTransaction / SignData，没有 SignMessage。探针自动断开，随后实际检查 SDK `connected === false`；没有请求付款、签名或广播。本轮连接未获得测试网账户，不能继续测试网 USDT 付费，也不能据此断言该钱包在任何入口都不支持测试网。

## 本轮连接对照与下一步

| 请求方式 | 钱包实际响应 | 当前结论 |
| --- | --- | --- |
| 显式指定连接网络 `-3` | `BadRequestError: Network is not supported` | 当前显式测试网请求被拒绝 |
| 省略连接 network | 成功返回主网 `-239`；探针自动断开 | 新入口已收回调，但未建立测试网连接 |

已请用户核对同一个自托管钱包中「Version & Network」当前实际勾选项，暂不重复连接。若仍勾选 Mainnet，先修正选择再建立新会话；若已勾选 Testnet，则继续定位钱包设置与 TON Connect 返回网络不一致的条件，不把重复换公网链接作为解决办法。需要用户确认的是客户端设置，本机已经能够检查连接响应与 SDK 状态。

[TON Connect 官方钱包规范](https://github.com/ton-blockchain/ton-connect/blob/main/guides/wallet-guidelines.md#networks)要求付款请求网络与钱包网络一致；连接绑定的账户也不随钱包中浏览的账户自动变化。因此不使用主网连接尝试测试网付款，不修改地址前缀伪装网络，也不自动改为主网小额实转。

## 后续实测判断标准

- 首先获得真实测试网连接，核对 W5、网络和可用代币；不能把主网地址前缀换成测试前缀就当作切网。
- 确认钱包实际接受的手续费代币，再准备少量对应测试资产；自建或社区测试币只证明其自身路径，不冒充 Tether 正式 USDT。
- 项目构造带唯一付款标识的 Jetton 转账，固定测试网络、测试收款方和小额本金；用户在手机钱包确认费用币种。若钱包要求原生币或不提供测试代币付费，应记录拒绝条件，不自动改成其他付费模式后宣称通过。
- 核对实际本金到账、memo、钱包扣款及手续费接收方，追踪完整接收结果和最终确认。只有钱包显示选项而未完成转账，标记为预览证据；不能据此认定实际网络费由 USDT 结算。

连接与原始账户数据保存在本机 `.tools/payment-feasibility/telegram-usdt-gas-2026-11-04/`，不入库。公开证据只保留必要能力、错误、合约类型和公共接口结果；记录不会发布会话存储或密钥。

[公开预检查证据](attachments/telegram-usdt-gas-2026-11-04/preflight.json)包含 W5 识别结果、公共手续费配置、测试网代币状态及显式测试网连接拒绝。[连接对照证据](attachments/telegram-usdt-gas-2026-11-04/connection-network-comparison.json)保存本次主网响应的必要能力、离线比对结果和 SDK 断开状态，不包含账户标识或会话链接。USDT 支付手续费仍未实转验证，本轮没有发起付款或扣费。
