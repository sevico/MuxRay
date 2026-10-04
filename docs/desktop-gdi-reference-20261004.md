# 具体图像列表与引用持有者：2026-10-04

本轮把保留的图像列表追到了 **Shell 内部集合的具体元素槽**。第二周期取消、接口释放、COM 清理和工作线程退出后，`CEnumerableObjectCollection` 接收者 `0x20c2d4b5fb0` 的数组仍有一个元素，槽 `0x20c342701b0` 保存 `CImageManager` 的接口 `0x20c28402b30`。实际 `AddObject` 调用先写入该槽，再对这个 manager 执行 AddRef；关闭时 UI renderer 释放另一份引用，manager 最后剩 1，其 lookup 和整条图像列表链继续保留。

**当前决定：不修改 MuxRay 的生产释放逻辑。** 已取得具体集合持有路径，尚未证明这个上层集合为什么没有清空或释放，也未穷尽其外部持有者。不能据此强制 Release、直接删除图像列表内的 DC/Bitmap，或宣布 Windows 的最终根因已经确认。底层两个引用还分别对应不同接口别名，不能把非零计数直接解释为多余引用。

接续 [取消/成功选择对象级对照](desktop-gdi-cancel-select-20261004.md)。本轮使用硬件指令观察点、数据写监视点和有限只读字段快照；没有启动新的 ETW。图像列表字段与句柄的对应关系来自真实目标内存，**不替代此前 ETW 的 GDI Create/Destroy 事件，也不证明本轮调用了哪些成功的 DeleteDC/DeleteObject**。

外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-reference-20261004`。下文路径均相对此目录，除快照自身标明的 `+08:00` 时间外，正文时间为 UTC。诊断工具、原始内存字段、调用栈及截图保留在外部证据目录，不纳入仓库。

## 条件与观测边界

- 复用既有独立原生宿主 `muxray-gdi-association-20261004/native-host/bin-001/MuxRay.exe`，SHA-256 `dae9d843d0e19906b86fda9a4b6a85e01be9fb760a36ab44d6dd12d688d0e980`。不含 Go/Wails/WebView2，每轮独立 STA，Show 后释放接口、CoUninitialize、等待工作线程真正退出并关闭 HANDLE。
- 复用同一 41,698,926 字节 FLV，SHA-256 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。不调用 GetResult、不改文件关联、不清理 Shell 缓存。
- 成功观测宿主 PID **26600**，创建 FILETIME **134355830275935246**，由 Computer Use 启动；全部只读和调试操作绑定 PID、创建时间、EXE 路径和哈希。桌面输入仅操作该宿主及其文件对话框。
- 两周期都观察到 Details、Name 升序和展开的日期分组。分别单击同一文件行后等待 84.081 / 125.046 秒，再真正取消。实际耗时保留，不把两次 Show 说成完全等时。
- 硬件控制器 `reference-controller-win32-004.exe` 使用最多四个 DR 槽，900 秒、20,000 次命中、64 MiB 日志上限；不修改目标代码、不调用对象方法。调试暂停会影响时序，不能与此前非调试采样直接比较速率。
- 普通首次异常以 `DBG_EXCEPTION_NOT_HANDLED` 交还应用处理。未知断点、未知 SINGLE_STEP 和第二次机会异常仍会停止采集；不吞掉应用异常。
- 对象图和字段读取不暂停目标，是有界、非原子快照。可读虚表、正引用计数和相同指针并不单独证明分配代际、完整所有权或跨观测间隙的连续存活；相应限制保留在原始收据中。

## 第一周期：把核心 DC/Bitmap 绑定到对象链

`run-pilot-hw-002/events.jsonl` 在打开前安装了四个指令观察点：lookup 初始化结果、manager 发布 lookup、Basic 图像列表初始化结果和 Basic 析构入口。四点按实际模块 SHA、RVA 和现场前八字节核对；全部线程，包括新增线程，均保存硬件设置及回读结果。

核心 Basic 实例在事件 **328** 完成初始化，EAX=0，DC=`0x67011512`，Bitmap=`0x79050baf`。栈经过匹配映像中的 Multi/Sparse 初始化与 `CThumbnailLookupTable` 创建核心，和对象图读取一致。同一个地址较早在事件 314/315、325/326 已发生其他初始化/析构，且句柄不同；这些较早阶段不能合并为事件 328 的同一资源阶段，也没有构造/分配/释放点来证明完整分配代际。

实际路径是 manager 的 lookup 槽 → lookup 的 Gang/Aggregate 容器 → Aggregate 第二个元素的 Sparse → Multi 的两张 DPA → 同一 Basic 的两个接口别名。lookup 同时直接持 Aggregate，并经 Gang 的元素再持它；因此 Aggregate 的 2 有明确的两个槽位来源。Multi 的两张 DPA 同样保存 Basic 的不同接口别名，Basic 的 2 不能单凭计数认定为泄漏。

| 对象 / 字段 | 第一周期地址或值 | 第二周期地址或值 |
| --- | --- | --- |
| CImageManager canonical | `0x20c2d145c80` | `0x20c28402b30` |
| manager `+0x38` lookup | `0x20c2d4dade0` | `0x20c2d4390e0` |
| 核心 CMultiImageList canonical | `0x20c2d49b3a0` | `0x20c343c3ae0` |
| 核心 CImageList canonical | `0x20c2d388bd0` | `0x20c2d5367f0` |
| Basic 主 DC `+0xc0` | `0x67011512` | `0x3e012042` |
| Basic 主 Bitmap `+0xa0` | `0x79050baf` | `0xffffffff880522ae` |
| Basic mask DC / Bitmap | 均 0 | 均 0 |

`graph-dialog-open-001.json`、`graph-before-watch-001.json` 和 `graph-watch-ready-001.json` 核对了安装切换前后的对象字段。指令观察阶段正常脱离后，`stage2-001` 改为监视第一周期 manager `+0x20`、lookup `+8`、Multi `+0x30`、Basic `+0x38` 四个 DWORD 引用字段。起点计数为 **2 / 1 / 2 / 2**。安装前 manager 曾从发布时的 3 降到 2，这段变化不属于此数据监视阶段的覆盖范围。

取消后的最后一次 manager Release 位于事件 **5565**，10:32:27.403028：确切 post-xadd 指令和 EDI 旧值证明 **2→1**。其调用路径为：

```text
UIImage::_Update / UIImage 析构
  → CDefImageRenderer::Release
  → CDefImageRenderer::~CDefImageRenderer
  → CImageManager::Release
```

这证明 renderer 关闭时释放了一份引用；这份 Release 栈本身不说明剩余引用由谁持有。第一周期工作线程 4140 于 **10:32:27.4440517** 真正退出，join、CloseHandle 成功。超过 60 秒后的对象图仍为 **1 / 1 / 2 / 2**，整个核心路径和同一组句柄字段不变。

封存阶段共 **5,768 条记录、15 次引用字段写入**：manager 7 次 AddRef、8 次 Release，其余三字段零写入。全部写入均核对线程代次、DR 槽归属、指令语义和调用栈。写后计数由精确指令的旧寄存器值计算；`observedValueAtDebugStop` 仅是暂停后读到的内存值，不能代替并发原子操作的实际结果。

## 第二周期：追踪上层集合取得引用

第一周期结束后，`stage3-001` 在再次打开之前安装四个静态点：manager AddRef 的 xadd 后、Release 的 xadd 后、lookup 发布、Basic 初始化结果。新 manager 为 `0x20c28402b30`。对象身份由接收者和已核虚表判断；共享函数的符号别名不能代替类型判断。

新 manager 的 **44 次已观测增减引用为 22 AddRef、22 Release，首个旧值 1、最终新值 1，没有观测到 0**。涉及两个线程代次；日志相邻算术值吻合，不把跨线程调试事件的交付顺序扩写为全局原子先后证明。该阶段没有构造赋值观察点，不能声称捕获了最初的 ref=1 写入。另有 11 次共享 AddRef 函数命中属于其他四组接收者，保留在原始数据中，没有混进这个 manager 的计数。

早期事件 229/230 是工厂 QueryInterface 的增引用及临时引用释放。事件 **231** 发生在 `CEnumerableObjectCollection::AddObject`，上层为 `CDefCollection::PrepareImageManagers`；事件 232 释放准备阶段的临时引用。匹配机器码证明 `AddObject` 先把 manager 写到 `[R8]`，再调用它的 AddRef，因此这里有实际的存储槽与增引用证据，而非只按计数抵消猜测持有者。

窗口关闭后，有限只读快照 `holder-fields-after-cancel-001.json` 取得以下值，十个字段读取均成功：

| 实际位置 | 读取结果 / 含义 |
| --- | --- |
| AddObject 接收者 | `0x20c2d4b5fb0` |
| 接收者 `+0x28` | 数组地址 `0x20c342701b0` |
| 接收者 `+0x30` / `+0x40` | 元素数 1 / 容量 4 |
| 事件 231 的 R8，即数组元素槽 | `0x20c342701b0` |
| 该元素槽的值 | manager 接口 `0x20c28402b30` |
| PrepareImageManagers 接收者 | `0x20c2d3e6ce0` |
| 该接收者 `+0xb0` | 集合接口 `0x20c2d4b5fb0` |

匹配 PDB、构造代码和接口 Release 调整 thunk 确认，PrepareImageManagers 的接收者是 `CDefCollection` canonical `0x20c2d3e6c50` 的 `+0x90` 接口；因此上述 owning 槽就是 **canonical `+0x140`**。补充快照中，CDefCollection 的 `+0xa8` DWORD 引用计数为 **2**，Enumerable 的 `+0x18` DWORD 引用计数为 **1**。

对偶清理路径确实存在：CDefCollection 析构会清零 `+0x140` 槽并 Release；Enumerable 的 RemoveObjectAt 和 Clear 会释放保存的元素。这些是当前匹配二进制中的静态代码证明，不能当作本轮已经执行过这些清理的证据，更不能声称实现中完全缺少 Release。

因此，本次观测已从末端 DC/Bitmap 追溯到上层集合的一个具体持有元素。仍需区分“集合为什么继续存在”“集合是否应该在此时清空”以及更上游的引用关系；单靠这些字段不能宣布循环引用或定位应修改的 Windows 函数。

第二周期工作线程 6604 于 **10:39:47.5076330** 真正退出；10:41:02 的 quiet 快照仍保留 manager=1、lookup=1、Multi=2、Basic=2 和上表第二组句柄。`stage3-001` 封存 **7,585 条记录、71 个指令点命中**，随后正常脱离。三段成功观测均验证了 DR 恢复、Suspend/Resume 配平和无残留调试器。

两轮 quiet 图分别晚于真实线程退出 **91.508019 / 74.854520 秒**。取各轮退出后第 30 至 60 秒的固定窗口，各有 30 个 GDI 样本，分别恒为 **171 / 173**；此计数与具体字段证据并列保存，不把计数差直接当成新的 GDI 对象生命周期证明。

## 采集器修正与失败样本

软件断点和早期 DbgEng 硬件版本未通过并发夹具的计数/脱离验证，未用于真实宿主。直接 Win32 版本先在独立夹具验证四个线程、递归、引用写入、四个指令点、主动达到事件上限、脱离后的原线程继续执行和实际 OS 正常退出。

首个真实尝试 `run-pilot-hw-001` 使用 003，遇普通首次 C++ 异常便过早停止；只取得 20 个 Basic 点，没有 manager/lookup 证据。该次 `cleanupPass=false` 原样保留，不能改写为通过；线程 DR/TF、Suspend 配平和调试器脱离各项恢复成功。随后通过正常 Cancel 和关闭结束 PID 10252，两个探针确认实际退出。

004 只修正普通首次异常的转交分类，主循环和清理阶段都交还目标自身处理。full、四点、事件上限三类夹具各自抛出并捕获 120 次 C++ 异常，计数、原线程继续运行和实际退出均通过。上限测试还覆盖主循环 16 次、清理阶段 104 次异常转交。未知硬件异常的严格处理没有放宽。

第二阶段启动器曾因查询句柄权限不足在附加前失败，未创建阶段目录或启动调试器。后续另存 005，补充只读 `PROCESS_QUERY_INFORMATION` 并区分查询失败与确有调试器，未覆盖旧脚本。所有这些失败均保留，不作为成功观测的证据。

## 修复判断

现有证据支持继续沿 **CDefCollection 保存的集合及其清理边界** 定位，而不是重复调整 MuxRay 的 IFileDialog Release、GetResult 或工作线程结束顺序。独立宿主同样保留该链，且已证实 dialog Release=0、COM 清理和线程退出。

当前不实施强制 Release、手工 DeleteDC/DeleteObject 或 Windows 内存补丁。若下一步选择应用层缓解，可单独验证文件选择器辅助进程正常退出是否完整回收资源，再评估取消、结果传递和异常退出对桌面流程的影响；此方案尚未实现或验收，不能称作已修复。若继续追根因，则以这两个集合接收者、元素槽和确切调用站点为入口，验证集合清空/析构和上层持有关系。

## 主要证据 SHA-256

| 相对路径 | SHA-256 |
| --- | --- |
| `helpers/reference-controller-win32-004.exe` | `cf21a6d1d46f021afb27feeb9159eef3aabff2414feb416cb08eb76cbb250fb1` |
| `offline-analysis/controller-win32-004-independent-review-001.json` | `eef297c05c508ad2a68e6525292bd4465ebe4c519ceea65bdc766591aa828eba` |
| `offline-analysis/pilot002-graph-independent-review-001.json` | `465fb52e3066f8dbf51d5a470b1747b4099e6a1156f6c6fc6afa3a52e1322344` |
| `run-pilot-hw-002/stage2-001/events.jsonl` | `056cf9e6d808bed8522e8024150df4fb8a32d3984227afecc27ed3588faa1948` |
| `offline-analysis/pilot002-stage2-write-review-001.json` | `4a131bb506ba17c992bbfd811b700af0229fe628c4a33ced92e460409d112f96` |
| `offline-analysis/stage2-manager-symbols-001/final-caller-review.json` | `1d88f8433836ffccabd273b036cdb3ff454e15bd0fb056fdc86850fcb6a7ad2d` |
| `run-pilot-hw-002/stage3-001/events.jsonl` | `8bb1d8ab9cecea30a5958d45fc930a4da4f3515578d9957560d8e3112d9c4b28` |
| `offline-analysis/pilot002-stage3-manager-review-001.json` | `d205986ea7e35976362e09a5ed7e3278f437901c474e607eb2586be49a30985f` |
| `offline-analysis/holder-path-001/holder-field-proof-001.json` | `e53cad7b0f61b77fedbb496b89c8e58eb36fa7a8a3bf7608c71d4c8509f7edf3` |
| `run-pilot-hw-002/holder-fields-after-cancel-002.json` | `da02a1abfb003ee229ba055e6e27dfb47aca6a137b72fe22fce34bda1ce9120f` |
| `offline-analysis/pilot002-final-acceptance-001.json` | `8e98d2ea8a936a4fc8b7985a780d84e6e867c0e461d7d5a33d705cf9689f0089` |

宿主于 10:50:26.595–10:50:26.807 通过 Alt+F4 正常关闭，主线程 CoUninitialize 完成；独立进程句柄确认 **10:50:26.9965422 实际 OS 退出、exit code 0**。资源探针共 1,471 条，记录 `target_exit_grace_complete`；GDI 探针共 1,465 条，记录同一进程创建身份和实际退出。宿主 EXE 与同一 FLV 的收尾 SHA 再次核对一致。

最终综合审核适用项全部通过。资源/GDI 序号连续，最大相邻间隔为 1.003222 / 1.006050 秒；只有首个资源样本的 CPU 因没有前一时点而为空，没有用零填充。三个成功阶段的控制器、两个探针及诊断 fixture 均无活动残留；旧失败宿主的终止对象虽仍可查询，等待状态已确认退出，旧失败采集的结论原样保留。

本轮仓库变更为诊断报告及索引，生产代码未修改。验证包括采集器并发与异常转交夹具、三段真实观测、两周期原生取消、匹配二进制/符号与具体持有槽的独立复核；没有为文档变更重跑无关业务测试。
