# scrapio

scrapio 是面向自用和小团队的采集平台。Go 服务负责采集任务、工作流、数据与管理界面；[scrapio-browser](https://github.com/nekoimi/scrapio-browser) 提供浏览器执行能力；PostgreSQL 保存任务和结果。当前仓库的 `web/` 是 Vue 3 管理界面，生产镜像会将其与 Go 服务一同打包。

当前实现包含声明式工作流、来源与资源管理、运行历史、插件任务监控，以及旧磁力采集能力。v2.1 的通用采集产品目标仍在[产品规划](docs/项目文档v2.1/采集平台v2.1产品规划.md)中，不应视为全部已交付。

新独立采集器/编排任务入口为 `/app/co/home`，支持多阶段输入衔接、表/文件冻结输入、固定版本计划、局部补采和数据导出。旧工作台保留；开发收尾与真实发版验收分开。配置和边界见 [O4实施与验收记录](docs/项目文档v2.2/输入来源与日常治理O4实施与验收记录.md)。

阶段可选择合并本次运行内的相同完整输入，保留全部来源并共享一次执行/下游交付；缺省仍逐来源独立执行。范围与发版核对见 [O5-01实施记录](docs/项目文档v2.2/同运行重复输入合并O5-01实施与验收记录.md)。

## 快速部署

需要 Docker Compose、可用的 CloakBrowser Manager 和浏览器 Profile。复制 `docker-compose.example.yaml` 为 `docker-compose.yaml`，然后在同目录提供 `.env`：

```dotenv
POSTGRES_PASSWORD=replace-with-a-strong-password
JWT_SECRET=replace-with-a-long-random-secret
CLOAK_MANAGER_URL=https://your-cloak-manager.example
CLOAK_PROFILE_ID=your-profile-id
# 按需填写
# CLOAK_AUTH_TOKEN=
# SCRAPIO_BROWSER_IMAGE=ghcr.io/nekoimi/scrapio-browser:test
```

```bash
docker compose up -d
```

示例 Compose 启动 PostgreSQL、scrapio-browser 和 scrapio，应用默认访问地址为 `http://localhost:8093`。浏览器镜像默认使用目前浏览器仓库手动发布工作流生成的 `:test` 标签；可通过 `SCRAPIO_BROWSER_IMAGE` 指定自行构建或发布的标签。浏览器服务需要能访问 CloakBrowser Manager。

已有 PostgreSQL 数据卷升级时，保留原来的 `POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD` 和卷名；示例中 `scrapio` 仅是新安装的默认数据库名与用户，不会自动重命名旧数据库。更多说明见[部署文档](docs/项目文档v1.0/deployment.md)。

## 本地开发

- Go 1.26
- Node.js 22、pnpm 10
- PostgreSQL 17（或与现有数据库兼容的版本）
- 运行中的 scrapio-browser（默认 gRPC 端口 8191）

```bash
git clone https://github.com/nekoimi/scrapio.git
cd scrapio
cp config/dev.yaml.example config/dev.yaml
# 配置 PostgreSQL DSN、浏览器地址及 JWT_SECRET
go run ./cmd/main.go
```

管理界面：

```bash
cd web
pnpm install --frozen-lockfile
pnpm dev
```

生产前端构建使用 `pnpm build`，根目录 `Dockerfile` 会自动执行并将产物复制到 `/workspace/ui`。不再需要 Git submodule 或 AriaNg 静态站点。

配置沿用当前兼容键 `CRAWLER_DRISSION_ROD_GRPC_IP` / `CRAWLER_DRISSION_ROD_GRPC_PORT` 连接 scrapio-browser。数据库使用 `DB_DSN`，生产环境应设置 `JWT_SECRET`。其他选项参见 `config/*.yaml.example`。下载交付功能由插件承载，需要时另行配置；它不是基础部署的必需服务。

SQL 明细默认关闭，避免后台任务空闲轮询刷屏。排查 SQL 时在 YAML 设置 `db.show_sql: true`，或使用环境变量 `DB_SHOW_SQL=true` 后重启；该开关独立于 `LOG_LEVEL`，关闭 SQL 明细不会关闭业务 debug、警告和错误日志。

独立任务worker的轮询SQL另按会话关闭，保留业务错误日志。O4全局容量通过 `SCRAPIO_CO_HTTP_SLOTS`、`SCRAPIO_CO_BROWSER_SLOTS`、`SCRAPIO_CO_SITE_INTERVAL_MS`、`SCRAPIO_CO_ACTIVE_RUNS`、`SCRAPIO_CO_SOURCE_STORAGE_MIB`、`SCRAPIO_CO_OUTPUT_STORAGE_MIB` 和 `SCRAPIO_CO_SOURCE_RETENTION_DAYS` 配置，compose已提供保守默认值。站点间隔约束工作项启动，JSON容量配额不替代数据库磁盘监控；具体预算、来源保留和排错见O4记录。计划复用已确认输入快照，新增目标需重新取样发布。

v2.2 设置页支持只写入的 HTTP/浏览器 Cookie 凭据。加密保存需设置 `SCRAPIO_CREDENTIAL_KEY`（32字节随机值的标准 Base64，可用 `openssl rand -base64 32` 生成），请在部署环境保存并备份。环境引用仅使用明确传入的 `SCRAPIO_SECRET_<NAME>`。浏览器授权需要同步升级 scrapio-browser 的 `editor.auth.v1`；旧协议不会降级为无授权采集。详见 [D04 凭据与授权会话](docs/项目文档v2.2/D04-凭据与授权会话.md)。

## 目录

- `cmd/`：Go 服务入口
- `internal/`：API、采集、工作流、存储和插件实现
- `web/`：Vue 管理界面
- `proto/`：浏览器协议
- `config/`：配置示例
- `docs/项目文档v2.0/`：历史改造方案
- `docs/项目文档v2.1/`：下一阶段产品规划

许可证：[MIT](LICENSE)。
