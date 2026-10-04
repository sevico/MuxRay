# 每轮独立 STA 的 GDI 对照：2026-10-03

每轮文件选择改用独立 STA，并执行接口释放、CoUninitialize、实际线程退出和线程句柄关闭后，GDI 仍逐轮增加。原三轮固定窗为 **171 → 173 → 175**，第三轮静置三分钟仍为 175；额外补采轮次为 **177**，三分钟后不回落。原前两轮加补采共三次完整对象级验证，均确认一对同源 DC/Bitmap 跨过工作线程退出后仍被保留。**每轮结束 STA 未解决本机这条复现路径，不作为修复合入生产。**

接续 [独立 Win32 宿主对照](desktop-gdi-native-host-20261003.md)。外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-sta-cycle-20261003`，下列证据路径相对此目录。所有时间均为 2026-10-03 UTC；程序、ETL、符号和原始收据不纳入仓库。

## 宿主与线程边界

- 独立 x64 C++ Win32 宿主，无 Go、Wails、WebView2；主 UI STA 和消息循环保持存活。每次按钮操作用 `_beginthreadex` 创建独立 STA，执行与前轮相同的 IFileOpenDialog 创建、筛选、Show、Options 读取及 Release，不调用 GetResult 或读取媒体。
- 每个工作线程配对 CoInitializeEx/CoUninitialize。主线程保留真实线程 HANDLE，确认 WaitForSingleObject 返回完成、GetExitCodeThread 为 0，记录 GetThreadTimes 的创建/退出 FILETIME，再关闭 HANDLE、重新启用按钮。`thread_return` 日志只代表即将返回，不当作实际线程结束。
- 实测 `native-host/bin-002/MuxRay.exe`，PID 5292、创建 FILETIME `134354802371066732`、普通权限。MSVC `/W4 /WX` 构建、链接和 manifest 检查通过。未运行的 build-001 在复核中发现 GetMessage 失败路径可能提前释放工作线程上下文，修正后才构建并运行 002。
- 预热取消为 host cycle 1，原三轮成功选择为 cycle 2–4，补采为 cycle 5。使用同一 `generated-long-avc-aac-2400s.flv`，41,698,926 字节，保留真实 GUI 双击收据及每轮 Show/post 模块快照。

## 观察窗与覆盖缺口

固定窗口以实际输入完成 UTC 为锚点，仅纳入整个查询、资源采样迭代或 CPU 区间完整落在窗内的样本。以下每窗均有 30 个 GDI、30 个资源样本、29 个 CPU 间隔。

| 阶段 | 输入完成 UTC | 相对固定窗 | GDI |
| --- | --- | --- | --- |
| 预热取消 | 05:52:06.380 | +30～60 秒 | 中位 169；28 条为 169，2 条为 173 |
| 原第 1 轮 | 05:59:23.871 | +30～60 秒 | 全部 171 |
| 原第 2 轮 | 06:06:42.908 | +30～60 秒 | 全部 173 |
| 原第 3 轮 | 06:08:35.953 | +30～60 秒 | 全部 175 |
| 原第 3 轮长静置 | 同上 | +150～180 秒 | 全部 175 |
| 补采轮次 | 08:43:07.724 | +30～60 秒 | 全部 177 |
| 补采长静置 | 同上 | +150～180 秒 | 全部 177 |

预热的两个 173 样本位于 05:52:50.768722、05:52:51.768670，未删除或挪动观察窗。原五窗在补采前独立封存于 `run-sta-001/live-fixed-window-review-001.json`；补充两窗见 `run-sta-001/supplement-fixed-window-review-001.json`。

原 600 秒 ETW 为 **05:58:06.1721084–06:08:06.3495378**。协调操作延迟导致原第 3 轮在 06:08:10 才创建线程，完全位于 Stop 之后；该轮只有计数和线程日志，不能计作第三次对象级验证。保留原跟踪，另冻结补采协议，在同一宿主增加一次选择，以独立 300 秒 ETW **08:41:58.2803790–08:46:58.3564467** 覆盖 cycle 5。不同会话的句柄代际分别重建，未跨空档拼接。

原资源/GDI 探针按 1800 秒上限于 06:21:06 结束；等待 UAC 期间没有连续采样。恢复时另冻结 900 秒独立段，08:41:40 重新附着同一进程。该空档未被补录或用直线外推；新段首个 GDI 值为 175，仅代表重新附着时的计数。

协调器 001 曾在启动采集前读到仍被独占写入的 GO 文件，dispatch 为 0；改为完整写入、关闭后在同目录原子发布，再由 002 完成原跟踪。003 的 UAC 等待未产生 ready/GO/dispatch，恢复时以 abort 和既有输出目录保护撤销；004 绑定新增采样协议后完成补采。所有旧脚本、协议及失败收据保留，没有改写成一次无中断运行。

采集均为全局 Win32k 455–458、keyword `0x0000010000000000`、level 4、直接 stack64、无 PID 预过滤，512 MiB 文件上限；独立 watchdog 分别为 615/315 秒。原始 Stop 和补采 Stop 均返回 0，采集端丢失计数均为 0。

## 对象级结果

原 ETL 为 150,208,512 字节；解码 227,617 条选定事件，455/456/457/458 分别为 97,469 / 97,452 / 30,644 / 2,052，全部有直接 stack64，解析、消费者与 ETL 头丢失检查通过。全局状态机仍处理所有所有者与完整句柄代际；13 个最终已知创建候选、10 个未知起源对象不全部计为泄漏。

前两轮各一对 DC/Bitmap 的连续八帧映像 SHA＋RVA，精确匹配前轮已验证的缩略图核心路径：`CThumbnailLookupTable_CreateInstance → InitThumbnailLookupTable → _CreatePerfectImageList → _CreateImageList → CreateImageList → CSparseImageList::Initialize → CMultiImageList::Initialize → CImageList::Initialize`。复用匹配 PDB GUID/age 和 PE 函数范围证明，不按最近符号名猜测。

| 跟踪轮次 | DC 完整句柄 | Bitmap 完整句柄 | 实际工作线程退出后无后续事件的最短观察期 |
| --- | --- | --- | ---: |
| 原第 1 轮，TID 20308 | `0xffffffff8701180e` | `0x0000000054050174` | 512.336 秒 |
| 原第 2 轮，TID 28412 | `0xffffffffeb0116f8` | `0x0000000040051384` | 73.280 秒 |
| 补采，TID 19988 | `0xffffffffb701167f` | `0xffffffffa9051350` | 227.918 秒 |

这些对象在对应 Show 内创建；跨过 Release、CoUninitialize、真实线程退出和 UI join 后，没有对应 Destroy、Owner 迁移或句柄变换。观察下界截至最后选定事件 06:07:56.3751083，不使用较晚 Stop 冒充观察时长。独立原始事件复核验证完整句柄、SessionId、低 32 位别名及工作线程身份，13 项检查通过。较宽的同一 Initialize 返回点另有 28 DC、36 Bitmap 匹配正常 Destroy，较深调用者不同，不能混作精确缩略图核心已销毁的证明。

补采 ETL 为 65,667,072 字节；解码 95,474 条选定事件，455/456/457/458 为 40,637 / 40,627 / 13,223 / 987，全部直接 stack64，解析和丢失检查通过。补采 DC/Bitmap 分别于 08:42:39.3143006、08:42:39.3143658 创建，在 cycle 5 的 Show 内，事件头 PID 5292/TID 19988 与当轮线程、payload owner 一致；精确八帧核心再次匹配，用户地址均有模块映射。

两对象均无后续 Destroy、Owner 迁移、变换或歧义。线程实际退出于 08:43:07.9275974，最后选定事件为 08:46:55.8452376，即退出后至少 227.9176402 秒仍未观察到后续生命周期事件。补采较宽路径另有 14 DC、18 Bitmap 正常 Destroy。独立复核逐条匹配 66 条 Create/End 原始记录并通过 16 项检查；旧四候选与新两候选分别保留各自会话及代际。

补采与原前两轮同类型对象的完整用户帧 SHA/RVA 序列一致，无未解析用户地址。原各轮 Show/post 为 136/132 个模块，补采为 138/134；按周期分别映射，不要求所有模块保持加载。原、补源比较与摘要分别保存在 `traces/sta-three-cycles-001/decoded/` 和 `traces/sta-supplemental-cycle-001/decoded/`；原始事件独立复核见 `offline-analysis/independent-original-core-review-001.json`、`offline-analysis/independent-supplement-core-review-001.json`。

## 正常退出与结论边界

五个工作线程均实际结束并关闭线程 HANDLE。主 UI 线程 TID 10100 的 COM 初始化与最终清理配对，日志 normal_exit 为 0；Alt+F4 输入完成于 08:47:37.742，OS 记录进程退出 08:47:37.9333292。源媒体最后排他读取和 SHA 校验通过，内容未变。

最终分段审核全部通过：原段 1,799 个资源、1,801 个 GDI 样本，新段 365 个资源、358 个 GDI 样本。原/新资源样本最大相邻 UTC 间隔为 2.143/1.022 秒，GDI 为 1.133828/1.000959 秒；各段仅首条 CPU 因没有前一采样而为空，其他指标完整。两段之间资源/GDI 的实际未观测间隔分别为 8,433.603/8,433.604014 秒，不能声称全程连续。

采样只观察到一个宿主进程，没有观察到后代；新段重新附着该精确进程身份并确认退出。四个探针、两组 collector/watchdog、已知 coordinator 均已结束，两个精确会话 Query 均为 4201。003 没有返回进程身份，只记录未出现 ready/GO/dispatch 及已撤销的证据，不虚构其退出证明。详见 `run-sta-001/final-segmented-sampling-exit-review-005.json` 和 `run-sta-001/final-owned-capture-cleanup-001.json`。

本轮同时改变了 COM apartment 和工作线程寿命，只能据实排除这组诊断改动解决当前保留的假设。主 UI STA 的退出发生在 ETW Stop 之后，未跟踪进程退出时的对象清理。系统模块内的创建路径不能单独证明 Windows 或某个 Shell 扩展应承担持有责任；模块快照只是点时记录。原生诊断程序不读取媒体，本轮也不构成生产打开/取消/错误/关机路径的功能验收。

下一步优先做文件关联对照：在两个新建、布局相同的目录中放置同一媒体字节的 `.flv` 与 `.bin` 副本，统一 `*.*` 筛选，分别用新进程、同一视图和真实选择验证计数及精确核心对象。该实验用于判断媒体关联、缩略图或属性处理路径是否必要，不预先归责第三方，不修改系统注册表或删除缓存。保留命中来源后再决定生产修复方式。

后续已完成 [同字节 FLV/BIN 文件关联对照](desktop-gdi-association-20261004.md)：两组均逐轮 +2，并保留同一缩略图核心路径的 DC/Bitmap；BIN 首轮另外捕获销毁后经通知/ResetImages 分支重建的对象。具体协议偏差、对象观察下界与解释限制见新报告，未作为生产修复。

## 主要证据 SHA-256

| 相对路径 | SHA-256 |
| --- | --- |
| `native-host/bin-002/MuxRay.exe` | `5e09d155e6eb71a3cafae712af9cd2676a7df2f6a88c54815bd6a1141e228f56` |
| `traces/sta-three-cycles-001/win32k-gdi.etl` | `5028eda5dbe7e409f02c9a8915ce856bccabf50c52fdaf1277b8c66dc5530413` |
| `traces/sta-supplemental-cycle-001/win32k-gdi.etl` | `eab4a96e2c70b34bc7abfb05fa8ae4a162d4f185f62afbc9ccf204e896d154be` |
| `traces/sta-three-cycles-001/decoded/sta-source-summary-original-001.json` | `85542dce0731fa78e182816fd39ff87ff53ba650902e20405fc89c7e1b89dc85` |
| `traces/sta-supplemental-cycle-001/decoded/sta-source-summary-supplement-001.json` | `a749e815d6f563d2047c5a544f662dae55af31c198eb740394526ab81b1ac7c6` |
| `offline-analysis/independent-original-core-review-001.json` | `8cf095de3e4a3b9912d8cfef773fca52a03712d8c741eaf37c70b791d960e8f1` |
| `offline-analysis/independent-supplement-core-review-001.json` | `fb2825079ffa8fd387d63f5960a5c8f3d4f8c8b3de4e2fd6dc264714079178cb` |
| `run-sta-001/final-segmented-sampling-exit-review-005.json` | `c8c479a2ba79fc88bf80fd391c83173adad32bb38a661e3e312f3ddaaa4d0d04` |
| `run-sta-001/final-owned-capture-cleanup-001.json` | `64e53a393c1a20705f2d6c6a8a5f8abe59ccd8619fafd9645c9bc51280868836` |

本轮仓库改动仅为报告及文档索引。执行了原生构建、实际 GUI 选择、计数/线程生命周期/ETW 验证；未修改生产代码或重跑无关业务测试。
