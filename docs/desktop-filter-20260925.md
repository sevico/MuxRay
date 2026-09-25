# 桌面 Tag 筛选与时间定位：2026-09-25 验证记录

## 状态

ADR-017 的自动检查、生产构建及下列浏览器交互检查已通过。Windows
生产程序的七个样本均完成了下述原生用例：混合样本联动、稀疏查询、
100/53 条结果分页、结构状态区分、截断载荷 Hex、空索引、时间范围与
uint32 上界，以及 Tag ID 跳转退出筛选。生产阶段记录截至 `121`
（2026-09-25 02:27:46.328 UTC），最后已关闭文件回到空状态。
独立受控 SummaryRecovery 也已完成真实 GUI 操作和凭证校验：同一文件会话
取消后可重新分析全部 32 Tag，随后关闭释放源。此次约定范围的验收已完成。

## 功能范围

类型、包含两端的原始 DTS 区间、结构状态按 AND 组合，查询扫描终态的
已提交索引。每次最多访问 4096 个索引记录、返回 100 个命中，保留全局
Tag ID 和源顺序。继续位置独立于命中数量；空页可能仍有后续待查记录。
PreviousTagSize 缺失与存在但不一致分别查询。部分索引不代表整文件。

查询不读 payload、不调用 Stat、不重扫文件，也不启动第二个读取 worker。
已观测到的源变化锁存后拒绝查询；后续详情和 Hex 使用现有源检查。
筛选命中项可打开详情与 Hex，已完成 Summary 保留。原有 CLI 合同不变。

## 证据

本地证据目录：
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-filter`。

浏览器合成宿主用于前端交互与故障注入；生产 Windows 可执行程序用于
真实文件选择、Wails 查询、详情及 Hex；Summary gate 仅用于受控阻塞
取消恢复，不能称为生产程序的自然慢盘观测。

## 自动验证

以下日志均位于上述证据目录。无输出的命令已从执行结果确认退出码为 0。

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| 根模块 Go 全量测试、race | 全部 16 个包通过 | `root-test.log`、`root-race.log` |
| desktop 模块 Go 全量测试、race | 通过 | `desktop-test-final.log`、`desktop-race.log` |
| 两模块 `go vet ./...` | 退出 0，无诊断 | `root-vet.log`、`desktop-vet.log`（空日志） |
| `CGO_ENABLED=0 go build ./...` | 退出 0 | `pure-go-build.log`（空日志） |
| 前端 `npm test` | 107/107 通过，无跳过 | `frontend-test.log` |
| JSON schema 与负例合同检查 | 通过，包含原有 CLI v1/v2 合同 | `contracts.log` |
| Python 离线工具测试 | 14 项：13 通过、1 跳过 | `offline-tools.log` |
| fixture 与真实 CLI 索引交叉核对 | 7 个文件、8651 条 Tag 全部一致 | `fixture-cli-check.json` |
| 最终验收工具自测 | 合成宿主 8/8、Python 样本 14/14；SummaryRecovery 与既有 gate 在 PowerShell 5.1/7 均通过 | `tool-validation-final.log` |
| Wails production 构建 | bindings、前端类型检查/编译、Go 编译成功 | `native-build.log` |

新增 Go 测试覆盖跨块稀疏命中、4096 次访问预算、100 条命中上限、空页
继续、DTS 包含端点及重复/回退、三类结构条件、取消后的部分索引和关闭
隔离。计数源验证查询不调用 `ReadAt` 或 `Stat`，且不会取消正在阻塞读取的
Summary；已就绪 Summary 保留，源变化锁存后拒绝查询。最大宽度字段组成的
100 条 Tag 响应仍小于 256 KiB。

前端测试覆盖 BigInt 坐标、空页显式继续、真实游标前后翻页、256 页历史
上限、无效条件不覆盖旧结果、快速改条件、超时重试、关闭/重开/源变化后的
旧回复隔离，以及筛选结果进入详情和 Hex、保留 Summary、精确 Tag 跳转退出
筛选。desktop host 测试还通过真实绑定完成了稀疏查询、AAC 详情、EOF Hex
及关闭重开；它没有驱动 Windows 窗口，不能代替原生 GUI 验收。

保留的 `desktop-test.log` 是测试文件编写途中缺少 `waitNativeHex` 辅助函数
导致的编译失败。补齐辅助函数后，最终完整测试及 race 均通过，最终结果以
`desktop-test-final.log`、`desktop-race.log` 为准。

CLI fixture 检查验证解析器输出与已知二进制事实，不验证 Wails 筛选接口。
结构损坏与截断 fixture 返回预期退出码 2，其他 fixture 返回 0。
最终 `fixtures-002` 的逐 Tag CLI 核对另存 `cli-check-001/check.json`。
工具日志保留了两次不存在的 PowerShell 安装路径调用；使用实际 bundled
PowerShell 路径后的两套自测均成功。这不构成产品运行失败。

## 生产构建身份

`build-identity.json` 于 2026-09-25 09:22:02 +08:00 记录：

- 路径：`C:\Users\swk01\go\src\MuxRay\desktop\build\bin\MuxRay.exe`。
- 平台：Windows amd64；Wails 2.14.0 production，未启用 devtools。
- 大小：11,281,408 byte。
- SHA-256：`b32bcb13a85cc9de17629fc18d0f6ff63ae03c6a7f55518126019a37bae895e2`。
- 前端资源：`index-BbtavzW2.js`、`index-Chni0iWk.css`。

这是本地预览构建，不是签名安装包。受控 Summary gate 已另行构建，
其身份保存在 `summary-gate-build.log`，不与生产二进制混用。

验收结束后的 `final-build-input-check.json` 确认 gate 构建记录中的 136 个 Go
输入、28 个前端输入和 3 个编译后资源仍与当前文件逐一匹配；生产可执行文件
SHA-256 也保持上述值。验收后仅补充说明文档，没有修改产品实现。

## 浏览器交互

使用 Codex 内置浏览器加载 production 前端资源，经真实页面控件操作
`browser-host.mjs` 合成宿主。以下断言均根据可见 DOM 验证，不读取应用内部状态。
它们验证前端交互，不能证明 Go 解析器或原生 Wails 链路通过。

| 场景 | 实际观察 | 证据 |
| --- | --- | --- |
| 稀疏音频 | 首次 `[0,4096)` 无命中但可继续；随后命中 4500/5000，再命中 8200；上一页恢复真实源游标 | `browser-sparse-final.txt` |
| DTS 包含端点与回退 | 10000–12000 命中 0/1/2/3/6/7/8/11，保持源顺序；反向区间显示输入错误 | `browser-range-inclusive.txt` |
| 分页上限 | 305 条索引中 153 条音频，第一页 100 条 `[0,199)`，下一页 53 条 `[199,305)` | `browser-multipage.txt` |
| 超时重试 | 首次查询不返回；5 秒后显示重试；重试恢复 12 条结果并清除通信错误 | `browser-timeout-retry.txt` |
| 关闭隔离 | 查询延迟 1.8 秒期间关闭，迟到响应后仍为空状态 | `browser-close-late.txt` |
| Summary → 筛选 → MP3 → Hex | 已完成的 305 Tag 概览保留；音频 DTS=12000 命中 51 条，Tag 6 详情及字段 Hex 保持筛选页 | `browser-filter-detail-hex-summary.txt` |
| 精确 Tag 跳转 | 跳转 Tag 7 退出筛选，概览保留 | `browser-jump-clears-filter.txt` |
| PreviousTagSize 状态 | 不一致只命中 1/3；缺失只命中 6，互不混淆 | `browser-structure-states.txt` |
| Payload 截断 | 仅命中 Tag 1，显示 2/10 byte、截断及部分索引提示 | `browser-truncated.txt`、`browser-truncated.png` |
| 清除、空索引 | 清除后恢复全部 Tag；空索引为 `[0,0)` 且禁止下一页 | `browser-empty-reset.txt` |
| 无匹配与 uint32 上界 | DTS=13000 无命中；其他类型且 DTS=4294967295 精确命中 Tag 9 | `browser-no-match-max-dts.txt` |

大于 JavaScript 安全整数的偏移 `9007199254740995` 在字段定位、Hex 窗口和
位选中范围中保持精确。1265 px 浏览器视口检查 document 宽度与 scrollWidth
一致，筛选表单和结果截图无横向页面溢出。

首次稀疏查询曾暴露合成宿主把可选 DTS 的 `undefined` 直接交给 `BigInt`
的问题，已改为先归一化为 null；相关首次失败记录保留在
`browser-sparse-first-failure.txt`。宿主 Summary 的计数和事件也已改为与
筛选数据集一致。宿主自测 8/8 通过，重启宿主后执行上述对应交互检查；
这些修复没有修改 production 产品代码。

仓库的 `browser-smoke.mjs` 已扩展场景并通过语法检查；本次没有运行该脚本，
上述交互通过内置浏览器客户端执行，不能把它们写成该脚本的执行结果。

## Windows 原生验收

以下结果来自本次 production `MuxRay.exe`、真实 Windows 文件选择器和
Wails 链路，使用 `fixtures-002` 样本。表中证据是本地目录内的准确前缀，
对应完整 JSON 与截图；断言使用动作后稳定的可访问性树及可见截图，
不把文件名中的 `ready` 或点击已分发当作实际完成。

| 场景 | 稳定观察 | 证据前缀 |
| --- | --- | --- |
| 混合样本概览 | 497 byte、20 Tag；覆盖范围内分析完成，音频/视频/脚本/其他 12/6/1/1，AAC/MP3 7/5、编码切换 2 | `013-summary-after-dispatch` |
| 重复 DTS 筛选 | 音频且 DTS=20 仅显示 Tag 4/6；检查 `[0,20)`、命中 2、已索引 20，结果保留全局 ID | `024-audio-20-result` |
| 筛选 → AAC 字段 → Hex | 打开 Tag 4 AAC 详情；字段选择 `[146,150)`、32 bit，实际 Hex 窗口 `[146,497)`、351 byte、EOF；筛选与已完成 Summary 保留 | `026-aac-detail-ready`、`028-aac-hex-ready` |
| MP3 范围与回退 | 音频 DTS 100–152 命中 Tag 12/13/14/15/16，DTS 为 100/126/152/152/140，包含端点、重复及回退后命中 | `033-mp3-filter-result` |
| 筛选 → MP3 → Hex | Tag 14 MP3 详情；选择范围 `[385,401)`、128 bit，实际 Hex 窗口 `[385,497)`、112 byte、EOF；5 条结果与 Summary 保留 | `036-mp3-detail-ready`、`038-mp3-hex-ready` |
| 错误草稿 | 草稿 153–152 被拒绝，已应用条件仍为音频 100–152；原 5 条结果、MP3 详情与 Hex 窗口保持 | `042-invalid-range-retained` |
| Summary 跳转退出筛选 | 从声明关键帧事件跳转 Tag 11，恢复全部 20 条普通索引、清空筛选区间和错误草稿，打开 Tag 11 AVC 详情；Summary 保留，旧 Hex 实际窗口清空 | `044-summary-jump-cleared` |
| 关闭混合样本 | 回到“从一个文件开始”的空状态，可再次打开文件；原筛选、详情、Summary 和 Hex 结果区域消失 | `048-mixed-closed-empty` |
| 稀疏查询与空页继续 | 8305 Tag：音频首窗 `[0,4096)` 为 0 条但“继续查找”可用；下一窗 `[4096,8192)` 命中 4500/5000；末窗 `[8192,8305)` 命中 8200，下一页禁用 | `058-sparse-empty-window`、`060-sparse-middle-result`、`062-sparse-last-result` |
| 稀疏上一页 | 从末窗返回 `[4096,8192)`，重新显示 4500/5000，使用真实扫描游标 | `064-sparse-previous-result` |
| 命中上限与扫描游标 | 305 Tag：音频第一页 100 条为偶数 ID 0..198，检查 `[0,199)`；第二页 53 条为偶数 ID 200..304，检查 `[199,305)`，下一页禁用 | `073-multipage-first-result`、`074-multipage-first-visible`、`075-multipage-second-result` |
| PreviousTagSize 不一致 | 7 条已保留索引中只命中 Tag 1/3，均显示 `99 ≠ 13`；检查 `[0,7)`，保留部分索引提示 | `083-structure-invalid-result` |
| PreviousTagSize 缺失 | 改为缺失条件后只命中 Tag 6；没有把缺失归入存在但不一致；仍提示仅覆盖已保留 Tag | `087-structure-missing-result`、`088-structure-missing-visible` |
| 截断结果及实际字节 | 41 byte、2 Tag，Payload 截断只命中 Tag 1，实际/声明为 2/10 byte；payload 范围与 Hex 实际窗口均为 `[39,41)`，仅 `AF 00` 两字节、EOF | `096-truncated-result`、`097-truncated-visible`、`098-truncated-payload-hex` |
| 空索引与清除筛选 | `filter-empty.flv` 应用条件后为 `[0,0)`、0 命中、已索引 0，明确已到末尾且翻页禁用；清除后恢复“当前显示全部 Tag”“当前索引没有 Tag”，清除按钮禁用 | `103-empty-filter-result`、`104-empty-filter-visible`、`106-empty-filter-cleared` |
| 专用范围样本 | `filter-ranges.flv` 全部类型、DTS 10000–12000，精确命中 0/1/2/3/6/7/8/11；检查 `[0,12)`，8 命中，保留包含端点、重复及回退后的匹配 | `112-ranges-inclusive-result` |
| 原始 DTS 上界 | 全部类型、DTS 起止均为 4294967295，仅命中 Tag 9；区间输入、应用条件及原始时间戳保持完整十进制值 | `115-ranges-uint32-result` |
| 确认无匹配 | DTS 起止均为 13000，检查完 `[0,12)` 后 0 命中、明确到索引末尾、下一页禁用 | `118-ranges-no-match-result` |
| Tag ID 输入跳转 | 从无匹配筛选输入 Tag ID 0 并跳转，清空筛选区间、恢复全部 12 条索引，出现 Tag 0 详情区域 | `120-ranges-tag-id-jump` |
| 生产阶段最后关闭 | 关闭 ranges 文件后回空状态，仅保留打开/选择文件入口，无旧查询结果或详情 | `121-production-final-empty` |

分页记录的完整 UI 树逐 ID 核对了第一页全部 100 个偶数 ID 和第二页全部
53 个偶数 ID，不只比较首尾行。截断 Hex 的两字节值另由 `098` 截图确认；
缺少的 8 byte 没有显示成真实源字节。相关稳定记录的 `window.app` 均指向
上述 production 路径；现场窗口 ID 为 1641300，它不是进程 PID。

七个生产样本分别为 `summary-mixed.flv`、`filter-sparse.flv`、
`filter-multipage.flv`、`filter-structures.flv`、`filter-truncated.flv`、
`filter-empty.flv`、`filter-ranges.flv`。各样本通过范围以上表为准，
不表示穷举了所有筛选组合或每个编解码字段。

### 操作与证据限制

最初 `001`–`009` 的文件名输入控件操作没有成功，用户随后选择了混合样本。
之后用系统文件选择器独立的 `screenshot-1` 定位列表项并双击打开稀疏样本
（`051`–`053`），再继续切换后续样本；文件选择器阻碍已经解除。

`010-user-opened-0.png` 实际画面是 Codex 窗口，虽然对应 JSON 的窗口身份
指向 MuxRay，也不能把这张图片作为产品视觉证据。原始记录保留；后续
将生产窗口置前并使用稳定结果截图。`012-mixed-summary-ready` 当次尚未
显示完成概览，Summary 完成断言采用 `013`；其他点击后立即捕获的过渡
状态同样不代替表中的稳定证据。

早期扩展名为 `.png` 的截图可能实际是 JPEG/JFIF；已检查 `010`、`011`、
`013` 和 `browser-truncated.png` 的字节签名均为 `FF D8 FF E0`。后续原生
证据按实际格式保存 `.jpg`，既有文件保持原字节与路径并注明格式错配。
截图时间使用 JSON 的 ISO `capturedAt`，不使用文件修改时间推断动作顺序。
部分 `document_text` 只返回焦点输入框值，完整结果须结合 `accessibility.tree`
与截图读取，不能把输入值误作整页文本。

未在原生混合样本中执行视频 30–80ms、script/other 类型筛选。
退出筛选分别由混合样本的 Summary → Tag 11 和 ranges 样本的
Tag ID 输入 → Tag 0 验证；不能把后一项写成在保留已完成 Summary 的
同一混合会话内执行了 Tag ID 输入跳转。
AAC 已测字段位置 146，不能写成从 Tag 头偏移 135 开始的原始 Tag 读取。
AVC 已观察 Tag 11 详情，不能写成视频筛选后打开 Tag 10 并读取 Hex。
这些范围不能用浏览器或单元测试结果冒充原生通过。

## 受控 Summary 取消与重新分析

使用独立的 `work/filter-native/summary-recovery-001` 运行目录，目录位于
上述本地工作区，与 `outputs` 同级。构建仅通过既有 overlay 阻塞真实源读取，
使用同版本产品代码及前端资源。生产程序在此之前正常退出。

- Run ID：`eb520878d8ff4a74b553c962721d2f05`；PID：`27520`。
- Process token：`27520-1790303292380507200`。
- gate 可执行文件 SHA-256：`d4f0e6b0c565ddcbe7a4dea52bcad9116cb4347abce21d39ba6a71dc0657a93f`。
- `native-controlled.flv`：497 byte、32 Tag；SHA-256：`16346b21c3e84be9a1d398f330a0023b5ba3b5df5a5c74d8756b5cda8f1e4437`。
- `entered.json` 确认 Summary 模式在第 67 次读取、offset 24 阻塞，已提交 32 Tag。

通过原生文件选择器打开该样本，索引完成后点击“分析媒体”。进入阻塞读取后，
先记录 `MarkSummaryCancel`，再点击实际界面的“取消媒体分析”；观察到等待取消
才释放 gate。随后直接点击“重试媒体分析”，全程没有关闭或重开文件。

| GUI 状态 | UTC 时间 | 实际观察 | 证据前缀 |
| --- | --- | --- | --- |
| canceling | 02:33:46.165 | 0/32 Tag；“取消尚未确认完成；正在等待底层读取结束”；重新分析及筛选禁用，关闭文件可用 | `131-recovery-canceling` |
| canceled | 02:33:59.196 | “任务已取消”；32 Tag 索引仍在，同一文件可重试，筛选控件恢复 | `132-recovery-canceled` |
| ready | 02:34:08.715 | “索引完整”“覆盖范围内分析完成”“覆盖 32 Tag”；音频/视频/脚本/其他 1/0/31/0 | `133-recovery-ready` |
| empty | 02:34:16.993 | 关闭后回空状态，仅保留打开/选择文件入口 | `134-recovery-empty` |

`summary-recovery-observation.json` 记录上述现场观察；ready 的 processed/total/
indexedTags=32 依据“覆盖 32 Tag、覆盖范围内分析完成”及原索引 32 Tag，不把它
写成界面另有一个 32/32 进度条。`check-summary-recovery.ps1` 于
02:34:42.597 UTC 通过，运行目录的 `acceptance.json` 确认 `passed`、
`orderlySourceClose`、`exclusiveSourceOpen` 均为 true，并记录每份证据的 SHA-256。

校验器核对同一 run/process、构建及样本哈希、严格的 entered → cancel marker →
canceling UI → release → released → canceled UI → ready UI → closed → empty UI
顺序与源独占重开。它不识别截图语义；GUI 状态来自本次实际操作后的截图与
完整可访问性树。恢复成功不依赖重建文件会话，也不意味着可以强制中断系统 I/O。
