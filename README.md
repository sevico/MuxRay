# MuxRay（码流透镜）

MuxRay 是一个面向 FLV 和音视频码流的跨平台诊断工具。项目已完成 P0/P1 技术基线，并开始实现 P2 基础分析；当前提供可复用、可测试的纯 Go FLV、AMF0、AAC、AVC/H.264 与 MP3 第一阶段解析核心和 CLI。

## 当前范围

- 有界 byte/bit 读取与统一源范围 `Span`；
- FLV Header、PreviousTagSize 和 Tag 索引扫描；
- 可取消的分块索引、按全局 Tag ID 查询和有界分页；
- Go 后台扫描会话、标量进度快照、取消/等待/关闭和结束后分页；
- 最小桌面 Go 适配层：单会话管理、旧请求隔离、精确整数 DTO 和 Tag/诊断分页；
- 独立 Wails/React 最小桌面壳：原生文件选择、进度、取消/关闭和有界结果页；
- 独立文件生命周期及 AMF0/AAC/AVC/MP3 桌面详情：取消后完整前缀可读、请求恢复、字段与位级源位置；
- 有界 Hex 桌面窗口、字段/诊断源位置联动及 Tag ID 跳转，共用保留源文件与读取任务；
- 媒体概览 v2 桌面视图：可取消分析、精确时间/码率指标、有界事件分页与 Tag/Hex 联动；
- 有界 Tag 趋势窗口：原始 DTS、声明/实际载荷、同类型及 Stream ID 前驱时间差，异常观察项可定位详情与 Hex；
- 桌面诊断快照 JSON 导出：冻结已有有界详情、Hex、概览和趋势，通过原生保存对话框写出并恢复丢失的保存回复；
- AVC 桌面有界前置配置查找、NAL 长度来源、SPS/PPS 摘要与不伪造的派生字段位置；
- 按 Tag、资源有界的通用详情分发；
- AMF0 ScriptData 详情解析；
- FLV AAC AudioTagHeader、AACPacketType 与 MPEG-4 AudioSpecificConfig 详情解析；
- FLV AVC VideoTagHeader、AVCPacketType、有符号 CompositionTime、AVCDecoderConfigurationRecord，以及 SPS/PPS 和 length-prefixed NAL 边界解析；
- FLV MP3 AudioTagHeader 与首个四字节 MPEG Audio Layer III 帧头的有界详情解析；
- AAC/AVC 跨 Tag 时间线、配置变化、声明关键帧/GOP、有覆盖区间定义的平均码率与基础 A/V 时间范围偏差；
- 可选的 media-summary v2：MP3 跨 Tag 时间线、区间码率、首帧头参数/码率变化与音频编码切换统计；
- 稳定、版本化的 JSON 输出和诊断代码；
- 正常、边界、损坏样本的 unit、Golden 与 fuzz 测试；
- 离线语料身份/差分校验与实际落盘大文件的扫描测量工具。

完整 VUI/PPS/slice 等深层 H.264 语义、MP3 多帧/VBR/CRC 深层分析、分页帧级曲线和实时 RTMP/HTTP-FLV 将在后续阶段加入。

## 开发

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/muxray inspect sample.flv
go run ./cmd/muxray tags --start 1024 --count 256 --pretty sample.flv
go run ./cmd/muxray summary --pretty sample.flv
go run ./cmd/muxray summary --schema-version 2 --pretty sample.flv
go run ./cmd/muxray detail --tag 0 --pretty sample.flv
```

`inspect` 默认输出单行、确定性的 `schemaVersion: 1` JSON；加入 `--pretty` 可获得两空格缩进格式。扫描发现结构错误时仍会输出报告并以状态码 2 退出。公开字段定义见 [`docs/schemas/flv-scan-report-v1.schema.json`](docs/schemas/flv-scan-report-v1.schema.json)，诊断码见 [`docs/diagnostics-v1.md`](docs/diagnostics-v1.md)。

`summary` 输出独立的 [`flv-media-summary-v1.schema.json`](docs/schemas/flv-media-summary-v1.schema.json) 文档，分析 AAC raw 与 AVC NALU 的 DTS/PTS、CompositionTime、声明关键帧/GOP、配置变化、平均 payload 码率和基础 A/V 时间范围偏差。它不自动展开 32 位 DTS，也不把最后一帧猜成具有已知时长；码率只使用同一连续段内已被下一更大 DTS 覆盖的字节组。关键帧是 FLV `FrameType=1` 声明，不等同于已验证 IDR；A/V 偏差也不包含 codec delay 或解码后唇音同步。该 v1 合同继续冻结为 AAC/AVC 范围；即使已有独立 MP3 详情，MP3 和其他 codec 仍会在 summary v1 中明确报告为未分类。完整语义见 [`ADR-005`](docs/decisions/005-bounded-media-summary.md) 与 [`ADR-006`](docs/decisions/006-bounded-mp3-detail.md)，新增诊断码见 [`media-summary-diagnostics-v1.md`](docs/media-summary-diagnostics-v1.md)。

`summary` 的退出码为：`0` 表示成功且没有 diagnostics 状态（不影响完整性的 warning 可以共存）；`2` 表示扫描或分析不完整/有 error，或 unsupported、filtered 等 warning 使摘要无法完整分类，此时仍会先输出 JSON；`1` 表示打开、读取、分析或写出等运行失败；`64` 表示参数或用法错误。

`summary --schema-version 2` 显式启用 AAC/AVC/MP3 汇总，新增 `audioCodecs` 和 `mp3` 统计。MP3 每个 Tag 仅读取首帧头；参数变化与声明码率变化分别计数，编码切换、损坏或参数变化会中断相应码率覆盖区间。Free-format 可参与 DTS 和区间码率统计，但不输出无法推导的声明码率。`--max-mp3-header-events` 限制保留的事件前缀（默认 256，最多 65,536），超出后继续准确计数；此参数仅适用于 v2。默认版本仍为 1。完整口径与示例见 [`media-summary-v2.md`](docs/media-summary-v2.md) 和 [`ADR-007`](docs/decisions/007-mp3-media-summary-v2.md)。

`detail --tag <id>` 在容器索引完整时，按 Tag 类型分发到对应的有界解析器，不改变默认扫描结果，也不会把所有 payload 或递归对象树保留在内存。ScriptData、AAC Audio、AVC Video 与 MP3 Audio 分别使用 [`flv-tag-detail-v1.schema.json`](docs/schemas/flv-tag-detail-v1.schema.json)、[`flv-aac-tag-detail-v1.schema.json`](docs/schemas/flv-aac-tag-detail-v1.schema.json)、[`flv-avc-tag-detail-v1.schema.json`](docs/schemas/flv-avc-tag-detail-v1.schema.json) 和 [`flv-mp3-tag-detail-v1.schema.json`](docs/schemas/flv-mp3-tag-detail-v1.schema.json)，由 `documentType` 区分，现有 AMF0/AAC/AVC v1 输出保持不变。AAC sequence header 会解析 AudioSpecificConfig，raw AAC 只报告源 Span。AVC sequence header 会解析 avcC 与 SPS/PPS 基础摘要；raw NALU 的长度字段宽度由 `--avc-nal-length-size 1|2|4` 明确指定，或在默认值 0 时从最近的有效 sequence header 自动取得，不会猜测四字节宽度。MP3 第一阶段仅解析 `SoundFormat=2` 的首个固定帧头，余下数据只报告源 Span；free-format 会保留完整详情但不虚构码率或帧长。

损坏或超限的 AMF0/AAC/AVC/MP3 语法仍会输出部分结果、精确到 bit 的诊断和 Tag payload 末端恢复位置，并以状态码 2 退出。非 AAC/MP3 Audio、非 AVC Video、尚未支持的编解码器以及不完整的容器索引不会伪装成详情文档。稳定诊断码见 [`docs/amf0-diagnostics-v1.md`](docs/amf0-diagnostics-v1.md)、[`docs/aac-diagnostics-v1.md`](docs/aac-diagnostics-v1.md)、[`docs/avc-diagnostics-v1.md`](docs/avc-diagnostics-v1.md) 和 [`docs/mp3-diagnostics-v1.md`](docs/mp3-diagnostics-v1.md)。

`tags --start <id> --count <n>` 扫描后输出一个有界 Tag 页，使用独立的 [`flv-tag-page-v1.schema.json`](docs/schemas/flv-tag-page-v1.schema.json) 合同。默认从 0 开始、每页 256 条，允许 1–4,096 条；ID 和 Span 不会按页重新编号。页尾 `next` 可作为下一页的 `start`；`start == total` 返回空页，超出则退出 64。当前每次 CLI 调用都会重新扫描，尚未接入 Go 后台会话，也不提供边扫描边查询。详细接口与退出码见 [`tag-index-v1.md`](docs/tag-index-v1.md)。

CLI 使用 `ScanContext` 按需构建每块 1,024 条的索引，`inspect` 流式导出且不复制整个索引；旧 `Scan` 及其 `Tags` slice 继续兼容。默认和硬上限仍为 1,000,000 个 Tag，诊断默认 1,024 条、允许范围 2–65,536 条。至少保留两条，才能同时提供首个问题和“诊断已截断”标记。更高 Tag 上限和磁盘索引仍未开放。

Go 应用层新增 `internal/session.Open`：一个会话拥有一个后台扫描 worker，支持 `Snapshot/Cancel/Wait/Page/Result/Close`。扫描期间仅发布有界标量进度，结束后才能访问完整或部分索引；文件在扫描结束后自动关闭，等待超时不强制终止底层 I/O。使用方式、错误与资源所有权见 [`后台扫描会话`](docs/background-scan-session.md) 和 [`ADR-009`](docs/decisions/009-background-scan-sessions.md)。这不是新的 CLI/JSON 合同。

`internal/desktop` 提供异步打开、状态查询、取消、关闭、Tag/诊断分页及详情和 Hex 启动/查询 DTO。`internal/document` 保留同一只读文件至关闭，扫描和后续读取使用独立取消上下文；详情与 Hex 共用单一读取 worker 和递增请求 ID。应用一次只拥有一个槽位；旧 ID 不能操作新文件，64 位数值通过十进制字符串传输。宿主退出有独立的清理入口，仍保持纯 Go 构建。初始适配层见 [`桌面适配层说明`](docs/desktop-adapter-v1.md)，源生命周期及读取协议见 [`ADR-013`](docs/decisions/013-retained-document-details.md) 和 [`ADR-015`](docs/decisions/015-bounded-desktop-hex.md)。

`desktop/` 是独立 Go 模块，接入 Wails v2.14.0 与 React/TypeScript。界面提供原生文件选择、进度、取消/关闭，以及扫描结束后的 Tag/诊断分页（每页 100 条）和 AMF0/AAC/AVC/MP3 按需详情。Hex 默认读取 512 字节、单次上限 4 KiB，支持字段/诊断源位置和 Tag ID 跳转；截断或未支持编码的原始字节也可独立查看。AVC NAL 长度由有界查找前置可用 sequence header 取得，缺失或达到预算时保留包头并明确诊断，不猜测长度；SPS/PPS 摘要和其他派生值不伪造连续源位置。MP3 只解析首个 MPEG 固定帧头，free-format 不虚构码率或帧长，CRC 标志不代表校验通过。根模块不引入 GUI 依赖；Windows 构建、测试和功能边界见 [`桌面开发说明`](desktop/README.md)、[`ADR-011`](docs/decisions/011-minimal-wails-shell.md) 与 [`ADR-014`](docs/decisions/014-bounded-desktop-avc-context.md)。本轮四类详情联动、大文件、源变化恢复及阻塞关闭实测见 [`Hex 原生验收记录`](docs/desktop-hex-20260913.md)。

扫描、详情、汇总和输出共用 Ctrl+C 取消上下文，取消退出码为 `130`，不会伪造文件损坏诊断。取消时 stdout 可能不完整，调用方应丢弃；核心返回的部分索引仍可分页读取。取消是协作式的，不能强制中断已阻塞的底层读取或写入。设计依据见 [`ADR-008`](docs/decisions/008-cancellable-chunked-index.md)。

桌面“媒体概览”按需分析已有索引，沿用 CLI v2 核心与保留的源句柄。它与详情、Hex 共用读取 worker 和请求 ID，使用独立的两分钟协作截止时间；完成的概览可在事件定位后继续查看。取消或读取失败不展示半成品，部分索引的结果明确标记覆盖范围。事件列表分别显示观测总数、保留条数与省略状态，诊断总数无法确定时显示未知。桌面采用固定资源预算，跨入口比对应使用相同预算。设计见 [`ADR-016`](docs/decisions/016-retained-desktop-summary.md)，自动、浏览器及 Windows 原生验收的结果与覆盖范围见 [`Summary 验证记录`](docs/desktop-summary-20260913.md)。

首批 8 个小样本与 1/2/10 GiB 实际落盘文件的验证结果见 [`语料与大文件实测`](docs/corpus-baseline-20260905.md)，复现方法见 [`tools/README.md`](tools/README.md)。媒体仅保留本地；公开样本尚未清权，另有一次首次取消延迟离群值待调查，不能据此宣告完整语料或性能验收通过。

后续 [`取消专项`](docs/cancellation-investigation-20260905.md) 已完成独立进程复测和 Windows 真实 Ctrl+C 事件验证；历史离群值仍作为开放观察项保留。

桌面 Tag 筛选通过已有索引组合查询类型、原始 DTS 区间和结构状态，每次最多
检查 4096 条并返回 100 个匹配项。稀疏结果使用实际源游标继续搜索，命中项可
打开详情与 Hex；部分索引、时间戳回退和查询覆盖范围均明确呈现。
查询与分页语义见 [`ADR-017`](docs/decisions/017-bounded-tag-filtering.md)。

桌面趋势按文件顺序显示每窗 256 个 Tag，不把容器 Tag 当作解码帧。独立查询
最多返回 512 点，向前最多检查 4096 条寻找同类型及 Stream ID 的最近前驱；
上下文不足时显示未知。重复、回退、大间隔和结构观察项可进入 Tag 详情或
直接查看实际原始字节，窗口与已完成的媒体概览保留。口径见
[`ADR-018`](docs/decisions/018-bounded-tag-trends.md)，验证见
[`趋势验证记录`](docs/desktop-trends-20260927.md)。

桌面导出采用独立的 `desktopDiagnosticSnapshot` v1 格式，只保存当前已载入的有界
证据，明确标记部分覆盖与省略项，不重跑媒体分析；原生保存支持结果恢复及源文件
保护。设计见 [`ADR-019`](docs/decisions/019-desktop-diagnostic-snapshot.md)，实际保存与
恢复结果见 [`诊断快照验收记录`](docs/desktop-snapshot-20260929.md)。
真实语料、40 分钟生成 AVC/AAC、原生导出与资源观测的范围和证据见
[`桌面语料回归记录`](docs/desktop-corpus-20260930.md)。

项目许可证尚待确定；在许可证落定前，不应把代码作为已授权的正式发行版分发。
