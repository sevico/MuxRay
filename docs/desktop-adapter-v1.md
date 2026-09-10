# 最小桌面 Go 适配层

本文保留 2026-09-06 初始适配层及当时验收范围。当前已接入原生窗口，
并按 [ADR-013](decisions/013-retained-document-details.md) 增加独立文档所有权、
`StartDetail`/`GetDetail` 和 AMF0/AAC 有界详情；现行使用说明见
[desktop/README](../desktop/README.md)。下文“六个方法”“尚无窗口”是历史状态。

本批对应 [ADR-010](decisions/010-minimal-desktop-adapter.md)，在
[后台扫描会话](background-scan-session.md) 上增加应用级生命周期和前端友好的 DTO。
代码在 `internal/desktop`；仍是纯 Go，没有新增 Wails/React 依赖，**还没有可启动的
桌面窗口**。真实 Wails 绑定生成、原生文件选择器和界面交互留给下一批验证。

## 宿主和绑定对象

```go
app := desktop.New(appCtx)
bindings := app.Bindings()
```

未来桌面壳只绑定 `bindings`，不绑定 `app` 或 `session.Session`。绑定对象恰好
暴露下表六个方法；返回具体 DTO，不暴露 context、Go error、索引、迭代器或 payload。

| 方法 | 请求 | 结果 |
|---|---|---|
| `OpenFile` | `OpenRequest{Path, MaxTags, MaxDiagnostics}` | 立即返回会话 ID 和 opening 状态 |
| `GetSession` | 会话 ID | 状态、进度、最终扫描完整性和任务失败信息 |
| `CancelScan` | 会话 ID | 请求取消后的状态，保留结束后的部分结果 |
| `CloseFile` | 会话 ID | 立即禁止新分页访问并请求清理，不等待阻塞 I/O |
| `GetTags` | `PageRequest{SessionID, Start, Count}` | 已结束扫描的有界 Tag 页 |
| `GetDiagnostics` | 同上 | 已保留容器诊断的有界页 |

`OpenFile` 先验证请求、占用唯一槽位，再用一个 goroutine 打开/stat 文件并启动
扫描会话。因此 opening 期间也能查询、取消和关闭。参数错误在请求响应的
`error` 中返回；打开文件失败通过后续会话的 `failure` 报告。

路径必须非空、有效 UTF-8、不含 NUL、最多 32,768 字节。相对路径沿用操作系统
语义，未来文件选择器应传入绝对路径。没有按扩展名判断 FLV 是否有效；内容错误
仍交给容器解析器诊断。扫描预算为 0 时沿用默认值，其他值遵守已有 FLV 上限。

## 唯一槽位与旧请求隔离

一个 App 同时最多拥有一个槽位；opening、running、finished、canceling 和尚未
完成的 close 都占用它。再次打开得到 `busy`，不会自动替换旧文件或丢弃旧结果。
需要先 `CloseFile(oldID)`，等待旧槽位变成 `closed=true` 且 `terminal=true`，
才能再次打开。不能靠反复关闭/打开来累积后台清理任务。

会话 ID 是 App 生命周期内不重用的字符串，如 `scan-1`。客户端把它当不透明
标识，不要转成数字或持久化后跨 App 使用。所有后续操作必须带 ID，旧 ID 不能
取消或关闭新文件。计数耗尽时明确返回错误，不回绕重用。

最近关闭的终态保留为一个小状态记录，直到新槽位占用它；这期间可以反复关闭和
查询。新文件打开后，旧 ID 返回 `session_not_found`。没有历史会话列表。

并发关闭前已经取得旧数据的分页请求可以完成，但成功响应里的 `sessionId`
仍是旧 ID。前端接收任何异步响应时都要比较当前 ID，忽略过期响应。

## 独立的 apiVersion=1 DTO

这不是现有 CLI 报告的 Schema 更新。接口字段由 Go DTO 和 JSON 合同测试固定，
暂未加入独立的桌面 JSON Schema 或生成的 TypeScript 文件；真实框架传输仍待接入验证。

例如，`OpenFile` 的请求 JSON 为：

```json
{"path":"C:\\media\\sample.flv","maxTags":0,"maxDiagnostics":0}
```

正常开始时返回：

```json
{
  "apiVersion": 1,
  "session": {
    "sessionId": "scan-1",
    "state": "opening",
    "terminal": false,
    "closed": false,
    "progress": null,
    "scan": null,
    "failure": null
  },
  "error": null
}
```

`GetSession`、取消和关闭沿用该响应形状。Tag/诊断查询返回
`{apiVersion, page, error}`；成功时 page 包含 `sessionId`、`bounds` 和
`tags` 或 `diagnostics` 数组。请求失败时结果为 null，error 为固定 code/message。
错误文本不回显完整路径或原始 OS error；宿主的 Go `Shutdown` 错误仍保留原始原因。

所有 **64 位整数**都使用规范十进制字符串，包括文件大小、逻辑字节、Tag 数、
ID、Span 的 byteOffset、DTS、页起点/总数/下一起点和诊断 expected/actual。
DTS 可以为负数字符串。已有有界 uint8/uint32 字段保持 JSON number。
前端若需要精确运算应使用 BigInt，不得直接将大偏移转成 Number。

分页起点为 `"0"` 或无前导零的正整数文本，最多 20 位且不能超过 uint64；
不接受 `""`、`"01"`、`"+1"`、`"1.0"`、指数格式、空白或 Unicode 数字。
count 必须显式为 1–4,096，0 不代表默认值；常用请求为：

```json
{"sessionId":"scan-1","start":"0","count":256}
```

start 等于 total 时返回独立空数组，大于 total 则报参数错误。所有 Span 都来自
核心记录的规范方法，不在适配层重新解析或猜测边界。诊断页使用新增的
`session.DiagnosticPage`，只复制请求窗口及其中的数值指针，不先复制全部诊断。
诊断 total 是保留数量，达到诊断预算后不再代表发现问题的总数。

## 状态与错误判断

状态为 `opening / running / canceling / finished / canceled / failed`。
`progress` 在真正创建扫描会话后才非 null；其 bytesScanned 仍是按块报告的逻辑
游标，不是物理 I/O 量、剩余工作预测或实时吞吐量。

`scan` 仅在实际扫描终止后出现，包含 `complete` 和 `hasErrors`。打开失败没有
扫描结果，所以 progress/scan 都为 null。`finished` 不等于文件有效：结构截断
可以是 finished、failure=null、scan.complete=false，并通过诊断页说明原因。
取消造成的 hasErrors=true 表示扫描不完整，不应被界面标成“发现文件损坏”。

请求错误与任务失败是两条通道：

| 通道 | code | 含义 |
|---|---|---|
| `error` | `invalid_argument` | 路径、预算、分页格式或界限不合法 |
| `error` | `busy` | 旧槽位尚未关闭并结束 |
| `error` | `session_not_found` / `session_closed` | ID 已过期或结果访问已关闭 |
| `error` | `not_ready` | 打开/扫描/源关闭尚未结束 |
| `error` | `shutting_down` / `id_exhausted` | 应用不再接受新打开请求 |
| `failure` | `source_not_found` / `source_permission_denied` / `open_failed` | 异步打开失败 |
| `failure` | `canceled` / `deadline_exceeded` | 任务被取消或其生命周期截止 |
| `failure` | `scan_failed` | 扫描或源关闭失败；不能用取消掩盖清理错误 |

在打开失败的槽位上请求页面时，请求 error 为 `open_failed`；具体打开原因看
会话 failure。客户端按 code 分支，不按 message 文本分支。

## 关闭和宿主退出

取消保留部分结果，关闭禁止新访问，两者不能互换。CloseFile 使用已取消的等待
context 请求 session.Close，因此不会等待卡住的读取；文件仍由会话 worker
独占并在退出后关闭，清理未完成前槽位不能复用。

宿主退出调用不在绑定对象上的 `App.Shutdown`：

```go
shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
defer stop()
err := app.Shutdown(shutdownCtx)
// 宿主处理 err；超时不意味着底层 I/O 已经终止。
```

Shutdown 永久禁用新打开、关闭槽位，并按调用方期限等待 opening 和扫描清理。
打开操作迟到返回的会话仍会被关闭；超时后保留清理所有权，可再次 Shutdown
继续等待。完成后的任务错误优先于已取消的等待 context。主动退出取消活动任务时
返回 context.Canceled 属于预期；实际扫描/关闭错误仍应处理。

这里不承诺能抢占任意阻塞的文件系统调用，也没有新增取消延迟保证。历史首次
取消离群值仍是 [开放观察项](cancellation-investigation-20260905.md)。

## 本机验证（2026-09-06）

- Windows/amd64、Go 1.27；`CGO_ENABLED=0` 的全量测试和构建通过，没有新增依赖。
- 全量 go vet、启用 GCC/CGO 的完整 race 通过。
- desktop/session 两个包的 100 轮 race 通过，覆盖并发打开、关闭、退出和旧响应。
- desktop 语句覆盖率 99.1%，session 为 99.2%；并非所有线程调度的证明。
- 新 FuzzPageStart 运行 30 秒、5,440,106 次执行通过，已加入 CI。
- DTO 测试覆盖超过 2^53 的偏移、uint64/int64 极值、空数组/null 形状和绑定方法白名单。
- 现有全部 CLI JSON 合同正反例和 11 个语料清单身份校验通过；Python 工具测试
  13 项通过、1 项平台相关跳过。已有 CLI Schema/Golden 未修改。

关键复现命令：

```text
go test -count=1 ./...
go build ./...
go vet ./...
go test -race -count=1 ./...
go test -race -count=100 ./internal/desktop ./internal/session
go test ./internal/desktop -run=^$ -fuzz=FuzzPageStart -fuzztime=30s -parallel=4
go test '-coverprofile=.cache/desktop-cover.out' ./internal/desktop ./internal/session
go tool cover '-func=.cache/desktop-cover.out'
python -B tools/validate_contracts.py
```

Windows race 工具链设置沿用 [后台会话说明](background-scan-session.md)。以上不包含
真实桌面窗口、原生对话框、WebView/TypeScript 绑定、跨平台 GUI 或远端 CI 验收。
下一批再接 Wails 壳和最小界面；详情、摘要、导出、多标签页仍需各自确定范围。
