# CDefCollection 引用来源与集合清理边界：2026-10-07

本轮在两个全新原生宿主中，分别完成一次选中同一 FLV 后取消，并观察到工作线程真正退出 60 秒以上。两组最终均读到 **CDefCollection=2、CEnumerableObjectCollection=1、manager=1、lookup=1、核心 Multi=2、Basic=2**；集合仍有一个元素，指向 manager。A 的 Clear 入口、B 的 RemoveObjectAt 入口及两组 CDefCollection 析构入口均未命中。

引用来源进一步落实到 ItemsView、FirstPageResults 和 CollectionCache 的具体取得路径及释放对偶。其中 FirstPageResults 的同一对象已执行对该集合的 Release，但析构后的原槽仍可读到相同接口指针。这是不能把残留内存字节直接当作活引用持有者的实际反例。

**本轮完成边界验证，尚未确认最后两份引用的持有者，也没有得到可安全修改的 MuxRay 生产释放点。** 因此没有强制 Release、删除内部 DC/Bitmap 或修改 Windows 代码。接续 [具体图像列表与引用持有者](desktop-gdi-reference-20261004.md)；下一步建议单独验证文件选择器辅助进程的资源隔离方案，而不继续无限扩展同一组根因采集。

外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-collection-20261007`。下文证据路径相对此目录，时间均为 UTC。原始调用栈、字段、截图和诊断工具保存在外部目录，不纳入仓库；上一轮证据保持不变。

## 条件与覆盖范围

- 两次均复用独立 Win32 宿主，EXE SHA-256 为 `dae9d843d0e19906b86fda9a4b6a85e01be9fb760a36ab44d6dd12d688d0e980`；不含 Go/Wails/WebView2。每次只有一个独立 STA 工作线程执行 Show，取消后 Release、CoUninitialize、等待真实线程退出并关闭 HANDLE。
- 同一 41,698,926 字节 FLV，SHA-256 为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。两组均为 Details、Name 升序、日期分组展开；单击文件行后分别停留 **60.455 / 63.316 秒**再 Cancel。没有 GetResult、媒体读取或文件关联改动。
- 普通权限运行，操作绑定 PID、创建 FILETIME、EXE 路径及 SHA。GUI 通过 Computer Use 操作，没有向终端注入命令。本轮不需要 UAC，也没有启动 ETW。
- 复用已通过上一轮并发、异常转交和事件上限夹具的 `reference-controller-win32-004.exe`，SHA `cf21a6d1d46f021afb27feeb9159eef3aabff2414feb416cb08eb76cbb250fb1`。本轮二进制未改，没有重跑相同夹具；重新验证实际模块、指令字节、四槽安装和每个线程的 DR 回读。
- 两组均在打开对话框前安装监测，直到退出后快照完成才主动停止。900 秒、20,000 点命中、64 MiB 上限均未触发；没有代码补丁或动态返回断点。
- 有限字段读取不暂停、不保留目标对象、不调用其 COM 方法。分阶段核对 canonical 虚表、接口别名、元素集合虚表、数组/数量/容量及元素；未知实现保持叶节点。快照非原子，不能证明完整分配代际，也不能排除间隙中的释放/复用。

当前 `windows.storage.dll` SHA 为 `8d6ac2b451af71e913665c8459935ac862e219458f0962c99f3a3d48ab9c251b`。本地匹配 PDB、PE 函数边界和机器码共同约束函数归属，未下载符号。四个硬件槽的配置如下：

| 监测点（Storage RVA） | A | B | 实际含义 |
| --- | --- | --- | --- |
| AddRef `0x1caadd` | 是 | 是 | xadd 后，EAX 低 32 位为旧值，新值为旧值 +1 |
| Release `0x2ddd3` | 是 | 是 | xadd 后，EBX 低 32 位为旧值，新值为旧值 −1 |
| CDefCollection 析构 `0x1586f4` | 是 | 是 | 仅入口，不等于析构完成、free 或 GDI 销毁 |
| Enumerable Clear `0x1f2810` | 是 | 否 | 仅入口 |
| Enumerable RemoveObjectAt `0x364c60` | 否 | 是 | 仅入口，不等于成功移除元素 |

这是两个独立进程的互补覆盖，不能写成每组同时监测了 Clear 与 RemoveObjectAt。入口零命中只约束这些具体位置与有限时段，不能证明全部内部、内联或其他清理路径都未执行。

## 具体对象与退出后状态

| 项目 | A：`run-a-001` | B：`run-b-001` |
| --- | --- | --- |
| PID / 创建 FILETIME | 37628 / 134358132904837566 | 19768 / 134358139573658578 |
| 工作线程 / 创建 FILETIME | 14080 / 134358136167005815 | 31680 / 134358140094813174 |
| CDefCollection canonical | `0x11b3b64e750` | `0x29857872200` |
| canonical `+0x140` 指向的 Enumerable | `0x11b3b589000` | `0x2985740b310` |
| Enumerable 数量 / 容量 | 1 / 4 | 1 / 4 |
| 数组元素中的 manager | `0x11b3b57c680` | `0x298578022b0` |
| manager lookup | `0x11b3b747e30` | `0x29857571700` |
| 核心 Multi canonical | `0x11b3b7477c0` | `0x29857a3d1e0` |
| 核心 Basic canonical | `0x11b41b48920` | `0x298579802a0` |
| Basic DC / Bitmap 字段 | `0x22010bcd` / `0x1605200b` | `0x32010cd1` / `0xffffffff80051e0b` |
| 工作线程实际退出 | 02:29:54.8302644 | 02:35:27.0016130 |
| quiet 快照起点 | 02:31:15.487677 | 02:36:40.864594 |

两组打开态均读到 DefCollection=18、Enumerable=1、manager=2；退出后分别等待约 **80.657 / 73.863 秒**，读到 DefCollection=2、Enumerable=1、manager=1。Enumerable 的数组仍只有同一 manager 接口；其后路径与上轮已经核实的 Gang/Aggregate → Sparse → Multi 两张 DPA → Basic 两接口别名一致。

这些是兼容的多时点字段观测，不把跨快照相同地址写成完整生命周期证明。本轮也没有重新捕获 GDI Create/Destroy；上述字段值不替代 ETW 对象生命周期事件，不能据此断言某次 DeleteDC/DeleteObject 成功或失败。

两组均记录 Show 返回 `0x800704c7`、IFileOpenDialog Release 返回 0、CoUninitialize 完成、真实线程 exit code 0、join 成功和 HANDLE 关闭。Release 返回值本身不证明全部内部对象已经释放。

## 引用取得路径与释放反证

A 的早期引用调用不仅有工厂 QueryInterface/临时引用，还包括：

| 路径 | 已核对的实际取得位置 | 取消时证据及解释 |
| --- | --- | --- |
| CItemsView::SetItemCollection | 事件 196，RSI 接收者 `0x11b3b5aaad0`；AtlComPtrAssign 的 RDI 为 `this+0x58`，AddRef 后存入 Def 的 `+0x20` 接口 | 事件 7656 的 `_ReleaseCurrentCollection` 调用该 Def Release；quiet 时原槽读到 0。没有仅凭被调用方覆盖过的 RBX 推回同一 this |
| CFirstPageResults::Initialize | 事件 201，R14 为 `0x11b3b64bef0`；具体赋值指令保存到 `this+0x60` | 事件 7674 从析构路径调用该 Def Release，旧值 7→6；RDI−0x98 恢复的 this 与取得阶段相同 |
| CCollectionCache HashBucket | 事件 198 的临时引用不当作持久槽；事件 199 的 bucket RBX 为 `0x11b3b2436b0`，`+0x10` 先存接口再 AddRef | 事件 7675 有 HashBucket 析构调用该 Def Release，6→5；quiet 时原 bucket 字段已不匹配，不推定仍是原对象或精确 free 时点 |

A 的 `upstream-fields-output-001.json` 只读取六个明确地址，全部读取成功。其中 FirstPageResults 的 `+0x60` 在取消后仍为 `0x11b3b64e770`。但匹配机器码及同一 this 的调用已证明其 Release 执行：智能指针析构读取并 Release，**没有把槽清零**。因此这份可读的相同指针不能作为“该对象仍持有一份引用”的证据，也不能用于宣布循环引用。

B 也捕获相同取得站点，具体接收者属于其自身进程。FirstPageResults 的事件 202 取得和事件 1922 的 `+0x60` 释放，通过相同寄存器/机器码推导，均对应 `this=0x2985786edf0`。离线派生的 `run-b-upstream-fields-input-001.json` 在正常关闭前未执行，保留为未使用候选；没有把它当成 B 的实际读取结果。两组都未因此增加额外运行或改变四槽配置。

最后两份引用仍未归属。Shell32 的部分帧缺少匹配私有符号，保留为模块/RVA；邻近导出名称不是函数身份，不据此命名最终持有者或根因。

## 引用账本的已知覆盖缺口

两组各有 **101 次 AddRef、109 次 Release**，共 210 个已分类点，均来自对应的工作线程。接收者已是调整 thunk 之后的 canonical，虚表为 Storage `+0x69d818`，没有再次减接口偏移。首个旧值为 1、最后新值为 2，均没有观测到 Release→0；各有 9 处相邻算术不连续。

不能用 `101−109` 推导最终引用，或把它配对成“少了几次 Release”。已匹配的 `CDefCollection::s_WndProc` 中，Storage **`0x2dca0`** 直接执行 `lock inc dword ptr [rax+0xa8]`（字节 `f0ff80a8000000`），绕过本轮 AddRef 点；随后路径调用普通 Release，返回 RVA 为 `0x2dcf7`。这证明普通 AddRef/Release 两点没有覆盖全部引用字段写入。

两组各有 9 次精确返回到 `0x2dcf7` 的 Release；各自 9 处 +1 间断中，6 处紧邻该 Release，另 3 处出现在中间调用点之前。数量吻合不等于已捕获每次内联写入，不能据此机械补成全局完整账本。析构代码另有把引用字段设为 1000 的直接赋值，本轮未进入析构，且该赋值也不是 AddRef。计数一律从原子指令后的旧寄存器值计算，调试暂停后读取的内存值另存，不拿它替代原子操作结果。

## 验证与修复判断

A 封存 7,917 条事件，39 个线程代次、51 次硬件编程；B 封存 2,095 条事件，33 个线程代次、50 次编程，回读全部匹配。普通首次异常分别为 7,409 / 1,606 次，按既有采集器策略交还应用处理；未出现未知断点、未知 SINGLE_STEP 或第二次机会异常。两组完整调用栈及模块归属均复核，均由 stop marker 正常结束、恢复 DR/TF、配平 Suspend/Resume 并脱离调试器。

两组实际工作线程退出后第 30 至 60 秒的固定窗口各有 30 个 GDI 样本，均恒为 **171**。开窗前冷基线分别为 33 / 27 个样本，均为 GDI=109、USER=74；冷启动到 171 包括首开初始化，不能把差值当作保留 DC/Bitmap 的数量，也不能与此前预热后的每轮 +2 直接相减。

A 有 343 个资源样本、336 个 GDI 样本，最大相邻间隙分别为 1.000882 / 1.0007745 秒；B 有 277 / 270 个样本，最大间隙为 1.000550 / 1.0008749 秒。只有 CPU 首点因缺少前一时刻而未定义，没有补零。调试暂停会改变时间，两个窗口不是完全等时实验，也不用于与非调试轮次比较分配速率。

A 于 **02:31:59.2949754**、B 于 **02:37:32.4223427** 实际 OS 退出，exit code 均为 0，GUI 正常关闭、主线程 COM 清理和进程探针相互核对。两个资源探针均完成退出宽限期，两个 GDI 探针均记录同代目标退出；本轮两个控制器及四个探针均无活动残留。原始事件、字段读取、前缀与最终文件绑定及退出复核均通过独立审核。

本轮缩小了判断范围：集合内部存在释放对偶；几个明确的上层取得路径也有实际关闭释放；但集合仍保留两份引用、元素未移除，最终持有者未确认。当前不应继续猜测 MuxRay 的 IFileDialog 释放次序或强行操作 Shell 私有对象。

建议下一项做**文件选择器辅助进程原型**，以辅助进程正常退出作为可控制的资源边界。验证成功选择、取消、辅助进程崩溃/超时、父窗口关联和路径结果传递，再对主进程做多轮 GDI/句柄采样，确认没有逐轮增长，并核实每次子进程实际退出。这属于待实施的应用层缓解；本轮没有实现、验收或宣称已修复。

本轮仓库仅增加诊断报告及索引。验证以实际两组原生操作、匹配二进制和符号、原始事件与快照独立审核、采集器正常脱离和进程退出复核为准；没有为文档变更重跑无关业务测试。

## 主要证据 SHA-256

| 相对路径 | SHA-256 |
| --- | --- |
| `case-protocol-001.json` | `f975ad7107a1703e13db35c85990ceb9afd466d07cf11ccb658ed87db43164ef` |
| `offline-analysis/collection-static-proof-001.json` | `d004f9da2bd4cee521a850d97307851ad9eb9b54e1546c2ed6dde4eb583d820b` |
| `offline-analysis/config-controller-compatibility-review-001.json` | `f5e4906e0bf405d5150471720f5c66c2db8dec2c1e0104fae24582b5cb823585` |
| `run-a-001/events.jsonl` | `f5953cf1bd688f08faee17b604abb98c60db16482e096a86451087d6d5f86d15` |
| `run-b-001/events.jsonl` | `c40d2bc46c902aa81ffc733df8c4f7758eae84bd1b00540f62c0b6f5c559d48c` |
| `offline-analysis/run-a-event-review-001.json` | `04ba7b8541dd40538348603432a8abf224d911d7eacf02652056439bed965d43` |
| `offline-analysis/run-b-event-review-001.json` | `e8fd577d4da2f4de7046c593256e96d46745700a47952264a685dc67cb414849` |
| `offline-analysis/run-a-completion-review-001.json` | `31ac10afb3b420f9ad41620e7bb182f34114793c85ddb2a3531f1c3d359bd6fc` |
| `offline-analysis/run-b-completion-review-001.json` | `dff2f2b4cd7b19fc7545f1eab13bcac31c51c92213e32f9e13b9941d1349729d` |
| `offline-analysis/preopen-baselines-001.json` | `417a7387c884f3deca2b8a1989eed9e1bb6b240d949208663abe5cc3f6b18504` |
| `offline-analysis/installation-coverage-review-001.json` | `9e542f7ee4079a8be2fa6ed252fa58ba2e91939345ed4ef9023d1389100ce49d` |
| `offline-analysis/caller-holder-final-001.json` | `88d2a2350a0e086a79ace2fce5f024e5d4427d95705672e33e31792c18620e63` |
