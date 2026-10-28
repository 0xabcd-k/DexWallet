# 项目入口

- 从 [项目文档](docs/README.md) 开始，当前内容为新版项目定义；`docs/archive/` 是历史依据，不是当前实施规范。
- 阅读 [开发规范](docs/engineering/开发规范.md) 与 [决议台账](docs/decisions/决议与待落实事项.md)，保留已确认规则与本版方案的区别。
- 说明、设计、规范、验收统一维护在 `docs/`。修改业务规则时同步相关流程、界面和验收，避免重复定义。
- 已初始化最小 Go module 与前端 workspace，准备和检查见 [工程结构](docs/engineering/工程结构.md)。已建立隔离契约样例与生成/迁移工具，实际入口见 [契约生成与迁移](docs/engineering/契约生成与迁移.md)；1.3 已建立健康接口、依赖客户端及后端公共基础，启动和验证见 [后端公共基础](docs/engineering/后端公共基础.md)；1.4 三端骨架及前端检查见 [前端公共基础](docs/engineering/前端公共基础.md)；正式业务 API 与业务迁移尚未建立。先改源定义，保留手写扩展边界。不得把文档中的概念契约声称为已实现接口。
- 工程清单检查：`backend/` 下执行 `go list -m -json`；`frontend/` 下按 `.nvmrc` 切换 Node 后执行 `pnpm install --frozen-lockfile`、`pnpm -r list --depth -1 --json`。
- 契约检查：`bash scripts/check_contracts.sh`；生成：`python3 scripts/generate.py`。MySQL/Redis/RocketMQ 集成测试使用独立入口，不随普通检查运行。后端补充检查：`go test -race ./...`、`go vet ./...`。
- 前端检查：`frontend/` 下执行 `pnpm typecheck`、`pnpm test`、`pnpm build`、`pnpm test:e2e`；浏览器需要先安装 Chromium/WebKit。真实健康联通为独立 `pnpm test:integration`，需要专用测试 MySQL DSN，详见前端公共基础。
- 1.5 本地准备、隔离账号和恢复清理见 [开发与测试环境](docs/engineering/开发与测试环境.md)。统一检查 `bash scripts/check.sh [common|browser|integration|http|all]`，默认 common 不连接中间件；真实范围必须显式提供专用配置，禁止生产目标。
- 文档检查：`python3 scripts/check_docs.py`；差异检查：`git diff --check`。
- 保护用户已有改动；每轮依用户要求先同步当前远端同名分支，交付时仅提交本轮内容并推送。新分支以已同步起点为准，不自动合并主分支。

本文件补充仓库入口，不替代用户提供的开发习惯、通知和 Git 操作要求。
