# 接入流程与时序

本图适用于任意接入客户端，与具体采集器、数据库或SDK无关。接口字段、限流和错误码详见[客户端接入指南](integration.md)。图中的客户端本地存储由接入方实现，不是服务端提供的接口。

## 1. 整体流程

```mermaid
flowchart TD
    A[接入应用] --> B{是否需要上报用量？}
    B -->|只展示榜单| READ[GET /v1/leaderboards/board/period<br/>不携带上报凭据，不加查询参数]
    B -->|需要上报| C{本地是否已有该身份凭据？}
    C -->|没有| D[生成32字节安全随机数<br/>无填充base64url编码，先持久化]
    C -->|已有| REG
    D --> REG[POST /v1/clients/register<br/>始终使用保存的同一凭据]
    REG --> R{注册结果}
    R -->|201新建 / 200已有| SAVE[保存client_id及返回资料]
    R -->|响应丢失 / 可重试故障| REGWAIT[保留凭据，遵守限流并退避]
    REGWAIT --> REG
    R -->|凭据或资料无效 / 禁用 / 新注册关闭| FIX[修正问题或暂停对应操作<br/>不自动生成新凭据规避失败]
    SAVE --> COLLECT[按UTC日聚合完整统计<br/>仅提交服务端允许的今天或昨天]
    COLLECT --> LOCAL[为该日分配更高revision<br/>持久化版本及完整待发正文]
    LOCAL --> PUT[PUT /v1/reports/utc_day<br/>Bearer凭据 + 完整日快照]
    PUT --> RESULT{响应结果}
    RESULT -->|200且响应revision等于本次请求值| ACK[本地标记该版本已确认]
    ACK --> NEXT[继续采集，按额度调度下一次更新]
    NEXT --> COLLECT
    RESULT -->|429 / 临时503 / 500 / 超时或丢失响应| WAIT[保留原revision和完整正文<br/>遵守Retry-After及有界退避]
    WAIT --> PUT
    RESULT -->|409版本冲突| CONFLICT[核对本地状态及并行写入<br/>不能盲目升版本覆盖]
    CONFLICT -->|能重建权威完整快照| COLLECT
    CONFLICT -->|无法重建| STOP[暂停该日自动写入并提示处理]
    RESULT -->|410日期关闭| CLOSED[停止该日补报，记录未确认状态<br/>不改日期重投]
    RESULT -->|401 / 403 / 其他数据或协议错误| FIX
    ACK -. 数据已提交，异步进入榜单计算 .-> CACHE[后台读取已提交统计并构建全部榜单<br/>完整轮次成功后整体发布缓存]
    CACHE -. 公开读取 .-> READ
    READ --> VIEW{读取结果}
    VIEW -->|200| SHOW[显示entries及实际日期 / generated_at<br/>空数组也是有效结果]
    VIEW -->|503尚无完整缓存| READWAIT[延迟重试，不伪装为空榜]
    READWAIT --> READ
```

- 注册只在首次接入或需要恢复身份时执行，不必在每次上报前注册。
- 头像选择为可选步骤：注册前读取 `/avatars/catalog.json`；省略 `avatar_id` 使用默认头像。
- 重试不绕过限流、禁用状态或日期窗口。暂时采集失败不能用 `items: []` 占位；它会清空该日已匹配统计。
- `409`返回当前服务端版本，但没有读取已提交快照正文的接口；不能只修改版本号来抢写。

## 2. 对外时序：客户端与排名服务

此图把排名服务视为一个整体，不暴露数据库或缓存组件。上报客户端与榜单读取方可以是同一应用；仅展示榜单时直接执行最后的GET即可。

`revision` 是该身份该UTC日的**数据修订号**，没有主/次版本，也不是软件发布版本。响应确认的是本次请求携带的修订号。兼容软件升级继续使用 `/v1` 与 `schema_version: 1`，保留身份与修订状态，详见[概念区分](integration.md#数据修订号确认与更正)。

```mermaid
sequenceDiagram
    autonumber
    participant C as 接入客户端
    participant S as 排名服务（公开HTTP接口）

    Note over C: 首次生成并保存secret；已有凭据则恢复使用
    opt 自选头像；省略avatar_id时无需此步骤
        C->>S: GET /avatars/catalog.json
        S-->>C: 200：默认头像及可选ID、资源路径
    end
    C->>S: POST /v1/clients/register + Bearer secret + 资料
    alt 新身份
        S-->>C: 201：client_id、display_name、avatar_id
    else 已有启用身份
        S-->>C: 200：同一身份及已保存资料，不修改资料
    end
    Note over C,S: 注册确认丢失时保留同一secret重试；限流或其他错误按契约处理
    Note over C: 保存返回资料；后续上报不必重复注册

    loop 按额度更新仍开放的UTC日
        Note over C: 生成完整日快照；先保存revision与待发正文
        C->>S: PUT /v1/reports/utc_day + Bearer + schema_version=1 + revision="2" + items
        alt 写入成功或相同修订号与内容的幂等确认
            S-->>C: 200：revision="2"
            Note over C: 与本次请求的revision核对一致后标记已确认
        else 限流或暂时过载
            S-->>C: 429 / 503 report_overloaded + Retry-After
            Note over C: 按Retry-After退避；保留原revision与完整正文再试
        else 网络超时或确认丢失
            Note over C,S: 客户端无法判断是否已提交；这不是一种HTTP响应
            C->>S: 退避后重发同一日期、同一revision、同一完整正文
            Note over C,S: 重试仍须通过身份、准入、日期及修订号校验，再按实际响应处理
        else 修订号落后或同号内容冲突
            S-->>C: 409 revision_conflict + 当前已提交revision
            Note over C: 核对权威数据与并行写入；不能盲目加号覆盖
        else 日期已关闭
            S-->>C: 410 expired_day
            Note over C: 停止该日补报并记录未确认状态，不修改日期重投
        else 凭据、身份、正文或内部错误
            S-->>C: 401 / 403 / 413 / 415 / 422 / 500及错误码
            Note over C: 无效请求先修正；禁用身份停报；500保留原任务有界退避
        end
    end

    Note over C,S: 写入确认 ≠ 立即入榜；客户端不调用内部刷新或结算接口
    C->>S: GET /v1/leaderboards/board/period（无需凭据，无查询参数）
    alt 有完整已发布榜单
        S-->>C: 200：entries、start、end、generated_at
        Note over C: 按实际生成时间展示；entries为空也是有效结果
    else 尚无完整榜单
        S-->>C: 503 leaderboard_unavailable
        Note over C: 延迟重试，不把失败展示为空榜
    end
```

`/healthz`、`/readyz` 可用于独立监控，不要求客户端在每次业务请求前先调用，也不能替代注册或上报确认。

## 3. 内部时序：服务端处理与异步榜单构建

保留客户端作为触发方，并展开本地持久化、HTTP处理、数据库和后台缓存；这些内部步骤不是要求接入方调用的新接口。

```mermaid
sequenceDiagram
    autonumber
    participant C as 上报客户端
    participant L as 客户端本地存储
    participant A as HTTP API
    participant D as 服务端SQLite
    participant W as 后台任务与榜单缓存
    participant V as 榜单读取方

    Note over C,L: 首次接入；已有凭据时直接恢复，不能重置身份
    C->>L: 保存安全随机凭据，再发起网络请求
    opt 自选头像
        C->>A: GET /avatars/catalog.json（无需凭据）
        A-->>C: 默认头像、ID与资源路径
    end
    C->>A: POST /v1/clients/register + Bearer
    A->>D: 按凭据摘要查找；新身份原子创建
    D-->>A: 新建身份或已有启用身份
    A-->>C: 201 / 200：client_id、display_name、avatar_id
    C->>L: 保存返回资料，不更换凭据
    Note over C,A: 注册响应丢失：原凭据重试，返回同一身份；受限流和状态检查约束

    loop 按额度更新仍开放的UTC日；同一身份串行调度
        C->>C: 从本地数据生成该日完整累计快照
        C->>L: 持久化该日新revision与完整待发正文
        C->>A: PUT /v1/reports/utc_day + Bearer + 快照
        A->>A: 身份、准入与正文校验
        A->>D: 写入事务内检查日期窗口、身份状态与版本
        D->>D: 原子提交版本、摘要及替换后的统计
        D-->>A: 提交成功
        alt 收到200且响应revision等于本次请求值
            A-->>C: 200：revision
            C->>L: 标记该版本已确认
        else 响应丢失或超时，客户端不知道是否已提交
            Note over C,D: 示例为已提交但确认丢失；也可能根本未提交
            C->>C: 退避；保留原版本和正文
            C->>A: 重发原PUT（仍须通过准入及日期窗口）
            A->>D: 核对同日版本与内容摘要
            D-->>A: 已提交则幂等确认；未提交则正常写入
            A-->>C: 200：revision（仅在校验通过时）
            C->>L: 核对版本后标记已确认
        end
    end

    Note over A,D: 拒绝分支：409核对冲突；410停止该日；429/临时503退避；其他错误修正或暂停
    Note over C,W: 上报200只确认写入，不代表本次数据已被榜单缓存采用
    loop 独立后台周期，不由每次PUT同步触发
        W->>D: 读取一致的资料与统计快照
        D-->>W: 已提交的数据
        W->>W: 计算5个榜单 × 6个UTC周期
        alt 整轮构建成功
            W->>W: 原子发布完整新缓存
        else 构建失败
            W->>W: 保留先前完整缓存及其原始生成时间
        end
    end
    V->>A: GET /v1/leaderboards/board/period（无需凭据）
    A->>W: 读取已发布缓存，不现场计算榜单
    alt 存在完整缓存
        W-->>A: 对应榜单、周期及构建时间
        A-->>V: 200：entries、start、end、generated_at
    else 尚无完整缓存
        W-->>A: 缓存不可用
        A-->>V: 503 leaderboard_unavailable
    end
    Note over V: 展示实际生成时间；未入榜不是上报失败；读取方可与上报客户端是同一应用
```

## 4. 时间与成功判定

| 阶段 | 完成标志 | 不代表什么 |
|---|---|---|
| 本地准备 | 凭据、待发版本及正文已可靠保存 | 服务器尚未确认接收 |
| 身份注册 | 收到201或200并保存返回身份 | 不代表已提交任何用量 |
| 日快照确认 | 收到200且响应 `revision` 与本次请求的值数值一致 | 不代表已刷新榜单或一定进入前100 |
| 榜单发布 | 后台完整轮次构建成功并发布 | 不保证包含轮次读取之后的提交 |
| 榜单读取 | 收到200及实际边界、`generated_at` | 不保证数据绝对新鲜；可能保留旧轮次 |

- 默认榜单构建周期为5分钟，可由部署者调整；这不是“上报后最多5分钟可见”的承诺，构建时间和失败都会影响可见性。
- `week` / `month` 分别读取上一完整UTC自然周（周一开始）/自然月；周一或月初的昨天仍可在日报窗口内修订，成功的后续缓存轮次才反映修订。
- 日界使用UTC。某日关闭后，不再接受该日重试或更正；后台日结将其统计归入周、月及累计数据。这是服务内部维护，不需要客户端调用“结算”接口。
- 后台日结与缓存构建独立调度，并不是每个PUT后顺序执行的两个步骤。
- `/healthz`仅检查存活；`/readyz`检查数据库、目录及初始缓存就绪，均不是某次上报的确认接口。
