# Solana 真实钱包签名与平台代付：2026-11-02

[前一轮协议验证](05支付可行性验证.md) · [阶段讨论](01讨论记录.md) · [阶段目录](../README.md)

## 实际结论

**Solflare 扩展 2.41.1 + Solana Devnet + SOL 已完成真实转账：钱包仅签名 → 后端校验 → 平台补签、模拟并广播 → 链上 finalized → 核对三方余额变化。** 使用用户协助领取的免费测试 SOL，没有主网交易。支付没有 memo，测试订单与消息摘要、交易签名在本机状态文件关联。

这是一个确定组合的技术验证，尚未实现充值业务、数据库订单或账本入账，不代表 Solana USDC/USDT、其他钱包、移动端或其他三条链已通过；5.1、5.3、5.5 均不因此勾选完成。平台在技术上支付网络费，也不替代 [链上费用逐项审核](../../../链上资金费用逐项审核.md) 中待用户审核的最终费用承担规则。

有一项客户端限制必须保留：Solflare 的在线安全预览显示服务器错误，本轮通过钱包原有的 `I trust this site` 选项确认固定的本机测试请求后签名；没有修改钱包程序、伪造风控响应或关闭签名校验。因此，签名与到账已通过，正常安全预览和正式用户体验尚未验收。

## 交易与资金证据

[支付交易浏览器](https://explorer.solana.com/tx/3uechSmrGT1Axu3zhxVj6URgbMgoLXLyzSd1rS2EnJ88LpRRfMkB8ahZ4Csh13o6VzFmiyoAnHmmoMQ2yYxv5FJn?cluster=devnet) · [公开原始回执](attachments/solana-live-2026-11-02/solana-live-public-result.json) · [钱包运行记录](attachments/solana-live-2026-11-02/wallet-runtime.json)

| 项目 | 本轮值 |
| --- | --- |
| 测试订单 reference | `39753cfc-1b33-4a9a-a098-6c26bb9bf78d` |
| 网络与资产 | Solana Devnet / SOL，legacy SystemProgram.transfer |
| 转出账户 | `9ucco3Ath4uS71gyfHggdo9jXzUnhkRr5oPHp8qVUVGf` |
| 平台 fee payer | `DcfKLiLsSsoJrrf3t6QXwkjfUarE4yW5sKjzF7xBQkPN` |
| 收款账户 | `7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5` |
| 支付金额 | 1,000 lamports = 0.000001 SOL |
| 预计与实际网络费 | 均为 10,000 lamports = 0.00001 SOL，平台支付 |
| 用户签名返回 | 2026-11-02 09:09:25 UTC；平台签名当时为空 |
| 后端广播 | 2026-11-02 09:09:28 UTC |
| 链上结果 | finalized，slot `517805174`，`meta.err = null` |
| 消息 SHA-256 | `9c2b4e318f943854a824531740d3fbe637628c4ede1c9a0966c3f39b2f834438`，钱包返回后不变 |

以下数值来自同一笔支付的 `preBalances` / `postBalances`，单位均为 lamports：

| 账户 | 支付前 | 支付后 | 变化 |
| --- | ---: | ---: | ---: |
| 用户 | 5,000,000 | 4,999,000 | -1,000，仅支付金额 |
| 收款方 | 1,000,000 | 1,001,000 | +1,000 |
| 平台 | 4,993,995,000 | 4,993,985,000 | -10,000，仅网络费 |

钱包签名后的独立记录见 [用户签名结果](attachments/solana-live-2026-11-02/solflare-user-signed.json)。服务端在补签前检查完整消息字节与固定订单一致、用户签名有效、平台签名为空、blockhash 未过期和费用上限；补签后校验全部签名，并以 `sigVerify: true` 模拟，成功后提交。链上回执再次核对付款人、金额和三方变化。

### 测试账户准备与正式支付分开

用户回复“领取完成”后，RPC 确认平台地址收到 5 Devnet SOL。另有一笔仅由平台签名的准备交易，给用户账户 0.005 SOL、收款账户 0.001 SOL，费用 0.000005 SOL。收款账户提前有余额，避免把新账户租金问题混入本轮最小转账。

[准备交易浏览器](https://explorer.solana.com/tx/2EzHUktER9SWu72ejKHCpf9WcW2kqTzqJ8zHo6jdGpG7nR7bqq1iDFGhcBN3vNqKj77m77i2yvAg2ipNJh4XNBx?cluster=devnet) 与 [准备回执](attachments/solana-live-2026-11-02/account-preparation.json) 仅证明账户准备，不作为真实钱包签名的证据。本轮尚未验证向全新收款账户付款的界面、最低金额和费用展示。

### 重复提交与重放的区别

[重复请求及余额快照](attachments/solana-live-2026-11-02/solana-live-replay.json) 保留了两种结果：

- 对同一 reference 再次调用后端 `/submit`，HTTP 200，返回 `reusedSubmission: true` 和原交易签名；后端没有构造新交易或再次扣款。
- 对保存的同一份完整签名字节调用 `/replay`，RPC 预检查返回 `BlockhashNotFound`，后端明确返回 HTTP 400；前后账户余额不变。**这证明过期重放未造成新扣款，不是有效期内 RPC 接受重复交易的成功证据。**

探针在任何广播前先保存完整签名和交易 ID。存在签名的订单禁止直接重新 prepare；未知结果必须先查原交易，显式重发也只能用相同字节。未提交且已过期的请求可以重新获取 blockhash，但必须再次经过钱包签名。这里使用 UUID 仅作隔离探针 reference，不改变正式业务对外 ID 沿用 idx 的约定。

## 钱包安装与运行环境

Node 24.22.1、Playwright 1.64.1、其配套 Chromium 156.0.8078.4，独立 persistent profile，headless 模式。扩展从钱包官网下载链接指向的 Chrome Web Store 获取，再经 Google 官方 CRX 更新服务下载；没有使用第三方镜像。

`scripts/payment_probe/verify_crx.py` 按 [Chromium CRX3 格式](https://chromium.googlesource.com/chromium/src/+/main/components/crx_file/crx3.proto) 验证 signed header、官方扩展 ID 对应开发者公钥及包签名，随后检查并解压 ZIP。SHA-256 和签名校验结果保留如下：

| 钱包 | 官方商店 ID / 版本 | 证据和结果 |
| --- | --- | --- |
| [Solflare](https://www.solflare.com/download/) | `bhhhlbepdkbapadjdnnojkbgioiodbic` / 2.41.1 | [包校验](attachments/solana-live-2026-11-02/solflare-package-verification.json)；真实连接、signTransaction、后端代付及到账通过 |
| [Phantom](https://phantom.com/download) | `bfnaelmomeimhlpmgjnjophhpkkoljpa` / 26.36.0 | [包校验](attachments/solana-live-2026-11-02/phantom-package-verification.json)；初始化受阻，本轮没有得到其支付签名 |

两包原始文件 SHA-256：

```text
Solflare e7294201cc7cfcabb428761f7bd8a750119967f717eabddc92bdb2067a35d546
Phantom  e7eb550bd42282a4df0713e99d193c8829ff4e8f5f426c665451408e90a5f6a4
```

Solflare 包没有 manifest `key`，未修改地加载解压目录后，Chromium 按本机路径分配运行 ID `jpglbfkabnemhpbebjhpjafgpkakdcfi`，与商店 ID 不同。这是官方包的隔离加载实测，不冒充通过商店正常安装的验收；更换目录后不要硬编码该运行 ID，应从 service worker URL 读取。Phantom 自带 manifest key，运行 ID 与商店一致。

### Solflare 操作与踩坑记录

1. 首次导入只提供 Recovery phrase / Ledger / Keystone。为本轮生成专用空助记词钱包并保存在忽略目录，完成初始化后，通过 My wallets → Add wallet → Import existing wallets → Private key 导入已准备的测试用户账户，选中 `9ucc...UVGf`。这与 [官方桌面私钥导入说明](https://help.solflare.com/en/articles/6134229-how-to-import-your-solana-wallet-into-solflare-using-a-private-key-desktop) 一致。
2. Settings → Network → Devnet 后，**还需在 Switching to Devnet 弹窗点击 Continue**，再核实设置页显示 Devnet。[本轮截图](attachments/solana-live-2026-11-02/solflare-devnet.png)。只点击下拉选项不能当作切网已完成。未确认切网的一次签名请求显示 Transaction expired，未提交。
3. 默认 1280×720 viewport 下，钱包连接返回 false，桥接错误 4001 `Connection rejected`。读取原包弹窗创建逻辑，并单独调用浏览器创建空白弹窗诊断后，得到 bounds 超出可见屏幕错误。将 viewport 调成 800×600 后连接弹窗成功；设置页临时使用 800×1000 可完整看到网络菜单。[诊断记录](attachments/solana-live-2026-11-02/solflare-headless-window-error.json)。不应将这里的 4001 误报为用户实际点了拒绝。
4. 真正连接需通过钱包 Connect 弹窗授权；[连接截图](attachments/solana-live-2026-11-02/solflare-connect.png) 和 [返回账户](attachments/solana-live-2026-11-02/solflare-connected.json) 已保留。`connect()` 失败可能返回 false，页面必须检查 publicKey，不能直接调用 `toBase58()`。
5. 使用 `window.solflare.signTransaction(transaction)`，未使用 signAndSendTransaction。钱包返回时服务端检查 fee payer 签名仍为空；服务端只在验证之后补签并调用 sendTransaction，链上交易才能完整执行。
6. 钱包安全预览出现服务器错误，页面显示 [Security check failed](attachments/solana-live-2026-11-02/solflare-sign-confirm.png)。本轮明确选择钱包提供的信任本机站点选项，再点击 Approve；未获得正常的金额/费用预览。未来正式接入需解决或单独评估这一体验，不能把截图中的警告隐藏为“预览通过”。
7. 开发时逐步暂停观察会使 blockhash 过期。最后一次把已核对的 UI 批准、后端验证及提交连续执行，约 5 秒完成 prepare 到广播。不能通过忽略过期或偷偷更换 blockhash 来复用旧签名。

Phantom 初始化过程中，官方 API 的 profile、wallet-storage、wallet-accounts 返回 403；provider 已注入，但 connect 返回 `Unexpected error`，账户初始化未完成。[运行记录](attachments/solana-live-2026-11-02/phantom-runtime-result.json) 保留具体路径。该失败限定于当前环境，不能据此宣布 Phantom 不支持代付。

## 复现入口与保留材料

代码仅在 `scripts/payment_probe/`，没有修改正式 API、生产页面、数据库或业务依赖；页面文案是隔离工程探针，不是产品国际化界面。独立 npm 包新增直接依赖 bs58 4.0.1，锁文件保留精确版本。以下命令从仓库根执行，先切换 Node 24.22.1。

```sh
# 首次安装独立工具依赖；已有专用夹具可跳过 solana.mjs
cd scripts/payment_probe
npm ci --ignore-scripts --no-audit --no-fund
cd ../..
node scripts/payment_probe/solana.mjs

# sponsor 已收到免费 Devnet SOL 后，准备两个测试账户
node scripts/payment_probe/solana-live-fund.mjs

# 服务固定在 localhost，只读平台密钥；另开终端运行下面的检查
node scripts/payment_probe/solana-live-server.mjs
node --test scripts/payment_probe/solana-live.test.mjs
node scripts/payment_probe/solana-live-check.mjs

# 显式检查同一订单重复提交并重发完全相同的签名字节
node scripts/payment_probe/solana-live-check.mjs --replay
```

`solana-live-check.mjs` 默认只查询原交易并核对最终回执；尚无支付签名会明确失败，不自动创建付款。`--replay` 会保存 RPC 拒绝的原始原因，不把过期错误转换成重放成功。

三个角色的测试夹具由前一轮 `solana.mjs` 创建并复用。准备脚本使用 `wx` 创建 `solana-sponsor-only.json` 和公开地址配置，已有内容必须匹配；不会覆写不同夹具。`solana-live-funding.json` 保留准备交易，重跑查原交易而不是再造一笔。服务端固定 Devnet、固定用户/收款方、固定金额 1,000 lamports、费用上限 20,000 lamports，不能改为通用主网出款工具。

CRX 校验示例（Python 需安装 cryptography）：

```sh
python3 scripts/payment_probe/verify_crx.py \
  .tools/payment-feasibility/solflare-2.41.1.crx \
  bhhhlbepdkbapadjdnnojkbgioiodbic \
  .tools/payment-feasibility/solflare-extension
```

Google 更新 URL 模板为 `https://clients2.google.com/service/update2/crx?response=redirect&prodversion=156.0.8078.4&acceptformat=crx2%2Ccrx3&x=id%3D<官方扩展ID>%26uc`。本轮通过现有代理、Range 分段下载，检查 HTTP 206、总长度及各段长度，再做完整 CRX 签名校验；不将下载成功本身视为包可信。

浏览器恢复依据 [Playwright 扩展运行说明](https://playwright.dev/docs/chrome-extensions)。在仓库根 `node -i` 中可使用：

```js
var { chromium } = await import('./scripts/payment_probe/node_modules/playwright/index.mjs');
var extension = process.cwd() + '/.tools/payment-feasibility/solflare-extension';
var context = await chromium.launchPersistentContext(
  process.cwd() + '/.tools/payment-feasibility/solflare-profile', {
    channel: 'chromium', headless: true, locale: 'en-US',
    viewport: { width: 800, height: 600 },
    proxy: { server: 'http://127.0.0.1:7890', bypass: '127.0.0.1,localhost' },
    args: ['--disable-extensions-except=' + extension, '--load-extension=' + extension],
  });
var worker = context.serviceWorkers()[0] || await context.waitForEvent('serviceworker');
var extensionId = new URL(worker.url()).hostname;
var wallet = await context.newPage();
await wallet.goto('chrome-extension://' + extensionId + '/wallet.html');
```

解锁密码仅从本机 `secrets.json` 读取并填入 Password 输入框，禁止打印值。之后用上文步骤核实选中账户和 Devnet。另开 context 内页面访问 `http://127.0.0.1:18792/`，依次触发 Connect wallet、Sign only in wallet、Submit via platform、Check chain result；每次需要的钱包确认弹窗通过 `context.pages()` 查找 `confirm_popup.html`。批准前等待加载结束，不能在 Approve 禁用时强行触发。实际无头操作使用 Playwright 的 getByRole/getByText/click，未调用钱包内部签名函数。

本轮已保存 finalized 状态，因此默认不能再创建付款。若要下一次独立实测，先确认原交易终态，再归档本机 `solana-live-state.json` 并重启探针；不要为了处理结果未知直接删除状态或换 reference。结束后 `await context.close()`，服务用 Ctrl+C 关闭。

| 本机保留路径（均在忽略目录 `.tools/payment-feasibility/`） | 用途 |
| --- | --- |
| `solana-test-keys.json`、`solana-sponsor-only.json`、`secrets.json` | 专用测试夹具、平台签名文件、钱包解锁配置，0600；不入库 |
| `solana-live-addresses.json` | 三个公开地址及角色 |
| `solana-live-state.json` | 测试订单、两阶段签名、原交易与回执，0600；不公开完整可广播字节 |
| `solana-live-funding.json` | 准备交易完整记录，0600 |
| `solflare-profile/`、`phantom-profile/` | 独立浏览器与钱包资料，不入库 |
| `*-extension/`、`*.crx`、`*-download.json` | 安装包、提取文件、Google 下载定位与诊断 |

服务进程只读取平台签名文件；开发测试夹具仍在同一台机器，这不等于生产密钥隔离验收。截图、公开地址、已上链签名、摘要及去掉 signed/userSigned 的回执进入附件；没有复制私钥、助记词、密码、浏览器资料或完整第三方代码。

## 检查与后续边界

本轮 9 项确定性测试通过：正确的用户单签；金额、地址、额外指令、blockhash、fee payer 被改；未签名/错误订单；损坏的用户签名；回执金额、平台费用和执行失败检查。HTTP 冒烟确认页面与脚本可访问、密钥路径 404、外部 Origin 和非指定测试账户被拒绝；只读检查脚本再次确认 finalized。JS/Python 语法、文档链接及差异检查通过。28 个本轮文件未发现本机已知测试密钥、助记词或密码；链上两个签名与保留记录一致。真实链上检查另以以上交易及附件为准。没有为了隔离探针改动重跑无关业务集成测试。

后续优先把同一流程扩展到 Solana Devnet USDC：核对 mint 和 token program、TransferChecked、账户创建租金及收款 token 实际变化；再分别验证其他钱包版本。SOL 的结果不能替代 SPL 资产验收。另需补有效期内同字节重放、钱包拒绝、RPC 结果未知恢复及正常钱包安全预览。生产充值必须把链上确认和已确认的账本幂等入口接起来，本轮未涉及。
