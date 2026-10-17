# Windows 稳定性复验与诊断：2026-10-17

新的严格六轮复验在 C1 发生父进程异常退出，完成轮次为 **0/6**，本轮明确失败。
用户确认只调整排序并选中文件、保持弹窗打开；没有取消、打开或关闭的完成证据。
随后独立进行的默认构建单次诊断流程完成了取消、打开、扫描、原生 JSON 保存、
关闭文件和正常进程退出。这个成功不解释 C1，也不改变前轮默认进程异常退出尚未归因的状态。
原型继续保留为显式构建选项，不提升为默认实现。

本文记录截至 `2026-10-17T04:46:57Z` 已复核的事实：两次有限 v2 诊断均未复现退出，
但生产 C1 保持失败；新 CI 语义修正的远端源字节与原生重放仍待完成。
时间统一为 UTC，本地时区为 Asia/Shanghai。
前轮结果见 [2026-10-16 稳定性报告](desktop-windows-stability-20261016.md)，其失败、
额外手动打开和视图偏差均保持原判，不用本轮材料覆盖。

外部证据根（下称 NEW）为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-windows-stability-20261017`。
OLD 指同级 `muxray-windows-stability-20261016`。
产品构建源固定为 `4f9478dc65826153677fd4d6c7b31944937f38f8`；随后 CI 工具修改为
`40c01438cc8565e63eab63ad16f064c92ec448fc`，没有把新的诊断覆盖写入产品源码。
二进制身份按构建收据和 SHA-256 判定，不能只用当前 HEAD 代替。

## 当前状态

| 项目 | 已证实结果 | 边界 |
| --- | --- | --- |
| 严格复验准备 | 原分析器计算规则保留，37 项合成测试通过；新登记绑定唯一子代、逐轮空态、同视图及四次独立源关闭检查 | 准备与合成测试不等于原生验收 |
| 生产原型严格复验 | 同一生产 EXE 已取得完整初始基线，C1 仅一个真实子进程；父进程随后 exit 1 | C1 失败，0/6 完成，无后续固定窗或四次源关闭验收 |
| 默认单次诊断 | 取消后空态、完整扫描、1861-byte 原生快照、schema/源读取、关闭媒体和实际 exit 0 通过 | 这是带日志的默认诊断构建单次流程，不是长期稳定性或原型通过证明 |
| v2 两次有限焦点诊断 | 同一固定诊断 EXE，分别先捕获后激活、先激活后捕获；均取消、恢复空态并实际 exit 0 | 未复现不等于修复；工具激活成功不证明触发 WM_SETFOCUS，不计入严格六轮 |
| 远端 CI | `40c0143` 核心 CI 4/4 成功，Windows 非 GUI 与三个 Linux 专项 job 成功；基础 codec job 遇到 SEI 预览语义断言失败 | full codec job 在最后保存的状态仍运行；不能宣布整套 CI 通过，Windows job 不覆盖 GUI |
| chooser 就绪修复 | 六个真实 Linux Open 均观察到 disabled → enabled 后只提交一次，后续场景通过 | 这是新运行的具体就绪证据，不反推旧失败所有时序 |
| SEI 验证器候选 | 独立源 oracle 区分完整用户数据的 64-byte 预览省略，保留语法、预算、字段库存和原有 source-link 检查；36 项针对测试通过 | 下载附件不含原 FLV；候选尚待同一提交的 Linux 源字节与原生 CI 重放 |

## 严格协议与初始基线

新协议沿用 C1/S1/C2/S2/C3/S3 次序、60 秒初始空态、每轮 60 秒静置、最后一轮
180 秒静置，以及 Details / Name 升序 / 展开分组。每个实际 Open 只能对应唯一
PID 与 creation FILETIME；六个登记子代必须与全部观察到的直接同映像子代精确相等。
上一子代实际结束且父界面重新空态后，才允许进入下一轮。

手动选择并驻留至少 30 秒的用户陈述仍是允许的证据模式。未知实际输入时间保持
`null`，不能写成工具计时。S1/S2/S3 各有独立源关闭收据；S3 静置满 180 秒后还需
第四份独立收据，并在最终关闭输入前完成。读回仅证明各检查瞬间可独占读取。
这些要求没有降低，也没有因本次失败改写协议。

分析器 003 只替换新协议 SHA 字面量，计算规则未改；37 项测试、GDI/USER 采样器、
源关闭 helper 均复用已验证字节。新的启动器另外校验自己的 ROOT 与协议 evidenceRoot
一致。原文件均保留。媒体仍只读使用 OLD 的 `picker-fixture/sample.flv`；本轮重新
运行固定 CLI，取得 41,698,926 字节完整扫描、172,504 个 Tag、0 条诊断，文件 SHA-256
仍为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。

生产 EXE SHA-256 为
`dc72f49bd5628e39be344c65716df03d5a424e18abda3c618708f569b4264250`，
与前轮生产 003 相同；本轮仍使用新的构建收据和证据目录。
父进程 `32036:134366837099625349` 为普通非提权进程，双采样就绪时间
`04:08:51.468761Z`。完整初始 60 秒由两流 sample 0–61 覆盖，最大起点间隙分别
1.001 秒与 1.000592 秒。基线 30–60 秒窗口有 30 个完整计数查询，GDI=118、USER=83、
handles=707–708、private bytes=90,537,984–90,640,384。基线独立审计时尚无选择器子代。
这只建立了失败发生前的基线，不能推导之后的资源平台或恢复结果。

## 生产 C1 失败现场

| 时间 | 已保存事实 |
| --- | --- |
| `04:10:26.088Z` | JavaScript 变量错误发生在任何 click 之前，记录 `guiInputIssued=false` |
| `04:10:34.976Z`–`04:10:53.806Z` | 唯一实际 Open 的输入与观察区间 |
| `04:10:35.0786787Z` | 子进程 `17564:134366838350786787` 创建，位于上述区间内 |
| `04:11:09.998Z`、`04:11:25.094Z` | 两次排序尝试被工具跨进程目标限制拒绝；不能记为排序已生效 |
| `04:13:34.114Z` | 先前保存的界面观察；不能冒充后来用户回复后的选中确认 |
| `04:15:39.842Z` | 收到用户“已准备好”并保存陈述 |
| `04:15:40.1198736Z` | 父进程真实 OS exit 1 |
| `04:15:40.1340449Z` | 唯一选择器子进程真实 OS exit 0，比父进程晚 14.1713 毫秒 |
| `04:15:49.966Z` | 记录 `failed to activate captured window`，未取得回复后的选中截图 |
| `04:19:15.842Z` | 用户补充“是，只排序和选中，保持弹窗打开”，报告没有 Cancel/Open/Close |

用户准备陈述的记录比父退出早 277.8736 毫秒；激活错误记录比父退出晚
9.8461264 秒。**原文件缺少激活调用实际开始 UTC，因此不能由错误记录时间断言
激活动作是在退出后开始，也不能判定激活动作导致退出。** 用户补充陈述说明其报告的
操作类别，不补实际输入时间、独立选中截图或退出原因。

资源流与 timing 各 415 条，GDI/USER 流 409 条。两个采样器对父退出 FILETIME/退出码
一致；raw 与 summary 的八个观察代际退出逐项一致，唯一直接同映像子代为 17564，
不存在本轮额外打开。父进程 exit 1，其余观察代际 exit 0，5 秒 grace 正常完成。
子进程生存约 315 秒，短于 600 秒 picker deadline；这仍不足以确定内部失败路径。
子进程 exit 0 不能替代取消回复、Show 返回或正常媒体关闭的证据。

只读查询 `04:13:00Z`–`04:18:30Z` 的 Application、WER-Diag、WER-PayloadHealth、
WerKernel 三个 Operational 日志，查询时均无匹配记录。未修改日志设置。日志缺失
不能排除崩溃或显式退出，也不能代替同代际产品日志。本生产构建没有可据此归因的
诊断日志，不能把前轮曾观察的 MoveFocus 故障直接套用到本次退出。

没有运行六轮分析器来制造完整结果，没有补写 `round-C1.json`、静置时间或成功取消，
也没有重新打开来替换失败轮。冻结失败审计与后到的用户陈述采用独立补充文件。

## 默认构建单次诊断

默认诊断 EXE SHA-256 为
`83ef2e06475130c2d1fef1c816d2a82c482525b7e6dca8aa8a18a9bea03bd353`，
父进程 `11976:134366843840889112`。构建收据的 350 份源输入已独立复核；日志首行的
PID 与 creation FILETIME 与采样器相同，避免仅用 PID 关联不同代际。

首先打开选择器。一次 cached element 111 不可用错误被保留，随后新观察仍见同一
对话框；实际坐标 Cancel 返回 `canceled=true selected=false`，并取得空态截图。
第二次打开固定媒体，实际 UI 显示 172,504 个 Tag、0 条结构诊断及 100% 扫描完成。

原生保存产生 `default-diagnostic-snapshot.json`，长度 1861 字节、SHA-256 为
`89ac0c032f74d638ea08c499aba4bf7ce7cd5989f5dce215ac4d6ac109278d09`。
保存输入后的 AX 文件名节点含完整目标路径，实际 UI 显示保存成功及相同字节数。
AX `focused_element` 误报 Search，未单凭该属性判定输入目标。独立
`snapshot_check.py --source ... --max-source-bytes 41698926` 读回 schema 与源 SHA 通过。
快照仅含扫描概况；未请求的 detail、Hex、summary、趋势或搜索页仍为 omitted，不能
把这个保存结果说成完整 codec 详情验收。

Close File 后实际截图恢复空态。`source-closed.json` 在
`04:22:18.308280Z`–`04:22:18.335664Z` 通过 share mode 0 的独占读取及文件哈希检查，
早于 `04:22:25.034Z` 的正常 X 输入。`04:22:25.146Z` 即刻窗口列表仍含目标，
该列表没有被当成退出证据。保留句柄的实际 OS 退出为 `04:22:25.3928833Z`、exit 0；
`04:23:14.135Z` 的后续列表已无目标窗口。

资源与 timing 各 153 条、七个观察代际全部实际 exit 0，raw 与 summary 逐项一致，
没有 discovery failure 或不完整树样本。五次日志 focus.before 均为 enabled=1，五次
MoveFocus 均为 HRESULT 0；日志包含取消、打开、保存三次返回及正常 shutdown。
本次未复现异常，不能解释旧默认 PID 19680 的 exit 1，也不能解释刚失败的生产 C1。

## v2 诊断构建与两次有限观察

为了取得同代际调用路径，新诊断使用干净固定源 `4f9478d`，其 350 份构建输入按相对
路径与生产输入逐字节一致。10 份 overlay 的原件/替换件、1751 份构建过程输入和最终
EXE 均已独立核对，构建前后稳定。EXE SHA-256 为
`0f73619583feaee9c5e79ef1c8527585b01398d96fe5292d0293424ede5d1e92`。

main 用 `picker.ChildArg` 动态识别 v2 子进程；专用回复管道、严格 parser/nonce 检查、
stdout/stderr 有界排空、Job/等待/清理、owner 恢复及错误返回均保留。严格 parser 去掉
唯一新增日志后与原件字节一致。Focus 仍调用 MoveFocus，原 errorCallback 仍执行
globalErrorCallback 后 `os.Exit(1)`，没有增加忽略错误、重试或自动重新打开。
新增结构化日志只记录身份、状态、HRESULT、类型、计数和布尔信息，不记录 nonce、
媒体路径或原始 IPC 内容。

诊断日志以 O_EXCL 创建，文件名含角色与 PID，首行绑定 creation FILETIME；同目录 PID
复用会失败而不是覆盖旧日志。`os.Executable`、文件创建、GetProcessTimes、SetCrashOutput
初始化新增失败路径；日志与非原子 Win32 状态查询也改变时序和资源使用。因此旧模板
`changesControlFlow=false` 只表示保留原产品分支，不能理解为绝对零行为差异。

两个独立协议都只允许一次 Open 和一次显式 parent activation，记录开始/结束 UTC；
意外退出即结束，不用重试替换。仍存活才继续手动 Cancel、空态和正常 X。只读采样
上限 900 秒，均未执行严格六轮或成功媒体选择。第二次专门改变回复后操作次序，
使用新的进程代际和证据目录，没有覆盖第一次。

| 项目 | run-001 | run-002 |
| --- | --- | --- |
| 父进程身份 | `2768:134366848230374378` | `12920:134366854980270814` |
| 唯一观察到的直接同映像子代 | `20724:134366848455123460` | `27972:134366855193116112` |
| 回复后操作次序 | 先捕获选中状态，再激活 | 不先读取状态，立即激活后捕获 |
| 回复记录到激活开始 | 11.642 秒 | 0.001 秒 |
| 单次激活区间 | `04:35:26.548Z`–`04:35:26.793Z` | `04:42:00.087Z`–`04:42:00.147Z` |
| 子代实际 exit 0 | `04:35:56.1285839Z` | `04:42:22.6802798Z` |
| 正常 X 输入开始 | `04:36:27.397Z` | `04:42:46.579Z` |
| 父进程实际 exit 0 | `04:36:27.7294186Z` | `04:42:46.9224755Z` |
| resource / timing 样本 | 555 / 555 | 255 / 255 |
| 最大样本间隙 | 1.012 秒 | 1.016 秒 |
| 同代际 focus.before / MoveFocus | 6 / 6 | 5 / 5 |

两次激活均返回成功，随后仍见模态窗口与 `sample.flv`；第一次选中状态在激活前已观察，
第二次只在激活后确认。对应激活调用区间内均没有新增 onFocus/MoveFocus 日志，不能
据工具成功推断 Windows 已投递 WM_SETFOCUS，或验证 disabled-owner 分支。
记录到的所有 focus.before 均 enabled=1，MoveFocus 全部 HRESULT 0，无 errorCallback。

用户随后各确认“已取消”，实际输入时刻仍未知。同代际子日志记录 Show 返回
`0x800704c7`、COM 释放与反初始化、v2 专用管道写出 canceled 并关闭；父日志记录
解析成功、等待子代 exit 0、Job 关闭、owner 恢复以及正常 shutdown，无 deadline 或
强制清理路径。两轮各八个观察代际全部实际 exit 0，raw 与 summary 逐项一致；采样
以五秒退出 grace 完成，没有 discovery failure 或不完整树样本。

取消后的新 AX 树显示空态和 enabled 主窗口。两次对应截图均被前景 Codex 遮挡，
不能记为 MuxRay 像素验收；正常 X 使用新 AX 目标，实际退出由保留句柄确认。
这两轮没有打开媒体，不提供成功选择、四次源释放或 GDI 长期平台的验收结论。

run-001 原输入清单共 42 项，误纳入共享诊断目录里当时仍在运行的 run-002 两份日志。
最终复核时仅 `parent-diagnostic-12920.log`、`child-diagnostic-27972.log` 已增长，
其余 40 项（含 run-001 同代际日志）仍与清单匹配。原清单和审核结果保留，不把
后续日志前缀视为第一轮关联证据。独立 `identity-scope-supplement.json` 另外确认两份
变化文件的旧长度前缀仍精确匹配原哈希，并以新清单绑定 40 项保留输入及原审核材料，
没有覆盖旧记录。run-002 最终清单 44 项当前全部哈希匹配。

两种操作次序都未复现生产 exit 1。日志改变时序，旧 C1 又缺激活实际开始时间，
因此不能确认窗口绑定缓存、状态读取或特定焦点路径是根因，也不能据此新增忽略
错误的产品分支。生产 C1 的失败与 0/6 完成状态保持不变。

## CI 回归状态

`4f9478d` 已推送并取得真实远端结果：核心 CI 的 macOS/Windows/Ubuntu test 及
race-and-fuzz 成功；Desktop 的 Windows tests/production build 为非 GUI job，成功。
五个 Linux 原生 job 都在绝对路径写入并读回后，首次 chooser Open 的 enabled 检查
处失败，实际 Open 未提交，后续 codec 场景未执行。原日志不能量化 GTK 异步延迟，
因此“就绪竞态”是需要验证的候选解释，不是已证明的产品根因。

后续提交 `40c01438cc8565e63eab63ad16f064c92ec448fc` 对 chooser 加入有界就绪等待，
动作前重新取得唯一且 enabled 的目标，并保留动作失败后不重放的要求。27 项本机
可移植测试通过；三个依赖 Linux `/proc` 或信号语义的测试明确未在 Windows 上代跑。
该提交已推送；真实 Linux 的六个 codec-search case 均记录首轮 Open disabled、第二轮
enabled，然后只提交一次成功动作，各 case 完成。它们验证了该等待路径能处理这次
观察到的就绪变化；不把本机可移植测试冒充 Linux `/proc` 或信号测试。

截至保存的 `04:43:08Z` job 状态，核心 CI `39002275810` 四项全部成功；Desktop
`39002275743` 的 Windows 非 GUI、direct MP3 / SEI timing、H.264 search / MP3
dependency、MP3 changes / SEI search job 成功，full codec job 仍运行。基础 codec job
在 AVC evidence 外部验证器失败，位置是 `native_browse_evidence.py` 的统一
`not detail.truncated` 断言。其 IDR 原生步骤自身已保存四次 source-link、JSON、关闭和
exit 0，但这不能替代外部语义验证通过。

实际快照为 complete=true、truncated=true、diagnostics=[]；SEI 消息完整，payloadSize
为 686，UUID 后有 670 字节用户数据，只展示其中 64 字节。产品已有逻辑会把该预览
省略标记为 truncated。候选仅修正回归验证器：从独立源字节 oracle 验证 type-5 SEI
的消息边界、UUID、预览、源范围和 trailing bits，并要求顶层 truncated 精确等于源
推导的预览省略。它继续拒绝真正语法截断、未覆盖的 SEI 类型、context/SEI/字段/JSON
预算、字符串省略和缺失/重复/额外字段，原 keyframe、slice、Hex source-link 与曲线
检查保留。不是简单删除 truncated 断言，也没有修改产品解码器。

最终候选 `ci-semantic-fix-002` 的 36 项针对测试及 diff 检查通过。预算负控保持合法
字段 ID 并精确匹配预算失败；字段库存负控保持合法 ID/parent 并匹配库存失败，避免
因别的校验提前拒绝而产生虚假覆盖。下载的 CI 附件没有原始 FLV，故不能用该快照
反构造 oracle，也未宣称本地重放了其真实源字节。包含候选与本报告的提交仍需真实
Linux CI 复验。Windows 非 GUI CI 无法覆盖本轮真实 C1 exit 1。

## 主要收据

以下路径相对 NEW，哈希均为 SHA-256。完整输入清单另存对应审计目录。

| 证据 | SHA-256 |
| --- | --- |
| `picker-preparation-001/receipt.json` | `efbd7c7dae9c655f3579f22f50ab04b1b56abec85346ae002fa9e5e9b01116eb` |
| `picker-case-protocol-001.json` | `bc6fc69382b58aea25716c470f4bc401870260dc77e4e15005190ad35549ed8e` |
| `picker-fixture/binding.json` | `0e5d7ac5a2a41f65f8cf590c601d22449c1e53d54aca2ca492ac7e53038cde6e` |
| `picker-live-review-001/summary.json` | `9ebbb3ac8ad1c1faac3445ad65de29c57071f25479c44e9c43abaddf468b8ebb` |
| `picker-live-review-002/summary.json` | `baccb8afbf54cf1504e007df8929d056cd43969b8f14eaa8262668a99a817700` |
| `picker-failure-review-001/summary.json` | `3d01435a8d3d7cd3771b0c8099c0feeb41c93f20afa1f0d3b10f75366dd79510` |
| `picker-failure-review-001/inputs.json` | `13cc5ad0fa66678d0e2e21d4fafaf6e8e91554a3e6bf1ef060f97dbcffd90c5f` |
| `picker-run-001/C1-failure-user-attestation.json` | `3744c8374ec273a86b4a5c4699128af8bb9f142636e6b6d540374e4eee96a938` |
| `picker-failure-review-001/user-attestation-supplement-001.json` | `9f7e6d8a2aec8c95de93693db063d6135dc4701667c2c54a78d927c26bde3f93` |
| `default-diagnostic-run-001/snapshot-check.json` | `fc8014cd04f4f4ae1dd518f53c5bce6fb5729ebe033ab9ae2f5e5534e35ce87e` |
| `default-diagnostic-run-001/final-review.json` | `a9e600482fb65073171f6da5453dd5de05e0d28813486c068ac70fa437ec3cbe` |
| `default-diagnostic-run-001/final-review-inputs.json` | `50b5d733f36beaf3df177bb5c1bc2ae9e0a27952933c6e6b40fba69e539e60f2` |
| `picker-v2-diagnostic-review-001/summary.json` | `066e70ba871620887e6742a46f4aaf005efd67a3aa9395623f2eae8a174a123c` |
| `picker-v2-diagnostic-review-001/inputs.json` | `1652c8df640c7ef5e9aa9b292188a6953c62d31b75d9bbabfb0d12bd50180c45` |
| `picker-v2-diagnostic-native-001/build-receipt.json` | `501bed92efa1ad194e7f91081ec80c3e17ef2a3f31328b7879cf6e2f2eff0565` |
| `picker-v2-diagnostic-run-001/final-review.json` | `810f18d5f69a4baf71b18534777193e25b5b33a18b519bfc47c773c0283aec4f` |
| `picker-v2-diagnostic-run-001/final-review-inputs.json` | `01cfa730e7da691924cc222a9ed34fed936472e00d59d7952c36a89667a72e72` |
| `picker-v2-diagnostic-run-001/identity-scope-supplement.json` | `3be4f028a1ae02e677b0dcf3c01bfc5878a4511cb9eabf5fe4ebf0487b8cac93` |
| `picker-v2-diagnostic-run-001/identity-scope-supplement-inputs.json` | `e818bda5f3148e66bf08f097f8c3a034eb5815102b225d65ed1341d6fa8f62ea` |
| `picker-v2-diagnostic-run-002/final-review.json` | `9c6c43db3c50dcfa960d2ed39e882f1d81f48a29dfab5a8f45bc753a62dac3ce` |
| `picker-v2-diagnostic-run-002/final-review-inputs.json` | `642d7464164bae4a05a4b66431c32604d1e4bc06f711c649626e168bfb787d3c` |
| `ci-run-002/ci-jobs-011.json` | `6db9d1b99bc8f4b3a8a8a3b0e291daaac2e730cd7f7cba8c3a3bfaaaac6ec863` |
| `ci-run-002/desktop-jobs-011.json` | `076e0c2f5dae3c19671aa9dbdf5958568594b976c10334c029405c1f6a477b8d` |
| `ci-run-002/codec-search-readiness-validation-001.json` | `68d32fe1127ee73cbb136076e9a6d47590b5a9f593e5e70f3819498368a35f0d` |
| `ci-semantic-fix-002/receipt.json` | `4bcb3b8da0bd234aaa1fbff23a586d223c34c5996fcff5b4054cdfcfdf27a36d` |
| `ci-semantic-fix-002/candidate.patch` | `be1b590505a7dd34d601d287a85209bb32f7a9af9234ff28da240ac73d0cf361` |

## 后续完成条件

先完成新验证器的远端源字节和原生流程回归，保持失败附件和候选收据可追溯。
Windows 后续若继续定位，应另行登记有界消息级观察，记录单次动作附近的原生消息
入口及只读 foreground/GUI-thread 状态，取得匹配失败路径后再决定修复；不能通过
盲目重复激活或合成 WM_SETFOCUS 把本次未复现改写成已修复。若修改产品，需重新
冻结构建并登记严格复验；当前失败轮保持失败。
在完整严格复验通过前，不推广 pickerprocess，也不宣称长期无泄漏或原故障已全部解决。
