# 项目入口

- 从 [项目文档](docs/README.md) 开始，当前内容为新版项目定义；`docs/archive/` 是历史依据，不是当前实施规范。
- 阅读 [开发规范](docs/engineering/开发规范.md) 与 [决议台账](docs/decisions/决议与待落实事项.md)，保留已确认规则与本版方案的区别。
- 说明、设计、规范、验收统一维护在 `docs/`。修改业务规则时同步相关流程、界面和验收，避免重复定义。
- 已初始化最小 Go module 与前端 workspace，准备和检查见 [工程结构](docs/engineering/工程结构.md)。已建立隔离契约样例与生成/迁移工具，实际入口见 [契约生成与迁移](docs/engineering/契约生成与迁移.md)；1.3 已建立健康接口、依赖客户端及后端公共基础，启动和验证见 [后端公共基础](docs/engineering/后端公共基础.md)；1.4 三端骨架及前端检查见 [前端公共基础](docs/engineering/前端公共基础.md)；2.1 已建立用户/邮箱/TG model、business 迁移与内部用例，入口见 [用户模型](docs/engineering/用户模型.md)；2.2 内部验证码、Mini App 验签与身份变更见 [用户鉴权](docs/engineering/用户鉴权.md)；2.3 已接通单 JWT、响应头续签、30 秒用户缓存与公开认证 API，2.4 已接通用户端登录、凭证持久化、账户与身份管理，2.5 已接通管理/商户登录、权限与 super，运行见 [后台鉴权](docs/engineering/后台鉴权.md)。先改源定义，保留手写扩展边界。不得把文档中的概念契约声称为已实现接口。
- 工程清单检查：`backend/` 下执行 `go list -m -json`；`frontend/` 下按 `.nvmrc` 切换 Node 后执行 `pnpm install --frozen-lockfile`、`pnpm -r list --depth -1 --json`。
- 3.1 已建立按用户/种类分行的资金账户、精确金额规则与本人余额查询，钱包/奖励页已接入；交付和数据库版本见 [账户模型实施记录](docs/design/工程工作记录/03-资金账户与账本/3.1-账户与金额模型/03实施记录.md)。3.2 已建立完整凭证记账与本人流水查询，入口见 [账本与流水](docs/engineering/账本与流水.md)。3.3 内部占用/核销/释放和本人明细见 [占用与释放](docs/engineering/占用与释放.md)。资金写入通过 Post/Hold/Consume/Release 与调用方事务；3.4 内部美元互转单据、幂等及恢复见 [幂等与事务](docs/engineering/幂等与事务.md)，数据库要求 business 8；公开充值、发奖和转账尚未开放。
- 契约检查：`bash scripts/check_contracts.sh`；生成：`python3 scripts/generate.py`。MySQL/Redis/RocketMQ 集成测试使用独立入口，不随普通检查运行。后端补充检查：`go test -race ./...`、`go vet ./...`。
- 后端共享依赖统一在 `backend/svc/` 的 `ServiceContext` 组装和关闭；API 内的 svc 仅保留 goctl 类型别名。正式 main/handler/context 共性改动先修改 `backend/template/api/`，再生成；已有手写骨架不自动覆盖，细节见契约生成与后端公共基础说明。
- 前端检查：`frontend/` 下执行 `pnpm typecheck`、`pnpm test`、`pnpm build`、`pnpm test:e2e`；浏览器需要先安装 Chromium/WebKit。真实健康联通为独立 `pnpm test:integration`，需要 `DEXWALLET_TEST_HTTP_MYSQL_DSN` 及本地测试目标配置，详见开发与测试环境。
- 1.5 本地准备、隔离账号和恢复清理见 [开发与测试环境](docs/engineering/开发与测试环境.md)。统一检查 `bash scripts/check.sh [common|browser|integration|http|all]`，默认 common 不连接中间件；真实范围必须显式提供专用配置，禁止生产目标。
- 文档检查：`python3 scripts/check_docs.py`；差异检查：`git diff --check`。
- 保护用户已有改动；每轮依用户要求先同步当前远端同名分支，交付时仅提交本轮内容并推送。新分支以已同步起点为准，不自动合并主分支。

本文件补充仓库入口，不替代用户提供的开发习惯、通知和 Git 操作要求。
