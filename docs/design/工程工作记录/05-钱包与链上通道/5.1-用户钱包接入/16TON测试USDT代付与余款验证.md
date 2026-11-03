# TON：测试 USDT 代付、收款钱包创建与余款验证

[阶段讨论](01讨论记录.md) · [原生币实转](15TON初始化与原生币代付实转.md) · [阶段目录](../README.md)

## 结论与适用边界

2026-11-03 已完成 **官方 Tonkeeper Android 26.10.4 + 已初始化 W5 + TON testnet 社区测试 USDT** 的真实签名、平台校验及提交、收款代币钱包首次创建、1 枚代币到账、余款返回与即时重放拒绝。用户代币余额从 10 变为 9，平台从 0 变为 1。领取 10 枚测试币也由原钱包 SignMessage 后交平台执行，无需本轮人工领取。

本轮代币是 TON 社区支付示例使用的测试资产，文档称 USDTTT，链上 Metadata 显示 Testnet USD / USDT，钱包显示 USD₮ 和 Unverified Token。**不把名称相同当作 Tether 发行证明。** 测试主合约、代币钱包与官方稳定币仓库的编译产物一致；主网 USDT 钱包采用同一代码的库引用形式，已做只读哈希核对。该结果支持此代码路径的可行性，但不代表主网发行方状态、冻结政策、库部署、网络费用或全部钱包版本已验收。

没有发送主网交易、使用第三方 Gasless 服务或读取用户助记词代签。实际平台提交仅读取平台专用测试助记词。首次 W5 初始化限制仍沿用[原生币记录](15TON初始化与原生币代付实转.md)，业务订单、中心化入账和正式收费政策仍未接入。

| 验证项 | 结果 |
| --- | --- |
| 代币身份 | 固定 master、6 位精度、两方代币钱包由 master 派生；owner、master、代码与状态复核通过 |
| 原钱包签名 | SignMessage 原生预览显示发送 1 USD₮ 至平台，确认与 PIN 后返回签名；钱包不广播 |
| 提交前校验 | W5 代码/公钥/网络/期限/seqno、单动作、代币钱包、数量、收款人、余款目标和附带 TON 等通过 |
| 反例 | 付款的 12 类反例均被拒绝，包括数量、收款人、余款地址和签名消息篡改 |
| 后端出资与执行 | 平台给用户 W5 附带 0.053 TON，用户 W5 将 0.05 TON 转入自己的代币钱包执行 transfer |
| 首次收款 | 平台代币钱包由未初始化变为 active，余额为 1000000 最小单位，即 1 枚 |
| 余款 | `response_destination` 为平台；收到 opcode `0xd53276db`、query_id `2026110301` 的 0.046810895 TON |
| 链上确认 | 付款完整 trace 含 6 笔交易、5 条内部消息关联，主链区块 95543880；V3 返回 finalized、非模拟、无待处理消息 |
| 即时重放 | 原外部消息未过期时重复提交，exit code 133，双方及两个代币钱包的余额和最近交易均未变化 |
| 通知边界 | 1 nanotON 到账通知到达平台 W5，但计算阶段因 no-gas 跳过；代币收款和余款处理成功，不能因此整单判失败 |

## 来源、代码与账户

来源为 [TON 社区支付示例](https://github.com/ton-community/tma-usdt-payments-demo)，其 README 给出测试 master 和领取 Bot。TON [支付处理文档](https://docs.ton.org/applications/payments/setup)说明真实 USDT 位于主网，测试币不按符号识别为正式资产。代码使用 [stablecoin-contract 固定提交](https://github.com/ton-blockchain/stablecoin-contract/tree/5a3b500267b0bdfc6505a08e5ac661c805cab8b0)的现有编译产物核对，未自行修改合约或发行同名币。

| 对象 | 值 |
| --- | --- |
| 测试 master | `kQD0GKBM8ZbryVk2aESmzfU6b9b_8era_IkvBSELujFZPsyy` |
| 主网只读参考 master | `EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs` |
| 测试领取合约 / master 管理地址 | `0:cd503242d2242f268669d2697e62d78bd4db535caa61534dc24c8e6fe15b4184` |
| 用户 W5 | `0QCjk6WUaIpT9hONiPNEeomRCTzDh_BGPQrNlVKJ-tvveqrM` |
| 平台 W5 | `0QAQB2nm2uV5z7xcoWN8iNbIOmwhlbPlcfHjXyC0Wy-k0Edp` |
| 用户代币钱包 | `0:90a51a0fd3524a8db2fd03b66567fa3c4c6f5b2153df8340558a440e10e897c2` |
| 平台代币钱包 | `0:db0cadcf093b72ab1d5cdd0db6daec3be55fab0c6a0665a99cbdc561a81248a0` |
| 主合约代码哈希，测试与主网一致 | `GNW254D/C7RRJUwsdg0J1uSFY4zRQHq7lweHUsPBye4=` |
| 编译后的钱包代码 / 测试钱包代码哈希 | `j0Utek39dAZraCNlF3JZ7QVzRDW+drX9S9XYryt8PWg=` |
| 上述代码的 library reference 哈希 / 主网钱包代码哈希 | `iUaPAseOVwgC45l5yFFvw43wfqdqSDV+BTbyuns+43s=` |

[来源证明](attachments/ton-jetton-relay-2026-11-03/source-proof.json)保留源码提交、下载归档 SHA-256、代码和库引用核对结果；[master 只读快照](attachments/ton-jetton-relay-2026-11-03/master-reference.json)保留链上返回。哈希关系由离线脚本复核，主网部分没有签名或转账。

运行环境沿用前轮 Android 15 独立 AVD、Node 24.22.1、TON Connect SDK 5.0.0、@ton/ton 16.3.0、@ton/crypto 3.3.0 和 Playwright 1.64.1，无新增依赖。钱包仍显示 GRAM，项目沿用 TON 名称。

## 测试币准备与网络诊断

在领取合约的真实成功交易中核对到消息格式 `opcode 0x29577499 + query_id:uint64 + amount:uint64`。本次请求 10000000 最小单位，即 10 枚测试币，用户签名动作附带 0.049 TON；平台中继另带 0.003 TON 执行预算，共 0.052 TON。原钱包预览显示将收到 10 枚 Testnet USD，随后实际到账并创建用户代币钱包。[领取预览](attachments/ton-jetton-relay-2026-11-03/claim-preview.png)、[领取校验](attachments/ton-jetton-relay-2026-11-03/claim-preflight.json)、[提交与余额](attachments/ton-jetton-relay-2026-11-03/claim-relay.json)和[完整领取 trace](attachments/ton-jetton-relay-2026-11-03/claim-trace.json)均已保留。

领取 trace 的主链区块为 95543171，7 笔交易均已 finalized，其中 1 笔微额通知跳过计算。平台账户净支出 0.052741075 TON，但这不是全额网络费：实际网络费为 0.004037778 TON；用户 W5 净增加 0.019684491 TON，用户代币钱包保留 0.000974433 TON，领取合约与 master 的 TON 余额也有增加。离线脚本按全部参与账户核对；这些属于测试备资，不采用为项目收费或补贴方案。

前轮之后两个测试地址出现其他小额入账，故本轮采用当次链上快照作为起点，没有直接沿用原生币记录的余额，也未访问入账备注中的推广链接。

第一次钱包连接在原生确认后 180 秒仍未收到连接回执，未取得签名、未提交交易。随后为独立 Chromium 显式设置本机 HTTP 代理，并为专用模拟器设置 `10.0.2.2:7890`，连接与签名成功。该实验同时改变两端网络路径，不能据此断言只有某一端是根因；analytics 请求仍有中止日志，但签名流程完成。收尾已将模拟器代理恢复为原来的空值并停止 AVD、探针与临时隧道。

V2 读取 master 的较长交易历史曾返回 `LITE_SERVER_UNKNOWN / lt not in db`，没有把它解释为转账失败或重新付款。改用 V3 的完整 trace，并与付款 V2 解码、主链头和账户余额交叉核对。[RPC 诊断](attachments/ton-jetton-relay-2026-11-03/rpc-history-diagnostic.json)保留具体错误。

## 付款、校验与链上落款

付款由原钱包签署一个 `transfer` 动作：目标为用户在固定测试 master 下的代币钱包；数量为 1000000；收款 owner 与 `response_destination` 都为平台 W5；`forward_ton_amount = 1`；`query_id = 2026110301`；无额外 payload、StateInit 或其他动作。0.05 TON 是动作执行预算，平台实际向用户 W5 附带 0.053 TON。金额与预算均以整数最小单位处理。

后端复核真实 W5 签名、公钥、代码、网络 walletId、当前 seqno、与请求一致的短有效期、mode 3 和完整已签消息体。代币钱包从 master 派生，复核 owner/master/code，并检查发送与接收锁定状态；本轮均为未锁定。原钱包[付款确认页](attachments/ton-jetton-relay-2026-11-03/payment-preview.png)显示发送 1 USD₮ 和平台地址，仍标注测试币未验证；截图美元总额不作为项目报价。

12 类拒绝检查：错误网络、改动附带 TON、改动代币钱包、改动 payload、改动期限、篡改已签消息、改动代币数量、改动收款人、改动余款地址、改动转发 TON、过时 seqno、过期授权。见[真实签名校验](attachments/ton-jetton-relay-2026-11-03/payment-preflight.json)。领取发生在新增部分付款反例之前，领取校验记录实际为 7 类，不追记成 12 类。

付款于 2026-11-03 15:08:58 UTC（北京时间 23:08:58）执行，双方 W5 seqno 3 → 4。完整关联如下，哈希和 message hash 见[付款 trace](attachments/ton-jetton-relay-2026-11-03/payment-trace.json)及[V2 解码](attachments/ton-jetton-relay-2026-11-03/payment-chain-report.json)：

| 交易 | 哈希 | 结果 |
| --- | --- | --- |
| 平台提交并附带 0.053 TON | `feedec45670c6bfc76d4b8231280c3517781757bf46e9baf89d7599449351922` | 成功 |
| 用户 W5 执行已签消息 | `9db7f5cd1c034c6c4a320477eded3684ce28c4a5c9c0c2d0e3a93b2426ba39dd` | 成功，向用户代币钱包提供 0.05 TON |
| 用户代币钱包发送 1 枚 | `8f28e12a9091da104e62ace52bb0fc7ebeac3af5e3b1a9d0f1ac4fe8bd783e88` | 成功，余额 10 → 9 |
| 平台代币钱包创建并接收 | `d50f0d7f4d8691b09b8951c37306be48e0002e59bb7e68c25d1f19002025a230` | 成功，余额 0 → 1 |
| 1 nanotON 到账通知到平台 W5 | `4d4736089e7289dc92e76747fb308dc2be68df20372320f595ca4435fd0133da` | `aborted: true`，计算 `skipped/no-gas`，1 nanotON 已记入余额，无 bounce |
| excess 余款到平台 W5 | `d994b6ba1ec091b4b77de3a978f09c03de3a1cc487766344a181033958ff6ffc` | 成功，收到 0.046810895 TON，再付自身接收费用 |

通知交易与实际收款应分开判断。本轮平台收款账户是普通 W5，1 nanotON 不足以运行其代码，但收款代币钱包已经成功执行 `internal_transfer` 并持有代币；不能要求整条 trace 每一笔都 `aborted = false`。正式入账仍须验证指定 master、owner/代币钱包、实际转入数量、状态与交易关联，不能单靠通知文本或 query_id 入账。本轮的关联没有依赖订单 memo，但尚未实现业务订单映射和幂等。

即时重放原平台外部消息返回 exit code 133。检查结束时间 15:10:48 UTC，早于该消息 15:10:55 UTC 的期限；双方余额、seqno、最近交易以及两个代币钱包的余额/最近交易均未变化。[重放证据](attachments/ton-jetton-relay-2026-11-03/payment-replay.json)不包含可执行 BoC。未用新的平台签名包装旧用户授权。

## 费用、余款与保留资金

下列仅为这笔 1 枚测试币付款，不含领取成本。各交易费用采用 `totalFees + 发出消息的剩余 forwardFee`，避免漏算或重复计算转发费。

| 网络成本 | nanotons | TON |
| --- | ---: | ---: |
| 平台提交 | 809016 | 0.000809016 |
| 用户 W5 执行 | 473881 | 0.000473881 |
| 用户代币钱包执行 | 1365545 | 0.001365545 |
| 平台代币钱包创建及接收 | 849136 | 0.000849136 |
| 余款到平台的接收处理 | 51868 | 0.000051868 |
| **实际网络费** | **3549446** | **0.003549446** |

| 资金去向 | nanotons | 说明 |
| --- | ---: | --- |
| Jetton excess 退回平台 | 46810895 | 扣自身接收费用前的消息金额，不是手续费减免额度 |
| 留在用户 W5 的中继余款 | 2526119 | 0.003 TON 执行预算减用户 W5 成本，未自动回平台 |
| 新平台代币钱包保留 TON | 974433 | 合约保留资金，与已经支付的网络费分列，不承诺自动退回 |
| 原用户代币钱包保留资金减少 | 10 | 既有储存费消耗；该钱包的准备资金来自上一笔平台出资领取 |
| **平台 W5 净支出** | **7049988** | 网络费 + 用户 W5 余款 + 新钱包保留资金 − 既有保留资金消耗 |

| 账户 | 付款前 TON | 付款后 TON | 代币变化 |
| --- | ---: | ---: | --- |
| 平台 W5 | 1.962434266 | 1.955384278 | 不直接存放 Jetton |
| 用户 W5 | 0.122034111 | 0.124560230 | 不直接存放 Jetton |
| 用户代币钱包 | 0.000974433 | 0.000974423 | 10 → 9 |
| 平台代币钱包 | 0 | 0.000974433 | 0 → 1 |

四个账户的 TON 总减少量恰好为实际网络费。用户 W5 没有净扣 TON，但不能把“所有用户关联账户的 TON 都不减少”写成保证；既有代币钱包保留资金确实消耗了 10 nanotons。平台应区分报价预算、实际网络费、合约保留资金和补贴余款。本次测试不裁定最终向用户收多少，也未实现按实际成本自动结算。

## 证据、复现与下一步

公开附件不含助记词、PIN、原始签名或可执行平台消息。原始 capture、BoC、RPC 诊断、源码归档和界面操作日志保存在本机忽略目录 `.tools/payment-feasibility/ton-jetton/`。签名现已过期或被消费，不得删除提交记录来强行重复付款。

新增脚本：`ton-jetton.mjs` 负责固定测试资产的链上状态、消息校验、领取/付款中继、交易报告和即时重放；`ton-jetton-wallet.mjs` 负责原钱包连接及仅签名；`ton-jetton-evidence.mjs` 负责离线核对代码关系、完整消息关联、主链确认、每个账户资金变化、代币数量和重放结果。

```bash
export PATH=/home/kelizhang/.nvm/versions/node/v24.22.1/bin:$PATH
node scripts/payment_probe/ton-jetton-evidence.mjs
node --test scripts/payment_probe/probe.test.mjs
NO_PROXY=testnet.toncenter.com node scripts/payment_probe/ton-jetton.mjs state
```

离线结果保存为 [accounting.json](attachments/ton-jetton-relay-2026-11-03/accounting.json)。V3 证据分别使用 `GET /api/v3/traces?trace_id=<根交易哈希>` 获取；主链头 [95544742](attachments/ton-jetton-relay-2026-11-03/confirmation-head.json)晚于两次执行区块。V2 `report claim` 若受旧历史缺失影响会明确报错，不能用空结果当作未发生交易。

实转顺序为原钱包 `claim` 签名 → `relay claim CAPTURE` → 领取到账核对 → 原钱包 `payment` 签名 → `relay payment CAPTURE` → 付款到账核对 → 未过期时 `replay payment` → 完整 trace 与余额核对。钱包脚本依赖前轮本机探针、当次 HTTPS manifest 和 AVD；本轮通过时显式使用 `PAYMENT_PROBE_BROWSER_PROXY=http://127.0.0.1:7890`，`PAYMENT_PROBE_PUBLIC_ORIGIN` 不复用已关闭的随机隧道域名。

下一项为首版范围内尚未独立验证的 **Solana USDT**。TON 后续仍需验证主网库引用环境的成本与状态差异、收款代币钱包复用时的成本、首次 W5 初始化和最终费用归属，并在实际开发阶段接入订单状态、链上确认及账本入账。普通测试通过不自动开放正式通道。

后续 [Solana USDT 代付机制验证](17SolanaUSDT代付机制验证.md) 已完成主网资产 / 程序只读核对与自建 Devnet 测试币的真实钱包代付，明确区分机制通过和官方资产实转。
