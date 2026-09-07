# MuxRay（码流透镜）

MuxRay 是一个面向 FLV 和音视频码流的跨平台诊断工具。项目已完成 P0/P1 技术基线，并开始实现 P2 基础分析；当前提供可复用、可测试的纯 Go FLV、AMF0、AAC、AVC/H.264 与 MP3 第一阶段解析核心和 CLI。

## 当前范围

- 有界 byte/bit 读取与统一源范围 `Span`；
- FLV Header、PreviousTagSize 和 Tag 索引扫描；
- 可取消的分块索引、按全局 Tag ID 查询和有界分页；
- Go 后台扫描会话、标量进度快照、取消/等待/关闭和结束后分页；
- 最小桌面 Go 适配层：单会话管理、旧请求隔离、精确整数 DTO 和 Tag/诊断分页；
- 独立 Wails/React 最小桌面壳：原生文件选择、进度、取消/关闭和有界结果页；
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

桌面详情树/hex/summary、完整 VUI/PPS/slice 等深层 H.264 语义、MP3 多帧/VBR/CRC 深层分析、分页帧级曲线和实时 RTMP/HTTP-FLV 将在后续阶段加入。

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

`internal/desktop` 在会话上提供六个 DTO 接口：异步打开、状态查询、取消、关闭、Tag 分页和诊断分页。应用一次只拥有一个槽位；旧 ID 不能操作新文件，64 位数值通过十进制字符串传输。宿主退出有独立的清理入口，仍保持纯 Go 构建。见 [`桌面适配层说明`](docs/desktop-adapter-v1.md) 和 [`ADR-010`](docs/decisions/010-minimal-desktop-adapter.md)。

`desktop/` 是独立 Go 模块，接入 Wails v2.14.0 与 React/TypeScript。界面提供原生文件选择、进度、取消/关闭，以及扫描结束后的 Tag/诊断分页（每页 100 条）。根模块不引入 GUI 依赖；Windows 构建、测试和功能边界见 [`桌面开发说明`](desktop/README.md) 与 [`ADR-011`](docs/decisions/011-minimal-wails-shell.md)。

扫描、详情、汇总和输出共用 Ctrl+C 取消上下文，取消退出码为 `130`，不会伪造文件损坏诊断。取消时 stdout 可能不完整，调用方应丢弃；核心返回的部分索引仍可分页读取。取消是协作式的，不能强制中断已阻塞的底层读取或写入。设计依据见 [`ADR-008`](docs/decisions/008-cancellable-chunked-index.md)。

首批 8 个小样本与 1/2/10 GiB 实际落盘文件的验证结果见 [`语料与大文件实测`](docs/corpus-baseline-20260905.md)，复现方法见 [`tools/README.md`](tools/README.md)。媒体仅保留本地；公开样本尚未清权，另有一次首次取消延迟离群值待调查，不能据此宣告完整语料或性能验收通过。

后续 [`取消专项`](docs/cancellation-investigation-20260905.md) 已完成独立进程复测和 Windows 真实 Ctrl+C 事件验证；历史离群值仍作为开放观察项保留。

项目许可证尚待确定；在许可证落定前，不应把代码作为已授权的正式发行版分发。
