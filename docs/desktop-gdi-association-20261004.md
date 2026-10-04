# 同字节 FLV/BIN 文件关联对照：2026-10-04

同一媒体字节改为 `.bin` 后，仍重现与 `.flv` 相同的 **169 → 171 → 173 → 175**，末轮三分钟计数不回落。两组各三轮均保留一对精确匹配同一缩略图核心分配路径的 DC/Bitmap；更换扩展名没有避开本机这条复现路径。**这不是生产修复，也不能据此认定某个属性处理器或第三方组件负责持有。**

BIN 第一轮另观察到一对同核心对象在 Show 内正常销毁，紧接着创建的新一对被保留；其上层调用链与常规创建不同。这提供了比计数更具体的释放/重建对照。FLV 存在缓冲时限偏差，BIN 末轮三分钟计数尾窗位于 ETW 之后，以下分别保留计数证据和对象观察下界。

接续 [每轮独立 STA 对照](desktop-gdi-sta-cycle-20261003.md)。外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-association-20261004`，下列路径相对此目录。时间均为 2026-10-04 UTC；诊断程序、媒体、ETL、模块快照和原始收据不纳入仓库。

## 对照条件

- 原文件为私有测试语料 `generated-long-avc-aac-2400s.flv`。两个新目录分别只有 `fixtures/case-a/sample.flv`、`fixtures/case-b/sample.bin`，均为 **41,698,926 字节**，完整 SHA-256 均为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。创建时匹配文件时间和 Archive 属性；每组开始前、末轮静置完成后均排他读取源文件与对应副本并校验完整哈希。
- 两组使用同一 `native-host/bin-001/MuxRay.exe`，各启动一个新的普通权限进程，先 FLV 后 BIN。FLV 身份为 PID 27964 / 创建 FILETIME `134355536193957859`，BIN 为 PID 1316 / `134355546998667996`。宿主无 Go、Wails、WebView2；每轮新建 STA 工作线程，在 Release、CoUninitialize 后确认真实线程退出、关闭线程 HANDLE，再启用按钮。主 UI STA 保持存活直到正常退出。
- 宿主首次选择 case 后锁定该进程的 case；每轮通过 SetFolder 指定对应目录，临时文件夹 IShellItem 在 Show 前释放。两组标题相同、筛选均为 `*.*`，宿主不调用 GetResult，也不自行解析媒体；这不表示 Shell 内部不会读取文件内容。
- 每组先打开并真正取消一次，静置至少 60 秒；之后在独立 600 秒 ETW 内成功选择三次，分别静置至少 60、60、180 秒。均为真实 GUI 操作，保存输入完成时间、界面证据及每轮 Show/post 模块快照，共每组八份。
- 两组最终选择前均确认 Details 视图、Name 升序，正式轮次都有日期分组。FLV 预热阶段校正排序并展开分组；BIN 预热初态没有分组，正式第一轮初态出现修改日期降序，等待后新截图已自行恢复 Name 升序。BIN 未实际执行排序校正输入；`cycle-1-view-correction-pre.json` 是观测收据，不是输入收据。保留这些视图初态、异步变化和等待时长差异，不能声称整个 Show 期间布局完全相同。
- 只读注册表快照显示 `.flv` 的默认类为 `PotPlayerMini64.flv`、PerceivedType 为 video；`.bin` 有 NoOpen/PersistentHandler 配置。BIN 不是“没有关联”的证明，这些配置也不能证明某个处理器实际被调用。未修改注册表或清理 Shell 缓存。

宿主源码独立复核通过，MSVC x64 `/W4 /WX` 构建、链接和嵌入 manifest 检查成功。启动前冻结协议 002、manifest 003 及输入哈希；一个有界提权协调器依次等待两次人工 GO，每次采集 600 秒、512 MiB 上限、615 秒独立 watchdog。第一组 Stop 成功、丢失为零且精确会话 Query 为 4201，第一宿主正常退出后，才允许第二组采集。

## 固定窗口

窗口以实际取消/双击输入的 `completedUTC` 为锚点，统计 +30～60 秒，末轮再统计 +150～180 秒。仅纳入查询、采样迭代及 CPU 区间完整位于窗口内的数据；线程 join、HANDLE 关闭及按钮启用须早于窗口起点。跨进程只比较各自取消基线后的增量，不用绝对基线判定关联影响。

| 文件 | 取消基线 | 第 1 轮 | 第 2 轮 | 第 3 轮 | 第 3 轮 +150～180 秒 |
| --- | ---: | ---: | ---: | ---: | ---: |
| FLV | 169 | 171 | 173 | 175 | 175 |
| BIN | 169 | 171 | 173 | 175 | 175，仅计数覆盖 |

两组共十窗，每窗均为 30 个 GDI、30 个资源样本、29 个完整 CPU 区间，GDI 值各自恒定。FLV 取消输入为 02:18:03.485，三次选择输入为 02:20:59.253、02:23:27.271、02:26:03.994；BIN 取消为 02:33:55.241，选择为 02:38:49.063、02:40:39.153、02:42:48.885。

FLV ETW 从 02:19:22.3950749 开始，Stop 于 02:29:24.3029687 成功。第三次输入位于起点后 **401.599 秒**，比预登记 +400 秒缓冲门晚 **1.599 秒**；末轮静置结束为 02:29:03.994，也超过 +580 秒缓冲门，但三轮 Show、实际工作线程退出和全部正式固定窗仍在实际采集范围内。保留此偏差，未移动窗口、延长采集或重做以改写结果。最终审核不能写成协议全部通过。

BIN ETW 为 **02:35:17.8403671–02:45:17.8728801**。第三次输入位于起点后 **451.045 秒**，超出 +400 秒门 **51.045 秒**。三轮 Show、实际线程退出及各自 +30～60 秒窗均在跟踪内；但第三轮 +150～180 秒窗为 **02:45:18.885–02:45:48.885，完整位于 Stop 之后**。该窗的 175 有连续计数证据，不能证明同一对对象在这段窗口中仍然保留。未缩短静置、延长跟踪或追加补采。

## 分配与释放路径

两个跟踪分别重建完整句柄代际，不跨会话拼接。全局 Win32k 455–458 无 PID 预过滤；事件头 PID、payload owner、Owner 迁移和句柄变换分别处理。用每轮模块 SHA/RVA 映射和既有匹配 PDB GUID/age、PE 函数边界证明核对八帧核心，不按相近符号名归因。

两组各三轮均在对应 Show、工作线程内创建一对最终保留、精确匹配既有路径的 DC/Bitmap：`CThumbnailLookupTable_CreateInstance → InitThumbnailLookupTable → _CreatePerfectImageList → _CreateImageList → CreateImageList → CSparseImageList::Initialize → CMultiImageList::Initialize → CImageList::Initialize`。它们在成功选择前已经创建，跨过接口释放、COM 清理、实际线程退出后，截至各自最后选定事件未见后续 Destroy、Owner 迁移或句柄变换。

| 文件/轮次 | DC 完整句柄 | Bitmap 完整句柄 | 实际工作线程退出后无后续事件的最短观察期 |
| --- | --- | --- | ---: |
| FLV 1 | `0xffffffffa0012019` | `0x000000007d051a55` | 499.891 秒 |
| FLV 2 | `0x000000001801192b` | `0x00000000760501ad` | 351.896 秒 |
| FLV 3 | `0x0000000040010c86` | `0x0000000049051beb` | 195.167 秒 |
| BIN 1 | `0xfffffffffa011957` | `0x000000001d0516f7` | 360.020 秒 |
| BIN 2 | `0xffffffffbe011651` | `0xffffffff930524e3` | 249.940 秒 |
| BIN 3 | `0xffffffff8d011ba3` | `0xffffffff92051929` | 120.192 秒 |

FLV 的观察下界截至最后选定事件 02:29:19.3032341，不用较晚 Stop 夸大观察期。同类型三轮完整创建栈一致，无未映射用户地址；较宽的同一 Initialize 返回点另有 42 DC、54 Bitmap 匹配正常 Destroy，深层调用者不同，不混作精确核心已释放的证据。全局最终已知创建候选 17 个、未知起源 8 个另列，不全部标为泄漏。独立复核重新扫描所有原始事件，逐条核对列出的 198 条 Create/End、完整句柄、SessionId 和低 32 位别名，11 项检查通过。

BIN 的最后选定事件为 **02:44:49.2286846**，因此末轮对象级保留下界为 **120.192 秒**。BIN 另外有一对精确八帧核心 DC `0x000000001c0116f7`、Bitmap `0xffffffffd205198f`，在第一轮 02:36:19.4438 创建，02:36:51.8198 正常 Destroy；随后 02:36:51.8200 创建上表第一对保留对象。原始 Create 序号 17018/17024、Destroy 87749/87750、新 Create 87754/87757 分别保留，不能把前一对的释放误配给后一对。

原已销毁 pair 的完整用户 SHA/RVA 栈与 BIN 第二轮保留 pair 相同；BIN 第一轮后来创建并保留的 pair 则有不同上层调用链。共同八帧核心不能扩写成全部完整调用栈相同，也不能推出从该核心创建的所有对象都不释放。BIN 较宽 Initialize 路径仍有 42 DC、54 Bitmap 正常 Destroy；全局最终已知候选 14 个、未知起源 11 个另列。独立扫描 476,018 条原始事件、核对 202 条 Create/End 后通过；各精确核心用户地址均可映射。两组均按每轮 Show/post 快照映射，没有把模块数相同作为前提。

新增的上层路径已复用本地 Windows.Storage PDB 离线解析，10 个 RVA 均通过映像 SHA、PDB GUID/age、地址与返回地址减一同处 PE x64 函数范围的检查：

| 创建情形 | 已核实的外层调用链，之后进入共同缩略图核心 |
| --- | --- |
| 常规创建，包括 BIN 第一轮后来被销毁的一对 | `CDefImageRenderer::Draw → SetPhysicalImageSizeAndDPI → CImageManager::SetPhysicalImageSizeAndDPI → _EnsureThumbview` |
| BIN 第一轮后续保留的新一对 | `CDefCollection::s_WndProc → _OnDSVFSNotify → _OnFSNotify → OnQIChangeNotify → UpdateImage → CImageManager::ResetImages → _EnsureThumbview` |

新 pair 的 DC/Bitmap 用户栈为 44/47 帧，原 pair 为 70/73 帧。原 pair 的完整用户 SHA/RVA 栈与 BIN 第二、三轮及 FLV 第一轮相同。证据将这次重建缩小到变更通知和图像重置路径，但没有识别通知来源、属性处理器调用或最后一个持有引用的对象。补证见 `offline-analysis/bin-upper-path-001/pe-symbol-boundary-review-001.json`；没有联网取新符号或增加采集。

跨组汇总 `offline-analysis/cross-case-comparison-001.json` 将每窗相对基线增量、各完整用户栈差异、实际视图收据、固定窗与 ETW 的重叠时间分别列出。两组增量均为 0/+2/+4/+6/+6；BIN 末轮长静置窗与 ETW 的重叠为 0 秒。

## 验证与解释范围

FLV ETL 为 182,452,224 字节，解码 276,370 条选定事件，455/456/457/458 分别为 124,994 / 124,973 / 23,481 / 2,922；全部直接 stack64，采集、ETL 头及消费者丢失为零，解析、Open/Process/CloseTrace 检查通过。

BIN ETL 为 304,939,008 字节，解码 476,018 条选定事件，455/456/457/458 分别为 217,320 / 217,294 / 37,817 / 3,587；同样全部直接 stack64，全部丢失与解析检查通过。两组都保留全局所有者事件和各自会话的状态机，没有以目标 PID 预先截掉 Owner 迁移。

FLV 正常 Alt+F4 输入完成于 02:31:12.838，OS 记录退出于 02:31:13.0042176，exit code 为 0。四个工作线程及主 UI COM 清理完整。资源/GDI 采样分别为 971/965 条，最大相邻 UTC 间隔为 1.026/1.00147 秒；只有首条 CPU 因缺少前一采样而为空，未用零填充。采样只观察到一个宿主进程，未观察到后代；两个探针完成正常退出收尾。

BIN 正常 Alt+F4 完成于 02:46:29.581，OS 退出为 02:46:29.7738656、exit code 0，四工作线程及主 UI COM 清理完整。资源/GDI 共 857/850 条，最大相邻 UTC 间隔 1.022/1.006115 秒；每个首次观察进程的首条 CPU 空值保留，其余指标完整。BIN 另观察到短命后代 `SogouInput/SGBizLauncher.exe`（PID 31232），02:36:48.8978468 创建、02:36:55.0906925 退出，exit code 0。它与首轮对象销毁/重建时间重叠，但时间重叠、模块加载或父子关系都不是因果证明，不能据此归责输入法。

BIN 首次退出界面检查仍显示正在关闭的窗口，原 `exit-confirmed.json` 保留；之后 `exit-confirmed-002.json` 确认目标窗口消失。最终收尾检查确认两宿主、协调器及四个探针的精确身份均不在运行；两个 collector 和两个 watchdog 的记录 PID 当前均不存在，未虚构其创建身份。两条精确会话 Query 均为 4201，冻结输入哈希未变。

最终采样审核为各组 `final-sampling-exit-review-002.json`：FLV 仅 `thirdSelectionBeforeTracePlus400` 不满足；BIN 另有 `formalCounterWindowsInsideTrace` 不满足。其余线程、资源、窗口、源文件、正常退出及完整性检查通过。FLV 初版审核因按键收据为 `press_key/key` 而非直接 `action=Alt_L+F4` 多报一次 schema 错误，已在兼容等价格式的 002 中修正，001 和原始输入未改写；真实时间偏差继续保留。

固定顺序、不同目录和文件身份、共享 Shell 缓存、视图恢复与等待时长、BIN 出现的输入法后代都限制因果解释；相同字节的 BIN 仍可能被内容嗅探识别。模块快照只有点时状态，创建栈不等于最终持有责任。预热取消在 ETW 之前、正常进程退出在 ETW 之后，不能据本轮宣称这两个阶段的对象释放已经被跟踪。排他读取通过也不能还原每个 Shell 文件句柄的寿命。本实验不构成生产媒体打开、错误或关机路径的功能验收。

后续已完成 [同一文件取消/成功选择的对象级对照](desktop-gdi-cancel-select-20261004.md)：两组均逐轮 +2 并保留同核心对象，成功接受不是该条件下存留的必要条件；取消组另在关闭输入前观察到精确核心对象经 lookup-table 释放/析构而销毁、随后经 ResetImages 重建。前述 BIN 样本的 Destroy 栈也完成离线补证。具体实例的引用持有责任仍未确定；本篇扩展名对照不能解释为禁用缩略图或属性处理器的干预。

## 主要证据 SHA-256

| 相对路径 | SHA-256 |
| --- | --- |
| `association-protocol-002.json` | `16c559ef9b4fa3c465b7c90aee82b59e52463bfb850ebe068f8ce4c2e61af5d5` |
| `manifest-003.json` | `cb94a833c8a2dd652c17ceb717508b6e28976e45aeb0dda9c910de137e4b62df` |
| `native-host/bin-001/MuxRay.exe` | `dae9d843d0e19906b86fda9a4b6a85e01be9fb760a36ab44d6dd12d688d0e980` |
| `traces/case-flv-001/win32k-gdi.etl` | `88676b87c520ce190b7038d3dc4760c055582de5f0a88b1d8b0933e9a0eec3e0` |
| `traces/case-bin-001/win32k-gdi.etl` | `1d89b8618486407ea11d8a06ad96ebec6f3a5a75108fcea3f8656b36514f3c4a` |
| `traces/case-flv-001/decoded/association-source-summary-001.json` | `0f0271401f2a44c2cdc8ba94169f772edc1f8eeda63f31ee33df5ea5ff9c41e0` |
| `traces/case-bin-001/decoded/association-source-summary-001.json` | `a17a904e860ea828e875bebd1ea5522b22aca1f6b82782bf75bb99ee78994837` |
| `offline-analysis/independent-flv-source-review-001.json` | `69816ca8fd0a5c657fc74579e9271e9f440fb1f372416a0bcafdcd17d8cfd73f` |
| `offline-analysis/independent-bin-source-review-001.json` | `f31ff580a34aa9c67b84f5641ee5fd309c149559b7cf61f259cc006fcdbdfc6b` |
| `offline-analysis/bin-upper-path-001/pe-symbol-boundary-review-001.json` | `30588e2a79a60532f1c07ae67ad1a28d0279ea1a69ef99644b96ea319511875c` |
| `run-case-flv-001/final-sampling-exit-review-002.json` | `c49e0f6a706d63b7abebba8157ff9370cbd088dbdac791a37ecf117664b83561` |
| `run-case-bin-001/final-sampling-exit-review-002.json` | `5229a916d872941164de647940b68887c6dbdc9bde56c06715d30a7af8f3c040` |
| `offline-analysis/cross-case-comparison-001.json` | `3a7a9134a40edee0e09fdc9349ae817e538899acb92a66328c6b02a23e09bd89` |
| `final-owned-capture-cleanup-001.json` | `405a7c5ab0b87012c44a81abb671538bca6cc118cd12996a9d40e3412e956b52` |

本轮仓库仅新增诊断报告和更新文档索引。执行了原生构建、八次真实选择器操作、十个固定窗、两段 ETW、独立原始事件及正常退出/清理验证；未修改生产代码或重跑无关业务测试。
