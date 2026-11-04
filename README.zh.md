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

需要 Node.js 24.15+（24.x）和 npm。运行时仅依赖 React 与 React DOM，当前没有业务功能。Tailwind 仅参与构建，统一色彩、字体和焦点样式在 `web/src/styles/tokens.css`；不请求外部字体或图标服务。

```sh
npm --prefix web ci
npm --prefix web run dev
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
```

前端测试及初始化放在 `web/src/**/test/`。由于 lint 解析器支持的 TypeScript 上限低于 6.1，暂使用 6.0。其传递依赖 `minimatch` 单独锁定到 11.0.1，修复 GHSA-g3hv-75wg-m249；上游允许安全版本后应移除该 override。

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

显式空值或非法配置会校验失败。不自动加载配置文件，不提交凭据或本地 `.env` 文件。
