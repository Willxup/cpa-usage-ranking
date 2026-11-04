# CPA Usage Ranking

[English](README.md)

独立的通用用量排名服务。目前正在搭建基础框架，尚未实现注册、上报、存储或榜单。

## 开发

需要 Go 1.26.10 或更新版本。

```sh
go test ./...
go run ./cmd/ranking
```

默认监听 `http://127.0.0.1:8080`。SIGINT/SIGTERM 停止新接入，并在配置的宽限期内等待进行中的请求完成；关停超时会关闭剩余连接并以失败状态退出。启动和关停事件以 JSON 写入 stderr。

测试统一放在对应模块的 `test/` 目录，不与生产文件混放。

## 前端开发

需要 Node.js 24.15+（24.x）和 npm。运行时仅依赖 React 与 React DOM，当前没有业务功能。Tailwind 仅参与构建，统一色彩、字体和焦点样式在 `web/src/styles/tokens.css`；不请求外部字体或图标服务。当前选用的 React Bits 控件仅使用 CSS，不新增动效运行时；固定来源、改动和 MIT + Commons Clause 授权见[第三方源码说明](THIRD_PARTY_NOTICES.md)。

```sh
npm --prefix web ci
npm --prefix web run dev
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
```

响应式预览包含页内导航、明确的空数据展示，以及未检测/加载/成功/失败状态组件；不伪造名次或用量统计。检测按钮按需调用真实的 `GET /healthz`、校验 JSON 协议，8 秒超时，支持重试，组件卸载时取消请求；不自动轮询。检测成功不代表排名业务就绪。

装饰动效遵循 `prefers-reduced-motion`，控件离屏、禁用或页面隐藏时停止。缺少观察器 API 时降级为静态控件；卸载时移除观察器与监听器。页面不使用 Canvas、WebGL、外部字体或后台 JavaScript 动画循环。

本地开发时，分别在两个终端执行 `go run ./cmd/ranking` 与 `npm --prefix web run dev`。Vite 绑定回环地址，将健康检查代理到 `http://127.0.0.1:8080`，可通过 `RANKING_DEV_PROXY_TARGET` 修改开发代理目标；无需放宽 CORS 或部署生产 Node 服务。

前端测试及初始化放在 `web/src/**/test/`。由于 lint 解析器支持的 TypeScript 上限低于 6.1，暂使用 6.0。其传递依赖 `minimatch` 单独锁定到 11.0.1，修复 GHSA-g3hv-75wg-m249；上游允许安全版本后应移除该 override。

## 全栈验证

安装 Go、Node/npm 和 Make 后：

```sh
make deps       # Go 模块及 npm 锁定依赖
make verify     # 格式、vet、Go 测试、前端类型/lint/测试，以及双端构建
make test-race  # Go 并发检查
make browser-install # 安装唯一的无头 Chromium 测试浏览器
make verify-all      # 基础检查 + race + 正式产物浏览器冒烟
make run        # 构建后由 Go 在 8080 端口提供页面
```

`BIN_DIR` 默认 `bin`，`GO_TEST_FLAGS` 默认 `-p 2`，均可覆盖。构建不依赖本地秘密或内部规划文件。前端产物在 `web/dist/assets/third-party-notices.txt` 中包含 React Bits 来源及完整许可，部署时保留该文件。

浏览器测试及配置位于 `web/test/`。`make test-browser` 构建并启动独立 Go 进程，使用回环端口 4187，验证桌面/手机布局、真实健康请求、键盘/触控、减少动效、失败重试及 404 边界，然后停止进程；不会接管已有服务。端口占用时可覆盖 `RANKING_TEST_PORT`。产物默认在 `web/test/results/`，可用 `PLAYWRIGHT_OUTPUT_DIR`、`PLAYWRIGHT_BROWSERS_PATH` 调整产物及浏览器下载位置。精简 Linux 环境可能需要管理员先补齐 Playwright 的 Chromium 系统依赖。这些冒烟测试不代表已经验证十万客户端容量。

## 正式前端托管

```sh
npm --prefix web ci
npm --prefix web run build
go build -o bin/ranking ./cmd/ranking
RANKING_WEB_DIR=web/dist ./bin/ranking
```

Go 同域提供构建后的页面和 API。部署二进制、`web/dist/` 及第三方声明/许可文件，线上不需要 Node 服务。`RANKING_WEB_DIR` 的相对路径以进程工作目录为基准；未设置时仅启用后端路由，设置后若缺少完整入口文件则启动失败。

仅提供 `/`、`/index.html`、`/favicon.svg` 和 `/assets/` 下实际存在的文件，不做任意 SPA 页面回退或目录列表。缺失资源、未知 API 和未知页面保持 404；拒绝目录穿越及越界软链接。HTML 和非指纹文件需要重新验证，带构建指纹的资源可缓存一年。运行期间不要原地修改资源，应停止进程、更换构建后重启。

## HTTP 基础

- `GET /healthz`：200 与 `{"status":"ok"}` 仅表示进程存活。
- `GET /readyz`：业务尚未实现时返回 503，不假报就绪。
- 未知路由和不支持的方法返回 JSON 错误（404/405）。
- 异常响应不暴露细节，日志不记录请求正文、请求头或 panic 值。
- 默认不信任客户端提供的转发 IP 头。

## 配置

| 环境变量 | 默认值 | 含义 |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `127.0.0.1:8080` | 监听地址，数值端口 0 表示选择空闲端口 |
| `RANKING_SHUTDOWN_TIMEOUT` | `10s` | 正数形式的优雅关停等待时间 |
| `RANKING_WEB_DIR` | 未设置 | 可选的前端构建目录；未设置时仅启用后端 |

显式空值或非法配置会校验失败。不自动加载配置文件，不提交凭据或本地 `.env` 文件。
