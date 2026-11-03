# TRON：同一笔代币付款的完整平台资源承担

[阶段讨论](01讨论记录.md) · [此前 USDT 实转](12TronUSDT实转与Energy来源.md) · [阶段目录](../README.md)

## 本次结论

2026-11-03（UTC）完成 **TronLink 4.11.2 + Nile 自建同代码测试币** 的真实钱包签名、后端校验及广播、固化确认、到账和原交易重放。用户付款前后均为 **0 TRX**；0.01 测试币付款实际使用 **14,650 Energy、345 Bandwidth，均由平台 Stake V2 委托提供**。部署者 Energy、用户免费带宽和燃烧 TRX 均没有承担这笔付款费用。

这是 TRC-20 USDT 转账机制和平台资源承担的对照实验，**不是新一笔 Tether 发行 USDT 的全资源代付验收**。官方 Nile USDT 已实转，但其合约设置为部署者承担 Energy；不能把这一结果写成项目平台承担。这里部署独立测试合约并设置调用方资源比例 100%，将部署者补贴排除。测试币名 `DexWallet Resource Test`、符号 `DWUSDT-PROBE`，未作为产品支持资产。

| 核对项 | 结果 |
| --- | --- |
| 测试合约 | `TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn`，6 位精度 |
| 用户 | `TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio` |
| 平台及收款人 | `TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH` |
| 用户测试币 | 20 → 19.99 |
| 平台测试币 | 980 → 980.01 |
| 用户 TRX | 0 → 0 |
| 平台 TRX（付款阶段） | 96.6785 → 96.6785 |
| 回执 | `SUCCESS`；`energy_usage = energy_usage_total = 14650`，`net_usage = 345` |
| 付款燃烧费用 | Energy fee、Net fee、总 fee 均为 0 |
| 原交易重放 | `DUP_TRANSACTION_ERROR`，两端测试币及 TRX 余额不变 |

付款交易：[Nile 浏览器](https://nile.tronscan.org/#/transaction/82734cb0e229e0f5c8fe3716fd8ccee522e49a603afa1a1e6654f52cc49f2182)。[完整资源与回执证据](attachments/tron-full-resources-2026-11-03/result.json)包含准备交易、前后资源、委托关系、付款事件、余额及重放。

## 合约身份和准备成本

从官方 Nile USDT `TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf` 读取创建代码及 runtime，校验固定哈希后剥离原构造参数，换成测试名称、符号、初始数量和同样的 6 位精度。新合约 runtime 与参考合约 SHA-256 均为：

```text
7ae2015d27ec7aec7c057f4a3dd026dfc224a0b9ee4017eccfa569ae167cbb2b
```

创建代码包含原构造参数，不能原封不动再追加新参数。脚本校验参考名称、符号和精度，定位唯一 runtime 后截取代码主体，再经 TronWeb 编码新参数。部署后重新读取 runtime、owner、symbol、decimals 和 `consume_user_resource_percent = 100`，再允许付款。

用户本轮协助领取 1000 免费 Nile TRX 后，平台可用余额为 1014.8559 TRX。准备消耗如下，与用户付款费用分开记录：

| 准备动作 | 测试 TRX 网络费 |
| --- | ---: |
| 部署测试合约 | 324.7454 |
| 给测试用户分配 20 枚测试币 | 3.31 |
| 新质押 227 TRX Energy | 1.769 |
| 委托上述 Energy | 1.793 |
| 新质押 356 TRX Bandwidth | 1.768 |
| 委托上述 Bandwidth | 1.792 |
| 合计 | 335.1774 |

新增质押 583 TRX 仍在链上质押，不算燃烧手续费。结合原有委托后，用户获得平台 **228 TRX 对应的 Energy 份额、856 TRX 对应的 Bandwidth 份额**。数量依据当时全网总配额、总质押权重、已有使用量和本次模拟动态计算；不应将这些测试网数值作为主网固定报价。资源委托没有变成单笔付款的原子预留。

当平台已有带宽配额较小时，`getcandelegatedmaxsize` 会为普通委托交易的带宽消耗预留份额，可能返回 0。本次测试准备交易附加固定 512 字节说明，使其完整带宽需求超过平台质押与免费配额，明确由平台燃烧测试 TRX 支付准备费用；签名前核对该条件，并按实际已消耗配额限制可委托的自有份额。此处理仅用于隔离实验，不能直接推广为生产资金池策略。依据见 [java-tron Wallet](https://github.com/tronprotocol/java-tron/blob/develop/framework/src/main/java/org/tron/core/Wallet.java)，读取文件哈希已存入[源码记录](attachments/tron-full-resources-2026-11-03/source-evidence.json)。

## 签名、异常和验收边界

TronLink 原界面完成一次拒绝、一次签名返回，钱包没有广播。后端校验网络、唯一合约调用、测试合约、收款人、原子金额 10000、附带 TRX 为 0、fee limit、原消息字节、有效期及真实签名者；广播前再次模拟并检查两类委托资源的来源和余量。`Transfer` 日志、用户扣币与收款增币必须同时匹配。

钱包仍出现 `Token precision error`。沿用已获授权的测试范围，通过原生 Metadata 核对 `transfer(address,uint256)`、收款地址、原子数量和完整原消息后签名，见[钱包记录](attachments/tron-full-resources-2026-11-03/wallet.json)、[签名预览](attachments/tron-full-resources-2026-11-03/wallet-sign.png)、[Metadata](attachments/tron-full-resources-2026-11-03/wallet-metadata.png)。正常人类可读资产名称及金额展示仍未验收，不能将测试操作做成用户付款流程。

部署的 JSON 广播曾两次超时，两个节点查询均未见原交易。随后用原签名字节构造 protobuf，经同一 Nile 网络的 [broadcasthex](https://developers.tron.network/reference/broadcasthex) 和本机代理成功提交，哈希保持不变；没有签第二笔部署交易。后续也出现代理 TLS 超时，探针只允许只读调用换直连重试一次；模糊广播结果必须按已保存 txID 恢复。切换编码及路由同时发生，尚未单独证明究竟是哪一项导致超时消失。

首次付款回执核对因资源自动恢复而失败：139.002 秒的读取区间内，EnergyUsed 实际增加 14629、NetUsed 增加 344，不能要求其与回执差值恒小于 2。现保留原始快照，按两端账户实际 usage、Stake V2 恢复窗口和读取时长计算恢复上界，同时要求回执数值精确匹配、两端 TRX 不变、委托来源明确及免费带宽未增加。窗口单位及优化标志按 [AccountCapsule](https://github.com/tronprotocol/java-tron/blob/develop/chainbase/src/main/java/org/tron/core/capsule/AccountCapsule.java) 和 [Parameter](https://github.com/tronprotocol/java-tron/blob/develop/common/src/main/java/org/tron/core/config/Parameter.java) 核对；超过 15 分钟的快照不接受为本实验资源审计。没有放宽资金和事件匹配。

剩余边界：官方资产及其他钱包完整实转、正常钱包预览、资源被并发消耗、委托回收、链上失败费用、主网资源报价和正式订单入账。已有零 TRX 用户和两类委托资源成功证据不代表这些项全部通过。

## 复现入口

沿用独立测试包、Node 24.22.1、TronWeb 6.5.1 和原版 TronLink 4.11.2，无新增依赖。专用账户密钥、未消费签名和 profile 均留在忽略目录；附件只保存已审阅的公共证据。

```sh
node --test scripts/payment_probe/tron-live.test.mjs scripts/payment_probe/tron-usdt.test.mjs scripts/payment_probe/tron-energy.test.mjs scripts/payment_probe/tron-resource.test.mjs
node scripts/payment_probe/tron-resource-live.mjs --inspect
# 本次已执行：--setup → --prepare → 钱包签名 → --submit → --replay
node scripts/payment_probe/tron-live-wallet.mjs --sign-metadata usdt-resource
node scripts/payment_probe/tron-resource-live.mjs --audit
```

现有订单已固化，不能清空状态重跑付款；钱包命令会拒绝已存在的付款。`--audit` 使用留存的付款前后快照，不用当前恢复后的资源冒充当时余额。单元测试覆盖资源不足、来源替换、免费配额冒充、部署者付费、燃烧费、错误币及收款数量、签名替换、延迟恢复和过旧快照。
