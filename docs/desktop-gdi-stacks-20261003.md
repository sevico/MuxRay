# GDI 三轮分配栈验证：2026-10-03

普通权限的 A 诊断版本连续三次成功选择文件后，固定静置窗内 GDI 为 **260 → 262 → 264 → 266**。每轮各有一个 DC 和一个 Bitmap，在打开选择器时由同一条缩略图图像列表分配路径创建，随后没有观察到该代对象匹配的 Destroy、所有者转移或变换事件。匹配符号及 PE 函数边界已把关键用户栈定位到 `CThumbnailLookupTable` → `CImageList::Initialize`。**分配路径已确认，持有责任尚未确定；另做的 COM 包装修正对照未消除逐轮增长，生产代码未作修复。**

接续 [前轮 ETW 与对象校准](desktop-gdi-etw-20261003.md) 和 [调用边界验证](desktop-gdi-boundaries-20261003.md)。
后续 [独立 Win32 宿主对照](desktop-gdi-native-host-20261003.md) 已在没有 Go、Wails、WebView2 的宿主中复现同一核心分配路径及每轮 +2；持有责任仍未确定。
本轮外部证据根为 `C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-stacks-20261003`，以下路径均相对此目录；ETL、解码数据、符号与实验工具不纳入仓库。
所有时刻均为 2026-10-03 UTC。

## 实验边界与预校准

- 目标为 A（`before-native-getresult`）诊断版本：保留原生选择器 `Show` 和 dialog `Release`，在原生 `GetResult` 前返回诊断取消结果，不打开媒体会话。
- PID `26848`，创建 FILETIME `134354735598072307`；EXE SHA-256 为 `4b186086ad39448f64e46482cbeda32308ee731101899f905e0188a8e42e5616`。身份和哈希记录于 `run-a-001/identity.json`。
- `run-a-001/target-token.json` 只读验证目标为非提升、medium integrity；仅独立 ETW 采集器使用管理员权限。
- 20 秒预校准由普通权限 x64 校准进程创建并释放六类已知 GDI 对象。按完整句柄和 payload OwnerPID，精确匹配 DC、Bitmap 各一条创建及销毁事件；四条事件均含可映射到校准进程模块的用户栈（26 或 27 帧），并非只有内核地址。
- 预校准 2,644 条选定事件全部带直接 stack64；Stop、解码返回码、丢失计数、TDH 解析错误均为 0。验收为 `preflight-001/preflight-acceptance.json`。Win32k 数字类型 1、5 分别由实际对象校准为 DC、Bitmap，不直接套用 `OBJ_*` 常量。
- 原始 `EVENT_RECORD` 离线解码器保留全局 Owner 的 455–458、完整句柄、原始 payload、事件头 PID/TID 和同事件扩展栈。使用 `ProcessTrace` 转换后的系统 FILETIME；旧 run4 的 103,110 条事件已与参考解码逐项核对。

## 正式跟踪与固定静置窗

正式采集 `traces/a-three-cycles-001` 使用 Win32k、关键字 `0x0000010000000000`、level 4，名义 600 秒；实际为 `04:02:17.7566358–04:12:18.6278302`，约 600.871 秒。ETL 为 299,565,056 字节，低于 512 MiB 上限。精确会话和路径核对后 Stop 返回 0，采集、ETL 头及消费者的丢失计数均为 0。

`decoded/decode-receipt.json` 位于上述 trace 目录：548,586 条 455–458 全部带直接 stack64，无未展开 stack-key、TDH 解析错误或 OpenTrace/ProcessTrace/CloseTrace 错误。455/456/457/458 分别为 217,812 / 217,789 / 110,088 / 2,897 条；这些是全局事件数，不能相减作为泄漏数。采集收据中的 `stackEmissionVerified=false` 是留给后续验收的初始状态，实际栈存在性由离线解码和预校准确认。

三轮真实选择均有 `run-a-001/cycle-N-open/selection.json` 操作收据。静置窗以操作完成时间为锚点，纳入整次查询均在闭区间内的样本；基线在正式 ETW 开始前，由连续计数采样覆盖。

| 阶段 | 锚点完成时间 | 固定窗 UTC | GDI 全部样本值 | GDI / 资源样本数 |
| --- | --- | --- | ---: | ---: |
| 预热 Escape 后基线 | 04:00:39.019 | 04:01:09.019–04:01:39.019（30–60 秒） | 260 | 30 / 30 |
| 第一次成功选择 | 04:03:18.062 | 04:03:48.062–04:04:18.062（30–60 秒） | 262 | 30 / 30 |
| 第二次成功选择 | 04:05:22.007 | 04:05:52.007–04:06:22.007（30–60 秒） | 264 | 30 / 30 |
| 第三次成功选择 | 04:07:24.038 | 04:09:54.038–04:10:24.038（150–180 秒） | 266 | 30 / 30 |

前两次选择完成至下一次目标输入分别静置 87.060、85.139 秒；第三次完成至正常退出输入静置 337.755 秒。CPU 比较仅使用整个区间均在窗内的 29 个间隔。四窗在采样结束后重新核对，与实时审核一致。

## 每轮 DC 与 Bitmap 的完整生命周期候选

对象代次按全局 455–458 重建，区分创建、销毁、所有者变化、句柄变换和重用；执行栈使用事件头 PID，所有权使用 payload OwnerPID。六个静态候选的创建头均为 PID `26848` / TID `11144`。

| 轮次 | DC 完整句柄（type 1） | Bitmap 完整句柄（type 5） | 创建时刻约值 | 无后续 455–458 事件的时长下界 |
| --- | --- | --- | --- | ---: |
| 1 | `0xfffffffffd011684` | `0x16050c9d` | 04:02:35.662 | 557.278 秒 |
| 2 | `0xffffffffe6011595` | `0x31051885` | 04:04:45.636 | 427.304 秒 |
| 3 | `0x69011f16` | `0x44052114` | 04:06:47.642 | 305.298 秒 |

创建发生在各轮打开选择器期间，早于最终文件选择。无后续 455–458 事件的保守截止点为最后一条已解码 Win32k 事件 `04:11:52.9406148`，不是 Stop 时刻或进程退出时刻。这里的“静态”仅表示此事件流中，该代完整句柄没有后续创建、销毁、所有者转移或变换事件，不描述 COM 引用计数。

全局审核还列出其他 11 个已知创建候选及 8 个未知起源候选；不得把变换候选或未知前驱直接计为泄漏。六个静态候选与每轮计数增加 2 一致，但计数本身不证明分配来源。

## 符号与函数范围验证

六条创建栈各 93 帧；同一类型在三轮的完整原始栈和模块＋RVA 签名完全一致。DC / Bitmap 分别有 68 / 71 个用户帧，全部唯一映射到目标进程模块。预热与各轮选择后的四份快照均含相同的 136 个模块，模块范围及映像哈希一致。

`traces/a-three-cycles-001/decoded/static-symbols-001.jsonl` 使用从[微软公共符号服务](https://learn.microsoft.com/en-us/windows-hardware/drivers/debugger/microsoft-public-symbols)取得的 11 份精确匹配 PDB；下载身份和哈希保留于 `symbols/candidate-001/download-receipt.json`。映像 SHA 和 PDB GUID/age 先核对，再解释符号。关键路径按外层到分配内层排列：

`CThumbnailLookupTable_CreateInstance` → `CThumbnailLookupTable::InitThumbnailLookupTable` → `_CreatePerfectImageList` → `_CreateImageList` → `CreateImageList` → `Comctl32!CImageList::Initialize`。DC 分支继续调用 `CreateCompatibleDC`，Bitmap 分支经 `_ReAllocBitmaps`、`_CreateBitmap` 调用 `CreateDIBSection`。

`static-symbol-boundary-review-001.json` 进一步验证上述 Windows.Storage 帧及 Comctl32 帧：返回地址、返回地址减一与符号起点属于同一 PE `RUNTIME_FUNCTION`。其中 DC 返回 RVA `0x32968`（符号偏移 296），Bitmap 返回 RVA `0x329ce`（偏移 398），两者的符号起点为 `0x32840`，函数范围为 `[0x32840, 0x32a7a)`。因此这两处可归入 `CImageList::Initialize`，不只是最近符号名推测。

未满足同一 PE 函数入口验证的其他帧保留为“符号＋偏移”。未解析的高位地址不擅自归入内核模块；本轮未取得源码行、私有对象字段或最终持有者证据。

## 已销毁对象的释放栈对照

按同一 `CImageList::Initialize` 返回地址寻找较宽的对照，可配对 42 个 DC 和 54 个 Bitmap 的创建及 Destroy；其更深层调用者与上述缩略图候选不同。每轮各抽取一组 DC/Bitmap，保留原始创建和销毁栈，并用匹配 PDB 与 PE 函数范围验证关键释放路径：

- `ImageList_Destroy` → `CImageList::Release` → `~CImageList` → `_Destroy` → `DeleteDC` / `InternalDeleteDC`。
- 同一图像列表销毁路径 → `_DeleteBitmap` → `DeleteObject` / `InternalDeleteObject` → `NtGdiDeleteObjectApp`。

证据见 `decoded/same-source-release-comparison-001.json` 与 `decoded/release-control-symbol-boundary-review-001.json`（均在正式 trace 目录下）。这些对照证明本次跟踪能够观察相关对象的销毁和释放栈；**不能把它们当作与六个静态候选完全相同的调用链**。完整静态创建栈及含 `_CreateImageList` 返回地址的路径，仍只有三对未见 Destroy 的候选。未见匹配 Destroy 也不证明没有尝试调用过释放 API。

## 正常退出与采样完整性

- ETW 自动停止后，以 Alt+F4 正常退出，输入为 `04:13:01.793–04:13:01.863`；根进程 OS 退出时间 `04:13:02.0180648`。根进程及六个观察到的 WebView2 后代均有原始退出码 0 证据。
- GDI 原始采样 781 条、资源及 timing 各 787 条，索引连续、身份覆盖完整、摘要数量及字节数与原始文件一致。GDI 与资源最大相邻 elapsed 间隔分别为 1.006499、1.006536 秒；UTC 最大间隔分别为 1.006501、1.138 秒。
- 唯一不完整资源指标在首条样本：无前一 CPU 区间，内存与句柄仍有效；不是退出转换异常。其余记录未见进程读取错误，四个固定窗指标完整。
- 最后 GDI 值仍为 266，查询完成距 OS 退出约 0.535 秒。GDI 探针随后确认目标退出；资源探针完成退出宽限期。两探针有正常结束摘要且 PID 已不存在，未推断已关闭探针的独立 OS 退出码。
- 精确查询本轮 ETW 会话返回 4201（不存在）；目标七进程、采样器、采集器、watchdog 和 coordinator 均已不存在。三轮后的源文件独占读取及 SHA 核对成功。

## COM 包装单变量对照

静态检查发现 Wails v2.14.0 的 open/save 包装把原生 COM 接口指针强转为包含 parent HWND 的 Go 结构，setter 因而写入原生接口地址之后 8 字节。原下载 ZIP 的 Go 模块 h1 与仓库 go.sum 匹配，open/save/common 文件也与全局缓存逐字节相同，排除了先前本地实验改入该字段的可能；偏移 8 的原生私有字段含义仍未知。

外部候选只为 open dialog 新建 Go 自有包装保存 HWND，所有 COM `this` 转发原生指针。保留 A 的 GetResult 截断、Show/Release 顺序、原 HRESULT 和 COM 初始化行为，不改 save、生产代码或依赖版本。先复现 A 的原 EXE 哈希，再构建候选；224 项输入检查通过，私有缓存最终恢复。

初次按模块包路径执行 `go test` 返回 `[no test files]`，不计通过。随后用明确文件清单实际执行了哨兵内存、Show 的 native `this`/HWND、单次 Release 转发测试，出现 RUN/PASS；详见 `abi-open-wrapper-proposal-001/test-execution-002/receipt.json`。独立构建复核为同目录 `final-build-review.json`。

候选 EXE SHA-256 为 `ec0cb78a86ac7c841f29bf2594083cab091017914d8650eed6722d456aee7f21`；普通权限新进程 PID `28512`，创建 FILETIME `134354756731701557`，证据在 `run-wrapper-001/`。按预登记协议取消预热，再在同一目录、视图和源文件上双击选择三次。前四个 30–60 秒固定窗均有 30 个 GDI 和资源样本，GDI 恒定为 **260、262、264、266**。第三次选择完成于 `04:40:38.726`，正常退出输入为 `04:44:19.327–04:44:19.377`，静置超过 180 秒后最后读数仍为 266。

最终审核补齐第三轮 150–180 秒窗 `04:43:08.726–04:43:38.726`，GDI 仍恒定为 266；五个窗口各有 30 个 GDI、30 个资源样本及 29 个严格落窗 CPU 间隔。第三轮至退出输入共静置 220.601 秒。

资源探针保存 575 条样本，以 `target_exit_grace_complete` 结束；GDI 探针保存 568 条样本，以 `target_exited` 结束。两条采样流索引连续，最大 elapsed 间隔分别为 1.005131、1.003056 秒；仅首条 CPU 区间未定义，其余指标与进程读取未见错误。根进程和六个观察到的后代均有原始退出码 0 证据；部分已退出进程对象仍可按 PID 打开，故结论是全部已退出，不声称所有 PID 都不存在。两探针正常结束且其 PID 已不存在。源文件再次可独占读取且完整哈希不变；此诊断版不读媒体，因此不把源收据当作媒体句柄释放证明。复核见 `run-wrapper-001/final-sampling-exit-review-001.json`。

**候选没有消除每轮 +2，不能作为已验证的 GDI 修复合入。** 本对照未开 ETW，不能仅凭相同计数证明候选仍保留原来那条分配路径。ABI 缺陷和本次 GDI 持有问题应分别处理，也不据此断言 Windows 自身存在泄漏。

## 限制与后续验证

零丢失证明本次采集器报告的完整性；三轮样本和事件流不能证明所有系统版本的行为，也不覆盖每秒采样间的瞬态活动。模块快照在各轮选择后取得，并非连续 loader 跟踪。ETW 在正常退出前已停止，不能据此判断这六个对象在进程退出中的清理细节。

当前证据支持反复选择器路径中的持续对象存留及其创建函数，不单独证明 Windows 组件或 MuxRay 的责任归属。下一步应以独立 C++ Win32 host、规范配对的 STA/COM 生命周期、同一原生文件对话框做对照，检查没有 Go/Wails/WebView2 时是否出现相同增长与分配栈，再追查图像列表的引用持有者。外部 `helpers/native-host-control-plan.md` 已给出最小方案；本轮未构建或执行该新对照。若不复现，仍须逐项区分宿主、线程和 Shell 状态差异，不能直接归责 Wails。

## 关键证据 SHA-256

| 外部相对路径 | SHA-256 |
| --- | --- |
| `preflight-001/preflight-acceptance.json` | `0ba2e3d5991a0415fba32a3e2fdea4de162ed4700edef1807214de5b093d9eb4` |
| `traces/a-three-cycles-001/win32k-gdi.etl` | `b5b95c15cc3bdfc977fe65e95eed68df99014c68c2828bd74849bca7ea51e9f2` |
| `run-a-001/live-fixed-window-review-001.json` | `1908ae61898fed9408da951e727565d23d3d5801dfe8b582b7df027d6a9f4b61` |
| `traces/a-three-cycles-001/decoded/three-cycle-static-stack-review-001.json` | `41bf3283c9ec10ecc87903197bc40225fbe165945e286d7f42023b8d909676d8` |
| `traces/a-three-cycles-001/decoded/static-symbols-001.jsonl` | `4f7806c8e028f3e045958a166b79b65067857a87c19abccc0a16593658a7a9f4` |
| `traces/a-three-cycles-001/decoded/static-symbol-boundary-review-001.json` | `cdb306ea416edf305960cadd6237f98c0aff13f6d727cc06b401f29e5cf70cd0` |
| `traces/a-three-cycles-001/decoded/same-source-release-comparison-001.json` | `7748503f0ba4914c0a04365ece4b7c4fb7a3689cabe7db3d1874ab401fcd8ee5` |
| `traces/a-three-cycles-001/decoded/release-control-symbol-boundary-review-001.json` | `0f82895a87f3dba349d17c7777ba099b997fb836a69282b9c52c7e26cd66ffb7` |
| `run-a-001/final-sampling-exit-review-001.json` | `d6ac1690c87d144dfeb07c4bbcf16301e722188a70c8572de9e8107376ec4569` |
| `abi-open-wrapper-proposal-001/final-build-review.json` | `efc5b0428944529777acfc9665775bbe3c08c43736d8e49067442cd1bbfeeb7c` |
| `run-wrapper-001/final-sampling-exit-review-001.json` | `2b8079016ae59351865db37ac6ed1d320deb2dc7e25fb2868868d4efc56426a1` |
