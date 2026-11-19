# TON 平台出款执行

[项目入口](../README.md) · [5.3 方案](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/02实施方案.md) · [到账核验](TON到账核验.md)

## 当前交付范围

2026-11-19 已接通平台出款意图持久化、签名核验和单次广播执行入口，当前数据库 **business 22**。支持专用测试网 V4R2 平台钱包发送原生 TON 或固定 master 的测试 USDT（Jetton），签名接口与广播接口分别注入；普通检查使用确定性测试替身，真实测试网实转单独验收。

第七批完成 0.01 TON 实转，第八批完成 0.1 测试 USDT 实转和三段交易关联，详见下节。第九批已接通已签名原请求的持久恢复调度，生产签名服务和公开付款接口仍未接通。共享 `ServiceContext.TONSendIntents` 默认只组装存储；只有显式配置 TONRecovery 才启动扫描，允许恢复已签名的原消息，不自动签名或迁移。内置/外部用户钱包授权、主网及其他链出款仍按后续批次接入。

这不是提现业务单：不检查用户资格、报价、限额或占用，不修改美元余额、账本、订单或通知；Reconcile 仅保存经核验的链上实收事实。未来 5.6/6.8/6.9 调用前必须有自己的授权、资金占用和稳定业务身份；同一业务不能用新请求键绕开原意图。平台钱包签名与用户钱包签名责任保持分离。本批也不批准尚待审核的费用承担规则。

## 内部调用与可信边界

实现位于 `backend/logic/chainsend`，复用已有 tonutils-go 1.15.5，无新依赖。

- `NewStore(conn, tx)`：共享 SQL 客户端和事务执行器。`Find(ctx, key)` 返回意图、消息指纹、状态及诊断，不返回签名 BoC。
- `NewTON(store, Wallet, Signer, Broadcaster)`：固定网络 `ton-testnet` 和 V4R2 钱包。原生资产为 `ton-testnet:ton`；Jetton 必须额外配置可信 master，详见第八批。Wallet 含公钥、subwallet ID 和可选 JettonMaster；地址由固定版本合约与公钥派生，不接受任意平台出资地址。
- `Prepare(ctx, Proposal)`：内部请求键、收款地址、最小单位整数金额、seqno 和 validUntil 必须明确。规范化地址后固定全部付款事实；同键同参数返回原记录，异参拒绝。不同请求不能占同钱包的同一 seqno。
- `Execute(ctx, key)`：执行一次签名/提交或重放已保存消息。它不创建新意图，也不自动刷新 seqno、延长有效期、退款或重新付款。

平台钱包限定已经初始化、workchain 0 的 V4R2；seqno 必须大于 0，不附带 StateInit，不执行插件操作或任意合约 body。收款同样限 workchain 0，拒绝自身、零地址、非规范金额以及超出 TON Coins 120 位范围的数量。原生币发送一条空 body、non-bounce 消息；Jetton 发送固定格式的 transfer 消息。两者使用 mode 3（单独承担发送费并忽略可忽略的 action 错误）。因此钱包接收外部消息甚至消耗 seqno，仍不等于收款成功。

签名内容在调用 Signer **之前**固定：subwallet、有效期、seqno、操作、发送模式、收款目标及金额均进入 payload hash。Signer 只返回 Ed25519 签名，不能自行广播；执行器验证该签名确实对应固定 payload，再自行构造外部消息，保存完整 BoC、外部消息 cell hash 和 signed body hash。恢复时重新核对公钥策略、付款事实、签名、完整字节及两种哈希；不能只信任数据库状态标签。

生产 Signer 仍需在独立信任边界校验钱包及业务授权、额度和费用政策；当前接口不代表已部署密钥隔离。Broadcaster 限定受信任节点并核对测试网身份，不能接受用户提供任意 RPC。V4R2 签名本身不绑定网络，**测试网必须使用专用密钥**，不能复用主网出资密钥。普通检查使用内存临时密钥；显式真实探针仅使用本机不入库的专用测试网密钥，不使用生产密钥。

V4R2 消息编码依据锁定 SDK 的 `ton/wallet/v4r2.go`；防重放和有效期语义参见 [TON 钱包工作原理](https://docs.ton.org/contracts/standard/wallets/how-it-works)，钱包能力参见 [V4 文档](https://docs.ton.org/contracts/standard/wallets/v4)。本实现固定单次请求的时间和 seqno，不使用 SDK 每次读取当前时间/seqno 的常规构造流程。

## 状态与恢复规则

| 状态 | 已知事实 | 允许的下一步 |
| --- | --- | --- |
| `prepared` | 原意图已提交，尚无持久签名消息 | 对同一 payload 请求签名；拒签或签名应答丢失仍保留原意图 |
| `signed` | 完整签名消息已提交，可恢复原字节 | 有效期内保存广播标记，再调用外部通道 |
| `broadcast_unknown` | 已先保存广播尝试标记；可能尚未实际调用，也可能已上链 | 查询原事实；本入口仅允许有效期内重播完全相同的消息 |
| `submitted` | 广播适配器返回节点接收成功，结果已记录 | 查询链上执行和收款结果；重复 Execute 不再次广播 |

签名、节点请求都在短 SQL 事务之外，不注册到现有只允许同库副作用的后台任务处理器。顺序固定为：意图提交 → 外部签名 → 签名消息提交 → 广播未知标记提交 → 外部提交 → 节点接收结果提交。数据库事务持锁期间不进行网络 IO。

- 意图或签名消息提交应答丢失：只有重新读到完全一致的持久事实才继续；否则返回 `ErrOutcomeUnknown`，不能广播未证实落库的消息。
- 广播标记提交应答丢失：本次不调用外部通道；之后可用原键重新读取并执行。`attempts` 是已保存的尝试标记次数，可能包含标记后进程退出而未发送的情况，不是精确 RPC 次数。
- 广播超时/取消/错误、或节点接收后的数据库失败：保留原意图和消息。新执行器可重播同一 BoC，不能换 nonce 或有效期重新生成；节点拒绝也不能在这里自动解释为安全退款。
- 并发执行可能多次提交同一条消息，TON 钱包 seqno 防重放限制同一签名请求的执行。首个持久签名不能被后来者覆盖，迟到的错误不能把 `submitted` 改回未知。无需在数据库长时间持锁等待节点。
- 有效期已过：本地检查及广播标记事务的数据库时钟检查拒绝继续提交，返回 `ErrExpired`。`signed` 或 `broadcast_unknown` 原状态和消息仍保留；过期不证明之前没有付款，不退款、不释放 seqno、不另开替代付款。

节点接收不是执行成功，`MessageHash` 不是收款交易 hash。TON 索引器可能规范化外部消息，因此同时保留 signed body hash 作为后续关联材料。第七批已实现按签名 body 定位付款交易和单条原生币收款，最终业务结算仍未接通；不能把 `submitted` 用作产品“付款成功”。完整核验需要结合 [TON 到账核验](TON到账核验.md) 的收款事实并核对发送来源及原意图。

## 迁移与验证

SQL-first 源为 `backend/manifest/sql/ddl/chainsend/tables.sql` 和 `constraints.sql`；`000020_chain_send_intents` 新建独立表。生成 model 已登记生成器，事务锁定/更新保留在手写扩展。对 `(network, request_key)`、`(network, sender, seqno)` 分别唯一；状态、消息完整性、uint32 范围及次数由数据库约束保护。

升级不改历史到账事实或资金表。只要有意图历史，20 → 19 回退就会拒绝，避免丢失曾经授权的请求；回退失败会按迁移工具保留 dirty 状态和诊断，不应通过删记录或盲目 force 继续。部署前显式迁移，不在服务启动时执行。

普通检查在 `backend/` 执行：

```bash
go test -race ./logic/chainsend
go vet ./...
```

按 [隔离环境说明](开发与测试环境.md) 配好测试依赖后，独立运行：

```bash
go run ./tools/testenv run go test -race -tags=integration ./tests/integration \
  -run 'TestTONSend' -count=1 -timeout=120s
```

覆盖金额/目标/授权字段篡改、错误签名、持久消息损坏、12 并发同键、seqno 冲突、各提交边界应答丢失、超时后新实例恢复、迟到失败、签名后过期及资金表不变。签名使用真实密码学计算，广播与时钟故障为确定性测试替身；这不计为真实钱包、节点或链上付款验收。实际结果见 [5.3 验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。


## 第七批：真实测试网发送与实收关联

2026-11-19 已通过新发送执行器完成 **0.01 TON** 实转，平台出资为专用测试网 V4R2 钱包；收款是此前专用测试 W5 平台钱包。数据库保持 business 20，没有新 API、迁移或美元余额写入。此前第六批的模拟结论与本批真实证据分别保留。

`ProcessSigner` 直接调用配置的可执行文件，不经 shell。业务进程通过 stdin 传递公共付款事实，子进程读取独立的测试 seed 文件和预先固定的单意图 policy，重建目标、金额、seqno、有效期、网络和 payload hash，只返回签名。测试签名器拒绝 policy 外的请求与超过 15 分钟的授权窗口；测试 seed 必须是仅所有者可访问的小型文件。执行器仍独立核对返回的签名。本机同一操作系统用户的两个进程只验证接口隔离，**不等于生产密钥权限隔离/HSM 已部署**；生产需独立身份/权限与业务授权政策。

`chainassets.SubmitTON` 只接受测试网，先核对节点 zero-state，再提交已持久化的 BoC；节点回执 hash 必须等于本地外部消息 cell hash。HTTP 错误、ok=false、哈希不符或超时都不能视作安全未付款，不自动重签/重发。沿用固定节点配置、超时和响应大小约束，公开无 key 请求间隔 1.1 秒。接口依据 [TON Center 发送接口](https://docs.ton.org/api/v2/send/send-boc-return-hash)。

`TON.Reconcile(ctx, key, receipts)` 先重新验证原意图及持久签名，再通过 [transactionsByMessage](https://docs.ton.org/api/v3/blockchain-data/get-transactions-by-message) 的 body_hash 定位付款交易，用 adjacentTransactions 的 out 方向取得收款交易。它不接受用户提交的“付款成功 tx”，也不凭金额相同借用另一笔交易。

- 必须只有一笔候选发送交易及一条对应原生币消息；发送账户、外部 signed body hash、无 StateInit、目标、金额、空 body、non-bounce 均须符合原意图。发送钱包计算/action 成功，收款按既有 TON 事实规则核验。
- 复用 TON 到账核验的 finalized、非模拟、主链/分片块、消息 hash/完整内容、LT 顺序、数量与目标校验。此为可信节点/索引器证据核验，仍不是本地轻客户端共识证明。
- 查不到/未最终确认保持未确认，失败或错误证据明确报错；不退款、不重新付款。Reconcile 不受原签名到期限制，因为到期不能否定此前付款。
- 实收事实先提交，再将发送记录至少推进到 submitted。两次短事务之间宕机可用原意图重查并复用同一事件；不会丢实收、重复记账或重新广播。submitted 仍只表达已被接收，查询结果另返回经核验的实收、付款交易及内部消息身份，不借此新增“用户提现成功”业务状态。

### 显式探针与恢复

已有 Node 探针依赖安装于 `scripts/payment_probe/`；本批未新增依赖。`scripts/payment_probe/ton-send-setup.mjs` 的 key/inspect/fund/initialize/policy 分别创建专用测试密钥、只读检查、转入 0.1 测试 TON、独立初始化 V4R2（转回 0.001 TON）、保存 0.01 TON 测试意图。fund/initialize 在发送前独占创建本地记录与 BoC；已有记录时拒绝另建，未知结果先查原交易，不能删除文件来重跑。seqno 0 初始化仅为测试准备，不纳入正式发送器支持范围；准备及主测试的数量和费用分开记录。

主探针位于 `backend/tools/ton_send_testnet`，只使用 `testenv` 验证过的本机专用数据库账号，第一次 prepare 创建独立数据库和资源登记，保留在 `--state` 文件。后续进程加载同一数据库与意图；失败不自动删除数据库或改变付款键。

在 `backend/` 构建：

```bash
go build -o ../.tools/ton-live-send/ton_test_signer ./tools/ton_test_signer
go build -o ../.tools/ton-live-send/ton_send_testnet ./tools/ton_send_testnet
```

在仓库根加载 [本机隔离配置](开发与测试环境.md) 后，命令形态如下（路径须指向本次专用测试配置）：

```bash
.tools/ton-live-send/ton_send_testnet --action prepare --state .tools/ton-live-send/run.json --policy .tools/ton-live-send/policy.json
.tools/ton-live-send/ton_send_testnet --action execute --state .tools/ton-live-send/run.json --policy .tools/ton-live-send/policy.json --signer /绝对路径/ton_test_signer --seed-file .tools/ton-live-send/v4-test.seed
.tools/ton-live-send/ton_send_testnet --action reconcile --state .tools/ton-live-send/run.json --policy .tools/ton-live-send/policy.json --evidence .tools/ton-live-send/confirmed-evidence.json
```

status 只查库。cleanup 必须重新核验成功且通过 `--evidence` 保存公共实收证据后才清理登记的数据库；未知结果不得使用通用清理命令删除发送记录。清理会输出资源诊断及结果 JSON，state 文件保留，不能将已清理的运行误当成新付款。普通检查不联网；真实探针必须显式调用，账户余额、节点不满足时保留诊断。

本次重复 execute 故意配置不存在的签名器和不可达 RPC，仍返回同一结果、attempts=1；再次 Reconcile 复用原实收事件，随后数据库已清理。公开证明与验证结果见 [第七批记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)，真实快照进入单元回归。第八批的 Jetton 出款、第九批的持久恢复调度见下节；业务接入、用户提现和费用扣取仍未开放。


## 第八批：测试 USDT / Jetton 平台出款

沿用同一发送状态与恢复路径，不新增第二套发送任务。`Proposal.Jetton` 全零时保持原生币历史编码；非空时必须完整提供 master、发送方代币钱包、attachTon、forwardTon 和 queryId。`Atoms` 表示代币最小单位数量，`Recipient` 是最终收款 owner，不能填成代币钱包。

| 固定事实 | 规则 |
| --- | --- |
| master / 资产 | 与可信 Wallet.JettonMaster 一致；本发送器的测试资产 ID 为 `ton-testnet:usdt:<规范 raw master>`，节点配置也使用同一 raw master；收款服务按资产 ID 与 master 同时匹配 |
| 发送代币钱包 | Prepare 和未提交的 Execute 通过已配置 master 的 get_wallet_address 派生平台 owner 地址，必须与固定钱包一致；网络 IO 在 SQL 事务外 |
| attachTon | 正的 nanoton 整数，单独的合约执行预算，不是 USDT 本金或实际费用；发送 action 费用另由 V4 钱包承担 |
| forwardTon | 非负且小于 attachTon；本次测试为 1 nanoton，用于通知 |
| queryId | 明确且固定的非零 uint64，不使用 SDK 随机默认值；不是订单幂等键，也不替代钱包 seqno |
| 余款 / 附加操作 | response_destination 固定为出资 V4 owner；不接受 custom_payload、非空 forward_payload、额外转账或任意合约调用 |

transfer 采用 [TEP-74](https://github.com/ton-blockchain/TEPs/blob/master/text/0074-jettons-standard.md) 与已有 tonutils-go TL-B 类型编码，发送到代币钱包的内部消息为 bounce=true、mode=3。独立测试签名器同时核对原始单意图 policy、资产、主币预算与精确 payload；不能用另一份预算重新签名来恢复原付款。机制依据 [TON Jetton transfer 说明](https://docs.ton.org/contracts/standard/tokens/jettons/transfer)。

`ReadTONJettonSendEvidence` 按原 signed body hash 查 V4 发出指令的交易，再沿两次出站消息查询取得发送方代币钱包和收款方代币钱包交易；同时读取 master 对两个 owner 的当前派生结果。`RecordJettonSent` 校验三段身份、完整消息对应、LT/主链顺序、执行状态、数量、query_id、TON 预算、余款目标与空 payload，然后复用原有收款事实持久化和事件去重。源钱包、代币钱包或收款环节尚无记录时保持未确认，不把节点接收当成到账。

收款方代币钱包成功处理 internal_transfer 才表示代币到账；后续 transfer_notification 的 no_gas 不推翻已发生的代币入账。当前 master 映射和可信索引器响应仍不是历史合约状态或轻客户端共识证明。`TONSentStored.SenderHash` 是 V4 发出指令的交易，`MessageHash` 是最终进入收款代币钱包的 internal_transfer 消息；数据库内已有外部消息 hash/body hash 不改变。

### 数据库与重复执行

business 21 增加 `chain_send_intent.jetton_json`，存储规范 JSON；旧原生意图为空字符串，无需改写。JSON 与原始付款事实共同参与幂等比较，恢复时重新构建并核对 payload 与签名 BoC。21 → 20 只允许无 Jetton 历史时执行；存在任何 Jetton 意图都拒绝回退，原生历史可完整往返迁移。回退失败保留 dirty 状态，不能删意图来解除限制。20 → 19 原有保护继续保留。

`Reconcile` 在签名过期后仍可查询原付款，先提交实收，再推进发送标记；中间提交失败可恢复同一事件。已 submitted 的 Execute 直接返回原事实，不访问派生 RPC、签名器或广播接口。

### 测试准备和证据

`scripts/payment_probe/ton-jetton-send-setup.mjs` 固定测试网、已有测试 master 与专用钱包，提供 inspect / fund / policy / report。fund 仅为本次测试从已有 W5 转入 0.1 测试 USDT，提交前以独占文件保存记录和 BoC；policy 固定 Go 执行器转回的单一付款，report 读取已确认的链上公开证据和两条后续通知/余款交易。重复运行不能删除原记录或改 key 绕开未知付款。主流程复用 `ton_send_testnet`，工作目录 `.tools/ton-jetton-send/`，不增加线上 API、自动任务或测试依赖。

本次是社区测试代币，不是主网官方 USDT；测试 master 与代码 hash、全部三段交易、余款和费用已保存于 [第八批验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。0.05 TON 仅为本次预留预算；完整路径观察成本为 2,771,965 nanoton，收款事实表的 FeeAtoms 只记录收款代币钱包交易的 849,141 nanoton。两者不能混用，也没有扣取用户费用。生产预算估计、费用政策、独立签名权限、持久扫描和业务结算仍待接入。


## 第九批：持久扫描与原请求恢复

当前数据库 business 22。新增 `chainsend.Recovery` 与 `RecoveryRuntime`，只扫描固定测试钱包下已持久签名的 signed / broadcast_unknown / submitted 请求；prepared 不自动处理。运行时不持有 Signer，`NewTON` 允许只读/恢复执行器不配置签名器，此时显式 Execute 未签名请求返回 ErrSigningUnavailable。它不会创建新付款、续签、换 seqno 或延长有效期。

调度规则以原消息为单位：

1. 先用原 signed body hash 核验发送与收款事实。核验通过时复用已有事件去重，保存实收，再推进发送状态及调度确认标记。
2. 原请求尚未确认、仍在有效期内，且发送状态为 signed / broadcast_unknown 时，最多执行一次原 BoC 恢复。submitted 只查询，不再次广播；RPC 错误或异常证据也不触发广播。
3. 原签名过期后继续只读核验。查询次数用尽、签名/证据不符或链上执行失败时转为 review，保留原消息和原因；review 不是付款失败、退款许可或资金释放。
4. 运营核实问题后可按原请求 key 恢复一轮有上限的核验。重试不修改任何签名事实、数量、目标、预算、有效期或 nonce，旧租约代次继续失效。

### 调度状态和并发

新增字段放在原 `chain_send_intent` 行内，避免签名已提交却丢失入队动作；旧 signed 记录在显式启用后同样可发现。原 `state` 仍表示发送进度，独立的 recovery_state 表示调度进度：

| 调度状态 | 含义 |
| --- | --- |
| ready | 已签名请求等待到期检查；未签名行也使用默认值，但不参与扫描 |
| running | 领取已提交，外部核验/原消息恢复进行中 |
| confirmed | 已核验并保存原收款交易 hash；不是用户订单或账本结算完成 |
| review | 证据异常或自动检查次数用尽，停止自动处理，等待核查 |

同钱包副本使用相同公钥、subwallet、代币白名单和恢复策略。数据库 READ COMMITTED 短事务、分别索引的到期/过期查询和 FOR UPDATE SKIP LOCKED 完成领取；时间使用数据库 UTC，generation 每次领取递增。RPC、广播和长等待不在领取事务内。结果只能由仍持有效租约的同代次写回；进程退出、超时或领取提交应答丢失由过期租约重新领取。

租约不能撤回已经发出的网络请求。极端停顿下，两个实例可能观察或提交同一 BoC；资金保护仍由原消息不可变、钱包 seqno 防重放及收款事件去重共同承担。旧实例不能覆盖新实例的调度结果。实收提交后、调度回写前失败，可在下次领取复用同一实收；发送状态更新只修改 state/attempts/last_error，不反复写出签名 BoC。

recovery_attempts 是领取次数，包含领取后未完成就退出的情况；与原 attempts（广播标记次数）分开。默认最多 60 次，每次未完成后间隔 30 秒，不是新的产品订单有效期或退款等待时限。没有新增消息队列、通用任务框架或链上费用规则。

### 显式配置与生命周期

仅 Environment=local/dev 且 Mode=dev 允许启用，网络固定 ton-testnet；不配置 TONRecovery 时完全不启动。使用专用测试钱包公钥，不复用主网钱包。配置示意如下，公钥须替换为既有已签名请求所属的专用钱包公钥；仅使用原生 TON 时可省略 JettonMaster。

```yaml
ChainAssets:
  Timeout: 30s
  Networks:
    - Network: ton-testnet
      URL: https://testnet.toncenter.com
      TestUSDTContract: 0:f418a04cf196ebc959366844a6cdf53a6fd6fff1eadafc892f05210bba31593e
TONRecovery:
  PublicKey: '<专用测试钱包的 base64 公钥>'
  SubwalletID: 698983191
  JettonMaster: 0:f418a04cf196ebc959366844a6cdf53a6fd6fff1eadafc892f05210bba31593e
  Workers: 1
  PollInterval: 5s
  OperationTimeout: 30s
  LeaseDuration: 60s
  RetryInterval: 30s
  MaxAttempts: 60
```

Workers 允许 1–8；租约须大于单次操作期限，操作期限不得超过 ShutdownTimeout。原生币和 Jetton 混用同钱包时，JettonMaster 必须与节点资产配置的规范 raw master 一致；缺少或错误的白名单会使不匹配的原请求进入 review，不能依靠换配置自动重新归属。沿用 ChainAssets 的 Redis/API 配额配置要求；无 API key 的公共节点只适合低并发验证。TON 恢复不依赖 RocketMQ 开关。

与 API 共用进程和依赖，通过 ServiceContext.StartBackground 显式启动；主模板和实际 main 同步调用 StopBackground / WaitBackground。停机先停止两个后台池的新领取，再等待既有工作，最后关闭链节点客户端和数据库。排空超时取消操作并报告错误，依赖保留到进程结束，未完成租约留待下一实例；不伪造完成标记。启动缺少迁移会明确失败，不自动建表。

### 运维与迁移

`backend/tools/ton_recovery` 默认只列出固定钱包的已签名记录，显示原目标/资产/金额/body hash、发送与调度状态、次数/代次、到期与租约时间、实收 hash 及错误；不输出 BoC。加载显式配置后，从 backend 执行：

```bash
go run ./tools/ton_recovery -f /本机配置/ton-recovery.yaml -action list -state review
go run ./tools/ton_recovery -f /本机配置/ton-recovery.yaml -action retry -key '<原请求key>' -reason '核实节点恢复，继续检查原消息'
go run ./tools/ton_recovery -f /本机配置/ton-recovery.yaml -action run-once
```

retry 仅接受 review，保留最后一次 OS 操作者和原因，重置本轮领取次数并增加 generation；不重置发送状态或已存在的链上事实，不提供强制成功/失败入口。run-once 显式执行一项到期工作，未确认或失败会返回非零并保留诊断，不能把命令退出码当成付款失败。工具不启动 API 或后台循环，也不持有签名能力。

business 22 新增调度字段、两个索引与状态/租约/计数/确认 hash 约束。空库和未签名、未调度的 prepared 历史可回退；存在已签名请求或恢复历史时拒绝 22 → 21，避免丢失待恢复付款或核验结果。迁移失败按既有流程保留 dirty 与诊断，不删除付款来解除保护。

本批已通过真实隔离数据库的并发、租约、故障和停机验证，并对之前已消费且过期的 TON / USDT 历史消息完成真实节点只读恢复。未读取私钥、未新增链上交易；证据见 [第九批验证记录](../design/工程工作记录/05-钱包与链上通道/5.3-平台链上收付基础/04验证记录.md)。生产签名隔离、prepared 请求的业务授权调度、充值归属/订单入账、其他链出款及正式用户开放仍待实施。
