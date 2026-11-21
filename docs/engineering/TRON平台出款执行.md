# TRON Nile 平台出款

5.3 第十六、十七批接通内部执行器 `chainsend.NewTRON`。平台测试账户使用单个 owner 权限签名，可向指定地址发送原生 TRX 或显式配置的 Nile 测试 USDT。显式执行工具为 `backend/tools/tron_payout_nile`。本批覆盖测试网出款通道，用户提现订单、自动恢复调度、生产签名和费用结算仍待接入。

## 原单与签名

原生 TRX 金额精确到 sun，1 TRX 等于 1,000,000 sun。准备时校验 Base58Check 收付款地址、正整数金额和稳定请求键，向已核对创世块的 Nile FullNode 读取引用区块。原单保留完整引用块 ID、高度、时间戳、十分钟有效期、原始 protobuf 字节及其 SHA-256 txID。相同请求键只能复用相同付款人、收款人和金额。不同请求键碰到完全相同的交易字节时返回冲突，原请求继续持有该 txID。

原生 TRX 签名消息只含标准 TransferContract。链上签名覆盖协议规定的引用块片段、时间戳、到期时间、双方地址和本金。测试签名器以单独查询的 Nile 创世块和引用块为依据，用 TronWeb 独立编码并核对原始字节与 txID。Go 执行器重建授权字段并恢复签名者地址，先保存原签名，再将相同字节交给节点。[TRON 交易格式](https://developers.tron.network/docs/tron-protocol-transaction) · [官方签名及广播流程](https://developers.tron.network/docs/api-signature-and-broadcast-flow)

## 测试 USDT 的授权、到账与费用

`PrepareUSDT` 固定 Nile 网络、平台地址、收款地址、具体测试代币合约、十进制最小单位金额和以 sun 计的 `FeeLimitSun`。两个测试币均为六位小数，各自使用包含合约地址的资产 ID。同一请求键修改其中任一付款字段会产生冲突。执行器在新建、绑定签名和广播时检查服务端配置的合约。更换配置后，已有签名的交易仍可按原合约核对固化结果。

签名只授权一个 `TriggerSmartContract` 调用，目标是预定合约的 `transfer(address,uint256)`，附带 TRX 为零。Go 和离线 TronWeb 分别生成完整字节并比较 txID。费用上限随原交易签名，控制调用方为 Energy 承担的最高成本。带宽及实际燃烧的 TRX 另按回执记录。[TRON 合约调用](https://developers.tron.network/reference/triggersmartcontract) · [FeeLimit 与 Energy](https://developers.tron.network/docs/feelimit)

确认出款需要固化交易的原签名、成功执行状态、区块包含证明，以及授权合约发出的唯一一条 `Transfer` 日志。核对该日志的发送方、收款方、金额并保存它在完整回执中的零基序号。两个测试合约都实测返回全零 ABI 字，但均发出匹配日志且两端余额实际变动。查无日志、出现多条候选日志或金额不符时保留原单供复查。用户提交的普通到账核验继续指定日志序号。

## 恢复与过期

| 原 txID 的观察结果 | 执行器的行为 |
| --- | --- |
| 已保存实收，或 Solidity 节点返回成功的固化交易和回执 | 核对原签名、发送人、收款人、本金、实付费用和固化区块，保存实收并确认出款。实收已提交而状态尚未写回时可离线恢复。 |
| 只看到部分固化证据，或 FullNode、待打包池已看到原交易 | 保留原单，等待固化。 |
| 两份固化查询均为空，且 FullNode 和待打包池均明确返回空对象 | 对尚未取得成功广播应答的有效授权，登记一次尝试并广播原字节。已有成功应答的单据继续观察。 |
| 节点断开、响应畸形、应答哈希不符或广播结果未知 | 保留原签名和尝试记录，下一次先查原 txID。 |
| 授权已过期，尚无固化实收 | 停止广播，继续核对原 txID。晚到的固化实收仍可完成原单。 |
| 执行失败或授权与链上证据不一致 | 返回可诊断错误，由调用方复查原单。 |

过期只关闭本次授权的广播窗口。判定资金结果仍需固化链证据，超时或单次空查询不能触发退款、换引用块或重新签名。账户资源消耗和实际燃烧的 TRX 分别保存在回执中。[TRON 广播错误与过期复查](https://developers.tron.network/docs/broadcast-and-rpc-errors) · [TRON 到账核验](TRON到账核验.md)

## 隔离复验

数据库要求 business 30。测试工具使用独立登记的测试库，从项目根目录加载专用测试配置。将工作目录 `work`、平台测试密钥文件 `testkey` 设为绝对路径，密钥文件只供离线签名进程读取。示例 policy JSON 如下。

```json
{"Network":"tron-nile","Key":"nile-native-example-01","Sender":"平台测试地址","Recipient":"已激活的测试收款地址","Sun":1000}
```

对于测试 USDT，在运行工具的 shell 通过 `DEXWALLET_CHAIN_TRON_NILE_TEST_USDT_CONTRACT` 或 `-usdt-contract` 配置经过核对的具体测试合约，并另行准备授权。以下为本批水龙头合约的参数示例，对照合约应使用各自的配置和请求键。

```json
{"Network":"tron-nile","Key":"nile-usdt-example-01","Sender":"平台测试地址","Recipient":"已激活的测试收款地址","Sun":0,"TokenContract":"TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf","TokenAtoms":"1000","FeeLimitSun":3000000}
```

```bash
(cd backend && go build -o "$work/probe" ./tools/tron_payout_nile)
probe() { "$work/probe" -state "$work/run.json" -policy "$work/policy.json" "$@"; }
probe -action prepare > "$work/prepared.json"
```

使用可信 Nile FullNode 的 `/wallet/getblockbynum` 分别查询区块 0 和 prepared 中的 `RefHeight`。在 `reference.json` 保存对象，`genesis` 填区块 0 的 blockID，`block` 填引用块的查询结果。签名器会核对高度、块 ID 和时间。测试工具的签名入口接受 JavaScript 安全整数范围内的 sun 值。

```bash
node scripts/payment_probe/tron-payout-sign.mjs "$work/policy.json" "$work/prepared.json" "$work/reference.json" "$testkey" "$work/signature.hex"
probe -action bind -signed-file "$work/signature.hex"
probe -action step
probe -action status
```

等待固化期间反复对同一 run 执行 `step`。确认后用 `evidence` 导出原单、实收和 RPC 证据，再用 `cleanup` 清理本次专用库。当前实转、故障注入和复验结果见[TRX 和 USDT 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
