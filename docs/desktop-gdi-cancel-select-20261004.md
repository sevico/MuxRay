# 同一文件取消/成功选择的对象级对照：2026-10-04

同一既有 FLV 文件在行选中并等待后，**真实取消和成功选择两组均为 171 → 173 → 175 → 177，末轮三分钟窗仍为 177**。两组各三轮均保留一对匹配同一八帧缩略图核心的 DC/Bitmap，跨过接口释放、COM 清理和实际工作线程退出。**成功接受不是本条件下这种存留的必要条件；这不证明 Cancel/Open 完全等价，也尚未确定最终引用持有者。**

常规创建的完整用户 SHA/RVA 栈在两组间相同，但取消组第二轮还出现不同上层栈的 ResetImages 重建：旧 pair 在 Cancel 输入开始前已正常 Destroy，新 pair 随后继续保留。不能把这次销毁归因于点击 Cancel，也不能把共同八帧核心扩写为全部完整调用栈相同。两组十个固定窗、两段零丢失跟踪、独立原始事件复核、正常退出及最终清理均通过；首次采集准备失败及明确修正完整保留。

接续 [同字节 FLV/BIN 文件关联对照](desktop-gdi-association-20261004.md)。前轮既观察到同核心对象被保留，也观察到一对对象在 Show 内销毁后经通知/ResetImages 路径重建；本轮使用这些既有证据区分分配路径，不预设取消一定释放、成功选择一定保留。

后续 [具体图像列表与引用持有者](desktop-gdi-reference-20261004.md) 已把核心图像列表追溯到 manager、lookup 及上层集合的实际元素槽；记录了取消后 manager 2→1 的调用者和集合取得引用的路径，尚未确定上层集合继续存活的原因。

外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-cancel-select-20261004`。以下未另注明的路径均相对此目录，时间均为 2026-10-04 UTC。媒体、诊断程序、ETL 和原始收据不纳入仓库。

## 已登记的条件

- 两组复用前轮**同一个既有文件**：`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-association-20261004\fixtures\case-a\sample.flv`。未重新复制、改名或替换文件身份。文件为 41,698,926 字节，完整 SHA-256 为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。私有源文件仍位于仓库工作目录的 `testdata/private/corpus-long-20260930/generated-long-avc-aac-2400s.flv`，同大小、同哈希。
- 两组使用前轮同一原生 `muxray-gdi-association-20261004/native-host/bin-001/MuxRay.exe`，不重编译或更改宿主。它不含 Go、Wails、WebView2；每轮创建独立 STA 工作线程，调用 Show 后释放接口、CoUninitialize，确认真实线程退出、关闭线程 HANDLE 后再启用按钮。主 UI STA 保持存活。宿主不调用 GetResult，也不自行读取媒体；Shell 内部仍可能读取内容。
- 先 `cancel`、后 `select`，分别新建普通权限进程。宿主真实 `case` 始终为 `flv`，实验组由额外的 `arm` 字段区分。SetFolder、`*.*` 筛选和 FLV 按钮保持相同。先前进程退出后才允许第二组采集，不把 `cancel/select` 伪写成宿主的两种 case。
- 每组预热也先单击 `sample.flv` 行，确认选中状态和文件名，等待至少 30 秒后真正取消，再静置至少 60 秒。预热位于正式 ETW 之前。
- 正式三轮均先确认 Details 视图、Name 升序及实际分组，单击同一文件行并保存输入与选中证据。由行输入 `completedUTC` 到关闭输入开始 `utc` 至少间隔 30 秒；等待后、关闭前保存 Show 模块快照。取消组单击 Cancel，成功选择组单击 Open，不使用双击。
- 关闭输入实际 `completedUTC` 分别作为 60、60、180 秒静置的锚点。模块 post 快照须在工作线程 join、HANDLE 关闭及按钮启用之后；每组预热及三正式轮共八份 Show/post 快照。必要的视图恢复操作及真实耗时另行保留，不能从一次最终截图声称整个 Show 布局相同。
- 每组开始前及末轮静置 180 秒后、正常退出前，排他读取原始源文件和同一既有 fixture，核对路径、大小及完整哈希；不在每次关闭后额外读文件。未修改注册表或清理 Shell 缓存。

协议为 `case-protocol-001.json`，文件与复用条件分别绑定在 `fixture-binding-001.json`、`reuse-binding-001.json`。启动前重新核对既有 EXE、源码、构建收据、13 个本机映像和两个核心 PDB；本轮另行完成实际模块映射、直接 stack64 和丢失检查，未把旧证明代替本轮采集验证。

## 采集范围与首次失败

两段正式跟踪各为 900 秒、768 MiB 上限，760 MiB 附近提前停止保护，独立 watchdog 为 915 秒，协调器等待 collector 上限为 945 秒。全局采集 Win32k 455–458，保留事件头 PID 与 payload owner，不预先按目标 PID 裁剪。两次人工原子 GO 严格按组顺序执行；每组 GO 等待上限 1800 秒，单个协调器总上限 6600 秒。第一段须 Stop 成功、丢失为零且精确会话 Query 为 4201，第一宿主退出后才允许第二段。

资源与 GDI 探针在预热前启动，固定上限 2400 秒、间隔 1 秒、正常退出宽限 5 秒，不自动延长。第三次关闭须在跟踪起点 +660 秒之前完成，第三轮长静置须在 +840 秒前结束；超时或未覆盖窗口要保留为偏差，不隐式追加采集。

首次 `arm-cancel-001` 于 **03:22:06** 派发，但 `StackSession.Start` 的托管参数白名单仍只允许 128/512 MiB，在调用原生 StartTrace 前拒绝了登记的 768 MiB。最初静态准备遗漏了这一层校验。原始 receipt 的状态为 `stop_failed`，其 finalStats 实际是会话不存在的 Query 4201；这不表示一个已运行的会话无法停止。

`failed-start-scope-review-001.json` 确认前后精确会话查询均为 4201，无 ETL、active、start-result 或 watchdog ticket，当时没有任何正式轮次 GUI 收据。原协调器 PID 32936 / 创建 FILETIME `134355573731888537` 已观察为不存在；预热后的普通权限宿主 PID 7584 / `134355574161147757` 仍为同一目标。原失败目录、原 manifest、原脚本及探针记录全部保留。

`correction-amendment-001.json` 明确登记了一次人工恢复：`StackSession-002.cs` 仅在容量白名单中增加 768，并修正异常文字；`collect-stacks-002.ps1` 引用新声明，其余采集界限不变。新 `manifest-002.json` 冻结 25 个本轮文件和 25 个外部输入，保留原协议 SHA。采集目录改为 `traces/arm-cancel-002`、`traces/arm-select-002`，原采样目录 `run-arm-cancel-001`、`run-arm-select-001` 不改。

修正只允许精确身份及完整 identity SHA 均匹配的已预热取消宿主继续，未重复预热；进程新鲜度仍以原协议登记时间为界。第二组继续要求独立的新进程和首组真实退出。新协调器还检查旧失败会话不存在、旧协调器不再存活、原目录无 ETL/active。协调器不自动重试、发布 GO、启动目标或执行 GUI 输入，探针时限未延长。修正版 C# 编译及 x64 ABI 布局验证通过；随后 002 的 active 收据记录了本轮 768 MiB 正式会话成功启动。

## 固定窗与对象判定

每组统计预热取消及三轮关闭后的 +30～60 秒，第三轮再统计 +150～180 秒，共五窗。仅纳入完整位于窗口内的 GUI 查询、资源采样迭代和 CPU 区间；工作线程 join、HANDLE 关闭及按钮启用须早于窗口起点。跨进程比较各自预热基线后的增量，不能把绝对基线差异直接解释为关闭行为影响。

| 实验组 | 预热基线与四个后续固定窗 | 精确核心 Create/Destroy/保留 | 正常退出与清理 |
| --- | --- | --- | --- |
| cancel，宿主 case=flv | 171 → 173 → 175 → 177 → 177 | 3 DC/3 Bitmap 保留；另 1 DC/1 Bitmap 正常 Destroy | 正常退出及清理通过 |
| select，宿主 case=flv | 171 → 173 → 175 → 177 → 177 | 3 DC/3 Bitmap 保留；未匹配到精确核心 Destroy | 正常退出及清理通过 |

两个会话分别重建完整句柄代际，不跨会话拼接。独立复核逐条核对完整句柄、SessionId、低 32 位可能别名、Create/Destroy、Owner 迁移与变换，并区分工作线程创建和其他线程/进程上下文。审核没有预设候选数目或保留结果，零匹配及全部销毁也可作为合法观察结果。

沿用已核实的八帧缩略图核心 SHA/RVA，再分别核对已证明的 `CImageManager::SetPhysicalImageSizeAndDPI` 与 `CImageManager::ResetImages` 返回点。创建、销毁事件同时对照行选择和关闭输入区间；未按相近符号名归类，时间先后本身不证明因果。

“静态”只指截至该会话最后选定事件，没有同一完整句柄代际的后续 455–458。实际线程退出后的对象观察下界须以最后选定事件计算，并确认对象此前已创建，不能用较晚 Stop 时间或采样计数代替同一对象的证据。下表中的观察下界向下保留到毫秒。最终已知创建候选、未知起源和可能别名分别报告，不全部命名为泄漏。

## 取消组已核实的结果

正式采集为 **03:31:31.0662879–03:46:32.8230529**，Stop 为 0、丢失为零。ETL 为 438,829,056 字节，选定事件 700,670 条，455/456/457/458 分别为 318,493 / 318,468 / 58,802 / 4,907；全部直接 stack64，Open/Process/CloseTrace、解析和 ETL 头及消费者丢失检查通过。最后选定事件为 **03:46:22.4602715**，对象观察下界截至这里，不延长到较晚 Stop。

预热取消完成于 03:20:50.614，三次正式取消完成于 03:33:31.758、03:36:58.967、03:40:09.067。三次从行输入完成到 Cancel 输入开始分别为 66.391、74.114、86.689 秒；均满足至少 30 秒，但实际 Show 时长分别约 82.803、104.251、96.970 秒，并不相同。第三次关闭在跟踪起点后 518.001 秒，长静置结束为 +698.001 秒；660/840 秒门限和所有正式固定窗覆盖通过。

五窗各有 30 个 GDI 样本，GDI 值各自恒定。预热窗因完整迭代/CPU 区间纳入规则包含 29 个资源样本、28 个 CPU 区间；其余四窗各 30/29 个。各工作线程 join、HANDLE 关闭和按钮启用均早于对应统计窗，静置区间无额外输入。取消组最终采样审核全部检查通过。

| 正式轮次 | 保留 DC 完整句柄 | 保留 Bitmap 完整句柄 | 实际工作线程退出后无后续事件下界 | 已核实上层分支 |
| --- | --- | --- | ---: | --- |
| 1 | `0x0000000058011348` | `0xffffffff85051787` | 770.578 秒 | SetPhysicalImageSizeAndDPI |
| 2，重建后 | `0xffffffffcb0124e2` | `0xffffffffa3050121` | 563.370 秒 | ResetImages |
| 3 | `0x000000001f011a0d` | `0x0000000039052525` | 373.234 秒 | SetPhysicalImageSizeAndDPI |

这些对象均在对应工作线程的 Show 内创建，跨过各自实际线程退出，截至最后选定事件没有后续 Destroy、Owner 迁移或句柄变换，未发现可能的低 32 位其他拼写。第一、三轮保留 pair 在单击文件行之前已经创建；第二轮重建 pair 在行选中后、Cancel 输入开始前创建。取消结果由真实按钮输入和 Show 的 `0x800704c7` 共同核对，未用 worker exit code 0 代替取消判定。

第二轮原 DC `0xffffffffa2010121`、Bitmap `0x0000000075051be8` 的 Create 序号为 208356/208362；在 **03:36:55.0657128/03:36:55.0657466**，Destroy 序号 408271/408272 结束了这两个完整句柄代际。随后 Create 408274/408277 在 03:36:55.0658894/03:36:55.0659851 创建上表的新 pair，返回点精确匹配先前经 PDB/PE 证明的 ResetImages 分支。释放与重建均发生在行输入完成约 70.307 秒后、Cancel 输入开始约 3.807 秒前；旧 pair 的销毁不能记为新 pair 已释放，也不能解释为 Cancel 操作的效果。

只对这两条 Destroy 和前轮 BIN 第一轮对应的两条 Destroy 做了额外离线符号核对，未增加采集或下载符号。两组完整用户 SHA/RVA 栈并不完全相同：DC 为 58/55 帧，Bitmap 为 59/56 帧；从销毁端数起，前 40/41 个用户帧分别相同，未映射用户帧均为零。复用本地 Windows.Storage/Comctl32 PDB，28 个选定地址全部通过映像哈希、PDB GUID/age、地址及返回地址减一与符号起点同处 PE 函数范围的核对。Destroy 栈明确经过 `CImageManager::ResetImages → CThumbnailLookupTable::Release → CThumbnailLookupTable` 析构，再经多层图像列表容器的 Release/析构到 `CImageList::Release → ~CImageList → _Destroy`；Bitmap 还经过 `_DeleteBitmap`。这是精确核心旧 pair 的正向释放路径证据，并非仅较宽 Initialize 对照；但调用栈没有给出对象接收者指针、引用计数或通知来源，不能据此声称后续保留的新实例也执行了同样释放。详见 `offline-analysis/exact-core-destroy-path-001/pe-symbol-boundary-review-001.json`。

较宽的同一 Initialize 返回点另有 42 DC、54 Bitmap 正常 Destroy，深层调用者不等同于精确八帧核心。全局最终已知创建候选 18 个、未知起源 7 个分别保留，已知候选歧义计数为 0；这些总数不全部解释为泄漏。独立审核重新扫描全部 700,670 条原始事件，核对列出的 202 条 Create/End、源核心及上层 SHA/RVA、GUI 相对时序和实际线程退出，13 项检查全部通过。

取消宿主 OS 正常退出于 **03:47:40.8137204**，exit code 0；四个工作线程及主 UI COM 清理完整。资源/GDI 共 1,752/1,747 条，最大相邻 UTC 间隔为 1.847/1.522990 秒，连续序号和覆盖审核通过。仅首次 CPU 因没有前一采样而为空，未用零填充；只观察到根宿主，未观察到后代，两个探针完成退出收尾。源文件与同一 fixture 的前后排他哈希通过。

## 成功选择组及跨组复核

成功选择组为新的普通权限进程 PID 27028 / 创建 FILETIME `134355593228384086`，在取消宿主退出后创建。正式采集为 **03:52:57.8654109–04:07:59.8243022**，Stop 为 0、丢失为零。ETL 为 388,497,408 字节，选定事件 607,619 条，455/456/457/458 分别为 283,416 / 283,393 / 37,686 / 3,124；全部直接 stack64，解析、消费者及 ETL 头丢失检查通过。最后选定事件为 **04:07:56.3357722**。

预热取消完成于 03:51:33.409，正式 Open 输入完成于 03:54:51.867、03:58:00.729、04:01:09.303；三轮行选中到关闭开始分别为 65.484、88.813、72.056 秒，Show 总时长约 79.404、100.875、92.206 秒。第三次关闭为跟踪起点后 491.438 秒，末轮长静置结束为 +671.438 秒，均满足门限。五个固定窗各有 30 个 GDI、30 个资源样本和 29 个完整 CPU 区间，GDI 各窗恒定。

| 正式轮次 | 保留 DC 完整句柄 | 保留 Bitmap 完整句柄 | 实际工作线程退出后无后续事件下界 |
| --- | --- | --- | ---: |
| 1 | `0xffffffff8f0124e1` | `0x000000006e051f32` | 784.298 秒 |
| 2 | `0xffffffffe30119ae` | `0xffffffffc8051c05` | 595.464 秒 |
| 3 | `0xffffffffd10119ac` | `0xffffffffbf051548` | 406.883 秒 |

六个精确核心对象均由对应工作线程在 Show 内、单击文件行之前创建，跨过该线程真实退出后没有后续 Destroy、Owner 迁移或句柄变换，也没有低 32 位其他拼写。它们均命中 SetPhysicalImageSizeAndDPI 分支；本段精确核心匹配记录没有 Destroy 或已验证 ResetImages 返回点命中，不能据此声称整个进程没有执行过 ResetImages。较宽 Initialize 返回点仍有 42 DC、54 Bitmap 正常 Destroy。全局最终已知创建候选 16 个、未知起源 9 个，已知候选歧义为 0。独立审核重扫 607,619 条事件并核对 198 条列示 Create/End，13 项检查全部通过。

成功选择组三轮同类型完整用户 SHA/RVA 栈相同，并与取消组常规创建相同：DC 70 帧、Bitmap 73 帧，均无未映射用户帧。这包括取消组第二轮后来正常 Destroy 的原 pair；该轮随后保留的 ResetImages 新 pair 则为 47/50 帧。因此，**同一完整创建栈既对应正常销毁，也对应保留**；创建栈本身不能判定最终引用为何继续存在。两组的全部完整栈签名集合不同，不能只凭最终各 +6 抹去释放、重建与时长差异。

成功选择宿主 OS 退出于 **04:09:07.4333859**，exit code 0；四个工作线程及主 UI COM 清理完整。资源/GDI 共 1,196/1,192 条，最大相邻 UTC 间隔为 1.948/1.001213 秒，覆盖和连续性审核通过；仅首个 CPU 区间为空，只观察到根宿主且正常退出，两个探针完成宽限。源文件与 fixture 前后排他哈希通过。两组所有正式固定窗都完整落在各自最后选定事件之前，末轮三分钟计数有同期对象跟踪覆盖。

两组八次选择器操作的各次界面观察均确认 Details、Name 升序及日期分组展开，未执行视图校正输入。16 份模块快照齐全，每轮 Show/post 分别记录 136/132 个模块；实际 SHA/RVA 映射另行逐一审核。界面观察和模块快照只代表各自记录时点。

`offline-analysis/cross-arm-comparison-001.json` 的七项跨组检查通过：同一 EXE、协议及既有 fixture，新进程顺序，独立会话代际，以及两组采样、独立原始审核与正式窗口覆盖均匹配。最终 `final-owned-capture-cleanup-003.json` 九项检查通过：失败尝试的一个精确会话及成功采集的两个会话 Query 均为 4201；两个协调器、两个宿主和四个探针的精确身份均已不在运行。三个 collector、两个 watchdog 仅有记录 PID，当前均不存在，未虚构创建身份或历史退出码。原 manifest 的 11+25 项及修正版 25+25 项文件哈希重新核对一致。

## 解释范围与下一步

两组固定先取消后选择，共享 Shell 缓存与外部环境；相同行选择最低等待时间不保证总 Show 时长相同。应保留视图恢复、线程时序、模块加载及观察到的后代进程，不从时间重叠归责第三方。模块快照是点时映射；分配栈不是最后引用持有者。预热、启动到探针附加之前的时间，以及正式 ETW 后的进程退出阶段均有各自观测边界。首次准备失败额外增加了取消组的预热后等待时间，也须作为组间差异保留。

常规 pair 在单击文件行前已经分配，说明这些分配本身不必等待行点击；这不证明行选中对后续持有没有影响。本实验支持成功接受不是此次存留的必要条件，不把 Cancel/Open 认定为普遍等价，也不把所有全局候选标成泄漏。两组跟踪均在进程退出前停止，正常退出及句柄计数不能替代退出阶段的对象销毁证据；生产代码未修改。

下一步应沿具体图像列表实例与 DC/Bitmap 的对应关系，检查 `CImageManager`、lookup-table、各图像列表的 Release/析构及实际引用持有边界。已有精确核心 Destroy→ResetImages 重建样本可作释放正对照。若保留 pair 所属实例已完成析构而仍没有匹配 Destroy，则“只是最后引用未释放”的假设不成立；若只有销毁样本完成析构，则继续追查保留实例的上层引用，但不能仅凭缺少析构就断定持有者。该诊断须先核实实例指针和接口别名，不直接强制 Release、修改 Wails 或把创建路径当作生产修复点。

## 主要证据 SHA-256

| 相对路径 | SHA-256 |
| --- | --- |
| `case-protocol-001.json` | `3da4933dcb17b967f7fa216181db0417b89c507f087db722fcde40866679b333` |
| `manifest-001.json`，失败尝试 | `9b1ab93a0b7663cd6fd7c995e51bd0228eb90cae9154059b518f902dd9a614d3` |
| `correction-amendment-001.json` | `9397050f59a26dc1792cdefcdaea1c20fe9d89f6c0ed590cfecc80415bfdedfa` |
| `manifest-002.json`，人工修正 | `be6febd182b4ad917b7f2a75557b2c706106234560823686f429cf72c74f5c7f` |
| `traces/arm-cancel-002/win32k-gdi.etl` | `d7a1a11bedc25872d16a30032646957b31c872cd8632be0181ed0fde5bac05e4` |
| `traces/arm-select-002/win32k-gdi.etl` | `03f20cb660520858e00db6386af66d4ce43e15604040c6b8f605dc7710337a4c` |
| `traces/arm-cancel-002/decoded/cancel-select-source-summary-001.json` | `fcfcad429d4acfcd9365577bec74b686707f974f79e6fc4be0a4ba4eba7adf02` |
| `traces/arm-select-002/decoded/cancel-select-source-summary-001.json` | `fe176ff91dfee5290f5c4558d1d32b5a768eff08e2fa60bf02cda850692a2de7` |
| `offline-analysis/independent-cancel-source-review-001.json` | `59bf961b3b6b7380ce89f0ecab03439e6452a2052aca4a20bfded63e2844c464` |
| `offline-analysis/independent-select-source-review-001.json` | `9ee314b2cd781144b1f6f2fde1f018280b7cfdcd1adc6183b7818ef3aa03c53a` |
| `run-arm-cancel-001/final-sampling-exit-review-002.json` | `953188597c9599cfac7d21ae84fd92a2e50c74214cd612b798b0667e54d93e49` |
| `run-arm-select-001/final-sampling-exit-review-002.json` | `f6f2ff2864de1bc34fd56ebe41c4854ccfc9fa5c60ba2bef82b3485295289569` |
| `offline-analysis/exact-core-destroy-path-001/pe-symbol-boundary-review-001.json` | `5e1b7e94d932d2db55a9abe6826f1012bef51b336c64f69668263250f0f8715a` |
| `offline-analysis/cross-arm-comparison-001.json` | `91e870aeb63b5011486c0274d812923ba6bce1e9832da0a56e44f116decbe34d` |
| `final-owned-capture-cleanup-003.json` | `b5b2448c30b7922434c409f88dee9435f0998c81691320f06d7c35044d820e83` |

本轮仓库仅新增诊断报告并更新索引和前篇后续链接。完成八次真实选择器关闭、十个固定窗、两段正式 ETW、独立原始事件及既有 Destroy 离线符号复核、两宿主正常退出和最终清理；未修改生产代码或重跑无关业务测试。
