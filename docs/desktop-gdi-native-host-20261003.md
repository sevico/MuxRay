# 独立 Win32 宿主的 GDI 对照：2026-10-03

独立 C++ Win32 宿主在没有 Go、Wails、WebView2 的情况下，三轮真实文件选择仍重现 **171 → 173 → 175 → 177**。每轮各有一个 DC 和一个 Bitmap，精确匹配前轮已验证的缩略图核心分配栈，跟踪期间未见对应 Destroy、所有者转移或句柄变换。**这些宿主组件不是复现的必要条件；分配来源已确认，持有责任仍未确定，生产代码尚未修复。**

接续 [三轮分配栈与 COM 包装对照](desktop-gdi-stacks-20261003.md)。本轮外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-native-host-20261003`，下列证据路径均相对此目录；实验程序、ETL、符号和原始收据不纳入仓库。所有时刻均为 2026-10-03 UTC。

## 宿主与实验边界

- `native-host/native-host.cpp`、manifest 和构建脚本生成独立 x64 `MuxRay.exe`；MSVC 14.44、SDK 26100，`/W4 /WX` 编译、链接与 manifest 提取通过。内嵌 Common-Controls v6、asInvoker、PerMonitorV2，构建收据为 `native-host/build-001/receipt.json`。
- 一条 UI STA 成功初始化一次 COM，正常退出时同线程配对 `CoUninitialize`。每轮新建 `IFileOpenDialog`，设置相同标题和 FLV 筛选，读取 Options，执行 `Show(ownerHWND)`，再次读取 Options，释放接口。不调用 GetResult，也不打开媒体。
- HWND 存于宿主自有状态，不写原生 COM 对象的未知字段；正确区分取消和失败 HRESULT。日志使用 CREATE_NEW，每次调用前后记录 UTC、FILETIME、QPC、PID、TID、周期及返回值；失败路径仍清理已取得接口。
- 原生程序由桌面工具启动，PID `25972`、创建 FILETIME `134354779961120014`、非提升权限；两个只读探针每秒采样，均限时 1800 秒。只有独立 ETW 采集器提权。
- 本轮同时改变语言运行时、窗口、消息循环、COM 配对、EXE 路径及应用身份，属于组合宿主对照。只比较各宿主内的增量及分配栈，不能跨宿主比较 GDI 绝对基线。
- 仍使用 `generated-long-avc-aac-2400s.flv`，41,698,926 字节；真实双击选择前保留目录、文件行截图与输入时间收据。取消预热后静置，再完成三轮选择；宿主日志 cycle 1 是预热，cycle 2–4 才对应正式三轮。

## 600 秒跟踪与固定观察窗

协议 `native-host-protocol-001.json` 和 `manifest.json` 在启动前冻结。复用前轮真实对象预校准：StackSession.cs 字节与 13 个关键系统模块哈希一致；未重复预校准。正式会话 `MuxRayGdi_4e843d49c9f446ea82cf1dfb6ec0191e` 采用全局 Win32k 455–458、keyword `0x0000010000000000`、level 4、直接 stack64，无 PID 预过滤；512 MiB 上限和独立 615 秒 watchdog。

采集为 `05:16:07.1510551–05:26:07.2198659`，约 600.069 秒；ETL 80,674,816 字节。精确会话/路径 Stop 返回 0，采集端、ETL 头及消费者丢失计数均为 0。离线解码 98,221 条选定事件全部带直接 stack64；455/456/457/458 分别为 31,954 / 31,934 / 30,691 / 3,642，无 TDH 解析错误、未展开栈键或 OpenTrace/ProcessTrace/CloseTrace 错误。全局事件数不能相减作为泄漏数。

下表以 GUI 输入完成时间为锚点，仅纳入整个查询或资源采样迭代均在窗内的样本。每窗均为 30 个 GDI、30 个资源样本，以及 29 个完整 CPU 间隔。

| 阶段 | 输入完成 UTC | 固定窗 UTC | GDI 全部样本值 |
| --- | --- | --- | ---: |
| 取消预热基线 | 05:14:36.938 | 05:15:06.938–05:15:36.938 | 171 |
| 第一次成功选择 | 05:16:55.524 | 05:17:25.524–05:17:55.524 | 173 |
| 第二次成功选择 | 05:18:32.801 | 05:19:02.801–05:19:32.801 | 175 |
| 第三次成功选择 | 05:20:20.876 | 05:20:50.876–05:21:20.876 | 177 |
| 第三次长静置 | 同上 | 05:22:50.876–05:23:20.876 | 177 |

前两轮至下一次输入分别静置 81.930、80.408 秒；第三轮至退出输入静置 391.537 秒。实时与最终审核一致，见 `run-native-001/live-fixed-window-review-001.json` 和 `final-sampling-exit-review-001.json`。

## 六个同源静态候选与正常销毁对照

使用已验证解码器和未修改的全局生命周期分析器重建完整句柄、代际、Owner 与变换关系。目标最终有 15 个已知创建候选和 10 个未知起源对象，不能全部计为泄漏。以下六个对象 origin 等于 lastEvent，无歧义、迁移或变换，事件头均为 PID 25972 / UI TID 30156，创建时刻均在对应 S_OK Show 的调用区间内、最终双击之前。

| 正式轮次 | DC 完整句柄 / 创建 UTC | Bitmap 完整句柄 / 创建 UTC | 无后续事件的最短观察下界 |
| --- | --- | --- | ---: |
| 1 | `0xffffffff9001165e` / 05:16:35.6656000 | `0x000000006d0516c9` / 05:16:35.6656617 | 569.148 秒 |
| 2 | `0x000000002a011230` / 05:18:18.0506476 | `0xffffffffa4050c7d` / 05:18:18.0507137 | 466.763 秒 |
| 3 | `0x000000003e011ae4` / 05:19:53.7758839 | `0x000000005a051223` / 05:19:53.7759493 | 371.038 秒 |

观察下界截至最后一条选定事件 `05:26:04.8139532`，不冒用较晚的 Stop 时刻。独立复核在全部 98,221 条原始事件中按完整句柄与 SessionId 检查，六个对象只有各自 Create，没有后续触碰或低 32 位的另一种句柄拼写。

核心连续八帧与前轮严格匹配的映像 SHA＋RVA 完全一致，复用前轮 PDB GUID/age 和 PE 函数范围验证：`CThumbnailLookupTable_CreateInstance → InitThumbnailLookupTable → _CreatePerfectImageList → _CreateImageList → CreateImageList → CSparseImageList::Initialize → CMultiImageList::Initialize → CImageList::Initialize`。DC 在 Initialize 内创建，Bitmap 经位图重分配路径创建；不是按最近符号名猜测。前两轮完整用户栈相同，第三轮外围消息分派片段多四帧，因此不能声称三轮全栈相同；六条栈的用户地址均有唯一模块映射。

较宽的“同一 Initialize 返回点”对照有 42 个 DC、54 个 Bitmap 匹配正常 Destroy，较深调用者不同。每轮各取一对 Create/Destroy 代表，释放栈与旧已验证的精确 SHA＋RVA 链匹配：`ImageList_Destroy → Release → ~CImageList → _Destroy`，随后 DC 经 DeleteDC，Bitmap 经 _DeleteBitmap/DeleteObject。**这些是较宽路径的正常销毁对照，不是上述缩略图核心路径已销毁的证据。**

详见 `traces/native-three-cycles-001/decoded/source-comparison-001.json`、`native-source-summary-001.json`，以及 `offline-analysis/independent-source-review-001.json`。独立复核逐条匹配 102 个 Create 和 96 个 Destroy 原始记录。

## 正常退出与限制

宿主四次 CoCreateInstance/Show/Release 顺序完整，取消一次、S_OK 三次，Release 返回值均为 0；它不能证明 Shell 内部关联对象已全部释放。一次 CoInitializeEx 与一次 CoUninitialize 均在 TID 30156，normal_exit 0。Alt+F4 输入完成于 `05:26:52.467`，OS 记录退出 `05:26:52.6355474`；只观察到宿主一个进程，无后代。两个探针、collector、coordinator、watchdog 均结束，精确会话 Query 返回 4201。

801 条资源与 794 条 GDI 采样连续，最大相邻 elapsed 间隔分别 1.007449、1.002943 秒，仅首条 CPU 因无前一采样而为空。采样创建后约 23 秒才附着，这段启动区间未测；所有正式固定窗完整。四份快照的 134 个模块地址、范围与映像哈希一致，但点时快照不等于完整加载历史。宿主加载了输入法和 Shell 扩展；六条创建栈没有第三方模块帧，仍不能排除第三方间接影响。源文件排他读取及原 SHA 校验通过。

本轮证明同源分配及保留可在独立宿主重现，不能据此归责 Windows、Shell 扩展或某个缓存持有者。ETW 在宿主退出和 CoUninitialize 之前停止，没有跟踪退出时对象清理；进程退出码 0 也不能代替对象级销毁证据。本轮不构成媒体打开的生产功能验收。

下一步做每轮独立 STA 线程的诊断对照：同样创建、Show、Release 后，在该线程执行 CoUninitialize 并结束线程，保持宿主进程存活；三轮检查计数回落及这对 DC/Bitmap 的匹配 Destroy。这是验证长期 STA 持有假设，尚不能当作已证实的修复合入生产。

## 主要证据 SHA-256

| 相对证据路径 | SHA-256 |
| --- | --- |
| `native-host/bin-001/MuxRay.exe` | `a0602b0f9a7acccc8e9fcbc645af2623637d3458500fe01738466722d36cd1fd` |
| `traces/native-three-cycles-001/win32k-gdi.etl` | `d72741f33ea90ac1aeda651b2542c6508e3a2cea5249116d06f828b06d391aa1` |
| `traces/native-three-cycles-001/decoded/source-comparison-001.json` | `8101e5d9e087e837b7448ddcbcc5dc94ce8eb555f1f02aa482184fbfb30c3059` |
| `traces/native-three-cycles-001/decoded/native-source-summary-001.json` | `53554995915bc55661a180a47f1e60a17e6b6a5c34ee05aaeb16a049f0a3010e` |
| `run-native-001/final-sampling-exit-review-001.json` | `648c0b0604e23acdbdb8b6cedf95c0d3782339184619c16026464c3e9c4bcdab` |
| `run-native-001/final-trace-cleanup-review-001.json` | `f472a54fd517891a5027c6fa7456480c4e9a2b25c0243027e158e261658253ea` |
| `offline-analysis/independent-source-review-001.json` | `97c04eb867b79b9f466a7a60953044d4c9d2cf2519349507df46f9d9e7b86e42` |

本轮仓库改动仅为报告与文档索引；完成原生构建、三轮 GUI/计数/ETW 验证及证据复核，没有改动生产程序或重跑其业务测试。
