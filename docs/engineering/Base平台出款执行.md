# Base Sepolia 平台 ETH 出款

5.3 第十三批提供内部执行器 `chainsend.NewBase` 和显式测试工具 `backend/tools/base_payout_sepolia`。平台测试账户向收款地址发送原生 ETH，并支付网络费。用户订单、账本结算、生产签名和自动调度由后续阶段接入。

## 授权与确认

1. 调用方给出稳定的请求键、收款地址、wei 金额、gas limit、每 gas 最高单价和优先费上限。金额和单价为十进制整数字符串。执行器核对 Base Sepolia 链 ID，从节点取得平台付款地址的 pending nonce，将授权字段和待签摘要一起保存。
2. 同一业务库中，付款地址和 nonce 唯一。同键同参数可重复准备。更改原键对应的目标、金额或费率会返回冲突。若另一个请求已占用当前 nonce，调用方须先处理该笔，待节点返回可用的新 nonce 后再准备下一笔。未签名的原单也保留占用。
3. 独立签名进程核对测试授权并签署 EIP-1559 类型 2 交易。消息使用空 calldata 和空访问列表。执行器使用 go-ethereum 解析签名交易，核对链 ID、签名摘要和恢复出的付款地址，在数据库保存完整签名交易和本地交易哈希后才允许广播。[EIP-1559 交易格式](https://eips.ethereum.org/EIPS/eip-1559)
4. 每次执行先按该哈希核对已存实收证据或向可信 RPC 取证。外层原生币转账、金额、付款人、收款人、交易位置、规范区块和成功回执均须匹配。节点返回的签名交易字段也须能重算原哈希。达到 finalized 且实收事件写入后，出款单才标记为 confirmed。[Base 到账核验](Base到账核验.md) · [Base 最终性](https://docs.base.org/specifications/transactions/transaction-finality)

| 查询结果 | 原单的下一次处理 |
| --- | --- |
| 交易和回执均明确返回 null，pending nonce 与原交易相同 | 记录广播尝试并发送原始签名交易，核对返回哈希。节点曾接收后又丢弃交易时也适用。 |
| 已看到交易、尚无回执，或所在区块尚未 finalized | 保留原单，继续查同一哈希。 |
| 交易和回执均为空，pending nonce 已变化 | 返回 nonce 不一致。运维核对原哈希及该地址已用 nonce 的交易，保留原授权。 |
| RPC 断开、广播回执丢失或回执哈希异常 | 返回错误。再次执行时先查原哈希。 |
| 最终执行失败、签名或链上证据不匹配 | 返回对应错误，供调用方核查。 |
| 实收已保存，出款状态写回前进程退出 | 重启后从已存证据重新核验，复用同一到账事件。 |

`attempts` 是已登记的广播尝试数。实收事件记录外层 ETH value。实际网络费由该交易的 L2 执行费、L1 数据费和 operator fee 相加。签名中的 fee cap 约束每单位 L2 执行 gas 的价格，本批保存的实际网络费供后续结算引用。[费用核验](Base到账核验.md)

## 隔离复验

从项目根目录加载本机专用测试配置。测试工具复用隔离库登记、迁移和清理机制。business 26 新增 `base_payout`。有任何意图时，降级到 25 会拒绝删除该表。

在本机私有工作目录保存独立的 policy JSON。字段为 `ChainID`（84532）、`Key`、`Sender`、`Recipient`、`WeiAmount`、`GasLimit`、`FeeCap`、`TipCap` 和签名进程独立核对的十进制字符串 `Nonce`。复验新出款前按测试账户当前 pending nonce 和节点估算准备这些值。测试密钥文件是只在本机读取的 JSON 私钥字符串。

```bash
# work 指向本轮私有目录，testkey 指向 Base Sepolia 专用测试密钥
(cd backend && go build -o "$work/probe" ./tools/base_payout_sepolia)
probe() { "$work/probe" -state "$work/run.json" -policy "$work/policy.json" "$@"; }
probe -action prepare > "$work/prepared.json"
node scripts/payment_probe/base-payout-sign.mjs "$work/policy.json" "$work/prepared.json" "$testkey" "$work/signed.hex"
probe -action bind -signed-file "$work/signed.hex"
probe -action step
probe -action status
```

`work` 使用绝对路径。签名脚本通过锁定的 viem 重建交易并校验摘要，签名输出文件只创建一次。可指定 `-rpc` 选择可信 Base Sepolia 节点。等待最终性时，通过同一 run 和 key 再次执行 `step`。确认后用 `evidence` 导出原意图、费用和链上证据，`cleanup` 清理该次专用库。真实交易与隔离故障测试见[第十三批验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。
