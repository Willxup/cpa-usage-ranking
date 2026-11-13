# 管理控制台

## 启用与登录

后台默认关闭，公开服务不受影响。设置 `RANKING_ADMIN_CONFIG_FILE` 指向静态资源目录之外的私有 JSON 文件，然后重启服务：

```json
{
  "username": "<填写主管理员用户名>",
  "password": "<填写自行生成的高强度密码>"
}
```

用户名为 3–64 个 ASCII 字母、数字、`_`、`.`、`-`，首字符为字母或数字；区分大小写。密码为 12–72 字节（bcrypt 上限），无默认密码。配置缺失、格式错误、无效字段或权限不安全会阻止启动；错误信息不回显配置内容。

文件由部署管理员管理，建议 `0600`，或由服务所属组读取的 `0640`；禁止其他用户读取、组写入。不要提交 Git、放进 `RANKING_WEB_DIR` 或将密码写进日志。账号变更通过修改文件并重启，不做热更新。

公开页右上角显示“登录”；登录后原位置显示“控制台”，可进入 `/admin/leaderboards`。退出或会话过期后恢复“登录”。未配置后台时不显示入口，后台路径不挂载。`GET /v1/admin/session` 仅匿名返回启用/登录状态；登录后才返回本人信息。

当前控制台可查看五个公开榜单的今日、昨日、本周、本月，直接读取相同的公开缓存和数值格式，不另行计算。公开榜单仍然匿名可读。

## 会话与部署安全

- 随机服务端会话固定 8 小时，最多 1024 个有效会话；保存在单进程内存，重启必须重新登录。密码使用 bcrypt 验证。
- Cookie 为 HttpOnly、SameSite=Strict，默认 Secure。管理写请求要求同源及会话 CSRF 令牌；登录只接受同源 JSON。客户端 Bearer 密钥不能用于管理登录。
- 登录独立限流：每来源每分钟 5 次、全站每分钟 40 次；来源状态有界。可信代理及 IPv6 来源归并沿用现有来源设置，不共享客户端注册计数。
- 对外必须使用 HTTPS。TLS 终止代理应设置正确的 `Host` 和 `X-Forwarded-Proto: https`，且直接代理地址须在 `RANKING_TRUSTED_PROXIES` 中明确配置；不要信任任意来源，也不要将后台 HTTP 监听直接暴露公网。
- 仅本机隔离测试可显式设置 `RANKING_ADMIN_ALLOW_INSECURE_LOCAL_HTTP=true`，要求服务绑定字面量 loopback IP，且请求的直接来源及 Host 也是 loopback。默认关闭，不能用于公网 HTTP。

### Docker 只读配置

容器默认使用 UID/GID `10001:10001`。在宿主机准备服务身份可读的文件，例如 root 所有、组为 10001、权限 0640。按现有部署方式额外挂载：

```yaml
volumes:
  - /etc/cpa-ranking/admin.json:/etc/cpa-ranking/admin.json:ro
environment:
  RANKING_ADMIN_CONFIG_FILE: /etc/cpa-ranking/admin.json
```

保留现有数据库卷、静态文件和网络设置。不需要启用后台时，无须新增挂载或变量。确认父目录允许容器运行身份遍历，文件只读可读而不可写。不要把真实配置打包进镜像。
