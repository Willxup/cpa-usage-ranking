# Docker 部署

[English](docker.md) · [返回 README](../README.zh.md)

镜像支持 Linux `amd64` 和 `arm64`，包含启用 CGO/SQLite 的 Go 服务、前端构建产物及第三方声明、内置头像和正式模型目录；运行时不需要 Node 或编译器。两个架构均已构建并做功能检查，其中 ARM64 使用显式 QEMU 用户态模拟，并非 ARM 原生实机验证。本项目不提供独立二进制发行包。

## 构建和启动

安装 Docker Engine、Buildx 和 Compose，在仓库根目录执行：

```sh
docker compose -p cpa-ranking up -d --build
docker compose -p cpa-ranking ps
docker compose -p cpa-ranking logs ranking
```

示例按构建机平台构建 `cpa-usage-ranking:local`，用 `cpa-ranking_ranking-data` 持久化 `/data`，只在宿主 `127.0.0.1:8080` 发布端口。也可单独构建指定架构而不运行：

```sh
docker buildx build --platform linux/amd64 --load -t cpa-usage-ranking:local .
# 将 linux/amd64 改为 linux/arm64 可构建 ARM64 镜像。
```

构建阶段运行在构建机架构上，必要时使用目标 C 交叉编译器；最终镜像阶段不运行目标架构命令。运行异构镜像仍需要合适的模拟器或原生主机。构建需要下载基础镜像和构建依赖；`--load` 需要支持将单平台镜像导入本机引擎的 Buildx 构建器。

**不要将此 HTTP 服务直接暴露到公网。** 注册和上报携带 Bearer 凭据，应通过 HTTPS 反向代理终止 TLS，限制对容器和本机监听端口的直接访问，并确保代理日志不记录 Authorization 或请求体。镜像在容器内监听所有接口，即使宿主端口只绑定回环，可访问容器网络的其他容器仍可能直连。默认不信任转发来源 IP；确需代理时只信任真实代理的 IP/CIDR，见[注册来源说明](registration-sources.md)。

Compose 示例未设置 CPU 或内存上限。请按主机预算配置 `cpus`、`mem_limit` 和 `memswap_limit`；资源配额不是吞吐保证。冒烟检查使用 2 CPU 配额、1 GiB 内存上限且不额外使用交换空间，这不表示生产最低配置要求。

## 数据、权限和停止

服务以 UID/GID `10001:10001` 非 root 运行。新的 Docker 管理数据卷内 `/data` 初始所有者为 `10001:10001`、权限为 `0700`；数据库文件权限可能更宽，但受该目录保护。已有数据卷、绑定目录、rootless UID 映射及 SELinux 策略需确保该 UID 可写；镜像启动时不会递归修改所有权。在常规 rootful Linux 主机上，管理员例如可用 `install -d -m 0700 -o 10001 -g 10001 /srv/cpa-ranking` 准备一个空绑定目录；应防止其他用户访问。

只运行一个实例，使用本地持久文件系统，不使用 NFS/网络共享或多个副本共用数据库。SQLite 使用 WAL：运行中保留 `-wal` 和 `-shm` 文件，不要删除它们或只复制打开中的数据库文件。停止容器会向服务发送 SIGTERM。Compose 默认留 30 秒，`RANKING_SHUTDOWN_TIMEOUT` 默认对各适用排空阶段分别为 10 秒；增大后也应相应增加 Docker 停止宽限并留余量。强杀或超时不属于正常关停。`docker compose down -v` 会删除数据卷，除非确实要删除数据，否则不要使用。

## 配置和健康检查

下列**镜像**默认值不同于 README 中源码运行的默认值：

| 环境变量 | 镜像默认值 | 用途 |
| --- | --- | --- |
| `RANKING_HTTP_ADDR` | `0.0.0.0:8080` | 容器内监听；Compose 宿主映射另行限制为回环 |
| `RANKING_SQLITE_PATH` | `/data/ranking.db` | 持久卷中的 SQLite 文件 |
| `RANKING_WEB_DIR` | `/app/web` | 前端构建目录 |
| `RANKING_MODELS_PATH` | `/app/config/models.json` | 必需的模型目录 |
| `RANKING_HEALTHCHECK_URL` | `http://127.0.0.1:8080/readyz` | 镜像健康检查目标，不是服务配置 |

修改监听地址或端口时，同时调整 Compose 端口映射和可从容器本机访问的 `RANKING_HEALTHCHECK_URL`。镜像以 `/readyz` 检查数据库、目录和首轮真实缓存是否就绪；`/healthz` 只表示存活。后续缓存刷新失败时保留上次成功结果，因此就绪不承诺数据新鲜度。关停、生产默认注册/上报限流等其余配置见 [README](../README.zh.md#配置)、[注册 API](registration-api.md)和[上报 API](report-snapshots.md)。显式空值或非法配置，以及缺失的必需文件，会导致启动失败。

## 离线备份与恢复

升级前先正常停止服务并确认退出成功。以下在仓库根目录执行，服务停止期间归档默认卷的**全部**内容；将归档安全保存到独立的持久位置，另行保存部署设置和自定义模型目录。客户端也必须自行保存凭据：服务端仅保存凭据摘要。

```sh
docker compose -p cpa-ranking stop -t 30 ranking
# 确认输出为 0 后再继续备份。
docker inspect --format '{{.State.ExitCode}}' "$(docker compose -p cpa-ranking ps -aq ranking)"
docker volume inspect cpa-ranking_ranking-data >/dev/null
umask 077
BACKUP_DIR="$HOME/cpa-ranking-backups"
mkdir -p "$BACKUP_DIR" && chmod 700 "$BACKUP_DIR"
BACKUP_FILE="$BACKUP_DIR/ranking-data-$(date -u +%Y%m%dT%H%M%SZ).tar"
docker run --rm --network none \
  --mount type=volume,src=cpa-ranking_ranking-data,dst=/data,readonly \
  --entrypoint /bin/busybox cpa-usage-ranking:local \
  tar -C /data -cf - . > "$BACKUP_FILE"
# 适当时恢复原服务：
docker compose -p cpa-ranking start ranking
```

恢复时再次停止服务，将**可信**备份解压到新的空数据卷，不覆盖原卷或运行中的数据库。例如：

```sh
docker compose -p cpa-ranking stop -t 30 ranking
BACKUP_FILE=/absolute/path/to/verified-backup.tar
# 若卷已存在，请换一个名字。
docker volume inspect cpa-ranking-restored >/dev/null 2>&1 && { echo 'volume already exists'; exit 1; }
docker volume create cpa-ranking-restored
docker run --rm -i --network none --user 0:0 \
  --mount type=volume,src=cpa-ranking-restored,dst=/data \
  --entrypoint /bin/busybox cpa-usage-ranking:local \
  sh -c 'tar -C /data -xf - && chown -R 10001:10001 /data && chmod 700 /data' \
  < "$BACKUP_FILE"
```

通过额外的 `compose.restore.yaml` 将 Compose 指向新卷：

```yaml
volumes:
  ranking-data:
    external: true
    name: cpa-ranking-restored
```

```sh
docker compose -p cpa-ranking -f compose.yaml -f compose.restore.yaml up -d --no-build
```

检查就绪状态和预期数据。原卷和备份仍会保留；恢复会丢失备份之后的变更。升级时保留已验证备份和旧镜像，构建新镜像，用原卷重建容器，再检查就绪和数据。启动时只向前执行数据库迁移，不自动降级；回退到不兼容的旧镜像时应恢复升级前已验证的备份到独立卷，不要假定迁移后的数据库可向后兼容。

## 可重复的镜像检查

在安装 Python 3 的本地 Linux Docker 主机上执行。脚本创建带唯一标签的临时容器和数据卷、不发布端口，结束后删除测试资源：

```sh
mkdir -p /path/to/test-work
python3 deploy/test/docker_smoke.py --image cpa-usage-ranking:local \
  --arch amd64 --work-dir /path/to/test-work
```

ARM64 镜像使用 `--arch arm64`。AMD64 主机未注册 ARM binfmt 时，可传入 `--qemu /静态/qemu-aarch64-static的绝对路径`；脚本显式调用该模拟器并在模拟运行中禁用 Docker 容器内健康检查，同时通过 HTTP 检查 `/readyz`。脚本不会安装或注册模拟器。检查覆盖初始化/迁移、页面和头像、真实注册/上报、重建后的持久化、权限、SIGTERM 正常退出及启动失败；不属于负载或容量测试。
