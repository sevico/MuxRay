# Win32k ETW 采集与对象校准：2026-10-03

独立采集器已通过 Windows 提权。后续 `run-etw-004` 完成普通权限 MuxRay 的真实文件选择、180 秒跟踪、六类对象校准与正常退出，重现 `260 → 262`，选择约 160 秒后仍为 262。**当前已取得对象候选，尚未确认分配调用栈或完成修复。** 下文保留早期失败尝试，再记录本次有效实验。

接续 [调用边界验证](desktop-gdi-boundaries-20261003.md)。外部证据根为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-boundaries-20261003`；下列证据名均相对此目录。原始 ETL、解码 XML、实验入口和采样不纳入仓库。

## 权限与准备

- 用户明确授权自动操作管理员终端，但桌面工具未返回该高权限窗口，未执行终端输入注入。实际使用 Windows `ShellExecute runas` 启动独立采集器，由用户处理 UAC。
- 采集器记录管理员权限为 true、PowerShell 7.6.5；目标 PID `11060`、创建 FILETIME `134354680416590232`，只读 TokenElevation 检查为 false。
- 目标仍是前轮完整 picker-only 诊断 EXE，SHA-256 `e1f638812ab786ea5dad59354e37f418fae1e5818c93efdf2f71c59b324f749f`，真实选择器调用保留，媒体会话打开被跳过。生产代码及生产构建没有修改。
- 控制入口 002 在调用采集器前因 UTC 转换错误拒绝启动：JSON 已解析成 UTC DateTime，再转字符串重解析导致约 8 小时偏差。入口 003 修复类型分支并通过四种时间输入验证；原失败收据保留，此时未创建 ETW 会话。

## 已完成的跟踪与解码

`run-etw-002` / `etw-traces/full-picker-elevated-002`：

| 项目 | 实际结果 |
| --- | --- |
| 提供程序 | Microsoft-Windows-Win32k，关键字 `0x0000010000000000`，level 4 |
| 采集 | 名义 150 秒；开始 `02:41:32.7096521 UTC`，停止 `02:44:02.7880600 UTC` |
| 停止 | 精确会话与 ETL 路径核对后 Stop 返回 0；独立 watchdog 保留 |
| 丢失计数 | eventsLost / logBuffersLost / realTimeBuffersLost 均为 0 |
| ETL | 48,562,176 字节，未接近 128 MiB 上限 |
| ETL SHA-256 | `f7fe2bee37417fce0c4fe5ea5c603ca3a51a95c75b934feaa89aecde115d860d` |
| 离线解码 | tracerpt 退出码 0，原始 ETL 保留；事件 XML 为 246,918,263 字节 |

Win32k 四类事件字段齐全；以下是事件条数，**不是对象存留数量**。所有者使用 payload `OwnerProcessId`，不以事件头 PID 代替。

| 事件 | 全局 | OwnerProcessId = 11060 |
| --- | ---: | ---: |
| Create 455 | 96,444 | 67,333 |
| Destroy 456 | 95,842 | 60,464 |
| UpdateOwner 457 | 18,347 | 3,032 |
| Transform 458 | 1,832 | 1,615 |

目标出现数字类型 `1、4、5、7、10、16`，当前没有经过实测校准的名称映射。不能用 Create 减 Destroy 计算泄漏；所有者转移、句柄转换、复用及采集开始前的未知前驱均需纳入分析。

解码还存在两项限制：XML 的 SystemTime 时区显示 `+07:59`，与 header FILETIME 锚点相差一分钟，尚未校准 GUI 时间对齐；另有一条 SystemTrace/PartitionInfoExtension 的 ProcessingErrorData。Win32k 455–458 必需字段未缺失，但这些限制不应省略。

## 原生操作与退出

取消预热后 GDI 为 260。有效跟踪期间成功打开选择器，随后双击输入被桌面工具以 `element 212 is not available in cached app state` 拒绝。重新观察仍为打开的对话框；剩余时间不足以按原门槛重试，因此没有宣称成功选择。

`gui-after-selection.json` 名称沿用了预定流程，实际记录的是失败输入后仍打开的对话框（GDI 359 / USER 211），**不是成功选择后的计数**。更正见 `selection-attempt-failed.json`。取消操作发生在 ETW 停止之后；之后 GDI 回到 260。此记录仅验证采集及解码通道，不能解释前轮逐次 +2。

原资源探针保存 601 样本，按 600 秒上限结束，未观察到退出。等待提权后另登记补充探针：241 样本，覆盖选择尝试、跟踪结束及正常 Alt+F4 退出，根进程与六个已观察 WebView2 后代均退出码 0。两段原始记录分别保留，不拼接成一段完整稳定性验收。

补充原始采样的最大相邻单调时间间隔为 1.001054 秒。独立复核见 `run-etw-002/final-etw-review.json`，SHA-256 为 `38e75b2a9a43572e3c9e553293f377fba3ebf427ccf34fe352c947bd22018fb3`。

随后准备 `run-etw-003` 用于重新观察坐标后选择及对象校准；Windows 提权返回“操作已被用户取消”。没有创建该轮管理员控制器目录或 ETW 会话，也没有成功选源。准备中的 PID `30752` 已正常 Alt+F4 退出；仅用于退出复核的 51 样本确认根进程与六个观察到的后代均退出码 0。

最终精确会话查询返回 4201（会话不存在）；本轮采集器、watchdog、资源探针均已结束，MuxRay 进程数为 0。收据见 `run-etw-002/final-cleanup.json`。

## 前轮证据与当时剩余工作

- `run-etw-002/`：身份、普通权限证明、两个控制入口收据、GUI 输入失败及取消、资源原始记录、正常退出。
- `etw-traces/full-picker-elevated-002/receipt.json`、`decoded/decode-receipt.json`、`decoded/preliminary-event-counts.json`：成功停止、ETL 哈希、解码与字段统计。
- `run-etw-003/elevation-dispatch.json`、`exit-resources/summary.json`：提权取消与准备进程清理。
- 在 run2/run3 结束时，`calibrate-gdi-types.py` 已准备并通过语法检查，尚未执行。它在独立普通权限进程中创建并释放六类已知 GDI 对象，记录实际句柄、GetObjectType、时间和释放结果；这些观测才可用于校准 ETW 数字类型。

当时剩余步骤是重新取得一次覆盖真实成功选择及静置期的跟踪，在同一次有效采集中运行对象校准，再依据全局所有者与句柄生命周期核对新增候选。以下 run4 完成了该采集；前轮未完成操作的记录继续保留。

## Run4：成功选择、静置与完整采集

新目标 PID `27776`，创建 FILETIME `134354711965021410`，使用同一完整 picker-only EXE。只读令牌检查确认 TokenElevated=false、limited token、medium integrity；仅控制器与采集器具有管理员权限。资源探针在 UAC 成功后启动，避免等待提权消耗采样时限。

桌面工具没有单独返回选择器窗口，但主窗口状态包含独立的选择器截图。实际操作使用该次新截图 `screenshot-1` 中已观察的文件行坐标 `(303,175)` 双击，未再使用失效的 AX 索引。输入在 `03:29:00.772–03:29:01.010 UTC` 完成，随后截图确认选择器关闭、诊断空页面恢复。该诊断版有意跳过媒体打开；空页面不能单独证明选择成功，须连同输入和文件行截图读取。

| 见证 | GDI | USER | 时间 UTC |
| --- | ---: | ---: | --- |
| 初始 | 118 | 84 | 03:26:33.715 |
| 取消预热并静置至少 30 秒 | 260 | 107 | 03:28:12.049 |
| 成功选择后 | 262 | 110 | 03:29:26.270 |
| 选择后约 80 秒 | 262 | 106 | 03:30:21.084 |
| 选择后约 160 秒、跟踪已停止 | 262 | 102 | 03:31:41.269 |

这是离散计数见证，不替代前轮连续固定窗，也不单独构成新的三轮增长斜率实验。

| 采集项目 | 结果 |
| --- | --- |
| 会话 | `MuxRayGdi_38f44508e80548a089e68f97fd39aa9c` |
| 时间 | `03:28:13.766–03:31:13.891 UTC`，名义 180 秒 |
| 门槛执行当时 | 打开前剩余 165.616 秒，选择前剩余 150.080 秒；精确会话查询成功且均小于 2 秒龄期。实际输入时效见下文 |
| 停止与丢失 | Stop 返回 0；三个原生丢失计数及 tracerpt Lost 均为 0 |
| ETL | 24,182,784 字节，低于 128 MiB 上限 |
| ETL SHA-256 | `5d264c12a3b3e9db0b21ac8d40e6eb84e058041932120b28b765ae19b099060d` |
| 解码 | tracerpt 退出 0；103,112 事件；XML 119,693,553 字节 |
| XML SHA-256 | `5488987bf16416308c060184d6a653a71d5033900e9f95405fae761d1e437fba` |

四类 Win32k 事件必需字段齐全，版本均为 0。全局 Create/Destroy/UpdateOwner/Transform 分别为 `43,970 / 43,969 / 14,288 / 883`，目标所有者事件分别为 `29,650 / 23,544 / 1,552 / 727`。仍有一条非 Win32k 的 SystemTrace/PartitionInfoExtension 处理错误，保留该限制；不以事件净差推算 GDI 保留数量。

选择前门槛与实际双击相距 17.008 秒。门槛表中的查询龄期只描述检查当时，不能当作输入瞬间的新检查；控制器持续保存的回执表明，输入前最近一次成功精确查询距输入 1.632 秒。这是事后核对，未改写原门槛收据。

## 本文件的时间对齐

103,110 条 Win32k 事件中，103,097 条 XML 时间标记为 `+07:59`，13 条为 `+08:00`，不能统一按一个偏移量修改。最终审计采用本文件 header StartTime FILETIME 加上事件与 header 的原始墙上时间差，以整数 100ns 计算；原始偏移与字符串全部保留。锚定后事件均落在 header 范围内，六项校准的创建或 Transform-new 事件均精确落在实际 API 调用前后 UTC 包围内；13 条 `+08:00` 事件直接解析值也与锚点一致。

该方法仅由本次跟踪的独立校准支持，不推广为通用时区修正。`calibration-and-candidate-review-001.json` 曾将带偏移时间直接相减，导致上述 13 条错移一分钟，已保留并由 `-002.json` 明确取代；以下候选时间采用最终审计。

## 对象校准与候选边界

独立普通权限校准进程 PID `31556`、创建 FILETIME `134354717662820754`，在本次有效采集中创建六个对象，再逆序释放。六次创建、六次释放和两次 GdiFlush 均成功。使用 payload 所有者、完整返回句柄及调用时间关联事件；`GetObjectType` 仅作旁证，未直接套用其数字。

| 实际创建 API | ETW HandleType | 观测 |
| --- | ---: | --- |
| CreateCompatibleDC | 1 | 返回句柄精确匹配 Create 与 Destroy |
| CreateBitmap | 5 | 返回句柄精确匹配 Create 与 Destroy |
| CreateFontW | 10 | 含符号扩展的完整句柄精确匹配 Create 与 Destroy |
| CreateRectRgn | 4 | 创建匹配；成功删除后出现 Transform |
| CreateSolidBrush | 16 | 创建匹配；成功删除后出现 Transform |
| CreatePen | 16 | 返回句柄匹配 Transform 的 NewHandleValue，随后匹配 Destroy |

此为本机本轮 API 与类型的关联，**type 16 不能单独命名为 Brush**。Brush/Region 转换发生在各自 DeleteObject 成功返回之后、校准清理阶段之内，并非同步落在各自删除调用区间。因此转换链保留不等于原 API 对象未释放；缓存或复用仍需进一步证据。

按全局 Create、Destroy、UpdateOwner、Transform 重建后，目标末端有 8 条带已知创建的候选链：type 1×1、5×1、16×2、4×4，另有 13 条创建前驱未知的链。未知项不纳入新增数量；这两个数字都不是泄漏数量。

优先追查其中两条从创建起没有所有者、类型或句柄转换，也没有匹配 Destroy 的候选：

| 候选 | 完整句柄 | 创建时间 UTC | 代际 |
| --- | --- | --- | ---: |
| DC | `0x05012114` | `03:28:28.7153301` | 7514 |
| Bitmap | `0xffffffff9e052275` | `03:28:28.7153573` | 7517 |

两者相差 27.2 微秒，发生在选择器打开期间，早于成功双击；直至跟踪末端至少 165.1749194 秒未出现后续四类事件引用。其余六条已知创建链均经过转换，不能同等看待。当前跟踪没有调用栈，也未覆盖对象的完整进程生命周期，因此**不能把这一对候选直接认定为 GetGuiResources +2 的确定来源或确认具体分配模块**。

生命周期分析保留完整原始句柄与 SessionId，未静默截断到 32 位；未知前驱、重复创建、所有者迁移、转换和别名均单独处理。分析器通过 24 个合成事件场景与前轮 XML 复核；原始 ETL/XML 不改写。

OwnerProcessId 使用事件 payload，未拿事件头 PID 替代；payload 中的 `0` 和 `2147483666` 原样保留，没有验证对应进程身份，不能解释为已确认的用户进程。

## Run4 原生退出与证据收口

实际 Alt+F4 正常退出后，根进程与六个已观察 WebView2 后代均确认退出码 0。资源探针以 `target_exit_grace_complete` 结束，329 个原始样本覆盖预热、选择、整个跟踪及退出；没有发现失败或进程树覆盖缺口。首样本因没有前一个 CPU 区间而 `cpuPercent=null`，内存与句柄读数完整；其余 328 个样本的聚合指标完整，不能误报首样本为退出阶段异常。

原始样本单调 elapsed 最大相邻间隔为 1.003929 秒，observedUTC 最大间隔为 1.566 秒，两种时间口径分别保留。采样不覆盖从进程创建到探针附加的准备阶段，也不声称没有错过任何瞬时峰值。最终精确会话查询返回 4201；MuxRay、控制器、采集器、watchdog 与资源探针均已结束。

源文件独占只读与完整 SHA-256 再校验通过，大小和哈希保持一致。第一次源收据因 PowerShell 将 `true` 误写为无前缀标识而落成 null，保留为失败记录；`source-after-selection-002.json` 是有效重测。该检查只证明源完整且当时可独占读取，不用来证明本来就未打开媒体的诊断版释放了媒体句柄。

本轮主要证据：

- `run-etw-004/`：预登记、目标身份和普通权限、controller-005 收据、原生输入及截图、GDI/USER 见证、校准、完整资源记录及退出。
- `etw-traces/full-picker-elevated-004/`：会话身份、停止及丢失计数、原始 ETL。
- 同目录 `decoded/decode-receipt.json`、`lifetimes-001.json` 和两个 jsonl 旁证：解码哈希、全局生命周期及候选。初始生命周期报告保留未对齐时间，阶段对齐须读取后续时间审计。
- `decoded/calibration-and-candidate-review-002.json`：最终类型、时间和候选审计，SHA-256 `e33d951f4600c40c70842002f038bea53f90dde8dc0f000500a3c61eb12e13eb`。
- `run-etw-004/independent-final-review.json`：独立原始采样、输入、权限及清理复核，SHA-256 `d66073461e5d5fc4d0dc19e08a7f437199e59d1995befcd77014c998fbd8d2fc`。

下一步是针对 DC/Bitmap 做创建与释放调用栈采集，沿既有 A 诊断版的预热与三轮成功选择协议，把完整返回句柄、稳定晚窗和分配调用链关联起来。确认重复增长属于同一调用链后，再决定修复位置。本轮没有修改生产代码，也没有宣称资源稳定性已经全项通过。
