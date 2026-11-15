# Android 本机密钥库

[项目入口](../README.md) · [Android 应用基础](Android应用基础.md) · [5.1 实施方案](../design/工程工作记录/05-钱包与链上通道/5.1-用户钱包接入/02实施方案.md)

## 当前边界

2026-11-16 已实现供内置钱包使用的原生加密存储组件和系统锁屏确认。没有开放创建、导入、备份或移除的产品页面，没有生成真实资金钱包，也没有地址派生、签名、链上查询或资金交易。Mini App 不调用此组件。数据库仍为 business 15，无后端契约或业务变化。

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
| put | userIdx、walletId、secret | 空对象 | 非空 UTF-8 字符串，最多 4096 bytes；验证后首次写入，重复拒绝 |
| get | userIdx、walletId | secret | 每次系统确认，格式/完整性/密钥异常明确失败 |
| remove | userIdx、walletId | 空对象 | 每次系统确认；删除密文和对应密钥，可清理损坏记录；重复移除仍需确认 |

userIdx/walletId 允许 1–128 个字母、数字、下划线或连字符。walletId 是客户端内部标识，不新增对外业务 ID。系统提示按保存、读取和移除分别说明目的；移除提醒后续需要备份恢复。

稳定错误：`VAULT_INVALID_INPUT`、`VAULT_EXISTS`、`VAULT_NOT_FOUND`、`VAULT_CORRUPTED`、`VAULT_KEY_UNAVAILABLE`、`VAULT_STORAGE_FAILED`、`VAULT_DEVICE_LOCK_REQUIRED`、`VAULT_CANCELLED`、`VAULT_BUSY`、`VAULT_AUTH_EXPIRED`、`VAULT_INTERRUPTED`、`VAULT_UNAVAILABLE`。错误不返回空秘密或触发自动重建。丢失系统密钥后，重新设置锁屏不能恢复原密钥；记录保留并拒绝覆盖，须明确移除后从用户备份导入。

## 后续页面接入条件

创建/导入前先通过现有原生持久存储保存本地 walletId 与必要的非秘密元信息，并等待落盘，避免原生写入已完成而前端丢失索引。提交应答丢失时用原 walletId 查询并在重新验证后核对，不自动生成另一钱包或覆盖原记录。

创建/导入/备份页面需固定支持的助记词方案、派生路径和 TON 合约版本，验证恢复同一地址；同时接入本机解锁、备份核对、移除前确认、敏感页面防截图/后台遮挡及异步返回清理。这些是完整内置钱包开放前的剩余工作。此组件不自动识别助记词，不把任意 BIP39/TON 输入当成同一个钱包。

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
