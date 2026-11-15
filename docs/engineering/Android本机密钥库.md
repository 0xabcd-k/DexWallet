# Android 本机密钥库

[项目入口](../README.md) · [Android 应用基础](Android应用基础.md) · [5.1 实施方案](../design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/02实施方案.md)

## 当前边界

2026-11-16 已接通 Android 内置钱包的创建、导入预览、备份核对和本机移除。首页/设置的「内置钱包」进入 `/local-wallets`，排在外部钱包连接之前。支持 TON、Base、Solana、TRON 首个账户地址；资产查询、收款码、签名和链上资金交易尚未开放。Mini App 构建排除此页面及原生钱包模块。数据库仍为 business 15，无后端契约或业务变化。

本机验证使用手机已有的 PIN/图案/密码，不新增项目付款密码，也不改变中心化付款流程；本批不接入生物识别。未设置系统锁屏时拒绝秘密读写和移除。产品身份登录不恢复本机密钥，卸载/清除应用数据或系统密钥丢失需要用户自行备份恢复。

## 数据与保护

`WalletVaultStore` 负责有版本的 AES-256-GCM 密文、完整性检查、记录隔离及落盘；`AndroidVaultKeys` 提供每条记录独立的 Android Keystore 密钥；`WalletVaultPlugin` 负责系统确认、并发与生命周期。源文件位于 `frontend/apps/mobile/android/app/src/main/java/com/dexwallet/app/`。

记录位于 `Context.getNoBackupFilesDir()/wallet-vault-v1/`，由用户 idx 和本地 walletId 生成文件/别名标识；版本、用户和钱包标识同时绑定认证附加数据。记录不包含明文秘密，密钥不进入 Preferences 或 Web Storage。AtomicFile 写入后显式同步、读回核验；已存在的记录拒绝覆盖，残留的中断记录也不会被当成空槽。

每次 put/get/remove 都重新打开系统确认。Keystore 自身要求最近 30 秒设备凭证认证，用于随后立即执行的加解密；客户端不能直接跳过提示，认证过期由系统拒绝。密钥不可通过 `getEncoded()` 导出，但返回给备份/派生流程的秘密仍会短暂进入 Java/JavaScript 内存，不能称作“助记词从不离开硬件”。硬件保护级别依设备而异；本批模拟器测试不代表真实硬件、root 设备或被攻陷的 App 已通过安全验收。

原生桥自动参数日志在 debug/release 均关闭，Gradle `preBuild` 检查 `loggingBehavior: none`，避免复用旧构建配置泄漏参数。失败日志仅含错误码与异常类别。调用系统界面前从可保存的 PluginCall 参数中移除秘密，只在进程内保存待处理字节；缓冲区尽力清理，不承诺 Java/JS 字符串可确定清零。进程结束不恢复未完成的秘密请求。Android 备份继续关闭，兼容旧系统显式设置 `fullBackupContent=false`。

## 原生桥契约

插件名 `WalletVault`；这是 App 内部原生桥，不是 HTTP API。产品用户 idx 是本地分区依据，不代替服务端鉴权；本机操作权限来自系统凭证与可信客户端。

| 方法 | 参数 | 成功结果 | 约束 |
| --- | --- | --- | --- |
| inspect | userIdx、walletId | exists、deviceSecure | 不读取秘密；exists 包括需处理的中断记录，不代表内容完整 |
| put | userIdx、walletId、viewToken、secret | 空对象 | 非空 UTF-8 字符串，最多 4096 bytes；验证后首次写入，重复拒绝 |
| get | userIdx、walletId、viewToken | secret | 每次系统确认，格式/完整性/密钥异常明确失败 |
| remove | userIdx、walletId、viewToken | 空对象 | 每次系统确认；删除密文和对应密钥，可清理损坏记录；重复移除仍需确认 |
| protect | token | token、active、revision | 开启 FLAG_SECURE、禁用自动填充并加原生遮挡 |
| acknowledge | token、revision | 空对象 | 页面清除秘密并完成渲染后，等待 WebView 新画面可绘制，再按前台 revision 移除遮挡 |
| unprotect | token | 空对象 | 立即作废当前 token；页面退出画面可绘制后释放保护，旧页面不影响新页面 |

`privacyChange` 事件返回 token、active、revision；原生暂停立即加遮挡并更新 revision，恢复后仍等待页面确认。读写/移除必须带当前保护页面的 viewToken，否则返回 `VAULT_PRIVACY_REQUIRED`；页面关闭后的操作结果不向新页面交付。

userIdx/walletId 允许 1–128 个字母、数字、下划线或连字符。walletId 是客户端内部标识，不新增对外业务 ID。系统提示按保存、读取和移除分别说明目的；移除提醒后续需要备份恢复。

稳定错误：`VAULT_PRIVACY_REQUIRED`、`VAULT_INVALID_INPUT`、`VAULT_EXISTS`、`VAULT_NOT_FOUND`、`VAULT_CORRUPTED`、`VAULT_KEY_UNAVAILABLE`、`VAULT_STORAGE_FAILED`、`VAULT_DEVICE_LOCK_REQUIRED`、`VAULT_CANCELLED`、`VAULT_BUSY`、`VAULT_AUTH_EXPIRED`、`VAULT_INTERRUPTED`、`VAULT_UNAVAILABLE`。错误不返回空秘密或触发自动重建。丢失系统密钥后，重新设置锁屏不能恢复原密钥；记录保留并拒绝覆盖，须明确移除后从用户备份导入。

## 助记词与恢复地址

新建钱包生成英文 BIP39 24 词，不使用附加助记词密码。导入须明确选择 BIP39 12/24 词或 TON 原生 24 词；TON 原生输入仅恢复 TON，不将其默认为四链助记词。固定支持的首个账户如下：

| 网络 | BIP39 派生路径 / 地址规则 |
| --- | --- |
| Base | `m/44'/60'/0'/0/0`，BIP32 / secp256k1，EIP-55 地址 |
| Solana | `m/44'/501'/0'/0'`，SLIP10 / Ed25519 |
| TRON | `m/44'/195'/0'/0/0`，BIP32 / secp256k1，0x41 Base58Check 地址 |
| TON | `m/44'/607'/0'`，SLIP10 / Ed25519；TON 原生词则按 TON 官方算法 |

TON 新建默认 V5R1，导入可选 V5R1/V4R2；workchain 0，V5 subwallet 0，主网 networkGlobalId -239、测试网 -3，V4 使用 SDK 默认 walletId 698983191。显示非 bounceable、URL-safe 地址，测试网使用 testOnly 标记。V5 主/测试网合约地址不同；V4 底层账户相同、友好地址测试标记不同。导入先预览再保存；不支持附加助记词密码、自定义路径、额外账户或历史合约扫描，用户必须核对原地址。

密码学复用锁定依赖：scure BIP39/BIP32/base 2.4.0、noble curves/hashes 2.4.0、micro-key-producer 0.10.2 的 SLIP10、TON Crypto 3.3.0 / Core 0.63.1 / TON 16.3.0。Buffer 6.0.3 只用于浏览器兼容。没有自建熵源、签名算法或助记词校验规则。秘密载荷版本 1 包含 scheme、words、tonVersion；恢复版本保存在密文中，不依赖易失默认值。

## 页面、目录与中断

非秘密目录通过 `NativeState.set()` 等待 SharedPreferences `commit()` 后保存到 `dexwallet:wallet-catalog:v1:<userIdx>`，包含 walletId、名称、创建时间、地址/路径、格式/TON 版本、备份核对状态和 pending/ready 状态，不进入同步 Web Storage 缓存。不同产品账户的目录与密钥分开；退出登录保留本机钱包，产品账户不能恢复丢失的秘密。

创建/导入先保存 pending 目录并等待落盘，再经系统确认写入秘密，最后标记 ready。应答或最后一次目录写入丢失时保留原 id，用「解锁核对记录」读取密文并恢复地址/版本；已存在记录不覆盖，不因未知结果重新生成钱包。目录损坏明确报错，不静默重置。已知系统取消等写入前失败撤销本次 pending 目录；目录移除失败可沿用原 id 再次确认。

备份每次系统确认，展示后要求随机三个词位核对，正确后才标记「已核对备份」。移除先展示后果并要求勾选保管备份，再经系统确认删除密钥/密文，成功后删除目录。它只移除本机访问，不删除链上账户或转移资金。

敏感页关闭自动填充，备份展示禁止复制，整个页面启用 Android 防截图。后台时原生遮挡先覆盖，前端清除助记词、导入输入、预览和核对状态，恢复后前端提交清空状态，原生再等待 `WebView.postVisualStateCallback`，保证下一次绘制包含清理结果后才移除遮挡。退出页面也先遮挡，再等待新画面后释放防截图，避免 DOM 已改而旧帧仍显示。切换用户或卸载页面使旧 token 失效；解锁返回后再次核对页面与 App 前台状态，迟到结果不展示。系统确认引发的正常暂停允许已提交保存继续完成，但不会恢复旧备份页面。秘密不写 URL、剪贴板、持久浏览器存储、日志或后端；Java/JS 内存仍是短时信任边界。

这一绘制顺序依据 [Android WebView VisualStateCallback](https://developer.android.com/reference/android/webkit/WebView.VisualStateCallback)，避免把 React 的 DOM 提交当成画面已经完成更新。

## 构建与验证

遵循 [Android 构建环境](Android应用基础.md#工程与构建)，Node 24.22.1、Java 21、Android SDK 36。先构建并同步 App，再执行：

```bash
# frontend/apps/mobile/android；ANDROID_SERIAL 必须为专用测试设备。
./gradlew :app:assembleDebug :app:assembleDebugAndroidTest :app:lintDebug --console=plain
adb -s "$ANDROID_SERIAL" install -r app/build/outputs/apk/debug/app-debug.apk
adb -s "$ANDROID_SERIAL" install -r app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk
adb -s "$ANDROID_SERIAL" shell am instrument -w \
  -e class com.dexwallet.app.WalletVaultStoreTest,com.dexwallet.app.AndroidVaultKeysTest \
  com.dexwallet.app.test/androidx.test.runner.AndroidJUnitRunner
```

原生测试要求专用模拟器已设置系统锁屏，使用非资金测试字符串，独立目录/别名测试后清理；不要在用户真机或有资产的环境中清除锁屏。12 项测试包含真实 Keystore 的 31 秒过期等待；文件行为测试只 fake 密钥供应边界，不能替代系统确认验收。真实取消/批准、重启、并发、锁屏移除及密钥失效结果见 [5.1 验证记录](../design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/04验证记录.md)。
