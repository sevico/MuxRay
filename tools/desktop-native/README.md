# Windows 受控原生验收

这组工具从新 checkout 生成独立的验收程序和结构样本，用真实 Windows 文件选择器、Wails IPC 和文档生命周期复现取消及阻塞退出。正常产品代码不含 gate；工具只通过 Go `-overlay` 替换本次构建读到的 `document.go`，不会修改仓库 Go 源文件。ADR-013 之后扫描终态仍保留源，只有关闭文档才释放句柄。

`build.ps1` 和 `verify.ps1` 的 `-GateMode` 默认为 `Scan`，也可显式选择 `Detail`、`AVCDetail`、`MP3Detail`、`Hex` 或 `Summary`。六种模式使用确定性生成的 FLV，不是可播放媒体。gate 只匹配本 run 的绝对样本路径，故意忽略取消，直到同一进程身份的 release 收据发布；这是受控 I/O 实验，不能称为正常生产程序的自然慢盘实测。

| GateMode | 结构样本 | gate 位置 | 允许的原生检查 |
| --- | --- | --- | --- |
| Scan（默认） | 493 byte，32 个空 Script Tag | 第 23 次 ReadAt，offset 163，已提交 10 Tag | Cancel、Exit |
| Detail | 497 byte，首 Tag 为 `AF 00 12 10` AAC-LC 配置，其余 31 个空 Script Tag，FLV audio 标志置位 | 扫描完成 32 Tag 后，首次详情读取第 67 次 ReadAt，offset 24 | DetailClose |
| AVCDetail | 504 byte，首 Tag 为 `17 01 00 00 00 00 00 00 02 65 88` 孤立 AVC NALU 包，其余 31 个空 Script Tag，FLV video 标志置位 | 扫描完成 32 Tag 后，首次详情探测第 67 次 ReadAt，offset 24 | DetailClose |
| MP3Detail | 498 byte，首 Tag 为 `2F FF FB 90 00` FLV MP3 声音头与 MPEG-1 Layer III 固定头，其余 31 个空 Script Tag，FLV audio 标志置位 | 扫描完成 32 Tag 后，首次详情探测第 67 次 ReadAt，offset 24 | DetailClose |
| Hex | 493 byte，与 Scan 样本字节相同 | 扫描完成 32 Tag 后，点击 Tag 0 偏移，首次 Hex 读取第 67 次 ReadAt，offset 13 | HexClose |
| Summary | 497 byte，与 Detail 样本字节相同 | 扫描完成 32 Tag 后，手动开始媒体概览分析，首次 AAC prefix 第 67 次 ReadAt，offset 24 | SummaryClose |

Scan 样本 SHA-256 为 `9758dc3635a5e05a780ffe4cd40659b243d2d23a4a9e704762add90fcd4c34fb`，Detail 样本为 `16346b21c3e84be9a1d398f330a0023b5ba3b5df5a5c74d8756b5cda8f1e4437`，AVCDetail 样本为 `8b9d282048f76e6cc7fa6821a83ce8c02726a2dd91592b8da493b421ba4cdda9`，MP3Detail 样本为 `d6414212c5fb29d116fdf198ae7a1ab66ab688956d6fbfbfee7a16e2cfb10543`。实际 fixture SHA-256、模式、读取次数、gate 偏移、文件长度和已提交 Tag 数均写入 `build-record.json`；收据和检查必须匹配这些参数。旧 v1 Scan 记录仍可读取，不修改历史 JSON。

Hex 样本沿用 Scan 的 SHA-256；模式及 gate 参数独立校验，不能混用两种 run 的收据。

Summary 样本沿用 Detail 的 SHA-256，但通过 `StartSummary` 进入全索引分析。
32 个空 Script Tag 不需要读取 payload，因此 Summary 必须保留首个 AAC Tag。
Summary 模式的收据、场景和 gate 身份单独校验，不能混用 DetailClose。

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

`self-test.ps1` 在内存中验证 JSON 收据时间，覆盖 Windows PowerShell 5.1 的字符串和 PowerShell 7 的 `DateTime` 反序列化，保留 UTC 身份及 .NET 的 100 ns 精度，防止时区偏移导致误判 PID 复用或丢失退出耗时的小数部分。同时验证五种模式的固定 fixture SHA-256、gate 参数、旧 Scan 收据兼容与模式/场景错配拒绝。该测试不创建 run 或修改旧收据。

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

此场景使用单独的 `Detail`（AAC）、`AVCDetail` 或 `MP3Detail` run，先完成结构扫描，再阻塞选中 Tag 的详情读取。

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

验证 MP3 时同样使用两个新目录：

```powershell
$detailVerify = 'C:\work\native acceptance\mp3-detail-verify-001'
$detailRun = 'C:\work\native acceptance\mp3-detail-close-001'
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $detailVerify -GateMode MP3Detail
& "$scripts\build.ps1" -Repository $repo -RunDirectory $detailRun -GateMode MP3Detail
```

三个详情模式的非 GUI race 测试都先确认 32 Tag 扫描完整、源仍保留，再请求 ID 0 的 AAC、AVC 或 MP3 详情到 gate。关闭请求必须立即使文档 `closed=true`、`terminal=false`，拒绝新详情和分页，且不能产生 `closed.json`；释放 gate 后 Close 应返回 nil、源才关闭并可独占打开。

原生步骤：

1. 启动该 run 的验收 exe，通过真实文件选择器打开该 run 的 `native-controlled.flv`。等待扫描结束，确认 32 Tag；此时应没有 `entered.json`。
2. 点击 Tag ID **0** 请求 AAC、AVC 或 MP3 详情，等待 `entered.json` 和详情读取中界面。该收据必须匹配所选 GateMode，且为 read 67/offset 24/32 Tag。
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

检查要求 `close-command < release < released < closed`，所有收据属于同一 run 和进程、source Close 无错误、fixture 可独占打开。`closing/32` 是 **Release 前**操作者观察的状态；MarkClose 仅证明操作意图记录，不能代替按钮操作、关闭中截图或释放后的空状态。DetailClose 不能用于 Scan run；Cancel、Exit、MarkCancel、MarkExit 和退出 watcher 也不能用于任何详情 gate run。

## Hex 读取阻塞时关闭文件

Hex 使用独立 run。样本与 Scan 相同，先扫描完整 32 Tag，再通过 Tag 表中的偏移按钮
请求原始范围；此时才进入 gate。

```powershell
$hexVerify = 'C:\work\native acceptance\hex-verify-001'
$hexRun = 'C:\work\native acceptance\hex-close-001'
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $hexVerify -GateMode Hex
& "$scripts\build.ps1" -Repository $repo -RunDirectory $hexRun -GateMode Hex
```

非 GUI race 验证调用 `StartHex(1, 13, 512)`，实测在第 67 次 ReadAt、offset 13 进入
gate。已提交 32 Tag；剩余文件只有 480 byte，实际读取范围受 EOF 限制。
关闭请求必须立即使文档 `closed=true`、`terminal=false`，拒绝新/旧 Hex 查询、详情
和分页；释放 gate 后 Close 返回 nil、源关闭且文件可独占打开。

原生验收步骤：

1. 启动该 run 的 exe，通过原生选择器打开 `native-controlled.flv`，等待 32 Tag 扫描结束，确认没有 `entered.json`。
2. 首次读取操作必须点击 **Tag 0 的偏移 13** 按钮。不要先点击 Tag ID 请求详情，也不要跳到其他地址；gate 使用固定读取序号，错误操作会使 run 失败并保留证据。
3. 等待 Hex 读取中和 `entered.json`，确认模式 Hex、read 67、offset 13、32 Tag。运行 MarkClose 后立即点击真实窗口“关闭文件”，截图确认“正在关闭”、详情与 Hex 内容清空且无法发起新请求。

```powershell
& "$scripts\control.ps1" -RunDirectory $hexRun -Action Status
& "$scripts\control.ps1" -RunDirectory $hexRun -Action MarkClose
# 现在点击真实窗口“关闭文件”，截图确认关闭中；此时应没有 closed.json。
& "$scripts\control.ps1" -RunDirectory $hexRun -Action Release
```

4. 等待 `closed.json` 和窗口空状态，再运行检查，最后正常关闭程序。

```powershell
& "$scripts\check.ps1" -RunDirectory $hexRun -Scenario HexClose -ObservedState closing -ObservedTagCount 32
```

HexClose 与 DetailClose 使用相同的严格 `close-command < release < released < closed`
顺序和进程/产物身份检查，但各自只接受兼容的 gate 模式。MarkClose 仍只是操作意图，
`closing/32` 来自 Release 前操作者的真实观察，必须保留截图。Cancel、Exit 和
DetailClose 均不能用于 Hex run；控制器没有 reset 或自动 GUI 操作。

## Hex 生产窗口与联动样本

```powershell
python -B tools/desktop-native/hex_fixtures.py --output-dir 'C:\work\hex-fixtures-new'
python -B -m unittest discover -s tools/desktop-native -p 'hex_fixtures_test.py' -v
```

生成器只用标准库，拒绝相对路径和已有目录，不生成大文件。默认产物如下；
`fixtures.json` 保存文件长度/SHA-256、所有 Tag 的 ID/偏移、可点击字段名称与
byte/bit span、诊断位置，以及请求窗口的预期十六进制字节、长度与 EOF 标志。
字段的 `links` 为 null 表示派生值没有原始位置，不能提供跳转按钮。

| 文件 | 长度 | 预期 |
| --- | ---: | --- |
| `hex-details.flv` | 4776 byte | 106 Tag，包含四种详情编码、未知 Tag 和短 AAC 语法诊断；结构索引完整，未知类型会有警告 |
| `hex-change.flv` | 4776 byte | 独立的相同内容副本，打开后受控改变大小/mtime，后续 Hex 请求应为 `source_changed` |
| `hex-truncated.flv` | 4789 byte | 追加 ID 106，payload 声明 10 byte、仅有 `AF 00`；详情拒绝，Hex 可读取现存原始字节 |

普通生产窗口打开 `hex-details.flv`。先在 Tag 表点击偏移，再选择 Tag 详情和字段
位置，核对内容身份、Hex 高亮与下表位置。Hex 请求会与详情共用单个读取 worker；
快速切换不能把旧文件、旧 Tag 或旧窗口的迟到结果显示出来。

| Tag ID / 点击目标 | 原始位置与预期 |
| --- | --- |
| 0：`boundary` 下的 `value` | byte 51、bit 0、8192 bit；1024 个 `0123456789ABCDEF` 循环字节。字段预览有省略，但原始 span 完整；从 51 读取 512 byte，下一窗口从 563 开始，均应保留字段高亮 |
| 1：`initialAudioObjectType` / `samplingFrequencyIndex` | byte 1095，前者从 bit 0 起共 5 bit，后者从 bit 5 起共 4 bit；后者跨到 byte 1096，值为 4 |
| 2：`lengthSizeMinusOne` | byte 1121、bit 6、2 bit，值 3；`SPS summary` 属于派生字段，没有可跳转 span |
| 3：`syncWord` | byte 1171、bit 0、11 bit，值 0x7ff；派生 `bitrateKbps`、`frameLengthBytes` 没有跳转位置 |
| 4：`nalUnitType` | byte 1199、bit 3、5 bit，值 5；来自 Tag 2 的配置上下文 |
| 5：未知 Tag / 结构诊断 | Tag offset 1205，payload offset 1216、2048 byte，为 `00..FF` 循环。详情不支持，原始范围与 `FLV_TAG_TYPE_UNKNOWN` 诊断位置仍可跳转 |
| 6：短 AAC 诊断 | `AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED` 位于 byte 3282、bit 0，是 ASC payload 后首次不可用的位置；Hex 显示的是这里真实存在的 FLV PreviousTagSize 字节，不能把它当作 ASC 内容 |
| 105：第二页 MP3 | Tag offset 4756，payload `2F FF FB 92 C0`；`padding` 在 byte 4770、bit 6、1 bit。跳至 ID 105 后应切到包含该 Tag 的索引页，不能仍显示首页 Tag 的内容 |

使用地址输入和前后窗口按钮覆盖跨窗、EOF 与未索引数据：主样本地址 4771 请求
512 byte 实际只剩 `C0 00 00 00 10`；地址 4776 是合法 EOF 空窗口，不能伪造字节。
截断副本地址 4787 只剩 `AF 00`，即使该 Tag 不能解析详情也可在 Hex 中检查。
超过文件末尾的地址必须拒绝。清单另给出 4096 byte 上限窗口和超限请求，供协议层
回归使用；原生界面默认使用 512 byte 窗口。

大文件场景可直接复用此前生成并验证过的 `mp3-large.flv`，不必再次生成。1024 MiB
填充版本为 1,082,131,440 byte，尾 Tag 65 offset 1,073,742,812；核对既有清单及
SHA-256 后，通过尾 Tag 或精确地址读取 512 byte，确认地址没有精度损失。
读取末尾窗口只检查请求范围，不代表加载全文或解码音频。

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

## MP3 生产窗口流程

```powershell
python -B tools/desktop-native/mp3_fixtures.py --output-dir 'C:\work\mp3-fixtures-new'
```

生成器只使用 Python 标准库，要求绝对的新目录，每个文件使用独占创建，拒绝覆盖。
`fixtures.json` 保存实际写入长度、SHA-256、每个 Tag 的 ID、文件偏移、payload 偏移和
预期值/诊断。三个默认文件是 `mp3-details.flv`（391 byte，19 Tag）、同内容独立副本
`mp3-change.flv`（391 byte）以及 `mp3-truncated.flv`（404 byte）。

普通生产窗口打开 `mp3-details.flv`，逐项选择以下 Tag；这些样本只含固定头和少量
opaque 字节，不代表完整音频帧或可播放音频。

| Tag ID | 预期 |
| --- | --- |
| 0 / 1 | MPEG-1 Layer III，128 kbps、44100 Hz，双/单声道，1152 samples/frame，推导长度 417 byte |
| 2 / 3 | MPEG-2，64 kbps、22050 Hz，双/单声道，576 samples/frame，推导长度 208 byte |
| 4 / 5 | MPEG-2.5，64 kbps、11025 Hz，双/单声道，576 samples/frame，推导长度 417 byte |
| 6 | padding=true，推导长度从 417 增加到 418 byte |
| 7 | free-format；头完整并有 `MP3_FREE_FORMAT_UNRESOLVED` 警告，码率及帧长度未确定，不能显示成 0 |
| 8 | CRC 保护声明；后续 `12 34 AA BB` 保持 opaque，不能声称验证了 CRC |
| 9–14 | 依次为损坏同步、保留 version、保留 layer、Layer II、非法 bitrate index、保留频率；显示部分结果及对应 `MP3_*` 错误 |
| 15 | 保留 emphasis；头仍完整，并显示 `MP3_EMPHASIS_RESERVED` 警告 |
| 16 / 17 | 仅两字节 MPEG 头 / 空 MP3 正文；`MP3_FRAME_HEADER_TRUNCATED`，保留 FLV 声音头 |
| 18 | SoundFormat 14 明确 `detail_unsupported`，不能因正文类似 MPEG 头就接受 |

FLV 声音头统一保留 `2F` 声明，MPEG-2/2.5 及单声道样本有意与其频率/声道声明不同；
应分别显示原始容器位域与 MPEG 头推导值。原始固定头在 `frameHeaderOffset` 开始，
精确字段可有 byte/bit 位置；推导的码率、频率、声道数、每帧样本数和帧长度没有独立
原始 span。推导帧长度不证明文件里存在该长度的完整帧，也不能据此跳读后续帧。

`mp3-truncated.flv` 在完整前缀后追加 ID 19，payload 声明 10 byte、仅剩 2 byte，
应拒绝该 Tag 的详情并返回 `detail_truncated`；前 19 Tag 仍可读。打开独立的
`mp3-change.flv` 后受控改变其大小或修改时间，再选择 Tag 应为 `source_changed`。
关闭文件后详情清空、源句柄释放。

可选 `--large-mib` 默认为 0（不生成），允许 64–4096，采用与 AVC 生成器相同的顺序
物理写入方式，每次最多持有约 1 MiB 写入块，不 seek、不创建稀疏文件、不加载全文。
参数仅指定前置 Script Tag **填充 payload** 的 MiB，另加 FLV framing 和一个带固定
MP3 头、**8 MiB opaque 正文**的尾 Tag。

```powershell
python -B tools/desktop-native/mp3_fixtures.py --output-dir 'C:\work\mp3-large-new' --large-mib 1024
```

1024 MiB 填充产生的 `mp3-large.flv` 为 1,082,131,440 byte，共 66 Tag；尾 Tag ID 65，
offset 1,073,742,812，仍在默认 100 条首页。以清单 `tailTag` 的位置选择它，核对
MPEG-1、128 kbps、44100 Hz、双声道、推导长度 417 byte，以及完整 opaque 范围。
详情只读取首个固定 MPEG 头，opaque 正文无需读取或解码。
验收前使用 `Get-Item` 与 `Get-FileHash -Algorithm SHA256` 独立读回，和清单的实际
`bytes`、SHA-256 核对；大文件成功扫描或选中尾 Tag不能解读为音频解码能力或完整
payload 读取吞吐量。

## Summary 读取阻塞时关闭文件

使用单独的 Summary run，完成 32 Tag 扫描后点击媒体概览的“开始分析”。
不要先点击详情或 Hex：gate 记录的是扫描后首个真实源读取，不能把其他读取
误当作分析读取。`GetSummary`、取消确认和事件分页均不应读取源文件。

```powershell
$summaryVerify = 'C:\work\native acceptance\summary-verify-001'
$summaryRun = 'C:\work\native acceptance\summary-close-001'
& "$scripts\verify.ps1" -Repository $repo -RunDirectory $summaryVerify -GateMode Summary
& "$scripts\build.ps1" -Repository $repo -RunDirectory $summaryRun -GateMode Summary
```

非 GUI race 验证调用 `StartSummary(1)`，记录第 67 次 ReadAt、offset 24；
逻辑关闭立即清空读取结果并拒绝 Detail、Hex、Summary、取消查询和事件/Tag 页，
但源仍归实际 worker 所有。只有释放 gate 后 `Close` 才完成、句柄才可独占打开。
Scan 模式的非 GUI 回归另外验证：取消留下的 10 Tag 索引前缀可完成 Summary，
`processed=total=10`、`scanComplete=false`，不能把它显示成完整源覆盖。

原生步骤：

1. 从真实 Windows 文件选择器打开 `$summaryRun\native-controlled.flv`，确认扫描完成 32 Tag。
2. 点击媒体概览开始分析，保存读取中 UI 和 `entered.json`，核对 Summary、read 67、offset 24、32 Tag。
3. 运行 MarkClose，随即点击真实“关闭文件”，保存“正在关闭”的 UI；概览、详情和 Hex 清空，新读取和打开入口禁用。此时不能出现 `closed.json`，源不可独占打开。
4. 发布 Release，等待空状态与 `closed.json`，再执行检查。

```powershell
& "$scripts\control.ps1" -RunDirectory $summaryRun -Action MarkClose
# 随即操作真实窗口关闭文件；保存 Release 前的 closing 状态。
& "$scripts\control.ps1" -RunDirectory $summaryRun -Action Release
# 确认窗口已恢复空状态后：
& "$scripts\check.ps1" -RunDirectory $summaryRun -Scenario SummaryClose -ObservedState closing -ObservedTagCount 32
```

`SummaryClose` 同样要求 `close-command < release < released < closed` 严格排序、
进程/run 身份一致、实际源 Close 无错误和独占打开成功。客户端等待超时不表示
实际 I/O 已结束；即使概览显示等待确认，关闭仍必须保留同一个源直到读取返回。
SummaryClose 不能用于其他模式；MarkCancel/MarkExit 和 Exit watcher 只适用于 Scan。

## Summary 取消完成后重新分析

此流程使用独立的新 `Summary` run，补充取消、实际读取退出和重新分析的连续操作。
它不使用 `SummaryClose` 的关闭前释放顺序，检查入口是 `check-summary-recovery.ps1`。

1. 构建 `build.ps1 -Repository $repo -RunDirectory $recoveryRun -GateMode Summary`，在真实窗口打开本 run 的 `native-controlled.flv`，扫描完成 32 Tag 后开始媒体分析。
2. 确认 `entered.json` 是 Summary、ReadAt 67、offset 24。运行 `control.ps1 -RunDirectory $recoveryRun -Action MarkSummaryCancel`，随即在窗口点击“取消媒体分析”。记录此时仍在取消/等待的原生截图与 accessibility JSON，以及实际观察时的 UTC。
3. 运行 `control.ps1 -RunDirectory $recoveryRun -Action Release`，等待 UI 明确显示任务已取消、可重新分析；保存第二组 UI 证据和 UTC。
4. 不关闭或重开文件，点击“重新分析”，等待完成 32/32 Tag 并出现概览；保存第三组证据和 UTC。gate 仅阻塞第 67 次读取，后续分析走实际文件读取。
5. 通过真实 UI 关闭文件，保存空状态证据和 UTC。将四次实际观察写到绝对路径 JSON，执行 `check-summary-recovery.ps1 -RunDirectory $recoveryRun -ObservationFile $observationFile`。

观察文件结构如下；时间和路径须使用本次实际记录，`runId/pid/processToken` 从 `entered.json` 取值：

```json
{
  "format": "muxray-summary-recovery-observation-v1",
  "runId": "actual-run-id", "pid": 123, "processToken": "actual-token",
  "observations": [
    {"state": "canceling", "timeUTC": "2026-09-25T01:00:02Z", "evidenceFiles": ["C:\\evidence\\canceling.json", "C:\\evidence\\canceling.png"]},
    {"state": "canceled", "timeUTC": "2026-09-25T01:00:05Z", "evidenceFiles": ["C:\\evidence\\canceled.json", "C:\\evidence\\canceled.png"]},
    {"state": "ready", "timeUTC": "2026-09-25T01:00:06Z", "processed": 32, "total": 32, "indexedTags": 32, "evidenceFiles": ["C:\\evidence\\ready.json", "C:\\evidence\\ready.png"]},
    {"state": "empty", "timeUTC": "2026-09-25T01:00:08Z", "evidenceFiles": ["C:\\evidence\\empty.json", "C:\\evidence\\empty.png"]}
  ]
}
```

检查要求 `entered < cancel-command < canceling UI < release < released < canceled UI < ready UI < closed < empty UI`，收据与观察文件的进程身份一致、ready 覆盖 32 Tag、Close 无错误、源可独占打开，并保存所有证据文件 SHA-256。这些 UI 状态由操作者提供，脚本只校验元数据与顺序，不会从收据推断界面显示成功。`summary-recovery-self-test.ps1` 可在 PowerShell 5.1/7 验证顺序与拒绝错误身份的逻辑，不构成 GUI 验收。

## ADR-017 Tag 筛选样本与索引核对

`filter_fixtures.py` 生成七个固定样本、SHA-256、全部索引字段和预期全局 Tag ID，写入绝对、新目录。`summary-mixed.flv` 与 Summary 生成器的 20 Tag/497 byte 样本逐字节相同，供筛选→详情→Hex 与已完成 Summary 的联动验收。

```powershell
python -B -m unittest discover -s tools/desktop-native -p filter_fixtures_test.py -v
python -B tools/desktop-native/filter_fixtures.py --output-dir C:\work\filter-fixtures-001
go build -o .cache/muxray-filter.exe ./cmd/muxray
python -B tools/desktop-native/filter_check.py --fixtures-dir C:\work\filter-fixtures-001 --muxray C:\work\MuxRay\.cache\muxray-filter.exe --output-dir C:\work\filter-cli-check-001
```

| 样本 | 确定性期望 |
| --- | --- |
| `filter-ranges.flv` | 12 Tag，类型齐全；10000–12000 ms 包含两端、重复与回退后再次命中；视频为 ID 1/7/11；4294967295 ms 为 ID 9 |
| `filter-structures.flv` | 7 Tag；PreviousTagSize 不一致为 ID 1/3，缺失为 ID 6；缺失不能归为“存在但不一致” |
| `filter-truncated.flv` | ID 1 声明 10 byte/实际 2 byte，同时命中 Payload 截断和 PreviousTagSize 缺失 |
| `filter-sparse.flv` | 8305 Tag，音频 ID 4500/5000/8200；首窗口 `[0,4096)` 无命中仍可继续，下一窗口止于 8192，最后止于 8305 |
| `filter-multipage.flv` | 305 Tag，偶数 ID 为音频共 153；第一页 100 条后游标为 199，第二页 53 条止于 305 |
| `filter-empty.flv` | 13 byte 合法头，0 Tag，查询直接到索引末尾 |

`filter_check.py` 用真实 Go CLI `tags --count 4096` 核对所有索引字段，保留命令、返回码和原始 JSON；它不调用桌面筛选 binding，不替代筛选单测或 GUI 验收。`fixtures-001` 等已生成目录不可覆盖，修改生成器后必须使用新目录。

浏览器合成 host 可在前端 build 后运行 `node test/browser-host.mjs`，通过 `?smoke=filter-ranges|filter-sparse|filter-multipage|filter-structures|filter-truncated|filter-empty` 选择同等数据；`filter-delayed` 延迟 1.8 秒回复以检查关闭/重开隔离，`filter-timeout` 丢失本会话第一次筛选回复以检查重试。`node --test test/browser-filter-fixture.test.mjs` 只核对合成 host 的分页与数据，不启动浏览器。现有 `browser-smoke.mjs` 已补筛选检查；真正浏览器/原生窗口是否执行应在本次验收报告单独记录。

## Tag 趋势窗口样本（ADR-018）

`trend_fixtures.py` 只生成新文件，不改变 ADR-017 样本；绝对输出目录必须不存在。
清单记录每个 Tag 的源元数据、指定窗口及前驱期望。前驱按 `(typeCode, streamId)`
分组，最多回看 4096 条源记录；找到当前窗口所有分组的前驱后可提前停止。
每次 API 最多 512 点，桌面默认 256 点，EOF 空窗口不读取上下文。

```powershell
python -B tools/desktop-native/trend_fixtures.py --output-dir 'C:\work\trend-fixtures-001'
python -B -m unittest discover -s tools/desktop-native -p trend_fixtures_test.py -v
```

| 文件 | 确定性期望 |
| --- | --- |
| `trends-mixed.flv` | 300 Tag，6496 byte；前 20 Tag 与 `summary-mixed.flv` 逐字节相同，后续 AAC/AVC 头链延续现有配置。窗口 `[0,256)` 后为 `[256,300)`；ID 256→254 的 DTS 差 0，257→255 为 -100，258→256 为 2001；259 首次 StreamID 1，无前驱；260 DTS 4294967295，261→259 为 -40，264→260 为 -4294967295；262 为 filtered Other |
| `trends-sparse.flv` | 4354 Tag；起点 4097、256 点时上下文 `[1,4097)` 恰好 4096 条。ID 4097 的 Video 前驱 ID 1 可用；4098 的 Audio 前驱在预算外，因此 unknown；4099 的前驱为窗口内 4098，差 10；4100→4097 为 -10；从未见过的 Other 4101 同样只能判为预算不足 |
| `trends-structures.flv` | 7 Tag；PreviousTagSize invalid 为 ID 1/3，missing 为 ID 6；ID 2 为 filtered；ID 4 的 StreamID 为 16777215 |
| `trends-truncated.flv` | ID 1 声明 10 byte、实际 2 byte，Payload 不完整且 PreviousTagSize missing；实际 payload 为偏移 `[39,41)` 的 `AF 00` |
| `trends-empty.flv` | 13 byte 合法 FLV 头，0 Tag；窗口为空、上下文读取 0 |

趋势保留原始 DTS，负差不做时间轴修复；`budget_exhausted` 不能解释成“没有前驱”。
这些文件用于容器、配置与固定头检查，不保证音视频帧完整或可播放。生成器的六项
单测独立解析实际 FLV 字节，并核对窗口前驱、预算边界及拒绝覆盖行为；它不调用
Go trend binding，也不替代原生 GUI 验收。

合成浏览器 host 支持 `?smoke=trends|trends-sparse|trends-structures|trends-truncated|trends-empty`，
类型、DTS、StreamID 和大小与上述场景一致，源 offset 特意大于 JavaScript 安全整数。
`trends-timeout` 丢失第一次趋势回复，独立重试可完成；`trends-delayed` 延迟 1.8 秒且
保留关闭/重开前的 session。`trends-huge` 是重复 300 条模式的虚拟索引，total 为
9007199254741300，起点 9007199254740993 可检查 Tag ID、offset、delta 与分页的精确性。
虚拟索引的 Summary 类型总数用算术计算；趋势合成场景不产生 codec Summary 事件。

```powershell
# 在 desktop/frontend 中执行；不启动浏览器。
node --test test/browser-filter-fixture.test.mjs test/browser-trend-fixture.test.mjs
```

`.github/workflows/desktop.yml` 的 Windows 非 GUI job 统一执行 bindings 漂移检查、
前端单测和构建、合成 host 契约检查、desktop Go test/vet、Python fixture 与保存
overlay 测试、资源采样/长媒体工具/快照校验自测、PowerShell 5.1/7 gate 自测，
最后调用 `desktop/build-production.ps1`。快照自测使用 `tools/requirements-schema.txt`
中的固定依赖；三种新工具的自测按模块名运行，不重复已有 fixture/overlay 测试。
有 gcc 时额外运行 desktop race；缺少 gcc 时显式输出跳过通知。根模块多平台、
Schema 与 fuzz 检查仍在 `ci.yml`。CI 不启动 Wails 窗口、浏览器或文件选择器，
不生成 FFmpeg 长媒体，也不读取真实语料，不能代替每次单独记录的原生验收。
资源采样器的 Windows 自测只观察测试自己启动的辅助进程。

## 诊断快照保存（ADR-019）

普通生产验收先运行 `desktop/build-production.ps1`，再通过真实窗口选择样本、载入
所需详情/Hex/Summary/趋势，点击“导出诊断 JSON”，操作 Windows 保存对话框。
取消后确认当前文档仍可浏览。原生操作及生成文件应分别保存证据；文件存在本身
不能证明保存状态正确，截图也不能证明 JSON 字节与源对应。

`snapshot_check.py` 验证独立 Schema、同一会话和请求 ID、section 与 `omitted`
一致性；显式传入本地验收源时还直接核对原始 Tag 头、Hex 实际字节及源 SHA-256。
它不复跑编解码语义分析。需要 `tools/requirements-schema.txt` 中的 `jsonschema`。
JSON 报告始终限制为 2 MiB；源默认限制为 16 MiB，超过时必须显式指定
`--max-source-bytes`，可选范围为 16 MiB–1 GiB。长样本示例使用 64 MiB。
源使用至多 1 MiB 缓冲分块读取，源顺序核对最多访问 100 万个 Tag 头，保留的
位置仅包括选中 Tag 与趋势点所引用的 Tag；校验还流式读回源哈希并核对前后文件身份。
它不单独证明报告声明的全部索引数量或完整媒体分析，仍需独立清单和 CLI 核对。
源校验有 120 秒协作截止，在读取前后检查，不能强制打断已经阻塞的文件 I/O。

```powershell
python -B tools/desktop-native/snapshot_check.py 'C:\work\saved-report.json' --source 'C:\work\trends-mixed.flv'
python -B tools/desktop-native/snapshot_check.py 'C:\work\saved-long-report.json' --source 'C:\work\long-media-001\generated-long-avc-aac-2400s.flv' --max-source-bytes 67108864
python -B -m unittest discover -s tools/desktop-native -p 'snapshot_check_test.py' -v
python -B -m unittest discover -s tools/desktop-native -p '*_build_test.py' -v
```

`save_recovery_build.py` 通过 Go overlay 构建独立的保存恢复验收程序，不修改
`desktop/save.go`，不在产品里加入故障开关。每种模式使用仓库外的绝对、新目录，
默认复制 20 Tag/497 byte 的 Summary mixed 样本为 `native-save-controlled.flv`。
可用 `--fixture` 指定不大于 16 MiB 的确定性 FLV。只影响该 run 第一次保存该样本。

| 模式 | 控制点 | 必须实际观察的界面结果 |
| --- | --- | --- |
| `lost_reply` | 真实写入和结果保留后，首次 Save 回复延迟 8 秒 | `return-released.json` 出现前，界面经原 ID 查询显示保存成功；没有第二次对话框或写入 |
| `write_failure` | 保存对话框返回后、目标 I/O 之前注入写入失败 | 显示写入失败，文档与已完成 Summary 保留；新目标不产生文件，若使用已有目标则核对其内容不变 |
| `close_during_save` | 对话框返回后、写入前等待 release，最多 30 秒 | 先关闭源文档到空状态，保存仍未完成；释放后成功，仍标注原源文件 |

```powershell
$repo = 'C:\work\MuxRay'
$run = 'C:\work\snapshot-save-lost-001'
python -B tools/desktop-native/save_recovery_build.py --repository $repo --output-dir $run --mode lost_reply
python -B tools/desktop-native/save_recovery_build.py --build-run $run
# 通过授权的原生操作启动该目录中的 MuxRay-save-acceptance.exe，只运行一次。
# close_during_save 模式观察关闭后的空状态后，才执行：
python -B tools/desktop-native/save_recovery_build.py --release-run 'C:\work\snapshot-save-close-001'
```

准备记录包含源代码、已有前端 assets、overlay、工具和 fixture 的哈希；构建
强制使用本地缓存，验证输入未变化，另记命令及实际 exe 哈希。准备后改变源码或
assets 必须新建 run。工具复用已构建的前端，因此应先完成正式前端构建。
收据带 run ID、PID、进程 token 和 UTC 时间，release 拒绝身份错配或覆盖旧证据。
若超过 30 秒，应保留失败证据并使用新 run；不能把超时释放称为手动关闭验收。
收据只证明控制点，不代替 GUI 截图；这些是受控故障实验，不是自然慢盘实测。

合成 host 另提供 `snapshot-success`、`snapshot-cancel`、`snapshot-lost-reply`、
`snapshot-delayed`、`snapshot-write-failed`、`snapshot-recovery-timeout` 场景。
`node --test test/browser-save-fixture.test.mjs` 检查合成协议，不操作浏览器或原生对话框。
本轮实测范围见 [诊断快照验收记录](../../docs/desktop-snapshot-20260929.md)。

## 生产桌面资源采样

`resource_probe.py` 只观察已启动的 Windows 进程，不启动、终止或操作生产窗口。
先取得明确 PID 的创建 FILETIME 和 exe 身份，再以同一身份开始采样；PID 复用、
exe 路径不同或目标已退出都会拒绝附加。记录期间保留进程查询句柄，并用父身份的
创建/退出区间核对子进程，避免仅凭进程名把其他应用的 WebView2 算入本次结果。

先通过真实桌面启动待测生产 exe，从本次窗口对应进程取得 PID，再在仓库根目录执行。
以下例子要求 `C:\work\native-evidence` 父目录已存在，`resources-001` 及其同名前缀
日志尚不存在。`--output-dir` 必须是绝对新目录，工具不会覆盖历史 run。

```powershell
$repo = 'C:\work\MuxRay'
$python = (Get-Command python).Source
$probe = Join-Path $repo 'tools\desktop-native\resource_probe.py'
$targetExe = Join-Path $repo 'desktop\build\bin\MuxRay.exe'
$targetPid = 12345 # 替换为本次实际生产窗口的 PID。
$resourceRun = 'C:\work\native-evidence\resources-001'

$identityJson = & $python -B $probe identity --pid $targetPid --exe $targetExe
if ($LASTEXITCODE -ne 0) { throw '目标进程身份核验失败' }
$identity = $identityJson | ConvertFrom-Json
$recordArgs = @(
    '-B', ('"{0}"' -f $probe), 'record',
    '--pid', [string]$identity.pid, '--created', $identity.created,
    '--exe', ('"{0}"' -f $targetExe),
    '--output-dir', ('"{0}"' -f $resourceRun),
    '--duration-seconds', '900', '--interval-ms', '500', '--exit-grace-seconds', '5'
)
$samplerProcess = Start-Process -FilePath $python -ArgumentList $recordArgs -WindowStyle Hidden -PassThru `
    -RedirectStandardOutput "$resourceRun.stdout.log" -RedirectStandardError "$resourceRun.stderr.log"
```

后台辅助进程使用 `-WindowStyle Hidden`；不要为采样打开额外的可见控制台。
等待 run 中 `manifest.json`、`status.json` 出现并确认 `running: true`，再标记阶段：

```powershell
Get-Content -LiteralPath "$resourceRun\status.json"
& $python -B $probe mark --run-dir $resourceRun --stage 'long-open-request' --note '下一步通过原生选择器打开长媒体' --wait-seconds 5
# 操作真实窗口；完成后标记实际观察到的状态。
& $python -B $probe mark --run-dir $resourceRun --stage 'long-summary-ready' --wait-seconds 5
# 需要提前结束采样时：
& $python -B $probe stop --run-dir $resourceRun --wait-seconds 5
```

`mark`/`stop` 只向采样器发布控制请求，不向 MuxRay 发送命令；`stop` 不会关闭目标。
请求与确认分别记录时间，`confirmation: unconfirmed` 不是阶段已生效的证明。
标记只是操作者提供的阶段标签，仍应保留 GUI 观察证据。若要记录退出，应通过真实
窗口关闭 MuxRay，让采样器继续观察：默认在确认根进程退出后等待 5 秒再完成；
也可等待总时长上限。不要先 `stop` 再声称本 run 观察到了后续退出。

参数范围：`--duration-seconds` 为 `(0,7200]`，`--interval-ms` 为 `[250,5000]`，
`--exit-grace-seconds` 为 `[0,60]`，控制确认 `--wait-seconds` 为 `[0,15]`。
进程树每秒刷新；最多保留 4096 个进程身份、同时追踪 128 个活跃进程、处理
1024 个控制请求，样本日志上限 256 MiB。触及限制或采样失败必须审阅最终结果，
不能只凭后台进程结束宣告验收通过。

`manifest.json` 记录进程创建身份、exe/工具哈希和预算，`samples.jsonl` 保存原始
逐进程及分组样本，`marks.jsonl` 保存已确认标记，`summary.json` 是最终收据。
`status.json` 只是便于观察的投影；读取状态文件导致临时共享冲突时以样本流和
最终收据为准。宿主、观察到的 WebView2 后代及其他后代分别统计，也提供总和。

- private bytes 是私有已提交内存，不是物理 RAM；工作集求和可能重复计算共享页。
- CPU 的 `cpuPercent` 以一个逻辑核满载为 100%，`cpuPercentOfMachine` 再除以逻辑核数；首次观察没有区间 CPU 值，缺失指标保留为未知。
- 最大值来自采样点，可能错过短峰和两次进程树刷新之间的短命进程；未观察到的后代、独立或共享 WebView2 进程不自动归属本次程序。
- 不测 GPU 分配、系统文件缓存或附加前的资源；跨阶段的 CPU 区间可能包含上一阶段工作。
- OS 退出及退出码不能证明源文件有序关闭；须结合 GUI 空状态及独立源文件释放检查。若采样因错误、时限或操作者停止而结束，应明确仍存活的已观察进程。

```powershell
python -B -m unittest discover -s tools/desktop-native -p 'resource_probe_test.py' -v
```

### 连续打开/关闭的固定窗口复核

同一生产进程重复运行 5–10 次打开、尾部详情、媒体概览、Hex 和关闭流程。
先确认原生窗口的空状态，记录 `baseline-idle`；之后每轮按顺序记录
`cycle-01-open-request`、`cycle-01-scan-ready`、`cycle-01-detail-ready`、
`cycle-01-summary-ready`、`cycle-01-hex-ready`、`cycle-01-closed`，后续轮次递增编号。
`closed` 必须在实际观察到空状态后记录，另存源文件独占只读打开的收据。
基线和每轮关闭后保持至少 30 秒静置，建议等待 35 秒再开始下一轮或退出，
以便固定窗口右边界也有采样。最后标记 `exit-request`，通过原生窗口正常退出，
等待采样器记录退出和 grace 完成；不要先停止采样器。

`resource_cycles.py` 离线核对 manifest、原始样本、已确认阶段及最终汇总，
再统计每个固定 30 秒窗口的最后 10 秒。内存和句柄取样本算术均值，CPU 按
相邻采样间隔加权；同时列出各轮活动阶段峰值。间隔过大、资源指标缺失、
阶段顺序或覆盖不足都不能算完整证据。工具不判断“是否泄漏”，也不把进程
退出当作源文件释放。截图、操作事实和独占打开检查仍由原生验收单独提供。

```powershell
python -B tools/desktop-native/resource_cycles.py --run-dir 'C:\work\native-evidence\resources-001' --cycles 5 --output 'C:\work\native-evidence\cycles-analysis.json'
python -B -m unittest discover -s tools/desktop-native -p 'resource_cycles_test.py' -v
```

若桌面锁屏、采样中断或操作无法完成，保留该 run 并说明未通过的覆盖，使用
新 run 重新开始连续流程；不能拼接采样空档来声称完成了连续验收。

### Document 扫描版本对比

`scan_compare.py` 把两个明确 Git 提交归档到新证据目录，为每个归档注入同一
测试探针并构建独立可执行文件，不切换当前工作区。计时使用生产
`document.Open` 到 `Wait` 返回及原始 `os.File`，每个样本为新进程。
先读取源哈希、各执行一次排除的预热，然后执行 5–30 对交替顺序的样本；
另外两次计数探针记录应用层 ReadAt 次数和字节，其耗时不进入延迟分布。
扫描完整性、每条 Tag 与诊断摘要必须一致；输入、命令和输出身份均保留。

```powershell
python -B tools/desktop-native/scan_compare.py prepare --repo 'C:\work\MuxRay' --source 'C:\work\long-media-001\generated-long-avc-aac-2400s.flv' --baseline 45d83b2 --candidate 112df8b --output 'C:\work\scan-compare-prepared'
python -B tools/desktop-native/scan_compare.py run --prepared 'C:\work\scan-compare-prepared\prepared.json' --output 'C:\work\scan-compare-run' --pairs 10
python -B -m unittest discover -s tools/desktop-native -p 'scan_compare_test.py' -v
```

这是操作系统缓存预热后的扫描比较，不是物理盘读取量、冷缓存速度、UI 延迟
或持续内存基线。计时期间应停止自己启动的下载、编译和其他测试；仍须说明
未隔离的系统后台负载。准备与运行目录必须是绝对新路径，不覆盖历史证据。
CI 仅运行这两种工具的小规模单测，不执行真实媒体性能测量或原生交互。

## 长时间有效编码的生成媒体

`long_media.py` 使用本机已安装 FFmpeg 的 lavfi，不下载媒体。默认生成 40 分钟
160×90、25 fps 的 H.264 和 48 kHz 单声道 AAC，视频目标 96 kbit/s、音频
32 kbit/s；固定 GOP 50、B 帧 2、禁用场景切换关键帧，各 codec 使用单线程。
这是**生成媒体，不是真实录制**，用于有效编码、大量 Tag 和事件保留溢出的桌面
资源观察；它与只含结构填充的 GiB 压力文件、真实媒体语料分别记录。

```powershell
python -B tools/desktop-native/long_media.py --ffmpeg 'C:\tools\ffmpeg\bin\ffmpeg.exe' --output-dir 'C:\work\long-media-001' --duration-seconds 2400 --timeout-seconds 600
python -B -m unittest discover -s tools/desktop-native -p 'long_media_test.py' -v
```

`--output-dir` 必须为不存在的绝对目录；单次时长允许 2–2400 秒，生成截止允许
1–600 秒。子进程使用 argv，关闭 stdin，stdout/stderr 各限制 64 KiB；FFmpeg
设置 256 MiB 输出预算，独立校验再拒绝超限或时长不完整的文件。工具只清理自己
启动的失败/超时进程，保留计划、失败记录和部分媒体，拒绝覆盖、删除或重置旧 run。

`generation-plan.json` 记录精确配方、工具/脚本哈希及预算；`ffmpeg-version.json`、
`generation-process.json` 保存有界子进程结果，`generation-result.json` 保存媒体
SHA-256/大小、独立 framing 计数、各类首末 Tag、DTS 及第 1025 个声明关键帧锚点。
固定字段遍历最多检查 50 万 Tag，不保留全部 Tag/payload；配方校验要求完整帧数、
配置/EOS 数量、固定 GOP 数量和 DTS 覆盖，不能把达到文件预算后的短片算成完成。
独立 framing 不等于解码验证；需要时另用 ffprobe 解码计数并保存其命令和结果。

登记样本见 [长媒体清单](../../testdata/manifests/longmedia/generated-long-avc-aac-2400s.sample.json)：
FFmpeg `7.1.1-full_build-www.gyan.dev` 生成 41,698,926 byte、172,504 Tag、60,000
AVC NALU 和 112,501 AAC raw Tag，声明关键帧 1200 个，超过桌面保留的 1024 个。
第 1025 个关键帧为 Tag 147200，DTS 2048000 ms；末 AAC DTS 为 2400059 ms，
尾 EOS DTS 为 2399960 ms。不要把文件级 duration、最后源顺序 Tag 的 DTS 和
所有媒体 Tag 的最大 DTS 混成同一指标。媒体字节不提交到仓库。

相同配方必须在另一个新目录复现，再比较媒体 SHA-256 和独立事实。字节身份依赖
FFmpeg 及编码器构建，版本字符串相同也不保证跨构建一致；变化必须人工审查，
生成器不会自动更新清单。此样本不进入 `corpus.py` 的 32 MiB/50,000 Tag 流程。
需要跨入口比较时，CLI 使用桌面相同的事件预算；正常默认百万 Tag 索引可覆盖它：

```powershell
.cache\muxray.exe summary --schema-version 2 --max-keyframes 1024 --max-configuration-events 128 --max-configuration-bytes 16777216 --max-analysis-diagnostics 256 --max-mp3-header-events 256 'C:\work\long-media-001\generated-long-avc-aac-2400s.flv'
```

检查完整扫描和分析、关键帧观测 1200/保留 1024/省略 176，随后通过原生窗口
验证后段 Tag、详情/Hex 与快照。生成成功、CLI 成功和 GUI 验收是独立结论。

## Summary 生产窗口、分页与数值样本

标准库生成器输出到绝对、新目录；默认另外流式写入一个 200000 Tag、约 3 MB
的索引压力样本。`--many-tags 0` 可省略，允许 32–1000000。所有小样本在清单中
列出 Tag ID、DTS、类型和源偏移，不包含可播放媒体声明。

```powershell
python -B tools/desktop-native/summary_fixtures.py --output-dir 'C:\work\summary-fixtures-new'
python -B -m unittest discover -s tools/desktop-native -p '*fixtures_test.py'
```

| 文件 | 结构与预期 |
| --- | --- |
| `summary-mixed.flv` / `summary-change.flv` | 497 byte，20 Tag；AAC/MP3 切换 2 次、AAC 配置变化 1 次、AVC NAL 长度 4→2、MP3 参数变化 1 次/码率变化 1 次；音频重复 2 次/回退 2 次，视频回退 1 次。变化副本用于 source_changed 锁存及精确恢复后必须重新打开的流程 |
| `summary-timing.flv` | 180 byte，6 Tag；无回退/切换的 AAC/AVC 时间轴，A/V 首尾极值偏移均 10 ms、交集 30 ms，音频/视频区间码率 800/2200 bit/s，闭合 GOP 40 ms |
| `summary-retention.flv` | 42423 byte，1660 Tag；声明关键帧观测 1100/保留 1024，AAC 与 AVC 配置各观测 130/保留 128，MP3 头观测 300/保留 256；截断列表不影响后续观测计数（配置变化各 129、MP3 码率变化 299） |
| `summary-diagnostics.flv` | 6013 byte，300 个损坏同步的 MP3 Tag；分析不完整，MP3 头观测/损坏均 300、保留 256；诊断保留 256，最后一条为 `MEDIA_DIAGNOSTIC_LIMIT_REACHED` |
| `summary-truncated.flv` | 510 byte，混合样本后追加 ID 20，声明 10 byte 只剩 `AF 00`；扫描与分析不完整，索引 21 Tag，新增 audio unknown 1，仍可浏览已保留事件 |
| `summary-many-tags.flv` | 默认 3000018 byte，200000 Tag；前 199999 个为空 Script，末个 MP3 固定头；用于大索引/进度和末 Tag 跳转，不是大物理文件或完整音频测试 |

Mixed 样本的 MP3 DTS 回退会重置比较基线，所以最后一个 128 kbps 头不追加一次
码率变化。音频区间码率为 `9 × 8000 / 46 = 1565` bit/s（整数截断），MP3 区间
码率为 `5 × 8000 / 26 = 1538` bit/s；这些是容器数据量覆盖的正向 DTS 区间，
不能混同 128/160 kbps 的 MPEG 头声明，也不能称为播放时长或实际音画同步。

`summary_check.py` 调用显式提供的本地 CLI，用清单中五项 ADR-016 汇总预算核对
独立事实，保存精确命令、stdout/stderr、退出码、可执行文件和样本 SHA-256：

```powershell
go build -mod=readonly -o 'C:\work\muxray-summary-check.exe' ./cmd/muxray
python -B tools/desktop-native/summary_check.py --fixtures-dir 'C:\work\summary-fixtures-new' --muxray 'C:\work\muxray-summary-check.exe' --output-dir 'C:\work\summary-cli-check-new'
```

损坏和截断文件的 CLI 退出码 2 为预期有诊断结果，仍必须通过数值断言。CLI 的
codec parser 默认预算与桌面不完全相同，但这些微小头均在两者限制之内；此工具
不证明 parser 预算边界，也不代替生产窗口/Wails IPC 验收。GUI 应逐页核对 retained
事件边界（最多 100 条），并验证事件跳回现有 Tag 详情/Hex 后 READY 概览仍保留。
超过 1 GiB 的物理文件沿用 MP3 生成器或已校验的大文件，再独立记录大小和哈希。

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
