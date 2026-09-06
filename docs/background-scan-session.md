# 后台扫描会话第一阶段

本批实现 [ADR-009](decisions/009-background-scan-sessions.md)：Go 后端可以启动
后台容器扫描、查询进度、请求取消、等待结束，并在结束后复用同一索引分页。
没有引入 Wails、前端、会话注册表或新的 CLI/JSON 协议；现有 CLI 仍逐次同步扫描。

## 接口与使用顺序

入口为 `internal/session.Open(ctx, path, flv.Options{})`。打开和 stat 一个普通
文件在调用方同步执行，随后启动一个扫描 worker；打开/stat 失败直接返回 error。
扫描选项验证、容器解析和后续 I/O 在 worker 中执行，由 `Wait` 返回运行错误。

| 方法 | 行为 |
|---|---|
| `Snapshot()` | 随时取得独立的、固定大小的状态/进度值；不返回活动索引 |
| `Cancel()` | 幂等地请求协作式取消，不等待，也不丢弃已提交前缀 |
| `Wait(ctx)` | 等待扫描与源文件关闭完成；等待 ctx 不会取消扫描本身 |
| `Page(start, count)` | worker 结束后查询；沿用 1–4,096 条上限和全局 Tag ID |
| `Result()` | worker 结束后取得扫描结果；诊断深拷贝，索引只读共享，不铺平成数组 |
| `Close(ctx)` | 禁止新结果/分页访问，请求取消，释放会话持有的结果，并按 ctx 等待清理 |

典型的应用层流程是：打开后保存会话引用；界面按需轮询 `Snapshot`；取消按钮
调用 `Cancel`；看到终态后读取任务错误和分页；不再使用时调用 `Close`。
本包不保存轮询历史，也不替应用层决定轮询频率或同时打开多少个文件。

```go
// 代码在本仓库的应用层使用，导入 internal/flv 和 internal/session。
s, err := session.Open(scanCtx, path, flv.Options{})
if err != nil {
    return err // 打开、stat 或启动前上下文已取消
}
// 应用保存 s，并在后续事件中调用以下方法。
status := s.Snapshot()
if status.State.Terminal() {
    taskErr := s.Wait(nil) // 已终止，立即返回；nil 表示不设等待期限
    result, accessErr := s.Result()
    page, pageErr := s.Page(0, 256)
    // taskErr 是运行错误；accessErr/pageErr 是访问或分页参数错误。
    // result.HasErrors() 决定是否有结构错误，不能只判断 taskErr。
    _, _, _, _, _ = taskErr, result, accessErr, page, pageErr
}
```

`Cancel` 和 `Close` 不能互换：如果需要展示取消后的部分结果，应先 `Cancel`、
等待结束、读取部分页，最后才 `Close`。扫描期间的 `Page/Result` 返回
`ErrNotReady`；一旦请求 `Close`，它们改为返回 `ErrClosed`。

## 状态、错误和进度口径

执行状态为 `running / canceling / finished / canceled / failed`。`finished`
只表示扫描和源关闭没有 Go error，不代表文件有效；结构损坏仍以诊断和
`Complete=false` 或 `HasErrors=true` 表示。`Complete/HasErrors` 只在终态有效。
取消自身不产生 FLV 损坏诊断，之前发现的真实诊断仍保留。

`Wait` 的 context 只限制该次等待。等待超时后，可继续查询、再等一次或明确
调用 `Cancel`。父 `scanCtx` 的取消/截止才会传播到扫描。已完成任务的记录错误
优先于已取消的等待 context；多个等待者互不影响。

取消与完成竞争时，以扫描函数实际返回的结果为准：扫描已经返回 nil、正在关闭
文件时再取消，仍可得到 `finished`。源关闭失败一律是 `failed`，并保留扫描和
关闭的错误，不能用取消错误覆盖掉关闭失败。

`Snapshot.Progress` 的字段如下：

- `SourceSize`：启动时的声明文件大小；扫描期间不重新 stat，也不追踪增长文件。
- `BytesScanned`：最后报告的逻辑游标，不是物理读取量、完成工作比例或 ETA。
- `TagCount`：最后报告的已提交 Tag 数；中断的 Tag 不提交，结构截断沿用已有部分记录规则。
- `DiagnosticCount`：已保留的诊断条数，达到上限后不再增长，不是发现的问题总数。

纯扫描核心另有 `flv.ScanContextWithProgress`：开始时、初始容器结构之后、每
1,024 个已提交 Tag 和返回前同步通知标量快照。最终计数精确，活动进度按块更新，
没有时间新鲜度保证。跳过 payload 会造成逻辑字节大跳跃；很短的扫描可能在两次
轮询之间结束；卡住的读取不会凭空推进进度。自定义回调必须快速返回且不能 panic。
原 `Scan/ScanContext` 不启用回调，不会取得进度锁。

## 生命周期和资源边界

源文件由 worker 独占，所有正常退出路径都会尝试关闭一次，再发布终态并通知
等待者；不依赖调用方记得 `Close` 才释放文件。`Close` 主要负责取消仍活动的
扫描和释放会话持有的索引/诊断。该阶段的会话不保留源文件用于后续 detail/summary。

`Close` 返回与 `Wait` 相同的任务/等待错误。它取消活动扫描时，最终返回
`context.Canceled` 属于预期结果；如果还存在关闭失败，错误链也会保留该错误。
例如，界面关闭文件时可以给等待设置预算：

```go
closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
defer stop()
closeErr := s.Close(closeCtx)
// closeErr 需要交给应用层处理；等待超时并不表示底层 I/O 已停止。
```

`Snapshot.Closed` 表示逻辑关闭和禁止新访问，不是物理清理已完成。底层
`ReaderAt/Close` 阻塞时，等待可以超时；worker 会在底层调用返回后继续清理。
不会并发强行关闭正在读取的源，也没有逐次读取 goroutine，不能承诺任意 I/O 的
最大退出延迟。终态表示关闭已尝试；关闭错误需检查 `Wait`。

关闭前已返回的页、诊断副本和只读索引仍有效；若调用方保留整个 `Result`，索引
内存也会由它继续持有。仅需要页面的界面宜使用 `Page`，不必保留整个结果。
并发关闭前已经取得结果引用的查询可以完成；关闭后开始的查询不会取得新引用。

每个会话仍限制为最多 1,000,000 个 Tag、每块 1,024 条、每页 1–4,096 条。
诊断默认 1,024 条，允许 2–65,536 条。限制是单会话的，不是全进程预算；未来
应用层必须限制并发会话数量。文件应在扫描期间保持不变；实时输入另行设计。

## 本机验证（2026-09-06）

Windows/amd64、Go 1.27、Intel Core Ultra 7 255H：

- 全量 `go test -count=1 ./...`、`go vet ./...` 和启用 GCC/CGO 的完整 race 通过。
- 后台会话单独 `-race -count=100` 通过；用 channel 门控的 ReaderAt/Closer
  验证取消、等待、并发关闭、部分分页、源关闭恰好一次和终态发布顺序，不依赖 sleep。
- `internal/session` 语句覆盖率为 99.0%；`ScanContextWithProgress` 函数为 100%。
  覆盖率不是所有线程调度或所有 I/O 行为的证明。
- `FuzzScanProgress` 运行 30 秒，5,825,777 次执行通过；已加入 CI。
- 现有全部 JSON 合同正反例及 11 个语料清单身份校验通过。Python 工具测试共
  14 项，13 项通过、1 项平台相关跳过；没有修改 Schema 或 Golden。

短基准 `-benchtime=200ms -count=3` 使用只合成结构字节的虚拟源：

| 测量 | 时间/次 | 分配字节/次 | 分配次数/次 |
|---|---:|---:|---:|
| 100,000 Tag，无进度回调 | 3.20–3.32 ms | 约 4,824,536 | 111 |
| 100,000 Tag，互斥锁标量回调 | 3.25–3.30 ms | 约 4,824,537 | 111 |
| 1,000,000 Tag，原分块入口 | 32.3–33.1 ms | 约 48,060,508 | 993 |
| 1,000,000 条索引取 256 条跨块页 | 3.56–3.58 μs | 12,288 | 1 |

这组短测没有显示新增回调分配，时间差在本次波动范围内；不是磁盘吞吐量、峰值
RSS、多会话总内存或取消延迟的验收结果。历史 607 ms 离群值仍见
[取消专项](cancellation-investigation-20260905.md)，本批没有宣称修复。
跨平台 CI 配置已更新，但尚未推送或取得远端 CI 结果。

复现主要命令：

```text
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
go test -race -count=100 ./internal/session
go test ./internal/flv -run=^$ -fuzz=FuzzScanProgress -fuzztime=30s -parallel=4
go test '-coverprofile=.cache/session-progress-cover.out' ./internal/flv ./internal/session
go tool cover '-func=.cache/session-progress-cover.out'
go test ./internal/flv -run=^$ -bench='Benchmark(ScanProgress|ScanContextTags|TagPage)$' -benchmem -benchtime=200ms -count=3
python -B tools/validate_contracts.py
python -B -m unittest discover -s tools -p 'test_*.py'
```

Windows race 需使用已安装的 GCC 并启用 CGO；例如当前本机 PowerShell 先设置：

```powershell
$env:CGO_ENABLED = '1'
$env:Path = 'C:\Users\swk01\scoop\apps\gcc\current\bin;' + $env:Path
```

下一阶段可在这个 Go API 上设计最小桌面适配层；源文件复用的详情/汇总会话、
扫描中分页、事件推送、磁盘索引、提升 Tag 上限不属于本批。

### 后续桌面适配层

[ADR-010](decisions/010-minimal-desktop-adapter.md) 已实现独立的单槽位 Go 适配层，
并新增 `Session.DiagnosticPage(start, count)`：只深拷贝一个有界诊断窗口，沿用
结束后查询、关闭后拒绝和每页 1–4,096 条规则。原有会话接口保持兼容。
适配层的整数传输、错误和宿主生命周期见 [桌面适配层说明](desktop-adapter-v1.md)。
