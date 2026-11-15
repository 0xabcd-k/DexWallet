# Android 应用基础

[项目入口](../README.md) · [前端公共基础](前端公共基础.md) · [8.4 实施记录](../design/工程工作记录/08-发布与多端扩展/8.4-独立App工程/03实施记录.md)

## 当前交付边界

8.4 第一批使用 Capacitor 包装现有 React 用户端，Android 包名为 `com.dexwallet.app`。App 独立构建并打包本地页面，复用邮箱登录、账户、任务、奖励、美元余额互转和账单；本批实测使用手动填写付款，相机权限与原生扫码尚未验收。App 完整功能的产品目标保持；本批基础工程不等于 8.4/8.5 全部完成。

5.1 第一批提供 TON 外部钱包连接页：Tonkeeper、MyTonWallet、名称搜索、连接、取消、切换、断开、会话恢复和原生返回。连接页仅显示钱包、地址、网络，不查询资产、不请求签名或发起交易。连接信息不建立服务端地址所有权，不允许据此入账。5.1 后续已接通独立 [Android 本机密钥库](Android本机密钥库.md)；创建、导入、备份页面及其他链仍待实施。

邮箱使用既有身份规则：通过 Mini App 已绑定的邮箱登录，可进入同一个产品账户；未绑定的新邮箱会按现有规则建立新账户。App 独立 Telegram 授权尚未实现，不能把浏览器输入的 Telegram ID 当成登录凭据。真实发信、真实频道、真实风控和生产抽奖开放仍按各自阶段验收。

## 工程与构建

- `frontend/apps/user`：既有 Mini App 构建保持；`vite build --mode native` 生成独立 `dist-app`。
- `frontend/apps/mobile`：Capacitor 配置、Android 工程与构建入口；没有另建一套业务页面。
- 原生模块和钱包连接路由仅在 native 构建出现，普通三端构建检查继续检查 Mini App 隔离。
- 固定依赖：Capacitor core/android/cli 8.6.2、App 插件 8.1.2、App Launcher 8.0.1、Preferences 8.0.1、TON Connect SDK 5.0.1。Android 工程来自该版 CLI，Gradle 8.14.3 bin 附官方 SHA-256，AGP 8.13.0；最低 API 24，编译/目标 API 36。

环境使用项目 Node/pnpm 版本、Java 21 和 Android SDK 36。先在 `frontend/` 执行 `pnpm install --frozen-lockfile`，配置 `ANDROID_HOME`，再执行：

```bash
export DEXWALLET_APP_API_URL=https://api.example.com
export DEXWALLET_APP_TON_MANIFEST_URL=https://app.example.com/tonconnect-manifest.json
pnpm --filter @dexwallet/mobile build:android
```

API URL 可包含部署前缀，不放密码或 token。Manifest 是公开 HTTPS JSON，包含 `url`、`name` 和 PNG 图标的 `iconUrl`，必须由外部钱包实际可访问；其中域名对应实际 App 服务。没有 manifest 配置时连接页明确显示未配置，其他页面可用。正式域名/图标由部署落实，临时测试隧道不能作为发布配置。

调试 APK 位于 `frontend/apps/mobile/android/app/build/outputs/apk/debug/app-debug.apk`。`build:android --release` 生成未签名 release 包；商店/分发签名、证书、域名、正式下载及 iOS 构建不在本批验收范围。不要提交 `local.properties`、密钥库、Google 服务凭据或构建产物。

## 本机模拟器与真实 API

调试仅可显式设置 `DEXWALLET_APP_DEBUG_HTTP=1`，API HTTP 主机限于 `127.0.0.1`、`localhost`、`10.0.2.2`。Android debug 网络规则也只放行这些主机；release 保持禁止明文，并拒绝沿用 debug 的混合内容配置。

`backend/api/tools/appdemo` 是带 `integration` 编译标记的本地夹具，不编入正式 API。它通过既有 testenv 守卫创建独立数据库和 Redis 命名空间，迁移至 business 15，创建已绑定 Telegram 的邮箱测试账户、100 USD 测试记账及频道 mock 任务。测试码只写本次私有目录的 `mail.json`，公开元信息在 `app.json`；不提供获取验证码的 HTTP 接口。监听仅 `127.0.0.1:18889`，正常停止会精确清理数据库和本次 Redis key，中断后按现有 testenv recovery 操作。

```bash
# 仓库根；配置文件只包含文档要求的本地专用测试连接，不使用生产目标。
set -a
source .tools/local.env
set +a
cd backend
go run -tags=integration ./api/tools/appdemo
```

另一终端连接本次模拟器，通过 `adb -s <serial> reverse tcp:18889 tcp:18889` 转发本机 API；`DEXWALLET_APP_API_URL=http://127.0.0.1:18889`、`DEXWALLET_APP_DEBUG_HTTP=1` 构建安装调试 APK。Manifest 如需临时隧道，只公开专用静态目录，禁止公开测试 API、验证码或 `.tools/`。

## 账户与连接生命周期

原生 WebView 使用 `https://localhost` 来源；API 配置增加可选 `CORSOrigins: ["https://localhost"]`，默认空列表关闭跨域。允许预检及读取 `X-Access-Token`、`X-Trace-Id`，继续使用相同 JWT 验签、续签和身份版本规则。CORS 不是 App 真实性证明，不作为未来链上 API 的授权依据。

`com.dexwallet.app://wallet-return` 只导航到连接页，拒绝携带 query、fragment、用户信息或其他目标。钱包地址、网络和连接结果从 TON Connect 的桥接会话获取；回到前台同步产品会话，不自动重复业务 POST。Android 返回键返回上一页或将 App 放入后台。

App 在加载业务模块前通过 `NativeState` 插件从 Android SharedPreferences 恢复本地状态，以同步缓存适配既有 Storage 调用；JWT、语言和原业务请求标识以原生持久数据为准。该插件在 Capacitor 插件线程使用 `commit()` 并检查成功结果，不能用 Preferences 插件的异步 `apply()` 回执代替落盘成功。登录/续签/退出完成及 API 发出前等待本地写入，写入失败则明确阻止请求，修复存储条件并重启后恢复，不能跳过原请求标识持久化后提交资金请求。Mini App 存储不变。TON Connect 协议会话材料通过 Capacitor Preferences 保存在 Android 应用私有偏好存储，并按产品用户 idx 隔离；重启实测暴露 WebView 连接存储丢失后改用此路径。关闭页面会停止其连接监听和后续存储写入，保留已保存状态以便恢复；取消连接清除本账户协议状态，切换先断开原钱包。SDK 分析上报已关闭。Android 禁止自动备份，并从设备迁移排除根目录和偏好存储。这些数据不是资金助记词/私钥。5.1 已另行建立 [Android 本机密钥库](Android本机密钥库.md)，不能把该 Preferences 路径当成资金密钥库。

## 验证与下一批

普通检查保持 `bash scripts/check.sh common`、浏览器回归保持 `pnpm test:e2e`；原生 APK 和 Android 实测单独执行，不能将浏览器 mock 当作钱包真机兼容结论。具体本轮结果见 [8.4 验证记录](../design/工程工作记录/08-发布与多端扩展/8.4-独立App工程/04验证记录.md)。

2026-11-15 续验已完成 Keeper 26.10.4 / My Wallet 26.10.7 的真实连接、回跳、切换、恢复、断开与取消边界。下一批继续 5.1 内置钱包密钥保护、创建/导入/备份及 App 独立 Telegram 授权；随后接 5.2 资产查询与选择。四链九资产组合仍在产品范围，逐通道签名、Gas、充值/转出和组合支付继续沿用已确认方案与独立验收。

Keeper Android 26.10.4 未声明目录中的 `tonkeeper-tc://`，本批根据 APK 和官方文档使用 `tonkeeper://ton-connect`。不能只根据目录声明就认定当前安装包兼容；MyTonWallet 使用其公布的连接 scheme。

官方依据：[Keeper 深链](https://docs.tonapi.io/tonkeeper/deep-linking)、[Capacitor Android](https://capacitorjs.com/docs/android)、[App 生命周期](https://capacitorjs.com/docs/apis/app)、[TON Connect SDK](https://github.com/ton-connect/sdk/blob/main/packages/sdk/README.md)、[TON Connect manifest](https://github.com/ton-blockchain/ton-connect/blob/main/spec/manifest.md)。

原生状态落盘依据：[SharedPreferences.Editor.commit](https://developer.android.com/reference/android/content/SharedPreferences.Editor#commit())。该保护不涵盖卸载、手动清除 App 数据或损坏的设备存储。

本地模拟器 37.2.12 曾伴随图形错误退出；本轮隔离安装 37.1.11 后完成对照验证。版本来源、校验值和启动命令见 [续验环境](../design/工程工作记录/08-发布与多端扩展/8.4-独立App工程/04验证记录.md#模拟器问题与可复用环境)，不要求覆盖开发者的 SDK。
