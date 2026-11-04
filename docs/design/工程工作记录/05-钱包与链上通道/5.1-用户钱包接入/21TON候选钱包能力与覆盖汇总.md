# TON 候选钱包能力与覆盖汇总：2026-11-03（2026-11-04 更新）

[阶段讨论](01讨论记录.md) · [候选扩展钱包](20候选钱包兼容与阻塞记录.md) · [阶段目录](../README.md)

按用户“验证完成不需要暂停”的要求，本轮连续完成 TRON 完整资源对照、Rabby、Backpack、OKX 及其他候选钱包检查，再补 My Wallet 手机端。通过项、测试替身、明确能力不匹配和外部阻塞分别保留；不据此勾选正式业务开发完成。

## My Wallet 手机端实际能力

使用既有独立 Android 15 / API 35 模拟器和已校验官方 **My Wallet 26.10.7，build 8180**。原 APK 来源及哈希见 [此前安装记录](05支付可行性验证.md)。通过原生 UI 创建一个空的专用钱包、完成恢复词核对并设置本地 PIN；恢复词、PIN 和设备数据均未入库。

通过 TON Connect SDK 5.0.0、官方 My Wallet bridge `https://tonconnectbridge.mytonwallet.org/bridge/` 和官方 deep link `mytonwallet-tc://` 发起连接。临时 HTTPS manifest 对应 `DexWallet test-only probe`，原生钱包页显示应用名和 origin，批准后真实返回：

```json
{
  "platform": "android",
  "appName": "MyTonWallet",
  "appVersion": "26.10.7",
  "maxProtocolVersion": 2,
  "features": [
    "SendTransaction",
    { "name": "SendTransaction", "maxMessages": 255 },
    { "name": "SignData", "types": ["text", "binary", "cell"] }
  ]
}
```

**没有声明 `SignMessage`。** 探针因此返回 `sign-unsupported`，没有发起支付签名、sendTransaction、转账或任何主网费用操作。本次连接的是新建空主网账户（chain `-239`），只验证客户端能力声明，不将它计为测试网实转。当前严格要求“原钱包返回可执行签名 → 我方后端校验 → 平台承担费用并提交”，所以该版本的 TON 和 USDT 路径均不通过；`SignData` 不是可执行转账，不能拿它替代。

[实际连接与能力响应](attachments/ton-candidate-wallets-2026-11-03/mywallet-runtime-capabilities.json) · [原生连接页](attachments/ton-candidate-wallets-2026-11-03/mywallet-connect.png) · [对应 tag 的 adapter 来源及哈希](attachments/ton-candidate-wallets-2026-11-03/mywallet-source-evidence.json)。源码分派只有 sendTransaction、signData、disconnect，与本次运行时观察相符；结论仅限此版本、此平台及连接方式。

本机操作经验：原生创建页有三个独立风险确认项，全部选中后才启用 Go to Words；恢复词页面可能弹出复制提醒，不能将点击后仍未转页误判成客户端无法创建。UI dump 和截图可能包含恢复词，必须只在忽略目录保存并过滤输出。重复 deep link 会叠加连接弹窗，宜先检查当前页面再重新发起。这里记录的是测试自动化经验，不是产品流程。

## Telegram 内的 Walt 自托管钱包实际能力

当前 TON 官方钱包目录把 `telegram-wallet` 列为 **Walt (ex. Wallet)**，另有 `telegram` 的 Telegram 原生钱包记录；两者都不能凭品牌名混同为 Tonkeeper W5。所取目录中，两者仅声明 SendTransaction / SignData，未声明 SignMessage。目录属于静态能力参考，不能冒充用户实际客户端返回。[目录快照和来源](attachments/ton-candidate-wallets-2026-11-03/ton-candidate-registry.json)保留具体 app_name、官方链接、bridge 和 features。

官方说明进一步区分 [Walt 的托管与自托管账户](https://help.walt.io/article/1080-welcome-to-walt) 和 [Telegram 原生钱包](https://docs.ton.org/onboarding/wallet-apps/telegram)。当前项目讨论的自托管钱包需按实际入口识别，不将托管提现、钱包自带免手续费额度或另一种钱包合约当成我方后端中继已支持。

2026-11-04 用户在手机上完成 **只请求 `ton_addr` 的 Walt 自托管钱包连接**。本机于 **02:28:42.778 UTC（北京时间 10:28:42.778）** 收到真实响应，已核对响应与当次连接请求属于同一会话，用户随后确认“已连接”。返回的能力为：

```json
{
  "platform": "android",
  "appName": "telegram-wallet",
  "appVersion": "1",
  "maxProtocolVersion": 2,
  "features": [
    "SendTransaction",
    { "name": "SendTransaction", "maxMessages": 255 },
    { "name": "SignData", "types": ["text", "binary", "cell"] }
  ]
}
```

**当前实际连接未声明 `SignMessage`，不满足既定的前端签名、后端中继流程。** `appVersion: "1"` 是钱包通过 TON Connect 自报的值，不是已核实的 Telegram 安装包版本。响应的账户链为主网 `-239`，本次只读取连接和能力，没有调用 SignMessage、SendTransaction 或 SignData，没有广播或支付费用。不能把“未声明必要能力”写成“已尝试支付并被链上拒绝”，也不能据此判断所有未来版本或另一种 Telegram 钱包都不可行。

该结果同时限制当前入口的 TON 原生币与 USDT：两者都缺少向平台返回可执行支付签名的已验证接口。SendTransaction 走钱包提交路径，SignData 是数据签名，均不替代本项目已接受的中继步骤。若客户端以后声明 SignMessage，仍须重新验证网络、账户合约、签名内容、平台费用与实际到账。

[真实连接的公开能力记录](attachments/ton-candidate-wallets-2026-11-04/telegram-wallet-runtime-capabilities.json)只保留设备声明、网络、请求范围和判断；用户钱包地址、公钥、walletStateInit 及完整原始响应留在本机忽略目录，未入库。本次结论对应 `telegram-wallet`，不覆盖目录中另一个 `telegram` 入口。

复现入口仍使用 TON Connect SDK 5.0.0，manifest 名称为 `DexWallet test-only probe`，连接源为 `https://t.me/walt?attach=wallet`，bridge 为 `https://walletbot.me/tonconnect-bridge/bridge`。仅发起 connect 的 `ton_addr` 请求并读取 onStatusChange 返回的 device.features，不为检查能力强行调用未声明的支付方法。前一天临时服务已经停止，因此本次先更新链接并检查公网 manifest 和跳转，再交给用户；临时连接服务设置四小时上限，采集结果后已主动关闭，旧链接不再作为复用入口。

## 四链九组合的当前证据

下表按“至少一个指定真实钱包”的机制证据归纳，不代表所有候选钱包、移动端和主网资产通过。

| 链 / 项目资产 | 已完成的真实钱包实验 | 范围限制 |
| --- | --- | --- |
| TON / TON | Tonkeeper 26.10.4，已初始化 W5，前端 SignMessage、后端中继、到账和重放 | 首次初始化曾用钱包自行广播；余款归属仍须落实，不能宣称新账户全流程已经通过 |
| TON / USDT | 同钱包、社区测试 Jetton，代付、到账、通知与余款核对 | 社区测试币，不是 Tether 官方主网 USDT 实转 |
| Base / ETH | MetaMask 13.53 官方独立对照，智能账户权限、平台执行及最终到账 | 13.55 的 Gator 权限 4100 尚未解决；普通 EOA 发送 ETH 不具备更换 gas payer 的同等路径 |
| Base / USDC | MetaMask、Rabby 0.94.14、OKX 4.22.2，Circle 测试币 EIP-3009，用户零 ETH，平台付费 | 三者均已 finalized；Coinbase 普通扩展连接仍挂起，Smart Wallet 路径另待验证 |
| Solana / SOL | Solflare 2.41.1、Backpack 0.10.219，用户单签、平台 fee payer、finalized | Phantom 服务 403；OKX Devnet 切网及余额预检阻塞 |
| Solana / USDC | 同两钱包、Circle Devnet USDC，代付和实际余额变化 | Solflare 覆盖新建/已有 ATA，Backpack 本次为已有 ATA；不扩大到任意 mint |
| Solana / USDT | 同两钱包、自建六位 SPL 测试币，主网资产身份与 Token Program 只读对照 | 不是 Tether 官方测试 USDT；不能改写成主网实转通过 |
| TRON / TRX | TronLink 4.11.2，平台委托带宽，用户仅扣本金，固化确认 | 账户激活和资源池准备另计；委托不是每笔交易的原子资源预留 |
| TRON / USDT | 官方 Nile USDT 实转；另以同 runtime 自建币证明同一笔支付的 Energy / Bandwidth 都来自平台 | 官方测试币原实验的 Energy 来自部署者；完整平台资源实验须保留替身身份，三个其他候选钱包当前未通过 |

对应证据入口：[TON 原生币](15TON初始化与原生币代付实转.md)、[TON 测试 Jetton](16TON测试USDT代付与余款验证.md)、[Base ETH](10BaseETH故障定位与代付实转.md)、[MetaMask USDC](08BaseUSDC授权与代付实转验证.md)、[Solana USDT 机制](17SolanaUSDT代付机制验证.md)、[TRON 完整资源](18TRON完整资源代付验证.md)、[Rabby / Backpack](19Rabby与Backpack钱包验证.md)、[OKX 与其余候选](20候选钱包兼容与阻塞记录.md)。

## 可继续工作的边界

- 已通过的固定组合足以作为后续接入开发的输入，但正式白名单尚需产品确认；不自动扩成所有钱包或主网验收。
- My Wallet 当前版本缺少必要能力，不继续用同一不支持入口重复转币。Trust、TokenPocket、Bitget 的测试网/余额预检阻塞、Phantom 403 和 Coinbase 连接挂起均已留下复现材料；变更客户端版本、平台或网络配置后再复测。
- Telegram 内 Walt 自托管钱包已完成上述真实能力检查，当前连接方式不通过严格中继要求，用户无需再为这一项操作。Coinbase Smart Wallet、TON 首次初始化全程由后端执行、资源池并发及主网支付环境仍未完成，不以已通过的对照实验代替。
- 本轮新增链上支出仅是明确允许的测试网实验；未购买或转移主网资金，未接入正式充值、记账或通知业务。
