# TON：首次初始化与原生币代付实转

[阶段讨论](01讨论记录.md) · [前轮连接与签名](14TON钱包连接与签名验证.md) · [阶段目录](../README.md)

## 结论与范围

2026-11-03 用户确认测试币领取完成后，**官方 Tonkeeper Android 26.10.4（1353）+ 已初始化 W5 + TON testnet 原生币**已完成真实钱包仅签名、后端校验、平台出资提交和 0.01 TON 到账。平台端没有读取用户助记词；用户授权由原钱包确认页和 PIN 完成。

首次初始化与普通付款必须分开看。本次先由平台提供测试启动资金，再通过钱包 `sendTransaction` 广播 0.001 TON 自转，使用户 seqno 从 0 变为 1。之后的付款才使用 `signMessage` 和平台中继。**这不证明新钱包从 seqno 0 起就能满足全部交易由后端发起的目标。** 前轮首次签名有效期为 `0xffffffff` 的限制仍有效，初始化方案尚待产品与通道设计确认。

本轮没有接入业务订单、美元账本、正式费用规则或生产钱包白名单，也没有验证 TON USDT。平台预付测试启动资金和中继多余资金留在用户钱包，均不自动成为产品规则。

| 检查 | 实际结果 |
| --- | --- |
| 测试币到账 | 开始操作时平台有 1.999999999 TON；此前有一笔 2 TON 入账及另一笔小额退回交易，不推断两笔发送者身份 |
| 用户首次初始化 | 平台转入 0.05 TON，原钱包广播 0.001 TON 自转，W5 激活且 seqno 0 → 1 |
| 初始化后的短有效期 | 请求与实际签名均为 `1793710834`，恢复为请求的 300 秒窗口 |
| 原钱包仅签名 | SignMessage 返回真实签名；界面提示由应用发送并承担网络费用 |
| 提交前校验 | 账户、公钥、W5 状态、网络、seqno、期限、动作数、金额、目标及消息内容通过；8 类反例被拒绝 |
| 平台提交与收款 | 三笔关联交易成功，用户向平台发送本金 0.01 TON；双方 seqno 1 → 2 |
| 链上确认 | 三笔交易均归入主链区块 95524260；后续只读复核时主链头为 95536956 |
| 即时重放 | 原平台外部消息尚未过期时再次提交，节点返回 exit code 133；双方余额、seqno、最近交易均未变 |
| 费用核对 | 实际网络费 0.001163336 TON；另有 0.002577573 TON 中继余款留在用户钱包 |

## 环境、账户与原钱包证据

继续使用前轮独立 Android 15 AVD、官方原版 Tonkeeper 26.10.4、TON Connect bridge 与临时 HTTPS manifest。安装包来源和 SHA-256 见[前轮环境记录](14TON钱包连接与签名验证.md)。Node 24.22.1，`@tonconnect/sdk` 5.0.0、`@ton/ton` 16.3.0、`@ton/crypto` 3.3.0、Playwright 1.64.1，沿用探针锁文件，没有新增依赖。

| 角色 | 测试网地址 |
| --- | --- |
| 用户 W5 | `0QCjk6WUaIpT9hONiPNEeomRCTzDh_BGPQrNlVKJ-tvveqrM` |
| 平台 W5 / 收款人 | `0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp` |

用户公钥为 `91a0d3523098850dc441f525f5fb22c8e25a7470a3888aada6868b093b37cb51`，网络标识 `-3`。地址 raw 格式、余额、seqno 与最近交易保存在附件 JSON 中。客户端测试网界面显示 GRAM，本项目仍沿用 TON 资产名称；截图价格不是项目报价依据。

- [初始化原生确认页](attachments/ton-native-relay-2026-11-03/initialization-preview.png)：0.001 TON 自转，属于钱包发送交易。
- [付款 SignMessage 原生确认页](attachments/ton-native-relay-2026-11-03/payment-sign-preview.png)：显示 0.01 TON、平台收款地址及由应用提交和支付网络费用的提示。该截图来自首次付款签名；限流后重新签署相同金额和目标，成功使用的签名时间见 preflight。
- [成功提交前校验摘要](attachments/ton-native-relay-2026-11-03/preflight.json)：真实签名于 12:56:38 UTC 返回，12:57:05 UTC 校验通过，记录钱包版本、seqno、期限及消息体哈希。

[官方 SignMessage 指南](https://docs.ton.org/applications/ton-connect/how-to/sign-message-gasless)仍将该能力标为 Alpha，并说明 Wallet V5 仅签名与应用中继的流程。此次通过依据是实际客户端与链上证据，不把指南中的 Jetton 示例等同于 TON USDT 已通过。

## 初始化与普通付款的边界

平台首次用自己的密钥激活平台 W5，同时向未初始化的用户地址发送 0.05 TON。用户接收记录的 compute phase 为 `skipped/no-state`、`aborted: true`，但 non-bounce 消息的 0.05 TON 已完整进入未初始化账户，不能仅凭这一标志把预充值判断成未到账。

随后原钱包 `sendTransaction` 完成 0.001 TON 自转，用户账户激活，本金返回自身。此次初始化成本为 0.001072338 TON，用户余额变为 0.048927662 TON。平台激活及准备资金这一步另花费 0.000980004 TON。二者都不并入后续 0.01 TON 普通付款的网络费。

初始化证据为 [fund.json](attachments/ton-native-relay-2026-11-03/fund.json)、[初始化后快照](attachments/ton-native-relay-2026-11-03/after-initialize.json)及[链上交易摘要](attachments/ton-native-relay-2026-11-03/chain-report.json)。对应交易哈希：

| 交易 | 哈希 |
| --- | --- |
| 平台激活并提供启动资金 | `581257b5c1ab6408c74b49078da09391b012352e996647b336f8f21a503469a2` |
| 用户接收 0.05 TON | `2b61c56d406a8bcafe9badb4ba06593655dfa81de141df1fb2cffa1435936096` |
| 原钱包广播自转、激活用户 W5 | `3935ad42a2f01c1c4d6ce9c259feab2120680633435bafe4dd9a3cf0202ac6c8` |
| 用户接收自转本金 | `0d9c707879c06a0b1c4cba905c9bb68baff11b0e6c5ccc2a5dc44559a89fc29a` |

后续开发要单独明确新账户如何初始化、谁承担启动成本及用户如何确认。本轮没有放宽 seqno 0 授权限制，也未把“先给用户充值再让钱包发送”采纳为默认业务流程。

## 提交前校验、限流与即时重放

`ton-live.mjs` 固定本次测试网、用户、平台和 0.01 TON 本金。后端验证 W5 code/data、网络 walletId、公钥、启用签名且无额外扩展的账户状态；验证实际签名、当前 seqno、与请求一致的短期限；只接受一个 mode 3 的原生币发送动作，目标必须为平台，金额必须为 10000000 nanotons，拒绝附加 payload、StateInit 或其他货币。外层目标必须为用户 W5。平台只读取自己的专用助记词，给中继内部消息附带 0.003 TON，再通过平台 W5 提交。

实际 8 类拒绝检查为：错误网络、改动请求金额、改动请求收款人、改动请求期限、改动外层中继目标、篡改签名消息、过时 seqno、过期授权。这些检查使用真实钱包返回值，不是模拟钱包生成的签名。它们也不代表生产资金入口已经实现完整校验。

前两次提交遇到 HTTP 429，提交记录停留在 `prepared`。恢复时先检查平台 seqno、余额与最近交易均未推进，再等旧平台消息过期；不能仅凭 HTTP 错误重新构造并付款。原用户签名也过期后，重新让原钱包签名。改成每次 RPC **响应完成后间隔至少 3 秒**，第三次提交成功。此前单独切换直连仍遇到 429，证据不足以把代理认定为根因。[恢复摘要](attachments/ton-native-relay-2026-11-03/relay-recovery.json)保留前两次 prepared 记录、首次平台消息过期检查和成功提交前的未变化状态；该附件不包含原始 HTTP 错误正文。

成功提交的用户期限为 13:00:34 UTC，平台外部消息期限为 12:59:12 UTC，实际执行于 12:57:15 UTC。整理后的脚本额外限制平台外层期限不得晚于用户授权，并在实际 RPC 调用前检查剩余时间；本次成功交易本身已满足该期限关系。提交状态文件采用独占创建，已有记录时不重复构造新付款；只观察 seqno 前进时输出 `seqno-advanced-needs-trace`，完整成功由后续关联交易与余额核对确认。

12:58 UTC 对同一已消费的平台外部 BoC 做了一次即时重放。节点返回 exit code 133；重放前后平台和用户的余额、seqno、最近交易完全一致，而且检查结束仍早于原平台消息期限。见[重放摘要](attachments/ton-native-relay-2026-11-03/replay-report.json)。本轮没有另签一条新的平台消息去包装旧用户签名；该结果也不能替代业务订单幂等。

## 三笔交易关联与费用

三笔交易发生于 2026-11-03 12:57:15 UTC（北京时间 20:57:15），工作链 0 的分片区块 100713694，主链锚定区块 95524260。后续复核主链头为 95536956。V2 解码结果与 V3 索引逐笔比对，两条内部消息的完整 message hash 首尾对应，同一 trace，计算及 action 阶段均成功、exit/result code 为 0、无 abort 或 bounce。

| 交易 | 哈希 / 查询 |
| --- | --- |
| 平台提交，附带 0.003 TON | [9e065e64…f85696](https://testnet.toncenter.com/api/v3/transactions?hash=9e065e64ea49ba4819b1c306716d33cc236af7229fb85b8a5d310ae3e0f85696) |
| 用户支付 0.01 TON | [a25829c0…ffba4](https://testnet.toncenter.com/api/v3/transactions?hash=a25829c06947349c89015789b193c4a08e2702f538fa144595f265d9f76ffba4) |
| 平台收到本金 | [53d778fb…25e98](https://testnet.toncenter.com/api/v3/transactions?hash=53d778fb27fe4ed339ba2514b5f3938964dcbc48b6b88a66fdea326cf5e25e98) |

平台外部消息哈希为 `453229c88b0d5b803f910b5bb98e3bc2c452d07208d6dd5f7cda8596d0268456`，用户授权消息体哈希为 `47d4beb88f0c02a62dbad28a2fbed2903bafdd2a657b14b308eb03dc54068671`。它们分别与提交交易入口、用户执行入口对应。完整摘要见 [relay.json](attachments/ton-native-relay-2026-11-03/relay.json)、[链上解码](attachments/ton-native-relay-2026-11-03/chain-report.json)和[V3 关联及主链头](attachments/ton-native-relay-2026-11-03/v3-confirmation.json)。

| 本次普通付款费用构成 | nanotons | TON |
| --- | ---: | ---: |
| 平台提交成本，不含给用户的 0.003 TON | 704508 | 0.000704508 |
| 用户执行成本，由附带资金覆盖 | 422427 | 0.000422427 |
| 平台收款执行成本 | 36401 | 0.000036401 |
| **实际网络费合计** | **1163336** | **0.001163336** |
| **附带资金中留在用户钱包的余款** | **2577573** | **0.002577573** |
| **平台补贴总成本，不计收到的本金** | **3740909** | **0.003740909** |

费用计算采用各交易 `totalFees` 加其发出消息的剩余 `forwardFee`：平台提交 `593839 + 110669`，用户执行 `377981 + 44446`，平台收款 `36401`。不能只加三笔 `totalFees`，否则漏掉转发费用；也不能再次叠加完整 `totalFwdFees`，其中 action fee 已计入 `totalFees`。

| 角色 | 付款前 TON | 付款后 TON | 核对 |
| --- | ---: | ---: | --- |
| 用户 | 0.048927662 | 0.041505235 | 前余额 − 0.01 本金 + 0.002577573 余款 |
| 平台 | 1.949019995 | 1.955279086 | 前余额 + 0.01 本金 − 0.003740909 补贴成本 |

用户净减少 0.007422427 TON，因此不能把本次余额变化写成“用户恰好只扣 0.01 TON”。平台收款消息本金确为 0.01 TON，该笔收款自身再支付 0.000036401 TON 网络费，单笔收款净增 0.009963599 TON；业务应按本金还是其他口径记账仍须遵循后续审核的规则。

本次原生币转账没有产生余款退回平台的消息。生产方案要明确附带预算、实际手续费、未用资金以及补贴的归属；不能将 0.003 TON 预算直接称为实际手续费，也不能宣称原生币流程已实现自动结算。下一步 Jetton 的余款处理应独立验证。

## 复现、证据与后续

新增的独立脚本位于 `scripts/payment_probe/`：

| 脚本 | 用途 |
| --- | --- |
| `ton-live.mjs` | 固定测试账户的只读状态/交易报告、启动备资与平台中继；平台端不读用户密钥 |
| `ton-live-wallet.mjs` | 原钱包连接、首次 sendTransaction 与后续 signMessage，原始结果仅保存在本地忽略目录 |
| `ton-live-check.mjs` | 对新鲜真实签名做提交前校验及 8 类拒绝检查 |
| `ton-live-replay.mjs` | 仅在原平台消息尚有效且双方 seqno 已推进时，重提同一消息验证拒绝 |
| `ton-live-evidence.mjs` | 离线核对公开交易关联、确认、初始化成本、完整费用、余额与重放结果，不使用网络或密钥 |

仓库根目录可执行只读或离线核对：

```bash
export PATH=/home/kelizhang/.nvm/versions/node/v24.22.1/bin:$PATH
node scripts/payment_probe/ton-live-evidence.mjs
node --test scripts/payment_probe/probe.test.mjs
NO_PROXY=testnet.toncenter.com node scripts/payment_probe/ton-live.mjs status
```

离线核对输出已保存为 [accounting.json](attachments/ton-native-relay-2026-11-03/accounting.json)。该脚本核对归档证据，不把历史签名当作当前仍有效的授权。`ton-live-check.mjs` 对本次已过期 capture 重新运行应拒绝，不能修改时间伪造新的可提交状态。

真实流程顺序为 `fund` → `ton-live-wallet.mjs initialize` → 初始化确认 → `ton-live-wallet.mjs payment` → 对返回 capture 运行 check → `relay PRIVATE_CAPTURE` → 核对交易与余额。继续实验前先读现有状态；本次 fund/relay 已完成，不删除记录或重复初始化以强行复跑。钱包脚本还需要前轮的本机探针服务、可达 HTTPS manifest 和 AVD，`PAYMENT_PROBE_PUBLIC_ORIGIN` 由当次临时入口设置。

本地 `.tools/payment-feasibility/ton-live/` 保留原钱包 capture、实际提交 BoC、两次未执行的提交记录及私有 RPC 诊断。公开附件只保留审阅后的交易/状态摘要、哈希和无敏感信息截图，不包含助记词、PIN 或可执行签名。收尾检查时临时探针、隧道及模拟器均已不在运行，测试资金保留供后续验证；上述余额是本次确认快照，不是永久余额。

下一项为 **TON USDT / Jetton**：先核实测试代币来源和合约身份，再独立验证真实钱包签名、平台附带 TON、代币钱包开户、收款与余款去向。若只能使用自建测试 Jetton，必须明确它不能证明真实 USDT 合约兼容。首次 W5 初始化、原生币补贴余款的业务规则及正式订单入账仍单列待决。
