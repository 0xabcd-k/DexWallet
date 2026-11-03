# TRON USDT：真实钱包实转与 Energy 来源

[阶段讨论](01讨论记录.md) · [TRX 实转](11TronLinkTRX资源代付实转验证.md) · [前期协议基线](05支付可行性验证.md)

2026-11-03 用户要求继续验证下一项，并协助领取 Nile 测试 USDT。**TronLink 4.11.2 已完成 USDT 真实拒绝/签名、后端校验广播、固化确认、到账和重复提交拒绝。** 用户只减少 0.01 USDT，TRX 仍为零；但 Energy 实际由测试币部署者承担，Bandwidth 使用用户免费额度，**不能将本轮记为项目平台 Energy 代付通过**。钱包代币精度显示异常仍未解决，尚不满足正式用户体验验收。

## 实转结果

| 项目 | 实际结果 |
| --- | --- |
| 网络 / 钱包 | TRON Nile，chain ID `0xcd8690dc`；TronLink 4.11.2 |
| USDT 合约 | `TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf`，精度 6，来自官方 Nile 水龙头 |
| 用户 | `TDHZum2JgP2ZgfnveW9f3VM3UjaafdJXio`，普通单签账户 |
| 平台及收款人 | `TPDBmTxJWYucGk5VjDsSi6jBdvVjpCnCsH` |
| 本金 | `10,000` 最小单位，即 **0.01 USDT** |
| 用户 USDT | `1,000,000,000 → 999,990,000` 最小单位，即 `1000 → 999.99 USDT` |
| 平台 USDT | `0 → 10,000` 最小单位，即 `0 → 0.01 USDT` |
| 用户 / 平台 TRX | 分别保持 `0` / `17,132,000 sun`，没有燃烧 TRX |
| 区块 / 时间 | `72386419`，UTC `2026-11-03 09:01:00` |
| 确认 | Solidity 节点可查交易和回执，交易及 VM 均为 SUCCESS，已固化 |
| Energy | 总计 **29,650**，全部为 `origin_energy_usage`；调用者 Energy 和燃烧费用为零 |
| Bandwidth | **345**，用户 `freeNetUsed` 增加，原委托的 `NetUsed` 没有增加 |

[付款交易](https://nile.tronscan.org/#/transaction/caff6737825af0e5dddb53d148336956a0a62c5a6fa67ff7385f2fe00c2b3006) 的 txID 为 `caff6737825af0e5dddb53d148336956a0a62c5a6fa67ff7385f2fe00c2b3006`。后端核对精确 calldata、签名、Transfer 合约/双方/金额、余额及费用来源，没有只凭广播成功认定到账。[完整证据](attachments/tron-usdt-2026-11-03/tron-usdt-public-result.json) 保存签名交易、固化回执、模拟、资源及余额快照、重放结果。

重复广播返回 `DUP_TRANSACTION_ERROR`，双方 USDT/TRX 均不变；重新执行 `--submit` 复用原交易，`--audit` 只读核对。本轮没有实现正式业务订单、充值入账或账本记录。

## Energy 来源与两网差异

固定 RPC 的 `getcontract` / `getaccountresource` 核对如下，主网仅查询公开数据，没有主网签名或交易。地址参照 [TronLink 网络与地址表](https://docs.tronlink.org/reference/networks/)。

| 项目 | Nile 测试 USDT | 主网 USDT（当次只读对照） |
| --- | --- | --- |
| 合约 | `TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf` | `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t` |
| 部署者 | `TKGRE6oiU3rEzasue4MsB6sCXXSTx9BAe3` | `THPvaUhoh2Qn2y9THCZML3H815hhFhn5YC` |
| `consume_user_resource_percent` | 0，RPC 省略默认零值 | 30 |
| `origin_energy_limit` | 1,000,000,000 | 10,000,000 |
| 部署者可用 Energy | 付款前约 1370 万 | 读取时 EnergyLimit / EnergyUsed 均为 9,669，可用为零 |
| 项目用户及平台 Energy | 均为零 | 未操作项目主网账户 |

百分比只是配置，实际分摊还受部署者资源和单笔上限约束，见 [官方 Energy 分摊规则](https://developers.tron.network/docs/paying-for-resources)。本次回执证明 29,650 Energy 来自**测试 USDT 部署者**，该地址不是项目平台；“用户零 TRX 付款成功”不能证明“项目平台代付已验证”。

平台此前 980 测试 TRX 仍用于 Bandwidth 质押，500 份额委托给用户，本轮没有新增质押/委托或解质押。付款前用户剩余质押带宽 68，不能覆盖 345，免费额度 600 足够，所以实际走免费额度。平台资源、第三方补贴、用户免费额度必须分开核验。

两网 `getcontract.bytecode` 字段 SHA-256 相同，但 `code_hash`、存储和费用参数不同，未据此宣称环境等价。另只读查询官方 Shasta USDT，其用户费用比例也为 0，部署者 EnergyLimit / EnergyUsed 当时均为 14；没有切网实转。[初始预检](attachments/tron-usdt-2026-11-03/tron-usdt-preflight-empty.json) 保留两网参数。读取不是同一区块的原子快照，付款前已重新核对。

**平台 Energy 委托消耗仍待受控对照验证。** 后续需准备实际消耗调用者 Energy 的测试场景及平台资源，不能把另一测试币冒充官方 USDT，也不能假设主网部署者会持续补贴。本轮未引入租赁服务、部署对照合约或修改待审核的收费规则。

## USDT 的 false 返回值

最初用户没有 USDT，模拟返回 `result.result=true`，但 `transaction.ret=[{"ret":"FAILED"}]`，消息为 `REVERT opcode executed`，`constant_result=[""]`，消耗 1,984 Energy。true 只表示节点接受请求；回退消耗不能当成功交易费用估算。这种失败格式已加入测试。

用户领取 1000 测试 USDT 后，模拟不再回退，产生正确的 Transfer 日志、消耗 29,650 Energy，却返回 32 字节零，即 bool false。最初“必须返回 true”的探针因此拦截；[备资后原始模拟](attachments/tron-usdt-2026-11-03/tron-usdt-funded-false-return.json) 保留当时结果。

[公开复现分析](https://gist.github.com/yorhodes/a6eccbeba27ff76355c3d761e84d6a35) 指出 TRON USDT 中间转账方法遗漏返回语句，成功也返回 false。本轮随后真实回执同样返回全零，但 VM SUCCESS、精确 Transfer 事件和双方余额变化共同证明实际转账成功，没有用外部分析代替实转证据。

兼容仅限固定 Nile USDT：精确校验调用、拒绝 FAILED/REVERT、检查布尔字和匹配的模拟事件后才接受 Energy 估算；实际确认还核对余额。**不能推广成任意 TRC-20 的 false 都可忽略**；缺事件、错合约、错金额仍拒绝。

收款地址已有 0.01 USDT 后，同额的[再次只读模拟](attachments/tron-usdt-2026-11-03/tron-usdt-post-payment-preflight.json) 估算降为 **14,650 Energy**；未广播第二笔付款。这说明估算受收款状态影响，不能把首次交易的数字写死成 USDT 固定费用。

## TronLink 精度警告与签名

沿用已校验的官方 4.11.2 扩展及独立 profile。网络、账户及链上 `decimals=6` 正确，但原签名页先缺金额，随后显示 **`10,000 undefined`** 和 `Token precision error`，按精度 0 展示。延长等待和对 Nile 相关域名单独直连均未解决，尚未定位到钱包服务端、客户端或网络的具体根因，不能称已经修复。

按 [官方添加资产接口](https://docs.tronlink.org/plugin-wallet/active-requests/) 尝试时，新 `window.tron.request` 返回 4200；`window.tron.tronWeb.request` 不存在；旧 `window.tronLink.request` 能弹原生界面，但显示 Invalid Asset / Cannot be added，已取消。见 [结果](attachments/tron-usdt-2026-11-03/tron-usdt-watch-asset-result.json) 与 [截图](attachments/tron-usdt-2026-11-03/tron-usdt-watch-asset-invalid.png)。未强行添加或修改钱包存储，实际版本能力优先于文档描述。

普通 `--sign usdt` 保持拦截异常金额。为完成限定金额的协议验证，本轮显式使用 `--sign-metadata usdt`，在原生 Metadata 核对 Nile、完整双方、官方合约、`transfer(address,uint256)`、`value=10000`，以及与后端预存内容完全相同的 `raw_data_hex`。后端再次解析为 0.01 USDT，无附带 TRX、approve、其他合约或 memo。

原窗口先 Reject，返回 `Confirmation declined by user`；再对同一交易 Sign，得到一个真实用户签名。两次后均确认交易未广播，后端随后才提交。没有修改扩展、隐藏警告、伪造钱包接口或使用用户私钥代签；DApp 明确显示 0.01 USDT。**这不是要求生产用户忽略精度警告的方案**，钱包 UI 仍不通过验收。

证据：[警告及详情](attachments/tron-usdt-2026-11-03/tron-usdt-wallet-sign.png)、[Metadata](attachments/tron-usdt-2026-11-03/tron-usdt-wallet-metadata.png)、[拒绝](attachments/tron-usdt-2026-11-03/tron-usdt-wallet-reject.png)、[钱包摘要](attachments/tron-usdt-2026-11-03/tron-usdt-wallet-result.json)，其中明确保留 `humanAmountPreviewCorrect=false`。

首个十分钟草稿 `470bc35070d8d6c2b2d3f5152cdd754cad2bdfa8523a71fb0689aba2321fa9df` 排查期间过期，确认无签名、无付款状态、链上查无交易后单独归档。最终草稿有效期 **30 分钟**，固定 0.01 USDT，`fee_limit=15,000,000 sun` 仅是 Energy 预算上限，不是估算或独立 fee payer；未丢弃结果未知的付款重试。

## 复现与交付

使用 Node 24.22.1、TronWeb 6.5.1、Playwright 1.64.1 及原依赖，无新增依赖。先前领取的人机验证证据见 [状态](attachments/tron-usdt-2026-11-03/tron-usdt-faucet-attempt.json)、[页面](attachments/tron-usdt-2026-11-03/tron-usdt-faucet.png)，该阻塞已由用户领取解决。没有购买主网资产。

```sh
# 只读预检；无币或失败模拟时退出码 2，无密钥访问/签名/广播。
node scripts/payment_probe/tron-usdt-check.mjs
# 以下是新验证环境顺序，本次成功状态不得清空盲目重跑。
node scripts/payment_probe/tron-usdt-live.mjs --prepare
node scripts/payment_probe/tron-live-wallet.mjs --sign usdt
# 当前金额展示异常时，普通 sign 拒绝；本轮显式核对原生 Metadata：
node scripts/payment_probe/tron-live-wallet.mjs --sign-metadata usdt
node scripts/payment_probe/tron-usdt-live.mjs --submit
node scripts/payment_probe/tron-usdt-live.mjs --replay
node scripts/payment_probe/tron-usdt-live.mjs --audit
node --test scripts/payment_probe/tron-usdt.test.mjs scripts/payment_probe/tron-live.test.mjs
python3 scripts/check_docs.py
git diff --check
```

`tron-usdt-state.json` 和 `tron-usdt-wallet-signed.json` 与旧 TRX 状态分开。prepare 拒绝覆盖订单，签名拒绝已提交付款；原交易及 txID 在广播前原子落盘，文件锁防并发，结果未知沿旧 txID 查询，不自动重签。广播前复查资源不等于原子预留第三方额度。后端不读取任何私钥；密钥、密码和 profile 不入库，发布的签名交易均已上链消费。

钱包探针默认仍为 TRX，新增显式 usdt 参数及对应校验器。13 项新行为测试和 10 项原 TRX 测试通过；真实实转、固化、重放、重复 submit、只读 audit、JS 语法、文档和差异检查通过。原始失败与最终证据分别保留。当前结论是“真实钱包到后端的 USDT 转账可执行”，平台 Energy 代付和钱包金额展示仍需完成。

修改后还实际执行默认 TRX 的 `--inspect`，重启钱包、解锁、连接和 Nile 地址核对通过，见[回归读取](attachments/tron-usdt-2026-11-03/tronlink-trx-regression-inspect.json)。
