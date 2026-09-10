# AMF0/AAC 桌面详情与 Windows 原生验收（2026-09-10）

本批以 `a54a9c6` 为基线，为桌面增加按 Tag 选择的 AMF0/AAC 详情，并将文件
所有权延长到文档关闭。生产窗口的详情、部分索引、源变化检查，以及受控扫描
取消、详情阻塞关闭和阻塞退出均已完成验收。所有本轮 MuxRay 窗口已实际退出。

本记录接续 [此前原生验收](desktop-native-20260910.md)。资源生命周期由
[ADR-013](decisions/013-retained-document-details.md) 描述；既有 accepted ADR 未改写。

## 本批行为

- `document` 持有同一个只读文件句柄，扫描借用该源。文档已接纳后，扫描完成或
  取消仍保留完整或部分索引，已提交 Tag 可请求详情；关闭文档才释放源。若取消
  发生在 opening 阶段，已经创建但尚未发布的文档会随打开上下文关闭，不能留下
  孤立句柄。关闭立即停止新请求，
  底层扫描或详情读取尚未返回时保持非终态，避免新文件占用仍未释放的槽。
- 每个文档只运行一个详情解析器，并保留一个可替换的最新待处理选择和一个有界
  结果。同请求 ID/Tag 的 Start 幂等，旧 ID 被拒绝。前端换 Tag、翻页、关闭或换
  文件后，旧结果不能覆盖当前选择；64 位 ID 和偏移通过十进制字符串传输。
- 详情解析的合作式时限为 3 秒；已经阻塞的 `ReadAt` 必须先返回，才可结束实际
  工作或关闭源。前端每个 IPC 等待有 5 秒期限，Start 响应不确定后查询原 ID。
  连续 25 个 pending 查询后暂停自动轮询，保留显式重试；确认重试复用原 ID，
  已明确失败后的重新读取才产生新 ID。
- AMF0 展示类型、成员、值和原始跨度，保留特殊浮点位模式、空字符串和部分解析
  诊断。AAC 展示 FLV 音频头及 ASC 位字段；raw access unit 只描述封装和范围，
  不解码音频。AVC、其他音频、过滤 payload 和容器 payload 截断返回明确错误。
- AMF 解析预算为 4096 节点、32 层及累计 64 KiB 字符串；展示至多 512 字段、
  64 诊断，文本预览至多 256 UTF-8 bytes。按标准 JSON 实际编码字节累计预算，
  详情对象不超过 256 KiB。`complete` 描述语法解析，`truncated` 同时表示语法
  截断或有界展示省略，因此两者可同时为 true。
- 详情读取前后检查同句柄的文件身份、大小、修改时间和模式；检测到变化后持续
  拒绝详情，要求关闭重开。这是可观察变化检查，不是不可变的内容快照。

## 自动检查

结果见证据目录中的 `integration-checks.json` 及对应日志。

| 检查 | 结果与证据 |
| --- | --- |
| 前端 | `npm.cmd test`：61 项全部通过；类型检查和构建通过。覆盖详情准入、精确整数、Start/Get 永不返回、同 ID 重试、快速选择、迟到响应、关闭/销毁清理、部分错误和空 UTF-8 值显示。`frontend-test.log`。 |
| 根 Go 模块 | `CGO_ENABLED=0 go test -count=1 ./...`、`go vet ./...`、`go build ./...` 全部 exit 0。`root-go-checks.log`、`root-go-exit-codes.json`。 |
| 根 Go race | `CGO_ENABLED=1 go test -race -count=1 ./...` exit 0，包含 document、detailview、desktop 和现有解析器/CLI。`root-race.log`。 |
| 独立 desktop Go 模块 | `go test -race -count=1 ./...`、`go vet ./...` 全部 exit 0。`desktop-go-checks.log`。 |
| 投影专项 | 12 个顶层测试及子测试通过，另通过 race/vet；覆盖跨度、损坏数据、NaN/±Inf/-0、UTF-8、解析/展示/JSON 预算、长字符串长度乘法溢出、AAC 扩展位字段、超大偏移和上下文取消。raw 测试确认不读取 opaque access unit。 |
| 受控 Scan 非 GUI | `verify.ps1` 默认模式通过，验证取消后 10 Tag 前缀和源保留、前缀详情、关闭后释放。`verify-scan-mode-001/`。 |
| 受控 Detail 非 GUI | `verify.ps1 -GateMode Detail` 通过；扫描后详情阻塞，关闭立即禁准入但不释放源，Release 后 Close 返回 nil 并可独占打开。Windows PowerShell 5.1 含空格路径的复测通过，证据在 `verify-detail-mode-ps51-001/`。 |
| 工具兼容性 | `self-test.ps1` 在 PowerShell 7.6.5 和 Windows PowerShell 5.1.26100.9444 通过，核对 UTC/100 ns 时间、两种 gate 参数与场景错配。旧 v1 Scan 收据可读取；已存在的 run 被拒绝且原构建记录哈希不变。 |

首次通过 `npm.ps1` 包装器启动测试时，包装器报 `Unknown command pm`，测试尚未
执行；随后使用 `npm.cmd test` 完整通过。该失败记录保存在退出码清单中，没有
将未启动的测试计入通过数。

## 构建与样本身份

本机环境为 Windows/amd64、Go 1.27、Wails 2.14.0、Node 24.4.1 和现有
WebView2 Runtime，依赖通过本机缓存离线使用。生产构建运行真实 Wails 生成绑定、
前端编译及应用编译，没有挂载受控 overlay。

| 生产构建 | 字节数 | SHA-256 |
| --- | --- | --- |
| 首轮功能验收，CSS 换行修复前 | 10,751,488 | `0d6811b63dca1573fbc1a11724116045e0e76ae7067cf9244917129c4b0b4df5` |
| 最终生产程序 | 10,752,000 | `3b0b867173721d4576a06fdbe0d327a7452badd39d15f63eb33223e7ec85d0ac` |

最终文件为 `desktop/build/bin/MuxRay.exe`，构建完成时间
`2026-09-10T01:26:02.2056902Z`；`production-final-build.json` 与
`production-final-build.log` 保留身份和成功构建记录。

最终输入一致性检查见 `final-source-identity.json`：以 DetailClose 的构建记录为
依据，重新计算当前仓库全部 106 个 Go 输入及 22 个前端输入的 SHA-256，全部匹配，
差异为 0。

样本由 `tools/desktop-native/detail_fixtures.py` 在新的本地目录生成，没有私有
媒体或下载。`fixtures.json` 保存完整 SHA-256 清单。

| 样本 | 大小 | 用途 |
| --- | --- | --- |
| `details.flv` | 2095 bytes | 8 Tag：AMF0 对象、AAC-LC ASC、raw AAC、损坏 AMF0、短 ASC、不支持 AVC、UTF-8 预览、600 元素展示预算。 |
| `details-truncated.flv` | 2108 bytes | 同一完整前缀，额外 ID 8 声明 10 bytes、仅保留 2 bytes payload。 |
| `details-change.flv` | 2095 bytes | 独立的同内容副本，专供修改时间变化实验。 |

## 生产窗口观察

使用真实原生文件选择器、Wails IPC 和生产 CSP，未注入 `window.go` 或模拟桥接
结果。首轮功能检查与最终程序复验分开记录。

| 范围 | 实际观察 | 主要证据 |
| --- | --- | --- |
| 首轮 AMF0 | ID 0 展示对象成员、值、类型、父子层次和原文件跨度。 | `amf0-overview.jpg`、`amf0-members.jpg`、`amf0-ui.txt` |
| 首轮 AAC | ID 1 识别 AAC-LC、44100 Hz、stereo；AOT 143:0/5 bit，采样索引 143:5/4 bit，声道 144:1/4 bit。ID 2 明确 raw 不解码。 | `aac-config-ui.txt`、`aac-raw.jpg`、`aac-raw-ui.txt` |
| 首轮损坏/不支持 | ID 3 保留 AMF 字段并显示 `AMF0_NUMBER_TRUNCATED`（192:0，期望 8/实际 1）；ID 4 显示 `AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED`（210:0，期望 4/实际 3）；ID 5 显示暂不支持。 | `amf0-damaged.jpg`、`aac-damaged.jpg`、`unsupported.jpg` 及相应 UI 文本 |
| 首轮展示边界 | ID 6 的 1200-byte UTF-8 字符串显示有界预览和省略标记，但原始位长仍为 9600；ID 7 的 600 元素结果标注展示省略。 | `amf0-preview.jpg`、`amf0-preview-ui.txt`、`amf0-budget.jpg`、`amf0-budget-ui.txt` |
| 最终程序：部分索引 | 截断样本显示 9 Tag、1 结构诊断和部分索引。选择 ID 8 拒绝不完整 payload；前缀 ID 0 的 AMF0 和 ID 1 的 AAC 仍能读取。 | `final-truncated-rejected.jpg`、`final-truncated-rejected-ui.txt`、`final-partial-amf0.jpg`、`final-partial-amf0-ui.txt`、`final-partial-aac-ui.txt` |
| 最终程序：源变化 | 在独立副本上仅将 mtime 增加 2 秒，文件大小与 SHA-256 不变。重新选择 Tag 返回源变化提示；继续选择其他 Tag 仍拒绝，旧详情不残留。 | `source-change-mutation.json`、`source-changed.jpg`、`source-changed-ui.txt`、`source-changed-latched-ui.txt` |
| 最终程序：关闭 | 关闭文件清空详情、恢复空态；截断样本与修改时间实验副本均可用 `FileShare.None` 独占打开。 | `final-partial-close.json`、`production-final-close.json`、`production-final-closed.jpg` |

首轮发现详情表“类型”列的长名称越过列边界，与相邻值列重叠，见
`aac-before-layout-fix.jpg`。已为类型及偏移/位长列补充换行规则，重新构建最终
生产程序并实际复验；`final-aac-config.jpg` 显示 `audioObjectType`、
`samplingFrequency` 等名称在列内换行，值和原始跨度可分别阅读。修复前截图保留
为问题证据。`budget-selection-missed.jpg` 是未选中目标 Tag 的中间记录，不作为
预算场景通过证据；预算场景的选择和省略提示依据之后的 `amf0-budget.*`。该 UI
文本提取本身受工具输出长度限制，未逐行完整保留；512 字段的硬上限依据自动测试，
没有将肉眼计数或截断的 UI 文本当作上限证明。

## 受控详情读取阻塞后关闭

使用 `build.ps1 -GateMode Detail` 的独立验收程序，非生产 exe。run 为
`detail-close-001`，ID `2a171d341b754afe9f8a7d61483a6869`，二进制 SHA-256
`07104c97c468b540e09a541adaf86f7f75160b3a1182dd8580a4e1a19671eed9`。
497-byte 合成样本首 Tag 为 `AF 00 12 10` AAC 配置，其余 31 个为空 Script Tag。

1. 真实窗口打开样本，结构扫描完整结束为 32 Tag、0 结构诊断。点击 ID 0 后，第
   67 次 `ReadAt` 在 offset 24 进入 gate；收据与 run 参数一致。
2. gate 保持阻塞时，前端在连续 25 次 pending 确认后暂停自动查询，显示超时与
   “重试详情查询”。没有以 UI 暂停冒充底层读取结束。
3. 记录 MarkClose，然后通过真实“关闭文件”按钮关闭。界面显示“正在关闭”和
   32 Tag；详情清空，“打开 FLV 文件”禁用。释放前独立检查确认源仍占用，且没有
   `closed.json`。
4. 发布 Release 后，原始读取返回、源关闭，窗口恢复空态。
   `check.ps1 -Scenario DetailClose -ObservedState closing -ObservedTagCount 32`
   通过，确认 orderly source Close 和独占打开。

收据时间均为 UTC：close-command `01:39:32.9245358Z`，release
`01:40:03.3691859Z`，released `01:40:03.4110017Z`，closed
`01:40:03.4115619Z`。这些时间证明操作意图、释放、读取返回和关闭的先后，MarkClose
本身不证明按钮动作；实际动作和状态由 UI 截图/文本补充。

截图和检查为 `native-detail-index.jpg`、`native-detail-pending.jpg`、
`native-detail-pending-ui.txt`、`native-detail-closing.jpg`、
`native-detail-closing-ui.txt`、`native-detail-before-release.json`、
`native-detail-closed.jpg`。run 的 7 份构建/操作/关闭/验收 JSON 已复制并逐一核对
SHA-256，归档在 `native-detail-close-001/`。

## 受控扫描取消与前缀详情

Scan run `cancel-001` 的 ID 为 `490605e0a93a4f1cae7ce9054afca079`，二进制
SHA-256 为 `3fe5e2d779d8f11466f88548c64f225100204601232093f889dbe322a92db132`。
493-byte/32 Tag 合成样本在 read 23、offset 163 处阻塞，已有 10 个完整 Tag。

MarkCancel 后实际点击“取消扫描”，窗口显示正在取消。Release 后显示已取消、
10 Tag、163 bytes、0 结构诊断和 ID 0–9 的部分索引。此时没有 `closed.json`；
选择 Tag 0 后仍可读取 AMF0 部分详情，准确显示空 payload 的
`AMF0_SCRIPT_NAME_MISSING`、`AMF0_SCRIPT_VALUE_MISSING`，均位于 24:0。
这同时验证取消扫描没有取消文档的后续详情能力。

实际点击“关闭文件”后窗口恢复空态，源关闭收据出现，
`check.ps1 -Scenario Cancel -ObservedState canceled -ObservedTagCount 10`
通过，确认 source Close 和独占打开。证据为 `native-cancel-running.jpg`、
`native-canceling.jpg`、`native-canceled.jpg`、`native-canceled-detail.jpg`、
相应 `*-ui.txt`、`native-canceled-source-retained.json` 和
`native-cancel-closed.jpg`；7 份 run 收据及构建记录已核对 SHA-256 后归档到
`native-cancel-001/`。

## 受控阻塞退出

Scan run `exit-001` 的 ID 为 `4e9e8f20827b4bfb86ff4b16b1caf753`，二进制
SHA-256 为 `7af52695f18fd056d1e5b95e9d035051ddc193c4b2dc14eaaf238ff6c0db2788`。
真实窗口打开 493-byte 样本并进入扫描 gate 后，先启动只读进程 watcher，再记录
MarkExit，通过实际 Alt+F4 关闭窗口，全程未 Release。

PID 5828 的 OS 退出码为 0，MarkExit 到 OS 退出为 **3299.207 ms**，通过
`check.ps1 -Scenario Exit` 的 6000 ms 观测上界。紧贴实际按键前另存的 UTC 时间
为 `01:45:51.758Z`，OS 退出时间为 `01:45:54.9648119Z`，区间
**3206.8119 ms**。两者都包含 computer-use 输入分发耗时，不能称为纯宿主耗时。

`release.json`、`released.json`、`closed.json` 均不存在，原文件可独占打开；
这是进程退出后由 OS 回收句柄，**没有 orderly source Close**。证据为
`native-exit-blocked.jpg`、`native-exit-blocked-ui.txt`、`native-exit-input.json`、
`native-exit-duration.json`；6 份构建/进入 gate/退出/验收记录已核对 SHA-256 后
归档到 `native-exit-001/`。

## 证据位置与验证边界

本机证据目录：
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-details`。
文中的证据文件名均相对此目录。原始受控 runs 位于同一工作区的
`work\details-native`；每次使用新目录，未覆盖旧 run，没有将 fixture/验收 exe
提交仓库。`native-build-results.json` 和 `native-*-build.log` 记录三个独立构建。
`evidence-manifest.json` 列出归档时各证据文件的相对路径、字节数和 SHA-256；
不包含清单本身，后续新增文件不自动纳入该快照。

永不返回的 IPC 回调、迟到 Start/Get、同 ID 幂等和跨会话身份由自动回归验证；
本轮没有在原生系统中人为丢弃 IPC 包。受控 gate 验证真实窗口到 IPC、文档、
底层读取和关闭的路径，但不等价于生产程序在自然慢盘上的测量。raw AAC 解码和
本批未支持的编解码器详情不在通过范围内。

本轮未运行 browser-smoke，界面结论来自 Node 自动测试与上述真实 Windows
交互。`tools/validate_contracts.py` 因本机 Python 缺少 `jsonschema` 未执行，
不计作通过；CLI schemas/goldens 本轮没有修改。此前批次的验证结果没有移作
本轮这些未执行检查的通过证据。
