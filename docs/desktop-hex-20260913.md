# MuxRay ADR-015 / Hex 桌面验收报告（2026-09-13）

**状态：本批 Windows 原生验收已完成；随后通过内置浏览器完成合成宿主 smoke 的 25 类场景。Chrome CDP 命令入口仍缺少调试连接，执行路径分别记录。**

本报告核对到原生证据编号 **127**，并核对 2026-09-13 03:03:48 UTC（北京时间 11:03:48）的最终进程记录。四类编码的字段联动、跨窗、诊断点、分页跳转、EOF、截断、源变化恢复、真实大文件末段 Tag 窗口和受控 Hex 关闭均有本批原生证据。

代码基线为 `2e41581a150dbd298cea582c34d498a5994d9cfb`，其上实现本批 [ADR-015](decisions/015-bounded-desktop-hex.md)、Hex 读取与界面改动，已提交为 `3128ebb`。验收前完成代码检查与构建；后续浏览器重试仅修正测试夹具和更新记录，没有改动已验收的生产代码或 dist。

本机证据根目录为 `C:/Users/swk01/Documents/Codex/2026-09-09/computer-use/`。下文 `work/` 和 `outputs/` 路径均相对此目录；未注明目录的构建/检查日志位于 `outputs/muxray-hex/`，原生 JSON 和截图位于其 `native-20260913-001/` 子目录。媒体、构建产物和截图保留本地，不纳入源码仓库。

## 产物与样本身份

生产程序：`C:/Users/swk01/go/src/MuxRay/desktop/build/bin/MuxRay.exe`，10,997,248 byte，SHA-256：

```text
51a45f756cb91a9c7ff2cde7c99a343bb089cd9541c3122901a2e29459be896f
```

`production-build.json` 与当前 exe 完全匹配；`wails-build.log` 记录 Wails 2.14.0 的 Windows production 构建、绑定生成、前端编译及应用编译成功。

受控程序：`work/hex-native/close-001/MuxRay-io-acceptance.exe`，SHA-256：

```text
7c47d48f98202be8412600e0f61c15b354c367aa1c8a8f40c7a7bdf56a89534d
```

该 run ID 为 `f3c8f7b4979e4fec80938f85fe91324b`；Hex gate 参数为 read 67、offset 13、已提交 32 Tag。原生验收前的独立哈希检查中，build-record 的 **119 个 Go 输入、24 个前端输入、3 个 dist 资产、9 个工具模板和 4 个产物均匹配当时冻结的文件**。这是注入阻塞 I/O 的专用验收程序，不能作为生产 exe 发布或称为自然慢盘实测。生产记录本身未保存完整独立输入清单；以上完整输入证明来自随后生成的受控构建清单，未进行可复现重构建。

小样本来自 `work/hex-fixtures-002/fixtures.json`：

| 文件 | 实际长度 | SHA-256 |
| --- | ---: | --- |
| hex-details.flv | 4,776 byte | `8cb7f27afd975364a859e5f9287807eca9c5b407b7709ab7cf74f4c50dd7f6c1` |
| hex-change.flv | 4,776 byte | `8cb7f27afd975364a859e5f9287807eca9c5b407b7709ab7cf74f4c50dd7f6c1` |
| hex-truncated.flv | 4,789 byte | `29d3a1de9e26248388d3c3dcd0def06b008be35b37e4a0332aebb4c79533910b` |

三文件验收前的长度与哈希已独立读回核对。fixture001 的两个预期元数据错误已在 fixture002 修正：AAC 字段名是 `initialAudioObjectType`；短 ASC 诊断位于 3282:0。两批 FLV 字节完全相同，旧证据保留。

## 已完成的生产原生流程

以下编号均指 `outputs/muxray-hex/native-20260913-001/` 内的 JSON 和同名截图。Pass 只覆盖表内明确描述的行为。

| 检查 | 观察结果 | 证据 | 状态 |
| --- | --- | --- | --- |
| 原生文件选择与索引 | 打开 hex-details.flv；106 Tag、1 条结构警告；首页起点 0、本页 100 条 | 002–007 | Pass |
| AAC 跨字节字段 | `samplingFrequencyIndex` 为 1095:5 / 4 bit；byte 1095 = 12 的 bit 5–7 与 byte 1096 = 10 的 bit 0 分别高亮 | 015、017 | Pass |
| AMF 长字段与跨窗 | `boundary/value` 保留 51:0 / 8192 bit；窗口依次为 [51,563)、[563,1075)、[1075,1587)。前两窗保留字符串高亮，第三窗不再高亮 | 020、022–025 | Pass |
| AVC 前置配置位置 | Tag 4 保留详情，来源 Tag ID = 2；点击 `sourceLengthSizeMinusOne` 定位 1121:6 / 2 bit，原始 byte = ff，bit 6–7 高亮 | 027、031 | Pass |
| 派生字段位置 | 已观察的 `sourceTagId`、`NALLengthSize` 显示“—”，没有源位置按钮 | 027、031 | Pass（已观察字段） |
| 未支持编码的原始字节 | Tag 5 明确拒绝详情；payload 链接仍显示 [1216,1728) 的 512 byte，保持实际/声明 2048 byte 的区别，可见字节为 00…FF 循环 | 034、036 | Pass |
| 语法诊断定位点 | `AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED` 定位 3282:0；只标记 bit 0 位置，不虚构范围。此处实际是后续 FLV PreviousTagSize 的 00 字节 | 039、041 | Pass |
| Tag ID 跨页跳转 | 输入 105 后切到起点 100、本页 6 条；选中 Tag 105，显示对应 MP3 详情，旧字段/Hex 不再残留 | 046–048 | Pass |
| MP3 字段与文件尾短窗 | `padding` 为 4770:6 / 1 bit，byte 92 的 bit 6 = 1；窗口 [4770,4776)，仅 6 byte：`92 c0 00 00 00 10`，后一窗口禁用 | 050–052 | Pass |
| 地址输入与空 EOF | 跳转 4776 显示 [4776,4776)、0 byte 和明确 EOF 空位置提示，无零填充字节 | 055–057 | Pass |
| 超出 EOF 拒绝 | 输入 4777 后显示“请求参数无效”；仍保留上一个合法 EOF 窗口，未产生越界字节 | 058–060 | Pass |
| 普通关闭的界面清理 | 关闭后返回空状态，详情、Hex、文件状态清空；程序仍运行时源文件可独占打开 | 061–063、details-close-handle.json | Pass |
| 容器截断详情拒绝 | hex-truncated.flv 有 107 Tag、2 条结构诊断；Tag 106 payload 声明 10、实际 2 byte，详情明确拒绝 | 065–069 | Pass |
| 截断 payload 原始字节 | Tag 106 原始窗口 [4787,4789)，仅 `af 00`，显示实际 2 / 声明 10，EOF=true，未补足缺失部分 | 070–071 | Pass |
| 结构诊断与 EOF 点 | 切换诊断页后旧详情/Hex 清空；`FLV_TAG_BODY_TRUNCATED` 在 4789:0 打开 [4789,4789) 空 EOF，只有位置提示 | 073–075 | Pass |
| 截断文件关闭 | 回到空状态，程序仍运行时源文件可独占打开 | 076–079、truncated-close-handle.json | Pass |
| 精确源变化锁存与恢复 | mtime 精确增加 2 秒后拒绝读取；恢复原始 UTC 时间后仍锁存；关闭、重开后 [13,525) 原始窗口正常 | source-change-002-*、095–104 | Pass |
| 真实大文件有限读取 | 1,082,131,440 byte、66 Tag；Tag 65 详情与原始窗口 [1073742823,1073743335)，512 byte，首部 `2f ff fb 90 00` | 110、114、116、large-final-close.json | Pass |
| 受控 Hex 关闭 | 第 67 次读取 offset 13 阻塞；Release 前 closing/32、视图清空且源仍占用；Release 后正常关闭、源可独占打开、界面空状态 | 122–127、controlled-before-release.json、controlled-acceptance.json | Pass |
| 进程和源释放 | 两次生产程序退出后涉及的 change/large 源可独占打开；受控源有 orderly Close 和独占检查；最终两类进程数均为 0 | source-change-final-close.json、large-final-close.json、controlled-acceptance.json、final-process-status.json | Pass（退出码未捕获） |

023、024 的 JSON 文本采样仍处于 loading，而紧随其后的截图已经是 ready；这两项以截图为终态证据，025 的稳定 UIA 补充确认第三窗。UIA、文档文本和截图并非同一时刻的原子快照。部分后续 JSON 不提供完整 document_text，仍保留 UIA tree 和截图。

UIA 字节按钮的可访问名称包含地址，不包含每个十六进制值。因此本表的原生核对证明窗口身份、可见字节和位高亮，不能称为在原生界面逐字核对了全部 512 byte。完整窗口内容比对另由下述非 GUI 证据支持。

## 已有自动检查与来源

| 检查 | 已有结果 | 证据与范围 |
| --- | --- | --- |
| 根 Go 模块 | Pass | `root-checks.json`、`root-checks-build.json`：CGO0 `go test -count=1 ./...`、vet、成功 build；CGO1 race -count=1 |
| desktop Go 模块 | Pass | `desktop-checks.json`、`desktop-checks-build.json`：同上 |
| 前端测试 | 82/82 Pass | 本次开发会话中 npm test 的工具结果；本输出目录没有独立 npm-test 日志，未为了补日志重复运行 |
| 前端类型与生产资源 | Pass | `controlled-build-001.log` 明确记录 tsc --noEmit 与 Vite build |
| Wails 绑定与生产构建 | Pass | `wails-build.log`；本轮完整构建后的相关文件哈希与受控清单一致 |
| Hex / MP3 / Scan 非 GUI gate | Pass | 三个 `work/hex-native/verify*/verification.json` 与 Go 日志：带 race 的真实文件生命周期检查；Hex/MP3 保留 32 Tag 后关闭，Scan 取消保留 10 Tag；释放后文件可独占打开 |
| PowerShell 工具 | Pass | `native-tools-checks-summary-001.json` 引用已完成工具输出；PS 5.1 / 7.6.5 自检、脚本语法、五种 gate 模式及收据身份检查 |
| Hex fixture | Pass | `hex-fixtures-tests-002.log`：4 项 Python 测试；`work/hex-audit-001/verification.json`：25 次真实 document/detailview 请求与 55 个完整窗口比对；fixture002 修正元数据有独立确认 |
| 浏览器 smoke | 内置浏览器场景 Pass；Chrome CDP 入口未执行 | 首次代理授权超时退出 1 的历史保留；随后以内置浏览器完成同一组 25 类场景、44 条值断言，详见追加验收 |

最初 root/desktop 的 build 因 Git VCS stamping 失败，原始失败日志保留；后续 `*-checks-build.json` 记录重试 exit 0，不能把第一次执行改写为成功。PowerShell 汇总也明确说明其 transcript 漏掉子进程输出，结论来源为对应工具执行结果。

已有测试覆盖单 worker、详情/Hex 共用递增请求 ID、迟到响应隔离、丢失 Start 的同 ID 确认、有限轮询、短读不发布部分字节、4096 byte 上限、源变化锁存和阻塞关闭。自动测试与原生观察各自证明对应范围；没有把非 GUI 检查写成原生交互。大于 2^53 的坐标属于合成传输/算术测试，没有打开该大小的物理文件。

## 生命周期与大文件的完成证据

### 源变化锁存、精确恢复和重开

hex-change.flv 原始 mtime 为 `2026-09-11T16:07:34.7382635Z`。第二轮记录 `source-change-002-mutated.json` 将它精确增加 2 秒至 `16:07:36.7382635Z`，095–096 显示读取被 source_changed 拒绝。`source-change-002-restored.json` 精确恢复原值并核对 SHA-256 不变；097 的截图及 UIA 仍显示源变化错误，详情/Hex 清空，Tag 与偏移操作禁用。

099–100 关闭后为空状态；101–104 经原生选择器重新打开，Tag 0 的 Hex 窗口 [13,525) 正常，首字节 12、512 byte，没有残留源变化错误。`source-change-final-close.json` 再次确认原始精确 mtime、内容 SHA-256、退出后源可独占打开和生产进程数 0。该场景测试可观察 mtime 变化，不声称覆盖所有同大小、同时间戳的内容突变。

第一轮 `source-change-mutated.json` / `source-change-restored.json` 受验收脚本 JSON 日期转换影响，产生 8 小时时差并丢失亚秒精度，不能作为“精确增加 2 秒并恢复”的证据。`source-change-restored-exact.json` 更正后重新进行了上述第二轮；所有历史记录保留，没有覆盖。第一轮仍观察到源变化拒绝，但精确恢复和锁存结论以第二轮及 097–104 为准。

### 真实大文件的尾 Tag 窗口

本轮复用 `work/mp3-fixtures-001/mp3-large.flv`。实际总长 **1,082,131,440 byte**；最终读回 SHA-256 为：

```text
d5da1ed883197c2f7e560020010997ec0f895ca6ce33c31c9a5c24f33e7c825c
```

110 确认扫描结束、66 Tag、无结构诊断；113–114 通过 Tag ID 65 取得 MP3 详情。尾 Tag header offset 为 1,073,742,812，本次点击的是其 MP3 audio 源范围，116 的实际窗口为 **[1,073,742,823, 1,073,743,335)**，512 byte，首部 `2f ff fb 90 00`，后续可见填充字节为 00。详情的完整声明范围保留到 1,082,131,436，界面只高亮当前实际窗口。

`large-final-close.json` 记录退出后源可独占打开、生产进程数 0，以及上列实际长度和 SHA-256。本次证明大文件末段 Tag 的有限读取和精确定位；该窗口没有到达 EOF，不是完整 payload 读取吞吐或音频解码验收。

### 受控 HexClose 与源句柄所有权

close-001 原生 run 为 `f3c8f7b4979e4fec80938f85fe91324b`，PID **16144**，processToken `16144-1789268389557066200`。`controlled-before-read.json` 确认首次原始读取前没有 entered/closed；entered 收据准确匹配 **Hex / read 67 / offset 13 / committedTags 32**。仅打开文件完成扫描不会触发此 gate。

阻塞时界面最终出现有限等待超时提示，未假报字节读取成功。MarkClose 后，125 截图以及 **126 的稳定截图和 UIA** 确认“正在关闭”、32 Tag、页面与 Hex 结果清空，打开、跳转和关闭按钮禁用。125 的 UIA 仍保留前一采样状态，不能单独用其 JSON 代替 126 的稳定观察。`controlled-before-release.json` 在 Release 前记录 closed=false、release=false，文件独占打开失败，证明底层读仍持有源。

收据独立核对为相同 run、PID、token、gateMode 和 exe 路径，UTC 顺序如下：

| 收据 | UTC 时间 |
| --- | --- |
| entered | 2026-09-13T03:01:42.5068059Z |
| close-command | 2026-09-13T03:02:28.3467999Z |
| release | 2026-09-13T03:03:08.2164682Z |
| released | 2026-09-13T03:03:08.2553114Z |
| closed | 2026-09-13T03:03:08.2559100Z |

127 确认 Release 后回到空状态；`controlled-acceptance.json` 与 run 内 `acceptance.json` 的 HexClose 检查通过，orderlySourceClose=true、exclusiveSourceOpen=true。MarkClose 是操作意图时间，不是精确点击时间；本报告不把上述人工控制间隔作为关闭性能数字。

### 最终进程状态与交付边界

`final-process-status.json` 于 2026-09-13T03:03:48.5982843Z 确认生产与受控进程数均为 0。该记录未捕获退出码，因此仅声明进程已经退出，**退出码未知**。源变化副本的原始时间及哈希已恢复；change/large 文件有退出后独占检查，受控文件有释放 gate 后、进程仍可显示空状态时的 orderly Close 和独占检查，二者结论不混淆。

`desktop/desktop.exe` 这一 go build 临时产物已清理；Wails 生产程序仍保留在 `desktop/build/bin/` 供本地使用。源码提交包含 ADR、实现、测试、可复现验收工具及本文；最终提交 ID 和工作区检查结果记录在本次开发会话及本机 `outputs/muxray-hex/final-delivery.json`，避免报告自引用提交哈希。

## 浏览器 smoke 追加验收

2026-09-13 03:27:31 UTC（北京时间 11:27:31），在 **Codex 内置浏览器、1280 × 720 viewport** 完成原 smoke 的 25 类场景，**44 条值断言通过，产品断言失败为 0**。保留 30 份编号 DOM 快照和跨字节高亮截图；采集的浏览器 error/warn 日志为空。测试页和本地测试服务均已关闭。

本次先重试 web-access 的连接检查：沙箱与 Windows 宿主两次均未发现 Chrome 调试连接。随后使用可用的内置浏览器和受支持的页面操作 API。`browser-retry-server.mjs` 从仓库 `browser-smoke.mjs` 提取同一个 `syntheticHost`，在回环地址托管既有 dist，沿用生产 CSP，并仅为测试页加入外部宿主脚本、场景选择器和可见读取日志。点击、输入、场景选择和只读 DOM 断言由内置浏览器接口执行。**没有运行原 Chrome CDP 的 `npm run test:browser` 命令，不能将本次记成该命令 exit 0。**

生产 JS/CSS 和原始 index.html 的 SHA-256 已在 `browser-retry-server.json` 核对记录；JS 为 `de937cc14adb804baca7b20d107aeb89d6ea978e2967f0969df365512713fb1c`，CSS 为 `62f4539a32ffcae2b45cfd5c3902a93190ddc27cdfbd3faf7435ea58d67c4e27`。测试页面在服务器内存中插入宿主控制，未改动磁盘上的 dist。

| 场景组 | 已验证行为 | DOM 证据编号 |
| --- | --- | --- |
| 基础界面与分页 | 空状态、100/100/5 分页、页尾禁用、精确大整数、部分结果、文件名及诊断文本转义 | 01–05 |
| 四类详情 | AMF0/AAC/AVC/MP3 内容和范围说明、预览转义、byte/bit 位置、AVC 派生字段无伪造 Span、缺配置及预算诊断、MP3 free-format/损坏头/未支持音频清理旧结果 | 06–15 |
| Hex 联动 | 未支持 Tag 只发起 Hex、512 byte 窗口、切 Tag 清理、跨窗范围保留、跨字节两侧精确位高亮、诊断仅为点、无可定位 Span 的派生字段无按钮、空 EOF、无窗口级横向溢出 | 16–23 |
| 跳转与生命周期 | Tag 105 定位起点 100 的一页、源变化同时清空详情和 Hex、关闭、取消扫描后保留有界页、取消选择返回空状态 | 24–30 |

重试前修正了 smoke 自身的三处问题：非空 Span 的起点从 UINT64_MAX 改到 UINT64_MAX−1，使排他末端合法；cross-byte 合成值从 7 改为与 `00 01` 所选位一致的 0；诊断等待条件增加读取终态和单个定位点，避免只看到新标签就抢先断言。`node --check test/browser-smoke.mjs` 和差异检查通过。这些修正没有改变生产实现。

逐项证据 `browser-retry-checks.json` 保留一次断言工具问题：跨 JavaScript realm 的数组用 deepStrictEqual 比较时失败，实际值与期望值的 JSON 完全相同；改为 JSON 值比较后重新通过，原记录以 `harness-assertion-error` 标识，未删除。`browser-retry-result.json` 汇总场景、44 条通过断言和限制，`browser-retry-01-*.txt` 至 `browser-retry-30-*.txt` 为交互状态，`browser-retry-cross-byte.png` 为画面，`browser-retry-cleanup.json` 记录服务停止。

此处是合成宿主的前端浏览器验证，不调用 Go 解析器、真实文件 I/O 或原生选择器；UINT64_MAX 文件大小与 EOF 仅用于合成传输/算术边界，真实 Go 文件仍受 int64 大小限制。Windows 文件生命周期的通过结论来自本文前面的独立原生证据。追加提交及证据清单见本机 `browser-retry-delivery.json`。

本批范围是本机只读、按需 Hex 与字段/诊断定位，不包含编辑、全文搜索、导出、媒体摘要、音视频解码、安装包或其他桌面平台验收。
