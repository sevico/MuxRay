# MuxRay（码流透镜）

MuxRay 是一个面向 FLV 和音视频码流的跨平台诊断工具。项目已完成 P0/P1 技术基线，并开始实现 P2 分析；当前提供可复用、可测试的纯 Go FLV、AMF0、AAC、AVC/H.264 与 MP3 解析核心和 CLI。

## 当前范围

- 有界 byte/bit 读取与统一源范围 `Span`；
- FLV Header、PreviousTagSize 和 Tag 索引扫描；
- 可取消的分块索引、按全局 Tag ID 查询和有界分页；
- Go 后台扫描会话、标量进度快照、取消/等待/关闭和结束后分页；
- 最小桌面 Go 适配层：单会话管理、旧请求隔离、精确整数 DTO 和 Tag/诊断分页；
- 独立 Wails/React 最小桌面壳：原生文件选择、进度、取消/关闭和有界结果页；
- 独立文件生命周期及 AMF0/AAC/AVC/MP3 桌面详情：取消后完整前缀可读、请求恢复、字段与位级源位置；
- 有界 Hex 桌面窗口、字段/诊断源位置联动及 Tag ID 跳转，共用保留源文件与读取任务；
- 媒体概览 v3 桌面视图：可取消分析、精确时间/码率指标、有界事件分页与 Tag/Hex 联动；
- 分页媒体曲线：每 256 个源 Tag 聚合音频/视频/MP3 码率与声明 GOP 时长，覆盖全部已索引窗口并可定位 Tag/Hex；
- 有界 Tag 趋势窗口：原始 DTS、声明/实际载荷、同类型及 Stream ID 前驱时间差，异常观察项可定位详情与 Hex；
- 桌面诊断快照 JSON v3/v4 导出：冻结已有有界详情、Hex、概览、趋势、当前媒体曲线页及检索页；v4 增加当前 MP3/SEI 检索页，通过原生保存对话框写出并恢复丢失的保存回复；
- AVC 桌面有界配置候选索引、长媒体前置配置定位、NAL 长度来源、SPS/PPS 摘要与准确源位置；
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

AVC/MP3 详情 v2 和桌面详情增加了 SPS VUI、Tag 内 MP3 多帧边界、Xing/Info/VBRI 声明及头部/side-info CRC 检查。AVC 详情 v3 和桌面进一步支持 PPS 必要字段、I/P/B slice 头前缀、参数集来源与长文件分段上下文复用；MP3 v3 增加 side-info 字段及同 Tag 主数据回溯窗口分析。AVC v4 已支持普通 NAL 1/5 的完整 slice 头，v5 增加 SEI 消息解析，v6 增加有可信 SPS 来源的缓冲周期与图像时序字段；桌面 MP3 详情支持分页、帧号直达、跨页逐帧操作与汇总帧联动。图像解码、MP3 主数据解码/bit-reservoir 重建、HRD/DPB 时间线重建、分页帧级曲线和实时 RTMP/HTTP-FLV 仍未实现。

当前媒体曲线采用源 Tag 窗口，不是解码帧或固定时间桶。完成媒体概览后，每页按需
读取 64 点；未知时长不补零，最后未闭合区间不猜时长。口径与边界见
[`ADR-022`](docs/decisions/022-bounded-media-curves.md)。Linux 原生回归入口见
[`linux_smoke.py`](tools/desktop-native/README.md#linux-生产程序-smoke)。
生成媒体试用、独立数值核对和 Linux 原生结果见
[`媒体曲线验收记录`](docs/desktop-media-curves-20261007.md)。

## 开发

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/muxray inspect sample.flv
go run ./cmd/muxray tags --start 1024 --count 256 --pretty sample.flv
go run ./cmd/muxray summary --pretty sample.flv
go run ./cmd/muxray summary --schema-version 3 --pretty sample.flv
go run ./cmd/muxray detail --schema-version 2 --tag 1 --pretty sample.flv
go run ./cmd/muxray detail --schema-version 5 --tag 2 --pretty sample.flv
go run ./cmd/muxray detail --tag 0 --pretty sample.flv
```

`inspect` 默认输出单行、确定性的 `schemaVersion: 1` JSON；加入 `--pretty` 可获得两空格缩进格式。扫描发现结构错误时仍会输出报告并以状态码 2 退出。公开字段定义见 [`docs/schemas/flv-scan-report-v1.schema.json`](docs/schemas/flv-scan-report-v1.schema.json)，诊断码见 [`docs/diagnostics-v1.md`](docs/diagnostics-v1.md)。

`summary` 输出独立的 [`flv-media-summary-v1.schema.json`](docs/schemas/flv-media-summary-v1.schema.json) 文档，分析 AAC raw 与 AVC NALU 的 DTS/PTS、CompositionTime、声明关键帧/GOP、配置变化、平均 payload 码率和基础 A/V 时间范围偏差。它不自动展开 32 位 DTS，也不把最后一帧猜成具有已知时长；码率只使用同一连续段内已被下一更大 DTS 覆盖的字节组。关键帧是 FLV `FrameType=1` 声明，不等同于已验证 IDR；A/V 偏差也不包含 codec delay 或解码后唇音同步。该 v1 合同继续冻结为 AAC/AVC 范围；即使已有独立 MP3 详情，MP3 和其他 codec 仍会在 summary v1 中明确报告为未分类。完整语义见 [`ADR-005`](docs/decisions/005-bounded-media-summary.md) 与 [`ADR-006`](docs/decisions/006-bounded-mp3-detail.md)，新增诊断码见 [`media-summary-diagnostics-v1.md`](docs/media-summary-diagnostics-v1.md)。

`summary` 的退出码为：`0` 表示成功且没有 diagnostics 状态（不影响完整性的 warning 可以共存）；`2` 表示扫描或分析不完整/有 error，或 unsupported、filtered 等 warning 使摘要无法完整分类，此时仍会先输出 JSON；`1` 表示打开、读取、分析或写出等运行失败；`64` 表示参数或用法错误。

`summary --schema-version 2` 显式启用 AAC/AVC/MP3 汇总，新增 `audioCodecs` 和 `mp3` 统计。MP3 每个 Tag 仅读取首帧头；参数变化与声明码率变化分别计数，编码切换、损坏或参数变化会中断相应码率覆盖区间。Free-format 可参与 DTS 和区间码率统计，但不输出无法推导的声明码率。`--max-mp3-header-events` 限制保留的事件前缀（默认 256，最多 65,536），超出后继续准确计数；此参数适用于 v2/v3。默认版本仍为 1。完整口径与示例见 [`media-summary-v2.md`](docs/media-summary-v2.md) 和 [`ADR-007`](docs/decisions/007-mp3-media-summary-v2.md)。

`detail --tag <id>` 在容器索引完整时，按 Tag 类型分发到对应的有界解析器，不改变默认扫描结果，也不会把所有 payload 或递归对象树保留在内存。ScriptData、AAC Audio、AVC Video 与 MP3 Audio 分别使用 [`flv-tag-detail-v1.schema.json`](docs/schemas/flv-tag-detail-v1.schema.json)、[`flv-aac-tag-detail-v1.schema.json`](docs/schemas/flv-aac-tag-detail-v1.schema.json)、[`flv-avc-tag-detail-v1.schema.json`](docs/schemas/flv-avc-tag-detail-v1.schema.json) 和 [`flv-mp3-tag-detail-v1.schema.json`](docs/schemas/flv-mp3-tag-detail-v1.schema.json)，由 `documentType` 区分，现有 AMF0/AAC/AVC v1 输出保持不变。AAC sequence header 会解析 AudioSpecificConfig，raw AAC 只报告源 Span。AVC sequence header 会解析 avcC 与 SPS/PPS 基础摘要；raw NALU 的长度字段宽度由 `--avc-nal-length-size 1|2|4` 明确指定，或在默认值 0 时从最近的有效 sequence header 自动取得，不会猜测四字节宽度。MP3 第一阶段仅解析 `SoundFormat=2` 的首个固定帧头，余下数据只报告源 Span；free-format 会保留完整详情但不虚构码率或帧长。

`detail --schema-version 2` 显式启用 AVC/MP3 深层详情，使用独立的 [AVC v2 schema](docs/schemas/flv-avc-tag-detail-v2.schema.json) 和 [MP3 v2 schema](docs/schemas/flv-mp3-tag-detail-v2.schema.json)。AVC 展开 SPS VUI 的宽高比、色彩、时序、HRD 和缓冲限制并检查尾位；MP3 在当前 Tag 内检查至多 64 帧及 1 MiB 覆盖范围，识别首帧 Xing/Info/VBRI 并校验受保护帧的头部/side-info CRC。未知、截断和预算外范围保持显式，不解码图像或音频，不将声明值当作实测结果。其他 codec 不支持详情 v2；默认详情 v1 和 Summary v1/v2 不变。语义与预算见 [ADR-023](docs/decisions/023-deeper-codec-details.md)。

`detail --schema-version 3` 的 AVC 结果使用独立的 [AVC v3 schema](docs/schemas/flv-avc-tag-detail-v3.schema.json)。PPS 解析到 `redundant_pic_cnt_present_flag`，slice 头前缀解析到 `redundant_pic_cnt`，显示 I/P/B 类型、帧号、条件 POC 字段和原始 SPS/PPS 引用。每个 slice 最多读取 256 字节，每包累计 64 KiB；上下文从来源配置分段顺序回放，每段最多 8,192 个 Tag / 1 MiB 实际读取，CLI 自动推进最多 128 段，桌面用“继续准备参数”推进并复用最多 8 个检查点。参数集缺失、失效或预算不足时保留已知字段，不沿用旧配置；NAL 长度查找仍独立有效。FMO 明确报告不支持，前缀完整不表示完整头部或图像可解码。详见 [ADR-024](docs/decisions/024-bounded-pps-slice-prefix.md)。

`detail --schema-version 3` 也支持 [MP3 v3 schema](docs/schemas/flv-mp3-tag-detail-v3.schema.json)：解析 MPEG-1/2/2.5 单双声道 side-info，保留每个字段的原始 bit 位置，区分本 Tag 内回溯、需要 Tag 之前数据及未知依赖。它检查主数据槽位的长度关系，不解码 Huffman 或重建 reservoir；缺少 Tag 前历史不算损坏。CLI 保留至多 64 帧 / 1 MiB 检查范围内的 side-info；桌面逐页检查，每页保留前两帧预览或展开所选帧，并保留跨页前序依赖历史及省略提示。详见 [ADR-025](docs/decisions/025-avc-checkpoints-mp3-side-info.md) 与 [ADR-026](docs/decisions/026-desktop-codec-evidence-snapshot-v2.md)。

`detail --schema-version 4` 支持 AVC 普通 NAL 1/5 的完整 slice 头，包括参考列表修改、加权预测、MMCO、CABAC/QP/QS 与去块滤波字段；PPS 支持有界 FMO 0–6、可选缩放矩阵和尾位。桌面默认使用此深度，字段可定位原始 bit。`summary --schema-version 3` 顺序汇总所有已索引 MP3 帧，提供完整帧数、样本数、精确分数时长、逐帧参数/码率变化和未解析字节；默认检查最多 100 万帧头、独立保留 256 条事件。超限、缺失上下文和损坏均保留部分状态，不代表图像或音频可解码。旧 CLI 版本保持兼容；边界见 [ADR-029](docs/decisions/029-full-codec-headers-and-frame-summary.md)。

`detail --schema-version 5` 使用独立的 [AVC v5 schema](docs/schemas/flv-avc-tag-detail-v5.schema.json)，解析 SEI 消息的类型、声明长度、恢复点和 UUID 用户数据（最多 64 字节数据预览）。未知消息保留源范围；跨防竞争字节的字段保留分段映射。每 NAL 最多读取 64 KiB / 保留 64 消息，每包累计 256 KiB / 256 消息；参数上下文回放不解析 SEI。恢复点仅为声明，不保证能独立解码。

`detail --schema-version 6` 在 v5 基础上解析 SEI `buffering_period` 与 `pic_timing`，保留 CPB/DPB 延迟、图像结构、完整或部分时钟字段及有符号偏移，并给出原始 SPS 和选择依据。上下文缺失或含糊时保留未知，不猜测活动 SPS 或播放时间线。仅新深度的参数回放保留有界时序摘要；旧 CLI v1–v5 与默认版本不变。详见 [ADR-031](docs/decisions/031-direct-mp3-and-sei-timing.md)。

桌面 **MP3 分页详情**每页最多 64 帧 / 1 MiB 覆盖 / 64 KiB 实际读取，支持上一页、下一页、输入 Tag 内帧号直达、跨页上一/下一帧及汇总完整帧记录直接定位。帧号与位位置保持 Tag 内原始值；远页每次最多推进 16 页，并可显式继续，文档最多缓存 64 个紧凑检查点。MP3 逐帧汇总仍独立保留 256 条事件，详情可以继续查看更晚的帧。源变化、取消或过期请求不能提交检查点。详见 [ADR-030](docs/decisions/030-mp3-frame-pages-and-sei.md)。

桌面 H.264 详情对照所选 Tag 的 FLV 关键帧声明、已观察 IDR NAL 和 slice 类型集合，分别说明 NAL 头与 slice 类型覆盖，并可定位原始声明和代表证据。I slice 不等于 IDR，多 slice 不代表多个完整图像；覆盖不全时不会把未观察到当作不存在。此对照复用已有解析结果，不增加源读取，也不改变 Summary 按声明统计的 GOP。

桌面新增 **H.264 批量证据检索**：按 NAL 头筛选声明差异、未知证据或全部候选，支持继续、取消、回看和 Tag/Hex 定位。每批最多 256 Tag、64 条结果、1 MiB 读取；每 Tag 最多 256 个 NAL 头，不批量解析 slice。**MP3 跨 Tag 依赖来源**可按所选帧显式回溯，列出主数据物理槽位的来源 Tag、帧和范围，支持精确跳转。历史不足、损坏、配置切换和预算耗尽保留未知，不重建 reservoir 或解码音频。范围与合同见 [ADR-027](docs/decisions/027-bounded-codec-search-and-mp3-sources.md)。

桌面另增 **MP3 变化与停止原因检索** 和 **H.264 SEI 消息检索**：按批显式继续，
支持大 Tag 内帧位置、跨批参数比较、时序上下文与来源跳转。SEI 同 Tag 的剩余消息
可分多页返回；预算耗尽与真实停止原因分别呈现。当前已完成页可导出快照 v4，
既有 v1–v3 合同保持兼容。资源与证据边界见 [ADR-032](docs/decisions/032-codec-change-and-sei-search.md)。

干净构建产物、独立字节对照与九轮 Linux 原生结果见 [批量检索与跨 Tag 来源验收](docs/desktop-codec-search-history-20261012.md)。
后续的独立 CI、检索页快照 v3、长文件及受控取消/关闭结果见
[快照 v3 与生命周期验收](docs/desktop-snapshot-v3-regression-20261012.md)。

完整普通 H.264 头和 MP3 全帧汇总的构建身份、独立对照及原生结果见 [完整头部与全帧汇总验收](docs/desktop-full-codec-20261012.md)。
后续分页详情、汇总帧联动与 SEI 的结果见 [MP3 分页与 SEI 验收](docs/desktop-page-sei-20261013.md)。
帧号直达、SEI 时序语法及后台性能基线见 [帧号直达与时序验收](docs/desktop-timing-navigation-20261013.md)。
MP3 变化、SEI 批量检索及回归工具提速与关闭取证见 [检索与回归工具验收](docs/desktop-codec-change-sei-search-20261015.md)。
最新 Windows 复核、文件选择器焦点与通信修复及各项验收边界见 [Windows 稳定性记录](docs/desktop-windows-stability-20261016.md)。
失效 CI 基线的重新登记及固定提交对照见 [回归基线记录](docs/codec-timing-baseline-20261016.md)。

新增编解码器的生成样本、独立字段/源位对照和 Linux 基础、PPS/slice、side-info/长文件原生验收已固化到 [回归工具](tools/codec-regression/README.md)，CI 使用相同入口，生成媒体不进入 Git。

新增 [native_browse_evidence.py](tools/codec-regression/native_browse_evidence.py) 检查 MP3 选帧、H.264 声明与 NAL 证据，以及当前曲线页随快照 v2 导出；命令和范围见 [桌面开发说明](desktop/README.md#linux-构建)。

长文件与 side-info 的既有十二轮结果见 [长文件与 side-info 验收](docs/desktop-context-side-info-20261008.md)。本轮 MP3 选帧、H.264 对照、快照 v2 的十五类场景与来源按钮补验见 [编解码器浏览与导出验收](docs/desktop-codec-browser-snapshot-20261008.md)。

损坏或超限的 AMF0/AAC/AVC/MP3 语法仍会输出部分结果、精确到 bit 的诊断和 Tag payload 末端恢复位置，并以状态码 2 退出。非 AAC/MP3 Audio、非 AVC Video、尚未支持的编解码器以及不完整的容器索引不会伪装成详情文档。稳定诊断码见 [`docs/amf0-diagnostics-v1.md`](docs/amf0-diagnostics-v1.md)、[`docs/aac-diagnostics-v1.md`](docs/aac-diagnostics-v1.md)、[`docs/avc-diagnostics-v1.md`](docs/avc-diagnostics-v1.md) 和 [`docs/mp3-diagnostics-v1.md`](docs/mp3-diagnostics-v1.md)。

`tags --start <id> --count <n>` 扫描后输出一个有界 Tag 页，使用独立的 [`flv-tag-page-v1.schema.json`](docs/schemas/flv-tag-page-v1.schema.json) 合同。默认从 0 开始、每页 256 条，允许 1–4,096 条；ID 和 Span 不会按页重新编号。页尾 `next` 可作为下一页的 `start`；`start == total` 返回空页，超出则退出 64。当前每次 CLI 调用都会重新扫描，尚未接入 Go 后台会话，也不提供边扫描边查询。详细接口与退出码见 [`tag-index-v1.md`](docs/tag-index-v1.md)。

CLI 使用 `ScanContext` 按需构建每块 1,024 条的索引，`inspect` 流式导出且不复制整个索引；旧 `Scan` 及其 `Tags` slice 继续兼容。默认和硬上限仍为 1,000,000 个 Tag，诊断默认 1,024 条、允许范围 2–65,536 条。至少保留两条，才能同时提供首个问题和“诊断已截断”标记。更高 Tag 上限和磁盘索引仍未开放。

Go 应用层新增 `internal/session.Open`：一个会话拥有一个后台扫描 worker，支持 `Snapshot/Cancel/Wait/Page/Result/Close`。扫描期间仅发布有界标量进度，结束后才能访问完整或部分索引；文件在扫描结束后自动关闭，等待超时不强制终止底层 I/O。使用方式、错误与资源所有权见 [`后台扫描会话`](docs/background-scan-session.md) 和 [`ADR-009`](docs/decisions/009-background-scan-sessions.md)。这不是新的 CLI/JSON 合同。

`internal/desktop` 提供异步打开、状态查询、取消、关闭、Tag/诊断分页及详情和 Hex 启动/查询 DTO。`internal/document` 保留同一只读文件至关闭，扫描和后续读取使用独立取消上下文；详情与 Hex 共用单一读取 worker 和递增请求 ID。应用一次只拥有一个槽位；旧 ID 不能操作新文件，64 位数值通过十进制字符串传输。宿主退出有独立的清理入口，仍保持纯 Go 构建。初始适配层见 [`桌面适配层说明`](docs/desktop-adapter-v1.md)，源生命周期及读取协议见 [`ADR-013`](docs/decisions/013-retained-document-details.md) 和 [`ADR-015`](docs/decisions/015-bounded-desktop-hex.md)。

`desktop/` 是独立 Go 模块，接入 Wails v2.14.0 与 React/TypeScript。界面提供原生文件选择、进度、取消/关闭，以及扫描结束后的 Tag/诊断分页（每页 100 条）和 AMF0/AAC/AVC/MP3 按需详情。Hex 默认读取 512 字节、单次上限 4 KiB，支持字段/诊断源位置和 Tag ID 跳转；截断或未支持编码的原始字节也可独立查看。AVC NAL 长度通过有界候选索引定位最近可用 sequence header，长媒体尾部不再受 8,192 条距离上限影响；每次仍验证原始配置并限制解析与读取预算，缺失或超限时保留包头并明确诊断，不猜测长度。此定位不依赖先前选择或媒体概览；SPS/PPS 摘要和其他派生值不伪造连续源位置。桌面所选详情启用 SPS VUI、完整普通 PPS/slice 头与 MP3 多帧/VBR/头部 CRC/side-info 检查；free-format 不虚构码率或帧长，CRC 结果不代表完整音频有效。根模块不引入 GUI 依赖；Windows 与 Linux 构建、测试和功能边界见 [`桌面开发说明`](desktop/README.md)、[`ADR-011`](docs/decisions/011-minimal-wails-shell.md)、[`ADR-014`](docs/decisions/014-bounded-desktop-avc-context.md) 与 [`ADR-020`](docs/decisions/020-indexed-desktop-avc-context.md)。四类详情联动、大文件、源变化恢复及阻塞关闭的既有实测见 [`Hex 原生验收记录`](docs/desktop-hex-20260913.md)。

扫描、详情、汇总和输出共用 Ctrl+C 取消上下文，取消退出码为 `130`，不会伪造文件损坏诊断。取消时 stdout 可能不完整，调用方应丢弃；核心返回的部分索引仍可分页读取。取消是协作式的，不能强制中断已阻塞的底层读取或写入。设计依据见 [`ADR-008`](docs/decisions/008-cancellable-chunked-index.md)。

桌面“媒体概览”按需分析已有索引，使用 CLI v3 核心与保留的源句柄。它与详情、Hex 共用读取 worker 和请求 ID，使用独立的两分钟协作截止时间；完成的概览可在事件定位后继续查看。取消或读取失败不展示半成品，部分索引的结果明确标记覆盖范围。事件列表分别显示观测总数、保留条数与省略状态，诊断总数无法确定时显示未知。桌面采用固定资源预算，跨入口比对应使用相同预算。设计见 [`ADR-016`](docs/decisions/016-retained-desktop-summary.md)，自动、浏览器及 Windows 原生验收的结果与覆盖范围见 [`Summary 验证记录`](docs/desktop-summary-20260913.md)。

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

桌面导出默认采用独立的 `desktopDiagnosticSnapshot` v3；当前编解码器检索页使用 v4，
只保存当前已载入的有界证据，包括选中编解码器详情、最多 64 点的当前媒体曲线页及最多 64 行的当前 H.264
检索页，总量仍不超过 2 MiB；v1/v2 合同保持兼容。
快照明确标记部分覆盖、曲线选择和省略原因，不重读源文件或补跑分析；显式 v1 与
未指定版本的请求保持原 v1 形状。原生保存支持结果恢复及源文件保护。设计见
[`ADR-019`](docs/decisions/019-desktop-diagnostic-snapshot.md) 与
[`ADR-026`](docs/decisions/026-desktop-codec-evidence-snapshot-v2.md) 和
[`ADR-028`](docs/decisions/028-search-snapshot-v3-and-native-regression.md)，既有保存与
恢复结果见 [`诊断快照验收记录`](docs/desktop-snapshot-20260929.md)。
真实语料、40 分钟生成 AVC/AAC、原生导出与资源观测的范围和证据见
[`桌面语料回归记录`](docs/desktop-corpus-20260930.md)。
长媒体尾部 AVC 配置候选定位及首次尾部详情、Hex、导出和正常退出的实测见
[`长媒体 AVC 验收记录`](docs/desktop-avc-index-20261001.md)。
扫描成本对比、真实长录制语料及连续原生回归的当前进度见
[`稳定性验证记录`](docs/desktop-stability-20261001.md)；5 轮原生功能与真实长录像验收已完成，资源采样空档与句柄信号的限制见记录。
新增 [`长时间句柄采样`](docs/desktop-handles-20261002.md)：90 分钟记录与独立原生退出已复核，5 轮功能通过；GDI 保留、两处采样空档及未知类型仍有待处理。
后续 [`GDI 隔离记录`](docs/desktop-gdi-isolation-20261002.md)：小文件五轮和同长目录取消三轮稳定；跳过媒体打开的诊断版仍连续三轮 +2，媒体扫描并非本次复现的必要条件。生产与诊断进程均正常退出，继续定位原生选择成功路径。
新增 [`GDI 调用边界验证`](docs/desktop-gdi-boundaries-20261003.md)：原版新进程、跳过 GetResult、获取结果后跳过路径读取三组均复现每轮 +2，18 个固定窗有效且全部正常退出。A 组有一处窗口外采样间隔超限；Win32k 跟踪因访问被拒未取得对象类型，尚未确认根因或修复。
后续 [`Win32k ETW 采集与对象校准`](docs/desktop-gdi-etw-20261003.md) 已完成真实成功选择、180 秒无丢失跟踪及六类对象校准，重现 GDI 260→262；优先候选为一个 DC 和一个 Bitmap，仍需调用栈与重复轮次确认。诊断进程及六个 WebView2 子进程均正常退出，尚未修复生产代码。
新增 [`GDI 三轮分配与释放栈验证`](docs/desktop-gdi-stacks-20261003.md)：600 秒无丢失跟踪重现 260→262→264→266，每轮保留一对来自文件选择器缩略图图像列表的 DC/Bitmap；匹配符号与函数边界确认分配路径。额外的 COM 包装修正对照仍逐轮 +2，未作为 GDI 修复合入；下一步用独立原生宿主区分宿主与 Shell 行为。
后续 [`独立 Win32 宿主对照`](docs/desktop-gdi-native-host-20261003.md)：去掉 Go、Wails、WebView2 后仍重现 171→173→175→177，三轮各保留一对匹配同一缩略图核心分配栈的 DC/Bitmap；600 秒跟踪与正常退出复核通过。这些宿主组件不是复现的必要条件，持有责任仍待确定；下一步验证每轮独立 STA 结束后是否释放。
新增 [`每轮独立 STA 对照`](docs/desktop-gdi-sta-cycle-20261003.md)：每轮 COM 清理和实际线程退出后仍逐轮 +2；原前两轮加独立补采共三次确认同源 DC/Bitmap 保留，未作为生产修复。七个固定窗、正常退出和采集清理复核通过；原第三轮缺少 ETW 覆盖及等待期间采样空档均保留，下一步做同字节 `.flv/.bin` 文件关联对照。
后续 [`同字节 FLV/BIN 文件关联对照`](docs/desktop-gdi-association-20261004.md)：两组均重现 169→171→173→175，各三轮保留同一缩略图核心路径的 DC/Bitmap；BIN 首轮另捕获正常销毁后经通知/ResetImages 重建的对象。双跟踪零丢失、正常退出及清理已复核；时间门偏差和 BIN 末轮三分钟计数尾窗超出 ETW 均保留。尚未确定持有者或修改生产代码，下一步做同条件取消/成功选择的对象级对照。
新增 [`同一文件取消/成功选择对象级对照`](docs/desktop-gdi-cancel-select-20261004.md)：两组均重现 171→173→175→177，各三轮保留同核心 DC/Bitmap，成功接受不是本条件下存留的必要条件。取消组另有关闭输入前经 lookup-table 释放/析构销毁并由 ResetImages 重建的样本；完整创建栈既可对应销毁也可对应保留。十窗、双跟踪、独立原始审核和正常退出/清理通过，首次采集准备失败及修正保留；下一步追查具体实例的引用持有，尚未合入生产修复。
后续 [`具体图像列表与引用持有者`](docs/desktop-gdi-reference-20261004.md)：两次取消后，manager 剩余引用均为 1，并继续持有 lookup 和核心图像列表。第二周期进一步验证 `CDefCollection → CEnumerableObjectCollection → 数组元素 → manager` 的具体持有槽与 AddRef 调用；集合清空和析构有对应 Release 路径，上层集合为何继续存在仍待定位。未修改生产释放逻辑。
新增 [`CDefCollection 引用来源与集合清理边界`](docs/desktop-gdi-collection-20261007.md)：两个新进程各取消一次、工作线程退出 60 秒以上后，集合仍为 2、元素集合为 1、manager 为 1；A 的 Clear、B 的 RemoveObjectAt 及两组集合析构入口均未命中。取得/释放对偶与内联引用增量已核实，析构后残留指针不能当作活持有者；最后两份引用仍未归属。两组正常退出与采集清理通过，下一步建议验证文件选择器辅助进程原型，尚未实施生产修复。

Linux 现提供 `desktop/build-production.sh` 和 Ubuntu 24.04 非 GUI CI；Linux Mint 22.3 上的真实 GTK/WebKit 打开、取消、诊断、导出、源释放和正常退出结果见 [`Linux 验证记录`](docs/desktop-linux-20261007.md)。Windows 辅助选择器的独立结果与限制见 [`Windows 稳定性记录`](docs/desktop-windows-stability-20261016.md)。

项目许可证尚待确定；在许可证落定前，不应把代码作为已授权的正式发行版分发。
