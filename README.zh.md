# CPA Usage Ranking

[English](README.md)

独立的通用用量排名服务。目前已具备受保护注册、日累计快照上报、后台日结、静态头像及 SQLite 存储。启动时校验[模型目录](docs/model-catalog.md)，将精确名称及别名映射至稳定 ID；本次交付未重新核实参考价格。统一缓存针对六个固定 UTC 周期构建五个真实榜单，详见[榜单计算说明](docs/leaderboards.md)；没有网站账号 UI。

## 开发

需要 Go 1.26.10 或更新版本、启用 CGO，以及 C 编译器（如 GCC）。

```sh
go test ./...
go run ./cmd/ranking
```

默认监听 `http://127.0.0.1:8080`。SIGINT/SIGTERM 停止新接入，并在配置的宽限期内等待进行中的请求完成；关停超时会关闭剩余连接并以失败状态退出。启动和关停事件以 JSON 写入 stderr。

测试统一放在对应模块的 `test/` 目录，不与生产文件混放。基于 SQLite 文件的 HTTP 上报生命周期回归覆盖注册、重试、修正、日结、缓存读取及重启恢复：

```sh
go test ./internal/httpserver/test -run '^TestCompleteIngestionLifecycle$'
```

## 存储基础

启动时在监听 HTTP 之前打开 `data/ranking.db`，可通过 `RANKING_SQLITE_PATH` 覆盖。自动创建缺失的父目录和迁移版本表，启用 WAL、FULL 同步和外键。每个迁移版本的变更与成功记录在同一事务中提交，失败回滚该版本，已完成版本不重复执行；版本 1 建立 `clients` 身份与资料表，版本 2 建立稳定标准模型 ID 字典，启动时以事务登记当前目录的标准名称；版本 3–4 增加持久封窗水位和日快照表，版本 5 增加已结周期统计；不填充模拟参与者。连接或迁移失败直接退出，不自动修复或回退。关停先排空 HTTP，再关闭有界读写连接池，业务就绪还要求真实缓存完整构建成功。具体约定见 [SQLite 存储说明](docs/storage.md)和[客户端存储说明](docs/client-storage.md)。

注册应用公开[昵称规则](docs/display-names.md)及头像目录校验，不提供资料修改接口。[静态头像目录](docs/avatar-catalog.md)校验编号、默认项和资源；二进制嵌入获准复用的 66 张 WebP（默认编号 1），通过 `/avatars/catalog.json` 和 `/avatars/images/001.webp` 至 `066.webp` 分发，不依赖前端目录。
[凭据解析与摘要](docs/client-credentials.md)接受客户端预先保存的 32 字节 CSPRNG 秘密，使用规范无填充 base64url Bearer 编码；服务端仅存 SHA-256 摘要，不签发秘密。
`POST /v1/clients/register` 已接通[受保护注册 API](docs/registration-api.md)：新建返回 201，相同秘密重试返回 200 及当前资料、不覆盖。客户端首次尝试前先生成并保存 32 字节 CSPRNG 秘密，通过 HTTPS Bearer 携带。来源/全局请求限流包含失败和重试；成功新建另受滚动配额（默认 3/小时、10/24 小时）保护。有界内存状态仅属于单机进程、重启清零。`RANKING_REGISTRATION_ENABLED=false` 重启后仅关闭新建，保留已有身份重试和鉴权；不是全站停机。请求、响应、状态码、代理信任与全部部署参数见 API 文档。内部[鉴权中间件](docs/client-credentials.md#internal-identity-enforcement)仅凭秘密选择身份，不增加演示 `/me` API。

`PUT /v1/reports/{utc_day}` 接收该凭据所属客户端的 UTC 今天或昨天完整快照。版本、目录无关的幂等重试、持久封窗、限流及原子替换见[日快照上报 API](docs/report-snapshots.md)。后台有界日结，并清理过期日摘要和周统计，不删除未结快照。内部候选读取在同一 SQLite 快照中合并已结和待结用量；日结后不保留逐模型日历史，见[存储说明](docs/storage.md)。

## 前端开发

需要 Node.js 24.15+（24.x）和 npm。运行时仅依赖 React 与 React DOM。Tailwind 仅参与构建，统一色彩、字体和焦点样式在 `web/src/styles/tokens.css`；不请求外部字体或图标服务。当前选用的 React Bits 控件仅使用 CSS，不新增动效运行时；固定来源、改动和 MIT + Commons Clause 授权见[第三方源码说明](THIRD_PARTY_NOTICES.md)。

```sh
npm --prefix web ci
npm --prefix web run dev
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
```

页面采用浅色宽屏布局、橙紫点缀和紧凑桌面尺寸，不使用页面 zoom，手机端独立适配。页面提供五榜和今日、昨日、本周、本月四个周期，默认今日，不伪造汇总数字；年度和累计仍由后端维护，但不在页面提供入口。项目图标资源及重新生成方法见[图标说明](docs/brand-icons.md)。

响应式页面提供榜单加载、空态、错误和重试，切换或重试时旧结果始终保留其原榜单、周期和缓存生成时间标签；还有独立的连接状态面板。不伪造名次或用量统计。昵称按文本显示，头像来自服务端目录；缺项使用目录默认头像，加载失败使用中性回退。检测按钮按需调用真实的 `GET /healthz`、校验 JSON 协议，8 秒超时，支持重试，组件卸载时取消请求；不自动轮询。检测成功不代表排名业务就绪。

装饰动效遵循 `prefers-reduced-motion`，控件离屏、禁用或页面隐藏时停止。缺少观察器 API 时降级为静态控件；卸载时移除观察器与监听器。页面不使用 Canvas、WebGL、外部字体或后台 JavaScript 动画循环。

本地开发时，分别在两个终端执行 `go run ./cmd/ranking` 与 `npm --prefix web run dev`。Vite 绑定回环地址，将健康检查、榜单和头像请求代理到 `http://127.0.0.1:8080`，可通过 `RANKING_DEV_PROXY_TARGET` 修改开发代理目标；无需放宽 CORS 或部署生产 Node 服务。

前端测试及初始化放在 `web/src/**/test/`。由于 lint 解析器支持的 TypeScript 上限低于 6.1，暂使用 6.0。其传递依赖 `minimatch` 单独锁定到 11.0.1，修复 GHSA-g3hv-75wg-m249；上游允许安全版本后应移除该 override。

## 全栈验证

安装 Go、Node/npm 和 Make 后：

```sh
make deps       # Go 模块及 npm 锁定依赖
make verify     # 格式、vet、Go 测试、前端类型/lint/测试，以及双端构建
make test-race  # Go 并发检查
make browser-install # 可选：没有可复用浏览器时才安装
make verify-all      # 基础检查 + race + 正式产物浏览器冒烟
make run        # 构建后由 Go 在 8080 端口提供页面
```

`BIN_DIR` 默认 `bin`，`GO_TEST_FLAGS` 默认 `-p 2`，均可覆盖。构建不依赖本地秘密或内部规划文件。前端产物在 `web/dist/assets/third-party-notices.txt` 中包含 React Bits 来源及完整许可，部署时保留该文件。

浏览器测试及配置位于 `web/test/`。`make test-browser` 构建并启动独立 Go 进程，使用回环端口 4187，验证桌面/手机布局、真实健康请求、键盘/触控、减少动效、失败重试、404 边界、66 张头像解码及真实注册/安全重试，然后停止进程；不会接管已有服务。端口占用时可覆盖 `RANKING_TEST_PORT`。产物默认在 `web/test/results/`，可用 `PLAYWRIGHT_OUTPUT_DIR`、`PLAYWRIGHT_BROWSERS_PATH` 调整产物及浏览器下载位置。浏览器检查使用结果目录旁 `databases/<port>/` 中的独立测试库，不访问应用默认数据库。精简 Linux 环境可能需要管理员先补齐 Playwright 的 Chromium 系统依赖。这些冒烟测试不代表已经验证十万客户端容量。

Playwright 不再要求精确版本（`@playwright/test: "*"`）。npm 锁文件保留干净安装时的已解析依赖记录，不是运行时版本检查；已有兼容依赖可以直接复用，不必再次执行 `make deps`。浏览器测试不会自动安装浏览器，也不要求项目专属的浏览器 revision。复用已有 Chromium 可执行文件：

```sh
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/path/to/chromium make test-browser
```

也可以设置 `PLAYWRIGHT_CHROMIUM_CHANNEL=chrome` 复用已安装的 Chrome 通道，两项只选一项。不设置时沿用 Playwright 默认查找方式（包括 `PLAYWRIGHT_BROWSERS_PATH`）；仅改变缓存目录并不能发现不同 revision 的浏览器，跨 revision 复用应指定可执行文件。只有确实没有可用浏览器时才运行 `make browser-install`，兼容性以实际运行的工具和浏览器验证结果为准。

## 正式前端托管

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/ranking ./cmd/ranking
RANKING_WEB_DIR=web/dist ./bin/ranking
```

Go 同域提供构建后的页面和 API。部署二进制、`config/models.json`、`web/dist/` 及第三方声明/许可文件，线上不需要 Node 服务。`RANKING_WEB_DIR` 的相对路径以进程工作目录为基准；未设置时仅启用后端路由，设置后若缺少完整入口文件则启动失败。

仅提供 `/`、`/index.html`、`/favicon.svg` 和 `/assets/` 下实际存在的文件，不做任意 SPA 页面回退或目录列表。缺失资源、未知 API 和未知页面保持 404；拒绝目录穿越及越界软链接。HTML、非指纹文件及公共资源子目录中的稳定文件名需要重新验证，顶层带构建指纹的资源可缓存一年。运行期间不要原地修改资源，应停止进程、更换构建后重启。

## HTTP 基础

- `GET /healthz`：200 与 `{"status":"ok"}` 仅表示进程存活。
- `GET /readyz`：仅在数据库和目录已初始化且首轮真实缓存完整成功后返回 200；此前即使 `/healthz` 健康也返回 503。后续刷新失败保留上轮完整缓存，不额外规定缓存过期 SLA。
- `POST /v1/clients/register`：受保护创建/安全重试，见[注册 API](docs/registration-api.md)。
- `PUT /v1/reports/{utc_day}`：受保护的完整日快照，见[上报 API](docs/report-snapshots.md)。
- `GET /v1/leaderboards/{board}/{period}`：固定公开缓存榜单，见[接口与评分说明](docs/leaderboards.md)。
- `GET`/`HEAD /avatars/catalog.json` 及目录列出的 `/avatars/images/*` 图片。
- 未知路由和不支持的方法返回 JSON 错误（404/405）。
- 异常响应不暴露细节，日志不记录请求正文、请求头或 panic 值。
- 默认不信任客户端提供的转发 IP 头；显式代理配置见[注册来源说明](docs/registration-sources.md)。

## 配置

| 环境变量 | 默认值 | 含义 |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `127.0.0.1:8080` | 监听地址，数值端口 0 表示选择空闲端口 |
| `RANKING_SHUTDOWN_TIMEOUT` | `10s` | 正数形式的优雅关停等待时间 |
| `RANKING_CACHE_REFRESH_INTERVAL` | `5m` | 内部缓存定时刷新间隔，必须为正数 |
| `RANKING_WEB_DIR` | 未设置 | 可选的前端构建目录；未设置时不提供前端路由 |
| `RANKING_SQLITE_PATH` | `data/ranking.db` | SQLite 文件路径，相对路径以进程工作目录为基准 |
| `RANKING_MODELS_PATH` | `config/models.json` | 模型目录文件，相对路径以进程工作目录为基准 |
| `RANKING_TRUSTED_PROXIES` | 未设置（无） | 逗号分隔的可信代理 IP/CIDR |
| `RANKING_REGISTRATION_IPV6_64` | `true` | 原生 IPv6 注册来源按 /64 分组 |

注册限额、请求体边界和新建开关见[注册 API 配置](docs/registration-api.md#startup-configuration)，上报接入和写入队列参数见[上报 API](docs/report-snapshots.md)。显式空值或非法配置会校验失败。模型目录为启动必需；环境变量不从 `.env` 文件自动加载，不提交凭据或本地 `.env` 文件。
