# Windows 受控原生验收

这组工具从新 checkout 生成独立的验收程序和结构样本，用真实 Windows 文件选择器、Wails IPC 和文档生命周期复现取消及阻塞退出。正常产品代码不含 gate；工具只通过 Go `-overlay` 替换本次构建读到的 `document.go`，不会修改仓库 Go 源文件。ADR-013 之后扫描终态仍保留源，只有关闭文档才释放句柄。

`build.ps1` 和 `verify.ps1` 的 `-GateMode` 默认为 `Scan`，也可显式选择 `Detail` 或 `AVCDetail`。三种样本均为确定性生成的 FLV，不是可播放媒体。gate 只匹配本 run 的绝对样本路径，故意忽略取消，直到同一进程身份的 release 收据发布；这是受控 I/O 实验，不能称为正常生产程序的自然慢盘实测。

| GateMode | 结构样本 | gate 位置 | 允许的原生检查 |
| --- | --- | --- | --- |
| Scan（默认） | 493 byte，32 个空 Script Tag | 第 23 次 ReadAt，offset 163，已提交 10 Tag | Cancel、Exit |
| Detail | 497 byte，首 Tag 为 `AF 00 12 10` AAC-LC 配置，其余 31 个空 Script Tag，FLV audio 标志置位 | 扫描完成 32 Tag 后，首次详情读取第 67 次 ReadAt，offset 24 | DetailClose |
| AVCDetail | 504 byte，首 Tag 为 `17 01 00 00 00 00 00 00 02 65 88` 孤立 AVC NALU 包，其余 31 个空 Script Tag，FLV video 标志置位 | 扫描完成 32 Tag 后，首次详情探测第 67 次 ReadAt，offset 24 | DetailClose |

Scan 样本 SHA-256 为 `9758dc3635a5e05a780ffe4cd40659b243d2d23a4a9e704762add90fcd4c34fb`，Detail 样本为 `16346b21c3e84be9a1d398f330a0023b5ba3b5df5a5c74d8756b5cda8f1e4437`，AVCDetail 样本为 `8b9d282048f76e6cc7fa6821a83ce8c02726a2dd91592b8da493b421ba4cdda9`。实际 fixture SHA-256、模式、读取次数、gate 偏移、文件长度和已提交 Tag 数均写入 `build-record.json`；收据和检查必须匹配这些参数。旧 v1 Scan 记录仍可读取，不修改历史 JSON。

AVCDetail 首 Tag 没有前置 sequence header。它用于验证 AVC 首次读取阻塞时的文档关闭协议，不用于证明 NAL 长度或视频解码正确；缺配置时产品保留包头并明确报告长度上下文不可用。AVC 上下文与位置语义见 [ADR-014](../../docs/decisions/014-bounded-desktop-avc-context.md)。

## 前提

- Windows amd64、PowerShell、Go（符合两个 `go.mod`）、CGO C 编译器、Node（符合 `desktop/frontend/package.json`）及 npm 已在 PATH；运行桌面程序还需要 WebView2 Runtime。
- Go 模块和锁定 npm 包已在本机缓存。工具强制 `GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`，执行 `npm ci --offline --ignore-scripts --no-audit --no-fund`。冷缓存会失败；首次准备依赖需在允许联网的开发环境分别为根模块、desktop 模块执行 `go mod download`，为前端执行 `npm ci --no-audit --no-fund`。工具不下载工具链或私有媒体。
- 仓库自带已生成的 Wails bindings。本工具无需运行 Wails CLI；它每次重新安装锁定的前端依赖并执行 `npm run build`，不会默默嵌入旧 `dist`。
- `Repository` 和 `RunDirectory` 均须绝对路径。每个 run 必须使用不存在的**仓库外**新目录，防止生成的 overlay Go 文件进入正常构建；取消、退出、非 GUI 验证各用一个 run。使用本地 NTFS 或支持硬链接原子发布的文件系统，不支持 junction/symlink，也不覆盖、重置或删除历史证据。
- 同一 run 只启动一次验收 exe、只打开一次该 run 的样本。再次打开或启动会因已有收据失败；请构建新 run。构建时不要同时修改源代码。

以下示例可直接在新 checkout 使用，替换路径即可（空格路径受支持）：

```powershell
$repo = 'C:\work\MuxRay fresh'
$verify = 'C:\work\native acceptance\verify-001'
$cancel = 'C:\work\native acceptance\cancel-001'
$exitRun = 'C:\work\native acceptance\exit-001'
$scripts = Join-Path $repo 'tools\desktop-native'

& "$scripts\self-test.ps1"
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $verify
& "$scripts\build.ps1" -Repository $repo -RunDirectory $cancel
& "$scripts\build.ps1" -Repository $repo -RunDirectory $exitRun
```

`verify.ps1` 不启动 GUI：在独立测试 overlay 中运行带 race 检查的真实文件文档测试，核对完整样本 32 Tag、gate、`running → canceling → canceled`、保留 ID 0–9、取消后仍可请求前缀 AMF 详情（空 payload 会有语义诊断）、文档 Close 才关闭源、Close 后拒绝分页，以及文件可独占打开。它不能代替以下原生交互验收。

`self-test.ps1` 在内存中验证 JSON 收据时间，覆盖 Windows PowerShell 5.1 的字符串和 PowerShell 7 的 `DateTime` 反序列化，保留 UTC 身份及 .NET 的 100 ns 精度，防止时区偏移导致误判 PID 复用或丢失退出耗时的小数部分。同时验证三种 gate 的固定参数及场景错配拒绝。该测试不创建 run 或修改旧收据。

## 原生取消

1. 手动或经已授权的 computer-use 工具启动 `$cancel\MuxRay-io-acceptance.exe`；点击“打开 FLV”，通过原生文件选择器选择 `$cancel\native-controlled.flv`。
2. 等待 `entered.json`，确认界面仍为扫描中。可用下面的 Status 查看 gate 收据。
3. 运行 MarkCancel 后，立即在真实窗口点击“取消扫描”。等待并截图确认“正在取消”，此时不要释放 gate。

```powershell
& "$scripts\control.ps1" -RunDirectory $cancel -Action Status
& "$scripts\control.ps1" -RunDirectory $cancel -Action MarkCancel
# 现在点击真实窗口的“取消扫描”，观察“正在取消”。
& "$scripts\control.ps1" -RunDirectory $cancel -Action Release
```

4. 等待并截图确认 `已取消`、10 Tag 的部分索引、无结构诊断；取消标记会使结果不完整。核对页中 ID 0–9，选择一个 Tag 确认详情可读取（这些空 AMF payload 会显示缺少事件名和值）。此时应没有 `closed.json`。
5. 在真实窗口点击“关闭文件”，等待空状态和 `closed.json`，之后运行：

```powershell
& "$scripts\check.ps1" -RunDirectory $cancel -Scenario Cancel -ObservedState canceled -ObservedTagCount 10
```

`check.ps1` 校验 run、二进制、fixture、overlay 身份，收据同进程且顺序正确、source Close 成功，以及 fixture 可独占打开。`ObservedState/ObservedTagCount` 是关闭前操作者实际读到的 UI 结果，**不是脚本自动测到的状态**；请保留对应截图。MarkCancel 仅记录操作意图，不证明按钮确实被点击。最后通过真实窗口关闭程序。

## 详情读取阻塞时关闭文件

此场景使用单独的 `Detail`（AAC）或 `AVCDetail` run，先完成结构扫描，再阻塞选中 Tag 的详情读取。

```powershell
$detailVerify = 'C:\work\native acceptance\detail-verify-001'
$detailRun = 'C:\work\native acceptance\detail-close-001'
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $detailVerify -GateMode Detail
& "$scripts\build.ps1" -Repository $repo -RunDirectory $detailRun -GateMode Detail
```

验证 AVC 时另选两个新目录，后续步骤中的 `$detailRun` 使用新的 AVC run：

```powershell
$detailVerify = 'C:\work\native acceptance\avc-detail-verify-001'
$detailRun = 'C:\work\native acceptance\avc-detail-close-001'
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $detailVerify -GateMode AVCDetail
& "$scripts\build.ps1" -Repository $repo -RunDirectory $detailRun -GateMode AVCDetail
```

两个模式的非 GUI race 测试都先确认 32 Tag 扫描完整、源仍保留，再请求 ID 0 的 AAC 或 AVC 详情到 gate。关闭请求必须立即使文档 `closed=true`、`terminal=false`，拒绝新详情和分页，且不能产生 `closed.json`；释放 gate 后 Close 应返回 nil、源才关闭并可独占打开。

原生步骤：

1. 启动该 run 的验收 exe，通过真实文件选择器打开该 run 的 `native-controlled.flv`。等待扫描结束，确认 32 Tag；此时应没有 `entered.json`。
2. 点击 Tag ID **0** 请求 AAC 或 AVC 详情，等待 `entered.json` 和详情读取中界面。该收据必须匹配所选 GateMode，且为 read 67/offset 24/32 Tag。
3. 运行 MarkClose，然后立即点击真实窗口“关闭文件”。截图确认“正在关闭”，32 Tag 的扫描信息仍在，无法打开新文件，详情被清空；此时不要 Release，且应没有 `closed.json`。

```powershell
& "$scripts\control.ps1" -RunDirectory $detailRun -Action Status
& "$scripts\control.ps1" -RunDirectory $detailRun -Action MarkClose
# 现在点击真实窗口“关闭文件”，观察并截图“正在关闭”。
& "$scripts\control.ps1" -RunDirectory $detailRun -Action Release
```

4. 等待源关闭及窗口恢复空状态，再运行检查，最后通过真实窗口正常退出程序。

```powershell
& "$scripts\check.ps1" -RunDirectory $detailRun -Scenario DetailClose -ObservedState closing -ObservedTagCount 32
```

检查要求 `close-command < release < released < closed`，所有收据属于同一 run 和进程、source Close 无错误、fixture 可独占打开。`closing/32` 是 **Release 前**操作者观察的状态；MarkClose 仅证明操作意图记录，不能代替按钮操作、关闭中截图或释放后的空状态。DetailClose 不能用于 Scan run；Cancel、Exit、MarkCancel、MarkExit 和退出 watcher 也不能用于 Detail 或 AVCDetail run。

## AMF0/AAC 生产窗口流程

```powershell
python -B tools/desktop-native/detail_fixtures.py --output-dir 'C:\work\details-fixtures-new'
```

生成器只接受绝对的新目录，写入三个合成文件及 SHA-256 清单，无外部下载或可播放
媒体。用普通生产 `MuxRay.exe` 的原生选择器打开 `details.flv`，依次选择 ID 0–7：
AMF0 对象、AAC-LC 44100 Hz 双声道配置、opaque raw、损坏 AMF0、短 ASC、缺少前置
配置的 AVC NALU 包、UTF-8 预览截断、600 元素超过字段展示预算。ID 5 现应显示 AVC
包头及 `AVC_NAL_LENGTH_SIZE_UNAVAILABLE`，不再显示“不支持”；样本字节保持不变。
核对字段类型、值、字节/位位置、
错误提示和选择切换。`details-truncated.flv` 另有一个 payload 不完整的 ID 8，
它应拒绝详情，前面的完整 Tag 仍可读。`details-change.flv` 是独立的同内容副本，
可在打开后受控改变其大小或修改时间，重新选择 Tag 验证 `source_changed`。
关闭文件应清空详情并释放句柄。这些截图属于普通生产交互，不包含 gate 或模拟 IPC。

## AVC 生产窗口流程

```powershell
python -B tools/desktop-native/avc_fixtures.py --output-dir 'C:\work\avc-fixtures-new'
```

生成器仅使用 Python 标准库，拒绝覆盖输出目录，不下载媒体。它写入四个合成 FLV 和
`fixtures.json`；清单保存每个文件的长度、SHA-256 以及 Tag 用途和位置。默认文件为：

| 文件 | 长度 | 选择与预期 |
| --- | ---: | --- |
| `avc-details.flv` | 1933 byte | 13 Tag，覆盖下述正常、缺配置与损坏场景 |
| `avc-change.flv` | 1933 byte | 上述文件的独立副本，用于打开后受控改变大小/mtime，验证 `source_changed` |
| `avc-truncated.flv` | 1946 byte | 追加 ID 13，其 payload 声明 10 byte、仅剩 2 byte；拒绝该 Tag 的详情，前缀仍可查看 |
| `avc-budget.flv` | 732 byte | 33 个不可用 avcC 候选与尾部 ID 33；选择尾部应为 `budget_exhausted` 和 `AVC_CONTEXT_LOOKUP_LIMIT_REACHED`，不能当作 `missing` |

使用普通生产 `MuxRay.exe` 打开 `avc-details.flv`，逐项记录：

| Tag ID | 预期 |
| --- | --- |
| 0 | 无前置配置；CTS −12，DTS 5，PTS −7；只保留包头和 opaque NAL 数据范围 |
| 1 / 2 | baseline avcC 与 1-byte 长度 NALU；Tag 2 的来源为 Tag 1，PTS 28 |
| 3 / 4 | high profile avcC 与 2-byte 长度 NALU；Tag 4 的来源为 Tag 3，PTS −20 |
| 5 / 6 | Tag 5 avcC 截断不可用；Tag 6 越过它使用 Tag 3 的 2-byte 声明 |
| 7 / 8 | 切换到 4-byte 长度；Tag 8 的来源为 Tag 7，PTS 132 |
| 9 | NAL 声明 20 byte 但仅剩 2 byte，保留部分结果与截断诊断 |
| 10 / 11 | end-of-sequence 标记；非 AVC 编码的视频仍明确不支持 |
| 12 | 300 个 NAL，触发解析/字段展示预算，保留有限前缀及省略提示 |

来源字段 `sourceTagId`、`sourceLengthSizeMinusOne` 原始 2-bit 位置和搜索计数应可见。
NAL 长度/头、avcC 声明与参数集整体范围使用原文件位置；DTS/PTS、NALLengthSize、
SPS/PPS 摘要等派生字段的单一位置为“—”，不能用整个参数集范围替代。
baseline/high 参数集含防竞争字节，用来验证去除这些字节后不会伪造连续源位置。
NAL 类型、FLV keyframe 声明不代表已解码 slice、验证 IDR 或视频画面可播放。

可选的大文件同样使用新目录，以下命令实际顺序写入 **64 MiB 填充 payload**，文件总长
另含 FLV framing 与首尾 AVC Tag，不是稀疏文件。`--large-mib` 默认 0（不生成），允许
64–4096；不要把填充大小误报为最终文件长度。

```powershell
python -B tools/desktop-native/avc_fixtures.py --output-dir 'C:\work\avc-large-new' --large-mib 64
```

打开额外生成的 `avc-large.flv`，按清单 `tailTag.id` 选择尾部 Tag，核对配置来源为 ID 0、
长度宽度 4、DTS 1000、PTS 988，以及精确尾部偏移。生成器记录写入时累计的长度和
SHA-256；验收前另用 `Get-Item`、`Get-FileHash -Algorithm SHA256` 读回核对，报告记录
实际 `bytes`、哈希与选择结果。填充是空字节容器压力数据，不能
换算成视频解码能力或完整 payload 读取吞吐量。

## 阻塞中退出

1. 启动 `$exitRun\MuxRay-io-acceptance.exe`，选择该 run 的 fixture，等待 gate 与扫描中界面。
2. 在一个独立的 PowerShell 进程执行 watcher（后台启动脚本时使用隐藏窗口）。watcher 先校验 exe 路径和进程开始时间、持有句柄，然后写 `exit-watch-armed.json`；它只观察，不发送关闭或终止进程。

```powershell
& "$scripts\watch-exit.ps1" -RunDirectory $exitRun -TimeoutSeconds 60
```

3. 确认 `exit-watch-armed.json` 已出现，在另一个 PowerShell 执行 MarkExit，随即通过真实窗口的关闭按钮或 Alt+F4 退出。**不要 Release。**

```powershell
& "$scripts\control.ps1" -RunDirectory $exitRun -Action MarkExit
# 随即操作真实窗口退出，等待 watcher 返回。
& "$scripts\check.ps1" -RunDirectory $exitRun -Scenario Exit
```

`check.ps1` 要求退出码 0、没有 release/released/closed 收据、原文件可独占打开，默认记录操作意图到 OS 退出不超过 6000 ms。产品阻塞退出预算是 3 秒；这里的 6 秒检查上界还包含操作者或 computer-use 输入分发时间，不能解读为精确宿主耗时。可以用 `-MaxExitMilliseconds` 显式选择 1000–15000 ms 的观测上界，必须在报告中记录该值，不能为掩盖失败而改变历史记录。若需要准确描述按键到退出的区间，应另外保存紧接实际按键前的 UTC 时间并与 `exit-observed.json` 比较。

gate 仍阻塞时没有 orderly source Close；进程退出后由 OS 回收句柄。它与释放 gate 后正常 Close 是两个不同验收结论。超时的 watcher 会留下失败收据并保留进程供调查，不自行 kill。

## 证据和失败

`build-record.json` 保存 run ID、源文件/前端输入和产物 SHA-256、工具模板身份、版本与 overlay 参数。控制及检查入口重新核对产物；收据绑定 run ID、PID 与每次进程生成的 token。JSON 先在同目录写完再原子发布，拒绝覆盖已有文件，读者不会看到半条收据。

保留整个 run 和截图，尤其是 `entered.json`、操作意图、`released.json`/`closed.json` 或 `exit-observed.json`，以及最终 `acceptance.json`。`verification.json` 只表示非 GUI 测试通过。构建或校验失败会保留已有产物；没有成功记录的 run 不算通过，请修复原因后使用新目录。工具不会加入交互 CI，也不修改私有样本、正常生产 exe 或既有 ADR。
