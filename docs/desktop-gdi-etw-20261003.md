# Win32k ETW 提权与首次采集验证：2026-10-03

独立采集器已通过 Windows 提权，普通权限 MuxRay 的 Win32k 跟踪成功保存并解码。**权限阻塞已解除，但本轮没有完成成功文件选择，尚未取得每轮 GDI +2 的对象归因或修复证据。**

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

## 证据与剩余工作

- `run-etw-002/`：身份、普通权限证明、两个控制入口收据、GUI 输入失败及取消、资源原始记录、正常退出。
- `etw-traces/full-picker-elevated-002/receipt.json`、`decoded/decode-receipt.json`、`decoded/preliminary-event-counts.json`：成功停止、ETL 哈希、解码与字段统计。
- `run-etw-003/elevation-dispatch.json`、`exit-resources/summary.json`：提权取消与准备进程清理。
- `calibrate-gdi-types.py` 已准备并通过语法检查，**尚未执行**。它在独立普通权限进程中创建并释放六类已知 GDI 对象，记录实际句柄、GetObjectType、时间和释放结果；这些观测才可用于校准 ETW 数字类型。

剩余步骤是重新取得一次覆盖真实成功选择及静置期的跟踪，在同一次有效采集中运行对象校准，再依据全局所有者与句柄生命周期核对新增候选。应保留本轮未完成操作的记录；不能将“已有有效 ETL”表述为“已定位新增两个对象”。
