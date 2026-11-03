# TON：真实钱包连接与 SignMessage 验证

[阶段讨论](01讨论记录.md) · [早期协议基线](05支付可行性验证.md) · [阶段目录](../README.md)

后续更新：用户确认领取完成后，已完成初始化及 0.01 TON 原生币代付，见 [后续实转记录](15TON初始化与原生币代付实转.md)。下文保留首次签名验证时的结果，seqno 0 限制仍有效。

## 本轮签名验证结论

2026-11-03 继续验证 TON 原生币的「钱包仅签名 → 后端校验 → 平台承担费用并提交」。已从官方 Tonkeeper Android **26.10.4（1353）** 的测试网 W5 账户取得真实 `internalBoc`，完成签名、账户、金额、目标及篡改检查；尚未广播或完成平台代付。TON USDT 尚未实转，5.1/5.3/5.5 正式业务仍未接入。

发现一个必须保留的限制：**首次 `seqno = 0` 的签名将请求的短有效期改为 `0xffffffff`。** 本次请求 120 秒有效期，钱包返回 `4294967295`；验签成功不代表可接受该付款授权。本轮审计明确输出 `relayAllowed: false`，没有广播该消息。后续应单独验证首次初始化与初始化后的付款，不能直接把这个特例放进普通付款通道。

| 检查 | 实际结果 | 边界 |
| --- | --- | --- |
| HTTPS manifest 与真实 bridge 连接 | 通过，解决早期 ManifestNotFoundError | 临时测试 origin，非产品部署 |
| 旧版 26.06.2 运行时能力 | 实际响应不含 SignMessage | 不将源码判断替代运行时证据 |
| 新版 26.10.4 运行时能力 | 主网及测试网 W5 均返回 SignMessage，maxMessages 255 | 仅当前 Android 发行版和账户类型 |
| 测试网账户 | 原生测试网导入入口，连接返回 chain `-3` | 主网账户签名由探针提前阻止 |
| 用户拒绝 | 原生确认页取消，SDK 返回 UserRejectsError | 没有伪造拒绝结果 |
| 用户确认 | 原生滑动确认及 PIN 后返回 internalBoc | 钱包不广播；不是后台用用户密钥代签 |
| 后端消息审计 | W5 地址/StateInit、公钥、签名、1 nanotoken 自转、send mode 3 均匹配 | 审计脚本只读，不是生产中继入口 |
| 篡改检查 | 金额、目标、有效期、walletId、签名类型任一改变，原签名均失效 | 基于本次真实钱包签名 |
| 首次签名有效期 | 不匹配，请求 1793704888，实际 4294967295 | 已定位官方客户端的 seqno 0 特例 |
| 测试资金 | 官方 Acton 自动领取已受理，十分钟后的余额仍为零 | 排队成功不等于到账，已请求用户协助官方 Telegram 水龙头 |
| 平台代付与到账 | 未执行 | 不计为 TON 完整支付通过 |

项目仍沿用原生资产名称 TON。此次官方客户端和水龙头界面显示 GRAM，记录原样保留，不据此自动修改产品币种范围。

## 环境与安装包

复用 [Android 基线](04验证记录.md) 的独立 Android 15 / API 35 AVD、设备 `emulator-5580`；启动使用 `-gpu swangle -feature -Vulkan`。保留原账户及 AVD，仅对专用测试环境执行官方 APK 的 `adb install -r`，未修改钱包源码、内部开关或存储。

- 新包来自 [官方 v26.10.4_1353 release](https://github.com/tonkeeper/android/releases/tag/v26.10.4_1353)，发布时间 2026-10-26，150,245,335 bytes。
- SHA-256：`fd4568185f6b1c7ee111044cbbb00ff494e38acc94e29173dcf95077ed5f05b6`，与官方 release API 的 digest 一致。
- APK 发布证书 SHA-256：`2cc80c10bd94b0cbec2ee3e84a1dde2deb4698cc029887a6aadbf93ca5e96d8a`，与原 26.06.2 相同；使用 Android build-tools 36.0.0 的 apksigner 验证。
- 固定源码 tag 对应提交 `8597b87a990c8f88afc1d27cb17ff35a42d873ec`。引用文件和文件散列见 [源码及包证据](attachments/ton-signmessage-2026-11-03/source-evidence.json)。未复制整份第三方源码入库。

客户端通过 Settings 底部版本号连续点击进入 Dev，使用 **Import Testnet Wallet**，明确选择 **24 words**，导入新生成的专用 TON 测试助记词。原账户保留，新账户命名 `TON Testnet`，首页显示 W5 TESTNET。助记词及 PIN 只在本机忽略目录和原钱包内保存，导入时不输出输入框内容或截图。

自动输入的两个实际问题：默认导入选项是 12 words；软键盘会遮挡输入框，在空框额外退格还会跳回上一框。复现应先选对词数，再逐框确认焦点/内容，避免把自动输入失败误认为钱包不兼容。

## 连接与签名证据

原本的本机 manifest 无法从钱包获取链路访问。本次使用官方 cloudflared **2026.10.7** Quick Tunnel，仅暴露已有探针服务的固定路径，钱包原生连接页成功显示应用名称和 HTTPS origin。探针服务只监听 `127.0.0.1:18791`，没有目录浏览或密钥下载接口；临时隧道不作为正式基础设施。

cloudflared Linux amd64 二进制 SHA-256：`1f99fb518b83dec2ba8d870a7cc991d82a77e6196295a3d2754ba17ab9f91872`，与 [官方 release](https://github.com/cloudflare/cloudflared/releases/tag/2026.10.7) API digest 核对。隧道只承载 manifest、探针静态文件和公开事件摘要，签名原文不上传事件接口。

[实际事件摘要](attachments/ton-signmessage-2026-11-03/wallet-events.json) 保留旧版、新版主网阻止、新版测试网连接、拒绝和签名成功。关键时间均为 UTC：

| 时间 | 事件 |
| --- | --- |
| 10:48:44 | 26.06.2 实际连接，不含 SignMessage |
| 11:02:04 | 26.10.4 主网 W5 实际连接，包含 SignMessage；探针阻止主网签名 |
| 11:18:24 | 专用测试网 W5 连接，chain -3、SignMessage 可用 |
| 11:18–11:19 | 原生确认页取消，收到真实 UserRejectsError |
| 11:20:04 | 原生签名返回，随后独立解码及验签 |

旧探针曾使用 raw `0:…` 地址调用 signMessage，SDK 首先报 Wrong address format；这是**探针输入错误**，不能解释为钱包拒绝该方法。现已改为测试网 friendly 地址，未声明能力时直接记录 unsupported，支持能力但连接主网时停止。

原生 [签名确认页](attachments/ton-signmessage-2026-11-03/sign-only-preview.png) 明确说明钱包不会发送，由应用发送并付网络费。1 nanotoken 在界面显示为 0 GRAM，这是本次极小额能力探针的显示精度现象；未据此验收正式付款金额展示。下一笔实际付款应使用可清楚展示的金额并再次核对。

本次专用测试用户公开信息：

```text
friendly: 0QCjk6WUaIpT9hONiPNEeomRCTzDh_BGPQrNlVKJ-tvveqrM
raw:      0:a393a594688a53f6138d88f3447a8991093cc387f0463d0acd955289fadbef7a
publicKey: 91a0d3523098850dc441f525f5fb22c8e25a7470a3888aada6868b093b37cb51
```

### 后端解码与首次有效期限制

[审计报告](attachments/ton-signmessage-2026-11-03/signature-audit.json) 来自真实客户端返回值。`internalBoc` 是完整的 **internal message 包装**，不是裸签名 body：外层目标为用户 W5，附带值为 0，首次包含 StateInit；外层信息须独立校验，不能当作用户签名覆盖的全部内容。

内层包含 `auth_signed_internal = 0x73696e74`、测试网 walletId `2147483645`、seqno 0、一个 mode 3 的 sendMsg，目标为本人、数量 1 nanotoken、无附加 payload 或子账户部署。签名位于 body 尾部 512 bits；重建其余 bits/refs 后验签成功。金额、目标、有效期、walletId 或 auth opcode 修改后，原签名均不再有效。

源码定位与实际响应一致：

1. `SendTransactionViewModel.signMessageBody` 按请求计算有效期；`signMessage` 调原钱包签名后返回包装消息，不广播。
2. `WalletV5R1Contract.createTransferUnsignedBody` 调用 `storeSeqAndValidUntil`。
3. `CellBuilder.storeSeqAndValidUntil` 在 **seqNo == 0** 时写入 32 个 1，其他 seqno 才写入传入的 validUntil。

因此问题不在后端验签算法，也不是 SDK 显示字段不一致，而是有效期实际进入签名消息时被替换。对于普通付款，后端应核对实际签名的截止时间与订单允许范围；不能仅检查前端请求的 validUntil。初始化后的行为当前只有源码依据，仍需真实签名验证。

另一个后续检查点是 mode 3 包含忽略部分 action 错误的语义。实转不能只看外层交易或 seqno 增长，必须核对付款子消息、接收方交易和实际到账。

## 资金准备与当前阻塞

依据 [TON 官方测试币指南](https://github.com/ton-blockchain/docs/blob/main/content/onboarding/wallet-apps/get-coins.mdx) 和 [Acton 钱包文档](https://ton-blockchain.github.io/acton/docs/wallets)，使用官方 **Acton v1.2.1** 命令行 faucet。包 SHA-256 为 `490d6763dcfe148e9c8346e3d56c398e950552d329a768b7e9168d4dd54afe3f`，已核对官方 release API digest；仅解压到忽略目录，不改项目技术栈。

平台专用测试网地址：`0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp`。

11:15:30 UTC，官方 CLI 完成本地 PoW 并返回 success、claim queued；未重复申领或绕过领取限制。11:25:33 UTC 再查 TON Center，余额仍为 0、state uninitialized、last transaction 为 0，另查 TonAPI 也未见到账。用户签名后账户同样未部署、无交易。[资金状态证据](attachments/ton-signmessage-2026-11-03/funding-status.json) 明确区分受理与到账。

已请求用户通过官方 `@testgiver_ton_bot` 领取一次免费测试币。该入口需要 Telegram 登录及验证码，只有实际到账后才继续链上初始化和代付；不购买或使用主网资产。

## 复现入口与保留材料

沿用独立 npm 包和锁定依赖：Node 24.22.1、TON Connect SDK 5.0.0、@ton/ton 16.3.0、@ton/crypto 3.3.0、Playwright 1.64.1。探针不纳入生产或默认 CI。

```sh
# 仓库根；先启动既有独立 AVD，再运行本机探针。
PAYMENT_PROBE_OUTPUT="$PWD/.tools/payment-feasibility/ton-connect-2026-11-03" \
  node scripts/payment_probe/server.mjs

# 另一个终端：仅临时开放上面的固定探针服务。
.tools/payment-feasibility/cloudflared tunnel \
  --url http://127.0.0.1:18791 --protocol http2 --no-autoupdate

# PUBLIC_ORIGIN 替换为本次隧道实际输出的 HTTPS origin，不沿用旧随机域名。
PAYMENT_PROBE_CAPTURE=1 PAYMENT_PROBE_PUBLIC_ORIGIN="$PUBLIC_ORIGIN" \
  node scripts/payment_probe/bridge.mjs

# 在原钱包批准连接、核对测试网及消息，再选择拒绝或确认签名。
# 捕获文件名由 bridge 输出；签名原文仅落在忽略目录，权限 0600。
node scripts/payment_probe/ton-signature.mjs \
  .tools/payment-feasibility/ton-signatures/capture-1793704804465.json \
  .tools/payment-feasibility/ton-signatures/verification-report.json

node --test scripts/payment_probe/probe.test.mjs
python3 scripts/check_docs.py
git diff --check
```

`ton-signature.mjs` 仅审计这一类 1 nanotoken 自转能力探针，不读取用户或平台私钥、不连接 RPC、不广播。退出成功表示审计过程正常完成，报告中的 `deadlineMatches: false` 和 `relayAllowed: false` 仍明确表示付款授权不可直接使用。实际链上 seqno、部署状态和费用需后续专项验证。

本地保留：`.tools/payment-feasibility/ton-testnet-secrets.json`（专用助记词）、`secrets.json` 中原 PIN、`ton-signatures/`（原始签名及私有捕获）、`ton-connect-2026-11-03/`（事件）、`ton-ui-2026-11-03/`（非秘密界面证据）、`ton-acton/`（领取配置/结果）、两版 APK 和独立 AVD。**seqno 0 签名没有预期的短有效期，原始签名不得作为公开附件发布。**

## 后续顺序

1. 核实平台测试币到账，必要时核对用户提供的领取结果；保存实际交易及余额，不能把 faucet success 当作资产到账。
2. 单独验证 W5 初始化，包括首次费用、签名生命周期与 seqno 变化；初始化若使用其他发送方式，明确与目标 sign-only 付款区分。
3. 使用初始化后的真实钱包签署可清楚展示的小额付款，核对签名有效期、目标、金额、钱包代码/状态和 seqno 后，再由平台附带费用发送；核对完整消息链、实际费用、余款归属和到账。
4. 之后单独验证 TON USDT 的 Jetton master/wallet、数量、通知与 excess 回流，不能把原生 TON 签名结果推广为 USDT 通过。

本次工具检查：4 项确定性探针行为测试通过；真实签名结构及 5 类篡改检查通过，有效期不匹配如实记录。没有声称真实代付、首次初始化、USDT 或业务订单/账本验收通过。
