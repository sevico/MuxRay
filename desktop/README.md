# MuxRay 最小桌面版

Wails **v2.14.0** + React **18.3.1** + TypeScript **5.9.3**，构建工具 Vite
**7.3.6**。这是本地开发预览，不是已完成清权、签名或安装器验收的发行版。
架构边界见 [ADR-011](../docs/decisions/011-minimal-wails-shell.md)。
打开响应恢复协议见 [ADR-012](../docs/decisions/012-recoverable-native-open.md)。
文件所有权和有界详情协议见 [ADR-013](../docs/decisions/013-retained-document-details.md)。
AVC 上下文查找和准确源位置见 [ADR-014](../docs/decisions/014-bounded-desktop-avc-context.md)。
长媒体的配置候选索引见 [ADR-020](../docs/decisions/020-indexed-desktop-avc-context.md)。

## Windows 文件选择辅助进程原型

`./build-picker-prototype.ps1` 通过 `pickerprocess` 构建标记生成独立命名的
`build/bin/MuxRay-picker-prototype.exe`。每次 FLV 打开由一个子进程显示原生对话框，
主进程等它实际退出并核对回复后才加载媒体；取消分支不创建会话。已实现超时、
子进程崩溃和协议错误的清理及窗口恢复逻辑，既有请求恢复不会重复启动文件选择器。
原生窗口恢复尚未通过验收，已知问题见下方验证记录。

这是 [ADR-021](../docs/decisions/021-isolated-file-picker-prototype.md) 定义的可选原型。
默认生产构建、JSON 保存对话框及非 Windows 路径保持原实现。子进程仍包含同一 EXE
的 Go 依赖初始化，但不进入 Wails 主循环、不创建 WebView、不扫描媒体。它提供进程
资源隔离，不是安全沙箱，也没有修复尚未归属的 Shell 内部引用。

非界面验证命令为 `go test -tags pickerprocess -count=1 ./...`、
`go vet -tags pickerprocess ./...`，具备 gcc 时再运行
`go test -race -tags pickerprocess -count=1 ./...`。测试子进程不会弹出文件对话框。

[原型验证记录](../docs/desktop-picker-process-20261007.md)：自动测试通过，首次原生取消出现主进程退出码 1，正在诊断。六轮资源回收验收尚未通过，暂不用于默认发布。

## 范围

- 原生 FLV 文件选择；取消选择不创建会话。
- 单文件后台扫描、200 ms 串行状态轮询、取消、关闭和宿主退出清理。
- 扫描结束后查看完整或部分 Tag/结构诊断，每页最多 100 条，不缓存历史页。
- 按 Tag 类型、DTS 区间和结构状态组合筛选；有界搜索后继续定位到详情与 Hex。
- 按需查看 Tag 趋势窗口：DTS、载荷大小和同类型时间差，观察项直接定位详情与 Hex。
- 通过原生保存对话框导出有界诊断快照 JSON，包含当前已载入证据及明确的覆盖/省略信息。
- 选择完整的 Tag 查看 AMF0 字段树、AAC 音频头、AudioSpecificConfig 或 raw 包范围，保留原始 byte/bit 位置和语法诊断。
- AVC Video 包头、有符号 CTS 与派生 PTS、avcC/SPS/PPS 基础摘要、NAL 长度及头字段，并说明所用前置配置的来源。
- MP3 FLV 音频头、Tag 内 MPEG Audio Layer III 帧边界、首帧 VBR 声明、头部/side-info CRC 与未检查范围。
- 按需媒体概览 v2：时间轴、区间码率、配置变化及保留事件分页，事件可定位到 Tag 详情与 Hex。
- 分页媒体码率与 GOP 曲线：覆盖全部已索引源 Tag 窗口，每页 64 点，可定位闭合 Tag 与实际 Hex。
- 偏移、DTS、Tag ID 和总数保留十进制字符串；分页和比例计算使用 BigInt。
- 文件只读，不上传媒体；界面只显示文件名，不存储历史路径。

“扫描结束”不是“媒体语义全部正常”。不完整索引明确显示部分结果，结构
诊断与打开/读取/通信失败分开。仍使用核心的百万 Tag 上限和诊断预算。
解码帧曲线、固定时间分桶、拖放、多文件、边扫描边分页尚未接入。

## 媒体码率与 GOP 曲线

先点击“分析媒体”，完成后点击“查看媒体曲线”。横轴每个窗口包含连续 256 个
源 Tag，页面显示 64 点，可切换音频、视频、MP3 码率和 GOP 平均/最长时长。
曲线来自同一次媒体分析，翻页不重读文件；最多保留 3907 点，事件列表超过
保留上限后仍有后续窗口。部分索引和分析不完整始终单独标明。

码率使用 Summary 的已覆盖字节和正向 DTS 区间，音频包含 MP3。区间和 GOP
归到闭合 Tag 所在窗口，起点可能在前窗；最后未闭合区间没有猜测时长。
GOP 依据 FLV 关键帧声明，不证明 IDR。未知值不补零，可在精确数据表中查看。
码率点定位窗口内首个正向区间的闭合 Tag；两个 GOP 指标均定位最长 GOP 的
闭合 Tag。选中后可继续查看实际 Tag 原始字节，当前曲线页会保留。

重新分析、取消/失败、源变化和关闭会清空旧曲线；查询超时可显式重试。
诊断快照 v2 可包含当前已加载的曲线页与所选指标；v1 仍不包含曲线。曲线定义见
[ADR-022](../docs/decisions/022-bounded-media-curves.md)。
生成样本、逐点独立核对和生产窗口结果见
[媒体曲线验收记录](../docs/desktop-media-curves-20261007.md)。

## 诊断快照导出

扫描结束后点击“导出诊断 JSON”，先冻结当前会话的有界结果，再选择保存位置。
界面显式请求独立的 `desktopDiagnosticSnapshot` v3 格式，包含文件名、生成时间、扫描
覆盖、前 100 条保留结构诊断、选中 Tag、匹配的已完成详情/Hex、已有 Summary 概览
与当前事件页，以及当前趋势窗口、阈值和局部观察项。v2 还包含当前已完成的媒体曲线页，
最多 64 点，保留起点、数量、指标和 Summary 请求身份；选中 MP3 帧与 H.264 对照信息
随已有详情字段树冻结。坐标保持十进制字符串，未知值保持 null。
缺失、未请求或已淘汰的部分显式列入 `omitted`；不会为导出补跑分析或读取媒体。
`mediaCurvePage` 始终为已有页或 null，不接受客户端提供曲线数据。未指定版本或
显式版本 1 仍输出原 v1 形状，不能携带曲线选择；v1/v2 合同保持兼容。

v3 增加当前已完成的 H.264 检索页，最多 64 条结果。`avcSearch` 只传请求身份、
批次起点和筛选条件；后端复制保留页，输出覆盖游标、索引完整性、未知原因和源位置。
`avcSearchPage` 始终为已有页或 null，并明确省略原因。未完成、已取消或已淘汰的批次
不会导出；没有命中的已完成批次仍会保存。详情/Hex 跳转不丢失该页，导出不会重新
检索或遍历历史批次。范围与生命周期见 [ADR-028](../docs/decisions/028-search-snapshot-v3-and-native-regression.md)。

快照最多 2 MiB，Hex 最多为已有窗口的 4 KiB 实际字节；报告可能包含 AMF 字段文本
和原始媒体字节。只保存源文件名，不包含源绝对路径。它不是完整 CLI 报告，也不是
全文件时间线；部分索引、事件保留上限和未知前驱仍保留原有含义。

关闭源文件不会取消已经冻结的保存。保存期间可继续浏览或关闭当前文件；新保存
和打开操作会等待结果确认。通信超时后查询原请求，失败时“重试”只确认保存结果，
不重复弹窗或写文件。写入失败不替换已有目标，源文件和硬链接别名禁止作为目标。
详情见 [ADR-019](../docs/decisions/019-desktop-diagnostic-snapshot.md) 与
[ADR-026](../docs/decisions/026-desktop-codec-evidence-snapshot-v2.md)，既有导出、保存恢复
及检查范围见 [诊断快照验收记录](../docs/desktop-snapshot-20260929.md)。
已有真实语料、40 分钟生成 AVC/AAC 及生产进程资源观测见
[桌面语料回归记录](../docs/desktop-corpus-20260930.md)。

## Tag 筛选与时间定位

[ADR-017](../docs/decisions/017-bounded-tag-filtering.md) 定义独立的索引查询。
类型可选全部、音频、视频、脚本或其他；DTS 上下界可留空，填入时使用
0–4294967295 的整数毫秒，包含两端。结构状态可选载荷截断、PreviousTagSize
缺失或不一致。条件取交集；时间是原始 FLV DTS，保留重复和回退，不代表播放时长。

每次查询最多检查 4096 个索引记录、返回 100 个命中，按文件顺序保留原 Tag ID。
空结果仍可能有后续待查范围，使用“继续查找”推进；界面显示检查范围和本页命中，
不把索引总量当作匹配总数。查询只读已提交索引，不重扫源或解析 payload。
部分索引照常可查，但结果不代表文件未索引部分。详情和 Hex 仍按既有协议读取源。

修改条件后点击应用，重置可返回普通索引；直接输入 Tag ID 也使用普通索引。
从筛选命中项进入详情或 Hex 保留筛选页与已完成媒体概览。条件切换、关闭、
源变化和新会话会隔离旧查询回复；查询超时可重试同一范围。

自动检查、浏览器交互、原生验收及取消恢复记录见
[2026-09-25 验证记录](../docs/desktop-filter-20260925.md)。

## Tag 趋势与异常定位

[ADR-018](../docs/decisions/018-bounded-tag-trends.md) 定义独立、只读索引的连续
趋势窗口。输入起始 Tag ID 后点击“查看趋势”，每窗展示 256 条；首页、上一窗
和下一窗只查询指定范围，不自动遍历整个文件。图中类型选择只改变当前窗口
显示，与 Tag 列表筛选独立；窗口覆盖与部分索引状态始终明确。

横轴按源 Tag ID 排列，可切换原始 DTS、声明/实际载荷大小以及 DTS 差。
时间差的前驱是相同 base Tag 类型和 Stream ID 的最近 Tag，包含配置、filtered
和损坏项。每次最多向前检查 4096 条；前驱未知不当作零，也不推断时间异常。
重复、回退和正向差值严格大于阈值（默认 1000 ms）是观察提示，不证明媒体损坏。
阈值修改只重算当前显示，不读取源。载荷截断与 PreviousTagSize 缺失/不一致
分别显示；未知差值不画点，缺失载荷不补齐。

点击图中点、选择本窗口 Tag 或点击观察项即可读取详情；由此进入普通 Tag
索引并退出原 Tag 筛选，趋势窗口与已完成 Summary 保留。Tag Hex 和 Payload Hex
直接使用索引记录的实际范围，不依赖详情解析成功；详情与 Hex 都提供返回趋势
入口。窗口查询沿用 5 秒通信等待、显式重试和旧回复隔离。坐标保持精确十进制
字符串，绘图仅将有界相对比例转为浮点像素。

这不是播放时间轴、帧时长、码率或 GOP 分析，不排序或展开时间戳回绕。
自动检查、浏览器和生产原生验收见 [2026-09-27 验证记录](../docs/desktop-trends-20260927.md)。

## Hex 与源位置导航

[ADR-015](../docs/decisions/015-bounded-desktop-hex.md) 增加只读 Hex 窗口：默认 512 byte、
单次最多 4 KiB、每行 16 byte，同时显示十六进制和可打印 ASCII。支持窗口翻页、
精确字节偏移输入、字段/Tag/诊断定位，以及输入 Tag ID 后只读取其所在的一页。
原始字段按 byte/bit 范围高亮；派生字段没有源 Span 时不提供定位，诊断只标记位置。
AVC 配置来源可以指向前置 Tag。大字段只高亮当前窗口的交集，不按整个 Span 分配。

Hex 不依赖详情解析成功；扫描终态后可以读取当前文件的实际范围，包括不支持编码、
截断数据及取消扫描后的未索引字节。原始字节可读不代表该范围已通过结构或语法校验。
到 EOF 只返回实际字节，恰在 EOF 的请求返回空窗口，不补零。读取前后沿用源变化检查。

Hex 与详情共用单一 worker、一个可替换待处理意图和跨类型递增的请求 ID；新请求取消
旧任务，每种结果最多保留一份有界内容。坐标以十进制字符串传输，前端使用 BigInt。
关闭立即拒绝读取并清空界面，等待实际 I/O 结束后再释放同一文件句柄。回复丢失时确认
原请求，前端等待超时不等于宿主任务已经取消。详细预算与恢复约定见 ADR-015。

已接纳的扫描结束或取消后，桌面文档保留同一只读源句柄，直到关闭文件或窗口。
在 opening 阶段取消会取消打开过程；若文件已经打开但文档尚未发布，迟到的
文档会关闭并清理，不保留详情。界面会收到关闭状态，清理结束后才可重新打开。
完整的已提交前缀 Tag 可以解析；截断 payload、filtered Tag 和不支持的
编码会说明原因。AMF0/AAC/AVC/MP3 语法错误属于所选详情，不影响其他 Tag。
详情每次最多显示 512 字段、64 诊断和每段 256 字节 UTF-8 预览，实际
详情 JSON 不超过 256 KiB；预算或预览省略会明确显示。AAC raw 不进行音频解码。
解析预算与显示预算独立：AMF0 最多 32 层、4,096 节点、每个容器 4,096 项，
单段及累计保留字符串均不超过 64 KiB；AMF0/AAC 各最多保留 64 条解析诊断。

AVC NALU 通过同一文档的配置候选索引定位最近可用 sequence header，长媒体尾部
不再受 8,192 个前置记录的距离限制，也不需要先查看配置或运行媒体概览。桌面扫描
对完整、未 filtered 且至少两字节的 video payload 仅读取两个前缀字节，记录可能的 AVC sequence
header；候选位图最多 125,000 字节，不保留 payload 或已解析长度。普通 CLI 扫描
保持原有结构读取范围。候选不是已验证配置，每次详情仍重新读取并解析原始声明。

详情显示 `found/missing/budget_exhausted/not_needed`、`lookupMethod=candidate_index`、
来源 Tag、原始长度声明位置与搜索计数。`scannedTags` 是本次实际访问的索引记录数，
不代表到源配置的 Tag 距离。每次最多解析 32 个候选配置，累计源读取不超过 1 MiB
（含所选包探测），查找与所选详情共享三秒协作截止。缺配置或达到预算时保留包头
与 opaque 范围，不猜四字节 NAL 长度。不可用的候选可跳过，但更深 SPS 语义不完整
本身不会否定已经完整的长度声明。扫描取消后，已提交且上下文覆盖明确的前缀仍可
独立读取详情；源变化、旧请求隔离与关闭等待继续沿用同一文档生命周期。

AVC 解析预算为 64 条诊断、64 个 avcC 参数集条目、256 个 NAL、每个参数集 EBSP
正文 64 KiB（不含 NAL 头），以及每次 4,096 个 RBSP 映射项。
包内 SPS/PPS 数量受 NAL 上限约束，全部结果仍独立受字段/JSON 展示预算限制。
FLV/avcC/NAL 的连续原始范围保留精确位置；DTS/PTS、尺寸和 SPS/PPS 摘要等派生字段
显示“—”，不会把去除防竞争字节后跨段的 RBSP 字段伪装成连续原始 Span。
SPS 详情进一步展开 VUI 的宽高比、色彩、时序、NAL/VCL HRD 和 bitstream restrictions，
并检查 SPS 尾位；每个 HRD 最多 32 个 CPB 条目。时序与 HRD 是声明值，不代表实测结果。
VUI 字段沿用 SPS 摘要的无单一 Span 展示，可从整个 SPS 定位 Hex；逐字段分段源映射
由 CLI 详情 v2/v3 提供，不将跨防竞争字节的范围伪装为连续 Span。

PPS 必要字段及 NAL 类型 1/5 的 slice 头前缀进一步显示预测标记、QP 声明、
I/P/B 类型、帧号、条件 POC 字段及所引用 SPS/PPS 的原始位置。单段原始字段可定位 Hex，
跨段字段保持无单一 Span。`prefixComplete` 仅表示解析到了 `redundant_pic_cnt`
（PPS 到对应 present flag），不检查后续头部、PPS 扩展或图像正文；FMO 明确不支持。
每个 slice 最多读 256 字节、每包累计 64 KiB。参数上下文与 NAL 长度查找独立，
从来源配置分段顺序回放至所选 Tag 前，每次最多 8,192 个 Tag / 1 MiB 实际读取；
显示覆盖计数及 missing/invalidated/budget_exhausted，遇坏配置、EOS、编码切换
或不可检查视频范围不沿用旧参数。长距离参数预算不否定已找到的 NAL 长度。
达到预算后显示已完成的 Tag 边界，可点击“继续准备参数”推进下一段；单 Tag 无法完成时不提供继续操作。每文档最多缓存 8 个紧凑检查点（合计上限 4 MiB），同文件随机跳转可以复用，取消或过期请求不提交，源变化/关闭即清除。每次仍受三秒截止和串行读取约束。CLI v3 自动推进最多 128 段，见 [ADR-025](../docs/decisions/025-avc-checkpoints-mp3-side-info.md)。
“H.264 关键帧声明与 NAL 证据”显示所选 Tag 的 FLV FrameType、IDR NAL 观察和
P/B/I/SP/SI slice 类型集合，并分别标明 NAL 头与 slice 类型覆盖。已观察到的 IDR
在部分覆盖时仍可报告；只有全部 NAL 头可用时，“未观察到”才是完整的负向结论，
否则显示未知。保留/未分类计数和覆盖提示可用于判断观察范围。
FLV 声明、NAL type 和单段 slice_type 证据可定位 Hex；派生集合不伪造源 Span。
I slice 不证明 IDR，多 slice 不等同于多个完整图像，声明与观察差异不新增损坏诊断。
此对照复用解析结果且不增加读取，媒体概览的 GOP 仍使用 FLV 声明，见
[ADR-026](../docs/decisions/026-desktop-codec-evidence-snapshot-v2.md)。

“H.264 批量证据检索”按 NAL 头筛选声明差异、未知或全部 AVC/未分类视频候选。
每批最多检查 256 Tag、保留 64 行、读取 1 MiB，每 Tag 最多检查 256 个 NAL 头；
不读取 slice/参数集正文。结果给出真实 `[start,next)` 游标，可以继续、回看或取消；
取消后重试当前批次，不跳过未完成范围。只保留当前结果页和最多 64 个历史起点。
扫描不完整时会明确提示只覆盖已有索引。来源按钮可跳转 Tag 或原始位范围；当前
已完成批次可随快照 v3 导出，也可打开所选 Tag 后附带其详细证据。

MP3 深层详情仅支持 `SoundFormat=2`；保留的 `SoundFormat=14` 仍不支持。
在当前 Tag 内按帧头推导的长度检查至多 64 帧、1 MiB 覆盖范围，仅读取帧头与必要的
元数据/保护字节，跳过压缩正文。解析首帧 Xing/Info/VBRI 声明，受保护的 Layer III 帧
检查头部末 16 位和 side-info 的 CRC。声明流总帧数与当前 Tag 观察帧数分别显示；
Xing/Info 元数据帧可能不计入声明总数。CRC 不校验主数据、bit-reservoir 或解码音频。
原始位字段保留 Span；采样率、声道数、码率、推导帧长等值没有单一原始位置。
free-format 的码率和帧长保持未知，不搜索下一个同步字、不跨 Tag 拼接。
损坏、截断和超预算保留已解析前缀及未检查范围，帧边界完整不保证可解码。
继续使用同一详情 worker、源检查、展示预算和请求恢复机制。CLI 默认首帧头合同
保持兼容，帧/VBR/CRC CLI 使用 `detail --schema-version 2`；参见
[ADR-023](../docs/decisions/023-deeper-codec-details.md)。
side-info 进一步展开 MPEG-1/2/2.5 的 SCFSI、granule/channel、长短块条件字段与精确源 bit 位置。每帧依赖摘要区分 `none/within_tag/before_tag/unknown`；`before_tag` 只表示需要 Tag 前历史，不是损坏。`knownPreviousBytes` 是同 Tag 的物理主数据槽位，不是 reservoir 剩余容量。不解码主数据或跨 Tag 拼接，详见 [ADR-025](../docs/decisions/025-avc-checkpoints-mp3-side-info.md)。

“MP3 逐帧详情”默认保留前两帧预览；也可通过选择框、上一/下一帧或“末个可选 MP3 帧”
浏览当前检查范围内的完整帧边界，索引从 0 开始。选帧仍从本 Tag 的有界前缀检查，
保留依赖计算所需的前序历史，不把所选帧当作新流起点；64 帧 / 1 MiB 的预算不变。
所有已检查帧的边界、CRC、依赖和 VBR 摘要保留，只展开所选帧的 side-info。
未展开记录继续通过 `sideInfoOmittedFrames` 和展示省略标记提示；截断边界、未知帧长
和预算外帧不可选。可返回“前两帧预览”，全部已检查帧仍可用 CLI v3 导出。

点击“定位 MP3 依赖来源”会为当前帧显式回溯前序音频 Tag（预览状态选择帧 0）。
最多回看 512 Tag / 64 音频 Tag，检查每 Tag 64 帧、合计 256 帧；源范围覆盖和实际
读取分别最多 1 MiB，最多 64 条来源。前序 Tag 必须从头定位到末尾；超过预算不能
跳过未检查尾部。视频/脚本交错不视为音频中断；配置、编码、流标识变化及损坏均
保留未知原因。来源表列出 `main_data_begin` 物理回指窗口与当前槽位的 Tag/帧/Span，
可打开来源帧或高亮 Hex。该字段组随现有诊断 JSON 详情导出，旧 CLI 合同不变。
不判断 reservoir 剩余容量，不拼接/解码主数据，详见
[ADR-027](../docs/decisions/027-bounded-codec-search-and-mp3-sources.md)。

跨 Tag / 批量检索的独立样本与 Linux 原生回归：

```bash
/usr/bin/python3 -B tools/codec-regression/search_history_oracle.py --output-dir .cache/sh-samples
/usr/bin/python3 -B tools/codec-regression/native_search_history.py \
  --input-dir .cache/sh-samples --binary /absolute/path/to/MuxRay \
  --output-dir .cache/sh-native --timeout-seconds 420
```

从仓库根目录执行，输出目录必须尚不存在；验收期间使用固定的二进制副本。
生成器需要 FFmpeg/ffprobe，原生环境和已有 smoke 工具相同。默认九轮覆盖 MP3
合并/拆分/视频交错/音频中断/历史不足，以及 H.264 分页/未知/部分正向证据/缺配置。
工具对原始位、物理来源范围、页面结果、快照、关闭后文件描述符和正常退出分别校验。
九轮已接入独立的 Linux CI job。长文件的 64 批历史边界和受控阻塞读取取消/关闭
另有独立入口，复现命令与测试构建边界见
[回归工具说明](../tools/codec-regression/README.md#long-search-and-controlled-native-cancellation)。
当前固定生产构建、v3 导出、长文件及受控生命周期的实测结果见
[快照 v3 与生命周期验收](../docs/desktop-snapshot-v3-regression-20261012.md)。

生成编码样本、CRC 结构样本及六轮 Linux 原生结果见
[深层详情验收](../docs/desktop-codec-details-20261007.md)。
PPS/slice 前缀、回归固化和最新八轮结果见
[PPS/slice 验收记录](../docs/desktop-pps-slice-20261007.md)。
长文件上下文、MP3 side-info 及最新十二轮结果见
[长文件与 side-info 验收](../docs/desktop-context-side-info-20261008.md)。

详情读取前后检查原句柄的文件身份、大小和修改时间，发现改变后要求关闭重开。
这不是不可变快照，不能识别所有同大小、同时间戳的原位修改。快速切换仅保留
最新选择，至多一个实际解析任务；前端 5 秒等待上限和后端 3 秒协作截止均
不能强制中断阻塞的系统读取。未结束的读取继续占用资源，关闭不会提前复用槽位。

详情确认每次响应后间隔 200 ms 串行查询；连续收到 25 次 pending 查询响应后
暂停自动确认，保留同一请求 ID 供手动重试，初次 Start 响应不计入这 25 次。
这是查询次数上限，不是详情必须在 5 秒内完成。Start 响应丢失时先查询原请求；
确认失败或超时也会暂停，不自动新建解析。快速切换 Tag 时，旧回复立即失效，
已发出的本地调用仍等到返回或 5 秒截止后再发送最新选择；切页、切换诊断页、
关闭文件或销毁界面则立即清除本地详情等待。只有宿主已确认可重试的解析失败，
用户选择重新读取时才递增请求 ID。

详情协议中的 `sessionId` 是不透明字符串，例如 `scan-1`；请求 ID、Tag ID 和
64 位位置使用规范十进制字符串。`GetDetail` 在尚未定位到当前已接纳请求时
返回前置错误，未知的 `tagId` 为空字符串；已接纳请求的结果保留准确 Tag ID。

关闭、取消、状态和 Tag/诊断分页调用均有 5 秒等待上限。关闭或取消的
响应丢失、报错或超时时，界面先查询当前会话确认实际结果，不自动重发
关闭/取消请求。确认前暂停新的分页读取；若状态查询也失败或超时，则
停止自动查询并保留“重试查询”入口。手动重试会先刷新状态，包括已结束
的会话，再恢复当前分页。迟到响应不能覆盖较新的操作或会话；控制器
销毁时释放本地等待和定时器。等待超时不等于宿主操作已被撤销。

打开响应失败或 5 秒内未返回时，界面按原请求 ID 确认打开结果，不重新弹出
选择器。选择器仍打开则继续等待；已接纳的扫描恢复为当前会话。确认调用
也有 5 秒超时，失败后可“重试查询”。宿主只保留最新打开请求的结果，并
封存尚未送达的请求，避免迟到的打开命令创建无人管理的会话。

## 媒体概览

扫描终态后可点击“分析媒体”生成媒体概览 v2，包括时间轴、区间码率、
编码变化、声明关键帧/GOP、AAC/AVC 配置及 MP3 首帧头事件。概览在进入
Tag 详情和 Hex 后保留；重新分析、关闭或发现源变化会清理旧结果。
只有索引前缀可用时明确标注“部分索引”。取消分析后可重新分析；底层
读取仍阻塞时保持取消中，关闭文件仍然可用。

Summary 使用 120 秒协作截止时间、每次 5 秒 IPC 等待、200 ms 串行查询，
最多 600 次 pending 查询后转为手动确认。响应丢失时按原 ID 确认结果。
关键帧最多保留 1,024 条、AAC/AVC 配置各 128 条、MP3 帧头及诊断各 256 条；
事件每页至多 100 条且还受 256 KiB 响应大小限制。界面采用精确字符串，
缺失值显示“—”，诊断截断后未知的总数不会被保留条数冒充。详见
[ADR-016](../docs/decisions/016-retained-desktop-summary.md)，当前验收状态见
[Summary 验证记录](../docs/desktop-summary-20260913.md)。

## Windows 构建

前提：根模块要求的 Go 版本、Node 22.18+（本机使用 24.4.1）、npm、
Wails CLI v2.14.0，以及已安装的 WebView2 Runtime。不要用 `@latest`
悄悄改变已审定的 Wails 版本。Linux 构建见下一节；macOS 桌面壳尚未验收。

```powershell
# 如尚未安装 CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0

# 从仓库根目录进入独立模块
cd desktop
.\build-production.ps1
.\build\bin\MuxRay.exe
```

构建会生成绑定，按 lockfile 安装前端依赖并嵌入生产 assets。
`-webview2 error` 不自动下载/安装 WebView2；缺少运行时应由用户安装。
`-nopackage` 不生成安装包，也不附带第三方媒体或原版图标。
不要直接用无 build tags 的 `go run .` 代替 Wails 的生产构建。

## Linux 构建

Ubuntu 24.04 / Linux Mint 22.x 使用 GTK 3 和 WebKitGTK 4.1。需要 Go、
Node 22.18+、npm，以及以下开发包：

```bash
sudo apt-get install --no-install-recommends build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev

# 在仓库根目录运行
./desktop/build-production.sh
./desktop/build/bin/MuxRay
```

脚本固定调用 Wails v2.14.0，默认使用 `GOTOOLCHAIN=go1.27.0`，允许调用者
显式覆盖该环境变量；不会修改全局 Go 配置。首次运行需要下载对应工具链和
依赖。构建使用 `webkit2_41` 标记、真实生成的绑定及 lockfile 中的前端依赖，
并将该版本生成器在 Unix 上添加的绑定文件执行位恢复为 0644。

Linux 使用 Wails 的 GTK 原生打开/保存对话框，不启用 Windows 的
`pickerprocess` 原型。产物是本机可执行文件，需要系统提供 GTK/WebKitGTK
运行库；当前不生成 AppImage、deb 或 rpm 包。

```bash
# 在 desktop/ 下，先完成上述构建
GOTOOLCHAIN=go1.27.0 go test -race -tags webkit2_41 -count=1 ./...
GOTOOLCHAIN=go1.27.0 go vet -tags webkit2_41 ./...
```

Linux CI 在 Ubuntu 24.04 构建并核对生成物、运行前端单测和 Go race/vet，
随后在独立 D-Bus/Xvfb 中执行真实 GTK 打开、详情/Hex、原生导出、FD 释放与正常退出，
并上传验收日志。本机复现命令与依赖见
[Linux smoke](../tools/desktop-native/README.md#linux-生产程序-smoke)。
新增的 [native_browse_evidence.py](../tools/codec-regression/native_browse_evidence.py)
复用同一隔离环境，检查 MP3 第三帧/末帧选择、B 多 slice 与 IDR 证据定位、曲线和
快照 v2，并与原始字节及 CLI Summary 核对。先按
[回归工具](../tools/codec-regression/README.md) 生成两组样本、构建 CLI 和生产桌面，再运行：

```bash
# 在仓库根目录运行；输出目录必须尚不存在
python3 -B tools/codec-regression/native_browse_evidence.py \
  --input-dir /absolute/path/to/codec-samples \
  --side-info-dir /absolute/path/to/side-info-samples \
  --binary desktop/build/bin/MuxRay --cli /absolute/path/to/muxray \
  --output-dir /absolute/path/to/new-browse-evidence
```

运行结果保存在对应证据目录的 `result.json`、日志和原生导出中。
本轮干净提交构建、十五类场景及来源按钮补验见
[编解码器浏览与快照 v2 验收](../docs/desktop-codec-browser-snapshot-20261008.md)。
此前真实 GTK/WebKit 窗口检查与其证据边界见
[Linux 验证记录](../docs/desktop-linux-20261007.md)。

## 检查与开发

```powershell
# 在 desktop/ 下
wails generate module
npm --prefix frontend ci --no-audit --no-fund
npm --prefix frontend test
npm --prefix frontend run build
go test -count=1 ./...
go vet ./...
# 已配置 GCC / CGO 的 Windows 开发机
go test -race -count=1 ./...
```

提交 `go.sum`、`package-lock.json` 和 `frontend/wailsjs/go/` 的真实生成物；
不要手改生成代码。修改 Go DTO 后重新生成并检查差异。Wails 的指针 TS
声明使用可选字段，`src/normalize.ts` 将实际 JSON 的 null 和声明中的
undefined 统一为显式 null，并保持结构类型检查。

根目录仍可独立运行 `go test ./...`、`go vet ./...` 和 CGO-disabled build；
Go 的 `./...` 不跨越嵌套模块。没有 `go.work`，没有根模块 Wails 依赖。
前端控制器单测只用 Node 内置 test runner，不需启动浏览器或 Wails。
生产资产不提交；`bindings` 专用编译文件使全新 checkout 能先生成绑定，
正常宿主 tests/build 则必须先构建前端，避免把占位页面打进程序。

受控 I/O 的 Windows 原生取消与阻塞退出验收使用
[可复现工具](../tools/desktop-native/README.md)。工具生成确定性结构样本，
用 Go overlay 构建独立验收程序，并为每次运行保留门控与资源关闭收据。
普通生产构建不包含门控；此工具不依赖私有媒体，也不代替自然慢盘测试。

前期自动检查和浏览器模拟检查见
[验证记录](../docs/desktop-smoke-20260906.md)。打开恢复协议和真实 Windows
窗口验收结果见 [2026-09-10 验收记录](../docs/desktop-native-20260910.md)。
取消/关闭的通信恢复、组合清理错误修复及最新构建结果见
[2026-09-09 回归记录](../docs/desktop-recovery-20260909.md)。
其余 IPC 等待上限和验收工具复现结果见
[2026-09-10 超时与工具记录](../docs/desktop-timeouts-tools-20260910.md)。
AVC 详情、1 GiB 文件尾部读取与阻塞关闭的本次实测见
[AVC 原生验收报告](../docs/desktop-avc-20260910.md)。
长媒体 AVC 配置候选定位、尾部首次详情和配置来源 Hex 的验证见
[2026-10-01 长媒体 AVC 验收记录](../docs/desktop-avc-index-20261001.md)。
扫描成本、真实长录制及连续资源验证见
[2026-10-01 稳定性验证记录](../docs/desktop-stability-20261001.md)：5 轮原生功能与真实长录像验收已完成，资源采样空档与句柄信号的限制见记录。
后续 [2026-10-02 长时间句柄采样](../docs/desktop-handles-20261002.md) 已完成 90 分钟记录、5 轮功能与独立原生退出复核；GDI 保留、两处采样空档及未知类型使资源验证仍未全项通过。
接续 [GDI 隔离记录](../docs/desktop-gdi-isolation-20261002.md)：小文件五轮和同长目录取消三轮稳定；跳过媒体打开的诊断版仍连续三轮 +2。生产段 18 窗与诊断段经基线修正后的 6 窗、两次原生退出均已复核；具体 GDI 分配点仍待定位。
最新 [GDI 调用边界验证](../docs/desktop-gdi-boundaries-20261003.md)：原版新进程与 A/B 三组均复现每轮 +2，18 个固定窗有效且全部正常退出；GetResult 和路径读取不是此次复现的必要条件。A 组全程资源连续性有一处窗口外超限，Win32k 跟踪被权限拒绝，具体对象和根因仍待定位。
后续 [Win32k ETW 采集与对象校准](../docs/desktop-gdi-etw-20261003.md) 已完成真实成功选择、180 秒无丢失跟踪及六类对象校准；GDI 260→262 后约 160 秒仍为 262。优先追查一个 DC 和一个 Bitmap 候选，尚未确认分配调用栈或修复；根进程及六个 WebView2 子进程均正常退出。
MP3 桌面接入、自动检查及原生验收的当前状态见
[MP3 验证记录](../docs/desktop-mp3-20260911.md)。
有界 Hex、四类详情源位置联动、Tag ID 跳转、源变化恢复及阻塞关闭见
[2026-09-13 Hex 原生验收记录](../docs/desktop-hex-20260913.md)。

ADR-013、保留源文件的详情读取和 AMF0/AAC 完整原生流程见
[2026-09-10 详情验收记录](../docs/desktop-details-20260910.md)。

配置保留 `wails dev` 开发入口（热更新循环尚未验收；不作为生产验收路径）。Vite 只
监听回环地址。生产版本不启动 Vite，不加载远程内容，不放开绑定来源，
使用 CSP 并禁用默认文件拖入；不加入任意 URL 打开或文件写入接口。

关闭文件是立即的逻辑关闭；若底层 I/O 未结束，仍占用槽位。关闭窗口会
先取消扫描，最多等待 3 秒清理，超时后由进程退出和 OS 回收资源。这不
改变核心无法强制中断任意阻塞 ReaderAt 的事实。WebView2 自身可能维护
OS 管理的浏览器 profile；应用不把它用作扫描数据库。

## 依赖与发布边界

Vite 7.3.6 替换了本机旧缓存版本，参考维护者的
[Windows 路径修复公告](https://github.com/vitejs/vite/security/advisories/GHSA-fx2h-pf6j-xcff)。
2026-09-06 的 `npm audit` 返回 0 项已知问题，不代表完整供应链安全审计。
Wails API/构建选项核对使用本机 v2.14.0 源码以及
[官方 CLI 文档](https://wails.io/docs/reference/cli/)；官网可能展示更高版本，
不能据此自动升级本项目。公开发布前仍须审核 Go/npm/原生组件许可证与
安全公告、确定项目许可证、完成签名和安装流程验证。
