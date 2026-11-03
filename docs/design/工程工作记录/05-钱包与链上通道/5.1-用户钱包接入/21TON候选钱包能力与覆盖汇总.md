# TON 候选钱包能力与覆盖汇总：2026-11-03

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

## Telegram 内的钱包仍待实际连接

当前 TON 官方钱包目录把 `telegram-wallet` 列为 **Walt (ex. Wallet)**，另有 `telegram` 的 Telegram 原生钱包记录；两者都不能凭品牌名混同为 Tonkeeper W5。所取目录中，两者仅声明 SendTransaction / SignData，未声明 SignMessage。目录属于静态能力参考，不能冒充用户实际客户端返回。[目录快照和来源](attachments/ton-candidate-wallets-2026-11-03/ton-candidate-registry.json)保留具体 app_name、官方链接、bridge 和 features。

官方说明进一步区分 [Walt 的托管与自托管账户](https://help.walt.io/article/1080-welcome-to-walt) 和 [Telegram 原生钱包](https://docs.ton.org/onboarding/wallet-apps/telegram)。当前项目讨论的自托管钱包需按实际入口识别，不将托管提现、钱包自带免手续费额度或另一种钱包合约当成我方后端中继已支持。

本机没有已登录 Telegram 的测试账号，已准备 **只请求 `ton_addr` 的 Walt 自托管钱包连接**，请用户用手机完成。未请求交易、支付签名、测试币、密钥或登录验证码。实际返回前，保留“待用户连接”，不将目录中缺少 SignMessage 写成所有 Telegram 客户端已经实测拒绝。若用户连接后能力仍缺失，当前严格流程即不通过；若未来声明支持，再准备独立测试网签名和资金验证。

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
- Telegram 内钱包的真实能力需用户完成上述连接。Coinbase Smart Wallet、TON 首次初始化全程由后端执行、资源池并发及主网环境仍未完成，不以已通过的对照实验代替。
- 本轮新增链上支出仅是明确允许的测试网实验；未购买或转移主网资金，未接入正式充值、记账或通知业务。
