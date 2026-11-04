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

## Telegram 缺少 SignMessage 时的替代方案

2026-11-04 用户追问替代办法后，核对官方协议、Walt 帮助与 W5 合约资料。以下是方案分析，未新增支付实测、未改变已确认的中继和费用规则。上文“不通过”限定当前入口直接从原钱包返回可执行签名的路径，不等于任何改变流程的方案都不可能。

| 路径 | 具体变化 | 能否保持原要求 / 尚缺条件 |
| --- | --- | --- |
| 支持 SignMessage 的钱包 | 使用已验证的 Tonkeeper W5 路径；资金若仍在 Walt，须先解决资金迁移 | 保留逐笔签名、平台校验和中继；现有实验证据仍限已初始化测试钱包，不自动覆盖迁移、首次初始化或主网 |
| Walt 钱包自行提交 | 后端创建并校验订单参数，用户通过 SendTransaction 确认，钱包广播；后端核对链上实际收款后推进订单 | 放宽“必须我方后端广播”。可以在构造请求时校验，但不能把最终签名交给平台作付费前的最后一道检查；回调也不能直接作为到账证明 |
| 预存到专用合约，再由 SignData 授权 | 用户先把资产存入能验证 SignData 的合约；之后签署含资产、金额、收款人、订单、有效期和防重放编号的付款授权，平台校验并提交 | 按协议可设计的候选，尚未实现或实测。只能支出已存入的资产；首次存入仍须解决钱包提交及费用，新增合约、退出机制和审计成本 |
| W5 扩展验证 SignData | 先由用户安装并授权受限扩展；后续扩展验证支付授权，再调用钱包执行 | 仅合约层面存在设计基础。仍需核实账户为 W5，以及 Walt 是否有可用的安装、撤销入口；现有 SendTransaction 声明不能证明能安装扩展，不作为当前可用方案 |

官方依据：[TON Connect SDK 的签名与广播区别](https://github.com/ton-connect/sdk/blob/main/packages/sdk/README.md)、[RPC 规范的 SendTransaction 与 SignData 合约验签规则](https://github.com/ton-blockchain/ton-connect/blob/main/spec/rpc.md)、[W5 的内部签名和扩展机制](https://github.com/ton-blockchain/wallet-contract-v5)。SignData 的签名有独立格式和应用域绑定，不能直接塞进现有 W5 普通转账消息；自定义合约能验签，也不意味着它自动取得用户原钱包的资金权限。预存合约和扩展方案是基于这些能力的设计推论，不是钱包官方已提供的集成产品。

[Walt 官方交易说明](https://help.walt.io/article/666-transactions)记载：W5 可用 USDT 等受支持代币支付手续费，原生币转账仍用原生币支付。这可以作为钱包自行提交路径的费用候选，但官方钱包内功能不证明 TON Connect 的指定请求也会走该路径，需另测网络、币种、费用和订单对应关系。“用户无需持有 TON”“我方先付 Gas”“平台最终承担成本”是三个不同条件，不能互相替代。

仅更换为 [TonAPI Gasless](https://docs.tonconsole.com/tonapi/rest-api/gasless/) 等中继服务也不能补齐缺失的支付签名：服务仍需要已签名的内部消息。提前给用户地址发送少量 TON 虽可供钱包付费，但不把两笔交易绑定，用户可以不完成后续付款；不建议用它绕过签名能力限制。成功后补贴费用则是另一项费用政策，不能默认采用。

当前建议：严格后端中继仍以已验证钱包为接入基础；若保留 Walt 的普通付款入口，可优先讨论并验证钱包自行提交的兼容流程。若要求继续使用 Walt 且后续每笔都由平台提交，才评估预存合约或可安装的受限扩展，并明确首次存入/授权的例外和成本。本次没有要求用户重新连接、签名、转币或提供密钥。

## 钱包自行广播后的到账核验

2026-11-04 用户提出前端可能提交伪造 tx 的问题。行业资料与现有项目规则均支持：客户端提供的 tx、BoC、截图和“支付成功”回调只作为查找线索，后端通过自己配置的节点或可信索引服务核验。短暂查不到交易可能是广播或索引延迟，不能立即判定造假，也不能先入账。本节是接入方案分析，尚未实现监听或修改既定代付政策。

真实性与订单归属是两个问题：链上确有转账，不代表属于提交者的订单。常见归属方式包括每订单专属收款地址、共享地址加唯一订单 memo，以及支付合约携带订单标识并产生可核验事件。[BTCPay](https://docs.btcpayserver.org/FAQ/General/)按发票生成并监控地址，其[集成流程](https://docs.btcpayserver.org/Development/ecommerce-integration-guide/)区分发现付款与满足确认条件；[TON 官方收款说明](https://docs.ton.org/applications/payments/jettons)支持由 forward_payload 中的发票标识或专属地址匹配账户。

若采用 TON 钱包自行广播的兼容路径，建议复用后端先建订单/付款尝试的流程，固定受益人、网络、真实资产、收款地址、应付数量与报价有效期；原生币用 comment 携带唯一付款标识，Jetton 用 forward_payload，先验证钱包确实保留这些内容。若某入口无法携带标识，应另选专属地址或明确不开放该入口，不凭相近金额和时间自动认领。订单标识不必充当秘密；公开标识和 tx 都不授予更改受益人或认领资金的权限。

后端处理边界：

1. 主动监听收款账户，并允许前端提交线索加快查询；前端关页或不回传 tx 不影响继续发现到账。Webhook 验证来源且幂等处理，配合断点补查；不把未经认证的通知正文当作链上证据。[TON 官方概述](https://docs.ton.org/applications/payments/overview)描述了监听、最终确认与补查流程。
2. 核对网络、允许的资产合约、实际收款目标与数量、订单标识及该链确认要求。付款来源有约束时再核对真实发送账户；本项目允许代他人充值，不把所有付款人强制等同受益人。账务永远按订单预先固定的受益人，不按谁上传哈希入账。
3. TON 钱包返回的外部消息 BoC/消息哈希不一定是收款交易哈希。沿消息追踪到实际收款，不能只看发送钱包成功；检查接收执行、退回和最终确认。USDT 还须验证允许的 Jetton master 及对应收款 Jetton wallet，不能信任代币名称或任意合约伪造的通知；通知模式要求付款包含适当 forward_ton_amount。[TON 交易核对说明](https://docs.ton.org/onboarding/explorers)、[Jetton 验证规则](https://docs.ton.org/applications/payments/jettons)。
4. 实收唯一标识按网络和稳定的转账/事件位置建立，不只按用户传来的字符串去重；一笔链上交易可能含多笔转账。同一实收的识别、分配额度约束、订单推进与账本写入遵循既有数据库事务和幂等规则，重复回调、扫描或跨订单提交不得重复增加可用资金。少付、多付、晚到和无标识的真实实收按既有异常资金流程保留，不凭钱包“成功”强行结单或丢弃资金。

以上延续 [钱包与支付](../../../../product/03钱包与支付.md) 的固定受益人、实收核验和异常认领约束，以及 [系统架构](../../../../design/04系统架构与契约.md) 的实收与分配模型。它能够解决钱包自行广播时的到账可信性；不补齐我方后端在广播前控制最终签名和 Gas 支出的能力，两件事须分别评估。

## 钱包直付与平台代付并行的讨论方案

2026-11-04 用户提出两条通道并行：扫码后建订单，钱包自行支付时以 memo 中的订单标识关联、用户提供 Gas；平台代付时由后端提交并追踪、平台提供 Gas。评估结论为方向可行，建议共用订单、实收核验和记账，仅区分付款执行与费用先付方。本节保留用户提议与补充建议，不把征询意见写成已完成费用审核或已实现通道。

**后续确认：** 用户随后明确“对于支持平台付的，优先平台付，不支持的就前端付”。执行方式据此自动按已验证能力选择，最终授权前展示费用；不再要求用户在两个同样可用的方式中自行选择。正式规则已同步 [决议台账](../../../../decisions/决议与待落实事项.md#2026-11-04-平台代付优先与钱包直付确认)，Telegram USDT 手续费的实际检查见 [专项记录](22TelegramUSDT手续费验证.md)。

| 通道 | 执行与关联 | 费用口径 |
| --- | --- | --- |
| 钱包直付 | 后端准备付款请求并自动带入订单/付款尝试 idx；用户确认后钱包广播，后端监听实际到账并匹配 memo | 用户钱包提供网络费；不得再重复扣取同一笔平台代付 Gas。钱包能否改用 USDT 付费须另测，不默认承诺 |
| 平台代付 | 用户签署可执行请求；后端先保存订单与签名消息/授权的关联，校验后提交，追踪对应消息、交易及实际到账 | 平台先提供 Gas；依已有充值方向从金额结算费用，不因“平台提供”推定平台免费承担，具体费用条目仍按逐项审核结果 |

建议补齐以下边界：

1. 扫码进入项目付款页时解析业务对象：已有商户订单则复用，账户收款码则建立对应充值/付款单；金额、币种与通道确定后生成可支付的付款尝试。直接用外部钱包扫静态地址码不会通知后端扫码，若提供该入口，应先建单再展示含付款标识的动态支付码。
2. 用户可查看订单号，但默认由支付请求/二维码自动填写收款地址、金额和 memo，手动复制只作备用。沿用项目对外 idx；若一单可能切换通道或重新报价，建议 memo 使用独立付款尝试 idx 映射回业务订单，保留各次确认的参数与费用。
3. TON 原生币使用 comment，Jetton 使用 forward_payload；钱包是否原样携带、通知及收款是否成功仍需实测。[TON 官方处理方式](https://docs.ton.org/applications/payments/jettons)支持从 forward_payload 读取订单标识。其他链币按自己的关联机制适配，不能推定都支持同样的 memo。
4. 平台提交后取得的交易标识不直接代表付款完成；广播前保留可用于恢复查询的签名消息或授权标识，广播后核验实际接收结果。TON 的消息 BoC、消息哈希和交易哈希须分别识别，[TON Connect RPC](https://github.com/ton-blockchain/ton-connect/blob/main/spec/rpc.md)明确区分签名返回与广播返回。两条通道最终使用前节同一套网络、资产、金额、目标、确认和去重规则。
5. “并行支持”指系统提供两种能力，同一有效付款尝试只选择一种。已签名可能广播或广播结果未知时，先追踪原尝试，不自动切换再扣一笔；沿用现有安全终止后再付款规则。关闭或过期不能阻止用户另行转款，监听继续，迟到或重复实收进入既有异常资金处理，不重复完成订单、不静默吞掉实收。
6. 付款框按钱包、网络和资产的实测能力提供“钱包支付”“平台代付”，在确认前显示费用支付方式和净到账。Telegram 当前只是中继能力不满足，钱包直付仍待 memo 与实际到账测试；不随本讨论自动列入已通过白名单。

最新确认已为现有“充值代付”方向增加能力不支持时的钱包直付分支，并同步 [费用逐项审核](../../../../design/链上资金费用逐项审核.md) 的 A02/A03 等受影响场景及产品、界面与验收；最终授权仍须明确费用，不在结果未知时切换。服务费、退款费以及平台是否补贴继续保持各自待审核状态。

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
