# Windows 稳定性与原生验收：2026-10-16

本轮已修复回归工作流失效的基线引用，完成 Windows CLI 对照和自动检查，并取得
文件选择器原型异常退出的实际调用栈，以及取消回复被 stdout 警告污染的原始字节。
原型已移除父窗口预禁用，并以 IPC v2 的独立管道传递回复。默认生产构建已完成
本轮六个 MP3/SEI case、32 份原生 JSON 保存、六份源文件关闭检查和正常 OS 退出复核。
最新生产原型已执行六个计划轮次及最终关闭，四次独立源读取和十四个已观察进程代际
的正常退出均已确认，父进程 GDI 固定窗口始终为 118。**本轮多出一次用户确认的
手动打开/取消，并有视图条件偏差，因此严格六轮协议未通过；原型仍不启用为默认实现，
也不据此宣布所有稳定性问题已解决。**

本报告记录截至 `2026-10-17T03:07:25Z` 已取得并复核的结果。本地时区为
Asia/Shanghai，本轮操作跨至本地 2026-10-17。
下文精确时间统一为 UTC。源码起点为
`f4c64cd8289516fa3338383cf6ed0f89b2a75ff3`；候选构建取自该提交之后的工作区改动，构建当时尚未提交。
具体构建由各自收据中的输入 SHA-256 绑定，不能只用 HEAD 代表二进制身份。

外部证据根（下称 NEW）为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-windows-stability-20261016`。
历史证据根（下称 OLD）为同级 `muxray-picker-process-20261007`。
历史文件、失败轮和诊断轮保留原身份，未被通过结果覆盖。

## 当前状态

| 项目 | 已证实结果 | 尚未证实的部分 |
| --- | --- | --- |
| CI 基线修复 | 新固定提交、源码树与祖先关系校验；20 个样本、100 份 v1–v5 输出逐字节一致 | 本轮没有运行远端 CI；新基线不等同于不可用的旧 SHA |
| Windows 自动检查 | 根模块 race/vet；IPC v2 后桌面默认/原型 test/vet/race 和 Linux picker 交叉编译七项通过；codec Python 138 项通过 | Linux 项仅交叉编译；自动测试使用非 GUI 子进程，不证明 Shell 对话框原生行为 |
| 默认构建 | 六个 case、32 份实际 JSON、相关内容/UI 和六份源文件关闭检查通过；PID 29280 正常 exit 0，全部七个已观察代际实际结束 | 首轮 PID 19680 exit 1 仍未归因；本轮功能流程通过不证明长期无泄漏 |
| 原型焦点诊断 | PID 11940 在 disabled 状态收到焦点，MoveFocus 返回 0x80070057 后 exit 1 | HRESULT 的底层原因、消息发送者和准确手动取消时刻未确定 |
| 候选修复 | 移除 owner 预禁用；IPC v2 独立回复管道及自动检查完成；生产 003 取得六个计划流程的空态/扫描证据，父进程最终 exit 0 | stdout 警告来源模块未确定；C1 最终空态之前有两个子进程周期，不能唯一关联到原登记子进程 |
| 生产资源观察 | 四份源关闭收据通过；14 个已观察代际全部 exit 0；8 个固定窗口 GDI 均为 118，USER/handles 初始升高后大致持平 | 额外子进程使 quantitativeChecksPassed=false；视图偏离及 C1 关联缺口仍保留。驻留时长来自用户确认，不能推论长期无泄漏 |

## 回归基线与自动检查

完整非浅克隆中的旧基线 `0e853bebd90f12658c1123f8703fb247cbf61530` 无法解析，
因此原 `git archive` 步骤不能执行。本轮重新登记功能引入前的
`bc799d6afebc7206d3c7bac1e6a8cc49dc1c41c1`，固定源码树并验证其为 timing v6
功能提交的第一父提交，以及功能提交可从 HEAD 到达。校验失败直接中止，不自动
回退或替换基线。没有证据证明新提交与旧 SHA 等价，也不从缺失对象推断历史改写原因。

两份固定提交源码在仓库外归档构建，使用本地 Go `1.27.0 windows/amd64`，关闭 Go
联网查询。20 个 timing 样本的 v6 schema、上下文、部分状态、预算和 461 处字段位映射
通过；同批样本的 100 份旧版 AVC CLI v1–v5 输出逐字节一致。详细身份、命令和哈希
见 [基线重新登记报告](codec-timing-baseline-20261016.md)。本轮 Windows CLI 结果
不改写此前 Linux 报告，也不表示远端 Linux CI 已执行。

`core-checks-002/receipt.json` 记录根模块 `go test -race -count=1 ./...` 与
`go vet ./...` 均退出 0，源码输入稳定。候选修复后的
`fixed-picker-checks-001/receipt.json` 记录桌面默认/`pickerprocess` 两种构建的
test、vet、race 共六项检查退出 0，输入稳定。测试覆盖并发请求、关闭期间取消、
晚到结果丢弃、子进程实际退出与清理、非法回复和 Job 回收。

IPC v2 修改之后，`fixed-picker-checks-002/receipt.json` 记录默认/原型两组
test、vet、race，以及 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c
./internal/picker` 共七项退出 0、350 个输入稳定，逐项 stdout/stderr 哈希与收据一致。
最后一项是从 Windows 执行的 Linux 测试二进制交叉编译，不是 Linux 测试运行或
Linux 桌面验收。`picker-ipc-v2-checks-001` 另保存详细子进程测试日志，71 条 PASS
记录包含子测试；这些受控进程没有运行 COM/UI。

Windows codec 检查器新增两个针对假通过的负控：要求 UI 明确显示当前展开的准确
MP3 帧号；不存在源跨度时拒绝出现对应的源码按钮。相关 Windows/oracle/snapshot
测试合计 42 项通过；之后 `python-all-001` 的 codec 工具全套 130 项通过。真实
Windows UI 捕获暴露 Hex 文本被 AX 分为多个节点，以及 EOF 后缀的格式差异后，
检查器增加限定 AX 组、按顺序拼接直属文本节点、完整跨度数字元组和实际窗口/EOF
一致性校验；错误组、乱序、错误数字、冲突窗口及错误 EOF 状态仍必须失败。
最终 `codec-tools-ax-final-002/receipt.json` 记录 **138 项通过**、退出 0、输入稳定。
六个 codec 样本的独立期望值包含 206 行，已冻结在 `codec-fixtures`，尚不能以这些
离线结果替代真实 UI、原生保存文件或 Hex 定位结果。

## 默认构建的异常与诊断对照

默认生产二进制 `default-native-001/MuxRay.exe` SHA-256 为
`a73593cc5f66cdb5cc562bfc3fc4eca2381cf5fe1370f7230b93f95cf13b5540`。
`default-run-001` 的父 PID 19680、创建 FILETIME `134366385688639844`，实际于
`15:42:21.2451917Z` 以退出码 **1** 结束。该轮未捕获对应父程序 stdout/stderr/崩溃
日志；没有足够证据将其归因为后续原型诊断发现的焦点路径。

独立默认诊断构建 `default-diagnostic-native-001/MuxRay.exe` 的 SHA-256 为
`366dc437e5b6504e60b80763695fe25c9bc60048d6551d4d8ee204f059e8b8b2`。
`default-diagnostic-run-001` 父 PID 19788、创建 FILETIME `134366391505226452`：
实际坐标取消返回可用空窗口；随后打开 `mp3-late-change.flv`，UI 显示 1,251,029
字节、1 个 Tag、0 条结构诊断及扫描完成，再关闭媒体回到空态。四次记录到的
MoveFocus 均返回 S_OK。父进程实际于 `15:51:24.7858777Z` 正常退出，退出码 0。

`media-closed.json` 仅显示“正在关闭”；最终空态证据是 `ready-after-close.json`。
`after-window-close.json` 的观察早于实际退出且仍列出进程，不能当作 OS 退出证据。
本轮为带日志的诊断对照，没有完成参数搜索、详情、Hex、快照的全套功能验收，
也没有独立证明媒体文件句柄关闭或默认生产版本的长期稳定性。

## 默认生产构建的六个 codec case 验收

`default-run-002` 使用相同的冻结默认生产 EXE，父 PID 29280、创建 FILETIME
`134366398711705847`。以下六个 case 已通过实际 UI 操作完成相应流程；原生保存得到
32 份互不重复的诊断快照 JSON，`checks` 中的独立检查收据验证其内容、源数据映射
及相应 UI 状态。此处统计实际保存的 JSON 文件，不把重复运行检查器计作新增保存。

| Case | 已保存 JSON 数 | 已完成的阶段覆盖 |
| --- | ---: | --- |
| `mp3-late-change` | 7 | 参数搜索三页；直接定位第 2600 帧的 preparing/ready；帧详情与源码 Hex |
| `mp3-bitrate-stops` | 4 | all、bitrate、stops 筛选；末尾源码 Hex 与 EOF 窗口 |
| `sei-pagination` | 5 | 初次/重新搜索一致性；三页搜索及源码 Hex |
| `sei-cross-256` | 9 | all、buffering_period、pic_timing 跨 256 边界翻页；原始证据、SPS、选择依据三个源码入口 |
| `sei-missing` | 2 | 缺失上下文状态与源码 Hex；没有虚构 SPS/选择依据按钮，Hex EOF 与源文件一致 |
| `sei-budget` | 5 | all 两页及 unknown、pictureTiming 筛选；明确保留 sei_message_limit 覆盖限制，源码 Hex/EOF 与源文件一致 |

每个 case 关闭媒体后均有独立 `*-source-closed.json`：`GENERIC_READ`、共享模式 0、
`OPEN_EXISTING` 独占打开成功，验证字节数及 SHA-256 未变后关闭检查句柄，没有写入
媒体。该事实证明检查时刻可以独占读取，不是对所有历史句柄的追溯证明。
阶段收据 `default-stage-review-001`、`002` 保留前四/前五个 case 的检查范围。
最终 `default-final-review-001/summary.json` 独立核对 **215 个输入**，确认 32 份
保存文件、实际输入的完整文件名、带准确文件名/字节数的保存完成提示、内容检查
收据、覆盖矩阵、六份源文件关闭收据和原始 OS 退出记录相互匹配；审计前后哈希不变。

`checks/native-save-index-001.json` 逐份绑定保存框输入、立即保存后捕获和最终完成
捕获。立即捕获中的“等待”文本不等同于完成；每一份另有准确完成提示。快照
createdAt 是产品生成元数据，不是 Save 点击时刻，没有从它推算输入时间。
`checks/codec-coverage-matrix-001.json` 汇总 12 个筛选序列、21 份搜索页导出、3 份
直接帧定位阶段导出和 8 份源码导出。分页完成与 codec 内容覆盖分别判断：预算样本
虽然 exhausted=true，仍显示 complete=false 的覆盖行及 `sei_message_limit`，
没有被错误解释为完整解码或所有负载有效。

早期 `mp3-source-hex-check-001.json`、`mp3-stops-source-hex-check-001.json` 的误报
及后续编号检查全部保留：前者未识别 AX 分节点，后者未识别真实 EOF 后缀。修复的是
检查器对实际 UI 格式的解析，原始 UI 和原生 JSON 未修改；不能把失败收据覆盖成通过，
也不能只凭 JSON 内容通过就代替 UI 检查。

原始资源采样共 **3,513 条**，索引及 timing 记录完整；没有发现/采样错误，最大相邻
采样间隔为 1.711095 秒。保留句柄确认 PID 29280 在
`2026-10-16T16:56:29.5214681Z` 实际退出，退出码 **0**，退出 FILETIME 为
`134366433895214681`。六个已观察 WebView2 进程也全部实际退出；summary 与每个
原始退出行逐项一致，采样器完成退出后宽限期。

因此本轮默认生产构建的指定六个 codec case、原生保存及关闭/退出流程通过。此处
没有 GDI/USER 的六轮固定窗口比较，也未证明长期无泄漏；进程轮询仍可能漏掉未发现
的短命后代。成功的 PID 29280 代际不能解释或抹去首轮 PID 19680 的未归因 exit 1。

## 原型焦点退出的实际证据

`prototype-diagnostic-run-001` 使用独立诊断二进制
`diagnostic-native-001/MuxRay.exe`，SHA-256
`7a8d344209466fa7bcf7fa1d1c35cd3350629fac9282b779d1353a0ed835938b`。
父 PID 11940、创建 FILETIME `134366395070154159`；子 PID 764、创建 FILETIME
`134366395394690200`。日志记录父窗口在启动子进程前从 enabled=1 变成 0。

`15:53:41.464865Z` 的焦点前置日志同时记录 enabled=0、inSend=1、当前线程与
owner UI 线程均为 14192；紧接着 MoveFocus(reason=0) 返回 `0x80070057`。
实际调用栈为 `WM_SETFOCUS → Wails onFocus → Chromium.Focus → errorCallback`。
冻结依赖源码中的 errorCallback 会调用 `os.Exit(1)`。保留的 OS 进程句柄确认父程序
于 `15:53:41.5803998Z` 退出，退出码 1，形成可证实的近端退出链。

子日志只有 `child.show.before`，没有 `child.show.after`；父日志没有 runner 返回。
子进程于 `15:53:41.5911397Z` 退出，退出码 0，比父程序晚 **10.7399 毫秒**。
该 exit 0 不能证明 Show 正常返回、COM 清理完成、取消回复有效或父进程接受了 IPC。
Job 清理与这一顺序一致，但没有在本轮直接跟踪 Job 的终止调用。

两次工具取消操作被跨窗口目标归属检查拒绝。用户确认“已点击取消”，记录时间为
`15:53:55.864Z`，没有精确输入 UTC。inSend=1 不识别消息发送者，Win32 多项状态
采样也不是原子快照。当前证据不能单凭 HRESULT 确认 WebView2 拒绝焦点的底层机制。
诊断日志会改变时序与资源计数，不能混入六轮生产构建验收。

### 历史诊断必须分开解释

复核 OLD 的 `native-002` / `run-native-002` 后，确认旧父 PID 2552 的日志也有相同
Focus/errorCallback 栈。但旧子 PID 32644 在 `2026-10-07T10:21:27.4679450Z` 已退出，
退出码 0；父程序到 `10:41:46.0415325Z` 才退出，退出码 1，两者相隔
**1,218.5735875 秒（20 分 18.5735875 秒）**。

旧子进程寿命为 600.0135868 秒，与十分钟期限一致，但没有独立子日志确认退出原因。
不能将历史轮描述为“子对话框取消后立即触发父进程退出”，也不能把新轮的
enabled/inSend/线程状态补写到旧轮。旧报告和旧二进制身份保持原样，本报告是新增复核。

## 候选修复与原生验收门槛

[picker_windows.go](../desktop/picker_windows.go) 与
[picker_owner.go](../desktop/picker_owner.go) 移除父程序对 owner 的预禁用，使原生
IFileDialog 接收到启用状态的 owner。父进程仍在进入前核对窗口所属进程与启用状态，
在 picker.Run 完成后核对身份并恢复启用，覆盖子进程异常或取消后的收尾。
Native.picking 仍串行保护打开和保存；子进程退出与资源收尾、owner 恢复在结果发布
之前完成；主窗口已开始关闭时，晚到的路径仍被拒绝。

这是一项针对已观察 disabled 焦点路径的候选修复，不把“Windows 如何保存和恢复
原 owner 状态”的推断当作已证实根因。新增 owner 测试验证的是策略模型、异常恢复
及销毁/异进程窗口拒绝；Windows 的真实模态恢复顺序必须由原生复验确认。

收紧注释中的因果措辞后，`fixed-picker-native-002/MuxRay.exe` 已按最终源码重新冻结为
无诊断 overlay 的优化生产原型，SHA-256
`282424538653a262b5e51416c4ac6cbdd57a4d332412512e9c60f6c446fb7499`。
`fixed-picker-native-001` 候选未运行并保留。002 未包含后续 IPC v2，不能作为最新修复
的生产身份；后续冻结 production 003 包含两项修复，以独立收据绑定输入和 EXE。
默认构建继续使用 Wails 文件选择器。

同一候选源码另冻结 `fixed-diagnostic-native-002/MuxRay.exe`，SHA-256
`7dbf12ef5e23b99138226dc6a5d7f412e38c579bb356c567dcc8a05f14b51afa`，用于先行验证
取消和成功选择时的焦点及原生 Show 返回。该构建已启动，证据位于
`fixed-diagnostic-run-001`：父 PID 11960、创建 FILETIME `134366436456644055`；
子 PID 18684、创建 FILETIME `134366436722404200`。子程序原始日志在
`17:01:12.263505Z` 记录 `child.show.before`，owner enabled=1。

`cancel-input-attempt-001.json`、`002.json` 分别记录两次工具取消操作因目标窗口
scope 不匹配被拒绝，刷新/激活后的实际 UI 也已保存。请求用户手动取消后，实际等待
跨过了原型的十分钟期限。父日志在 `2026-10-16T17:11:12.239053Z` 先记录
`runner.stop cause=deadline`；后续协议/管道错误没有改写最先保存的 deadline 原因，
`runner.end` 与 `run.return` 都明确是 deadline，canceled=false、selected=false。
保留句柄采样确认子 PID 18684 在 `17:11:12.2501078Z` 实际退出，退出码 0，寿命
600.0096878 秒；子日志没有 Show 返回。父日志随后记录 owner 从 enabled=0 恢复为 1。
**这是超时收尾证据，不能计为手动取消成功。**

该轮资源探针采集 1,800 条后于 `17:31:04.363Z` 达到三十分钟 duration_limit，
targetExitConfirmed=false、allObservedDescendantsExited=false。这表示探针停止时
父进程及部分已观察后代仍未确认退出，不表示采样持续覆盖了之后约九小时的等待。
用户后续回复“已取消”时已距原请求约九小时；没有准确输入时刻，不能将回复追溯绑定
为已结束的子 PID 18684 的取消操作。

随后同一父日志文件在 `2026-10-17T02:08:42.114910Z` 记录启动子 PID 11004。
`02:08:44.720127Z` 的 focus.before 记录 enabled=1、inSend=1，紧接着 MoveFocus
返回 S_OK。新子日志在 `02:08:44.773038Z` 记录 Show 返回 `0x800704c7`，随后
show.after enabled=1、response status=canceled；但父端在 `02:08:44.893437Z`
记录 protocol 错误，最终 selected=false、canceled=false，Wait 日志的 exitCode 为 0。
子端局部取消结果与父端接收失败必须同时保留，不能以焦点 S_OK 或 exit 0 宣称整条流程通过。

新子 PID 11004 没有处于旧探针的采样窗口内，当前只有日志 PID、没有独立采样绑定的
创建 FILETIME 与保留句柄退出证据，因而只能有限描述日志观察，不能与旧轮合并。

`fixed-diagnostic-run-002` 于 `2026-10-17T02:09:48.019Z` 重新附加只读采样，
绑定相同父 PID 11960、创建 FILETIME 与诊断 EXE SHA。用户原文“已点击取消”记录在
`received-manual-cancel-ui.json`，输入时刻仍为 null；该捕获的像素实际是前景 Codex，
虽然 AX 内容属于 MuxRay，也不能把这张截图当作 MuxRay 可见错误的证据。
随后 `cancel-error-visible-ui.json` 的真实 MuxRay 像素与 AX 均显示
“文件选择窗口打开失败。”；`keyboard-after-cancel-ui.json` 记录 Tab 后“重试查询”
按钮出现可见焦点框。AX focused_element 只报告窗格，未用它确认按钮焦点。
这证明了错误后的有限键盘响应，不证明取消被正确接收或后续打开功能成功。

点击窗口 X 后的即时捕获 `normal-close-windows-ui.json` 仍列出 MuxRay，且早于实际
退出；后来的 `normal-close-confirmed-ui.json` 确认窗口缺席。真正的退出依据来自
保留进程句柄：父程序实际在 `2026-10-17T02:10:56.5449386Z` 退出，退出码 **0**，
FILETIME 为 `134366766565449386`。新探针共 75 条样本和 75 条 timing，索引完整，
最大相邻观察间隔 1.016 秒，没有采样/发现错误。样本 69 同时记录七个已观察代际的
实际退出，均为 exit 0，与 summary 一致；随后五条空样本为正常退出宽限期。

独立审计 `fixed-diagnostic-review-001/summary.json` 绑定 12 个输入，并离线复核
三张原始截图。此次重新附加仅覆盖 `02:09:48.041Z` 至 `02:11:02.036Z` 的原始
采样，不能补回 PID 11004 的漏采身份/退出，也不能补回中间约九小时的连续性。
父进程正常结束没有消除取消的 protocol 错误；该诊断轮的取消流程仍未通过。
原始 stdout 的下一轮诊断结果如下。修复后的生产验收另用无 overlay 的构建执行，
没有复用诊断资源计数。

### 原始回复诊断与 IPC v2

`fixed-diagnostic-native-003/MuxRay.exe` SHA-256 为
`ab9c07682a0549c34f8b40b5867d4af3f1f492543df7bc6d614dd314f97315ba`，仍使用 v1，
增加严格解析前的原始字节捕获。`fixed-diagnostic-run-003` 绑定父 PID 6668、创建
FILETIME `134366769591493759`，及子 PID 28444、创建 FILETIME
`134366769804156258`。原始 UI 确认真实选择框存在；用户回复“已点击取消”，
准确输入 UTC 未知。取消时父 focus.before enabled=1、MoveFocus S_OK；子 Show
返回 `0x800704c7` 并记录 canceled 回复写入成功。

父端捕获 `ipc-read-response-6668-1.bin` 共 **125 字节**，SHA-256
`1eb19ca9933f6946429a22f5ac9aac0dbcfdf5e14524189726fbf1c104c481ff`。
前 48 字节为 `[WARNING] Ignore caches that are heterogeneous\r\n`；后 77 字节
为包含换行的 version=1、status=canceled JSON，nonce 与独立捕获的请求一致。
父端原始读取 readErr=nil，但整段不是合法 JSON，严格解析在 read-message 阶段拒绝，
最终返回 protocol 错误。激活后的实际像素及 AX 均显示“文件选择窗口打开失败。”。
这证实了该轮协议拒绝的直接原因，**没有证实是哪一个模块输出警告**。

179 条原始采样及 timing 索引完整，最大相邻观察间隔 1.018 秒，没有采样/发现错误。
保留句柄确认子程序于 `2026-10-17T02:17:32.0779800Z` 实际 exit 0；随后正常关闭
父窗口，父程序于 `02:19:03.7513956Z` 实际 exit 0。八个已观察代际全部实际结束，
与 summary 逐项一致。`fixed-diagnostic-review-002` 独立绑定 17 个输入及原始截图；
诊断轮的取消仍是失败，不能因 Show canceled 或 exit 0 改判为通过。该轮 WebView2
路径版本为 `155.0.4283.60`，前一轮为 `.45`，跨轮焦点差异不能全部归因于源码变化。

据此，私有协议升级为 v2，child 参数和请求版本一起变更；父端新建专用回复管道，
通过请求中的 responseHandle 和明确的继承句柄传给同一 EXE 的子进程。子端校验
管道类型、拒绝标准流句柄，接管并关闭回复句柄，并清除其继承标志。stdout/stderr
分别按 64 KiB 预算排空并丢弃；协议继续要求完整合法 JSON、正确版本和 nonce，
没有通过跳过未知前缀放宽解析。该隔离不是针对子进程内代码的安全边界。

新增受控进程检查覆盖合法回复伴随 stdout 噪声、专用管道中的畸形数据、标准流超限、
无效/错误类型回复句柄，以及后代持有继承管道时的清理。Job 分配失败的清理路径经过
静态复核，但没有注入该失败的测试。七项完整自动检查已通过；下文保留生产运行的
功能证据、资源统计和严格协议未通过的原因。

### 最新生产原型的功能与退出记录

`fixed-picker-native-003/MuxRay.exe` 已冻结为无 overlay 的生产原型，SHA-256
`dc72f49bd5628e39be344c65716df03d5a424e18abda3c618708f569b4264250`，构建收据 SHA-256
`20ab98e78b88ff5bb2bde954503c203e8d3b6d9ff09adb5cb4f3bd9ab8ecb228`。
`picker-run-001/identity.json` 绑定父 PID 5484、创建 FILETIME `134366778771309754`，
并记录 tokenElevated=false。双采样 ready 为 `2026-10-17T02:31:38.697603Z`；
最初 60 秒的 resource/GUI 两条流均有完整覆盖，分别由样本 0–61 包围。

六个计划轮次均已登记真实 UI 及子进程身份。三个成功轮的原始截图和 AX 均显示
sample.flv 扫描结束、172,504 个 Tag、0 条结构诊断、100% 进度；Close File 后均有
空态截图。三个取消轮也取得空态截图，其中 C1 的关联缺口另述。

| 计划轮次 | 登记子 PID / 创建 FILETIME | 已取得的功能证据 | 文件行选中证据 |
| --- | --- | --- | --- |
| C1 | 32076 / 134366779806414569 | 最终空态存在；其前有两个子进程周期，归属不唯一 | 用户陈述；fixture 最终视图未捕获 |
| S1 | 16000 / 134366783037987482 | 扫描完成、关闭媒体、空态、独占源读取 | 用户陈述 |
| C2 | 8848 / 134366785156500799 | 取消后空态 | 原图有 sample.flv 行高亮 |
| S2 | 10688 / 134366786845571949 | 扫描完成、关闭媒体、空态、独占源读取 | 原图有 sample.flv 行高亮 |
| C3 | 10928 / 134366788915645490 | 取消后空态 | 原图有 sample.flv 行高亮 |
| S3 | 32280 / 134366790727907333 | 扫描完成、关闭媒体、空态、独占源读取 | 用户陈述；两张 selection-observed 原图均无高亮 |

上述六个登记子进程均有真实 exit 0。所有轮次的至少 30 秒驻留均来自用户陈述，
精确手动选中和关闭输入时刻未知；有一张选中高亮截图也不等于工具测量了整个驻留。
S3 首次 Open 因工具检测到用户输入、要求重新获取窗口状态而被拒绝；保留原拒绝
记录，重新观察后才实际打开。该次被拒绝尝试没有对应的额外子进程。

`source-closed-S1.json`、`source-closed-S2.json`、`source-closed-S3.json` 均在各自空态之后独占读取，验证
同一 41,698,926 字节媒体及 SHA-256 未变。第四份独立 `source-closed-final.json`
于 `02:59:22.679776Z` 开始、`02:59:22.705923Z` 完成，晚于 S3 空态锚点
`02:55:55.363Z` 后 180 秒，并早于最终 X 输入 `02:59:44.070Z`。
四份不同路径/内容哈希的收据均符合只读独占读取条件；这仅证明各自检查时刻的事实。

原始资源流共 1,693 条，timing 索引完整；GUI 资源流共 1,686 条。保留进程句柄确认
父 PID 5484 于 `2026-10-17T02:59:44.3534433Z` 实际 exit 0，退出 FILETIME
`134366795843534433`；GUI 探针独立记录相同退出事实。父进程、六个 WebView2、
七个同镜像子进程共 **14 个已观察代际全部实际 exit 0**，原始行与 summary 一致，
没有采样/发现错误。最终窗口缺席只作为 UI 辅证，OS 退出以保留句柄为准。

### 严格协议未通过的原因

`picker-review-001/summary.json` 的 `quantitativeChecksPassed=false`，唯一自动
门槛失败为 helperInventory：登记六个同镜像直接子进程，实际发现七个。
额外 PID 27208、创建 FILETIME `134366781546797491`，实际寿命为
`02:35:54.6797491Z` 至 `02:36:25.3893130Z`，退出码 0。它出现在原 C1 子进程
结束之后、C1 最终 empty-ready `02:36:59.368Z` 之前。

针对该精确时间区间，用户回复“是，我又打开并取消过一次”；新增
`extra-helper-user-attestation-001.json` 保留原问题与回答。用户陈述解释了额外手动
操作，OS 证据独立确认进程代际；工具没有直接观测那次点击，也没有记录其选中文件。
这不构成已知的自动重复启动缺陷。**两次子进程之间没有 empty-ready 证据，C1 用户
确认及最终空态不能唯一绑定原登记子进程，C1 资源窗口也不能解释为一次干净取消的
资源差值。** 没有删除额外代际、改写 round-C1 或放宽分析器来取得通过。

视图条件也有明确偏差：C1 未捕获 fixture 目录最终选中/排序/分组；S1–S3 及
C2/C3 的实际截图显示 Details、Modified Date descending、Today 分组展开，与
预注册的 Name ascending 不同。未回填 C1，也未宣称固定视图完全一致。六个计划
功能路径的证据、用户陈述的驻留时长及资源观察分别报告；完整原注册协议不判为 PASS。

### 固定窗口中的父进程资源

下表直接报告原始样本中的范围。基线与各轮使用锚点后 30–60 秒，最后一行使用
S3 锚点后 150–180 秒；每窗 30 个完整 resource 和 30 个完整 GUI 查询。
虽全部固定窗口及源关闭/退出检查完成，整体分析仍因上述额外代际为 false。

| 窗口 | GDI | USER | Handles | Private MiB |
| --- | ---: | ---: | ---: | ---: |
| 基线 | 118 | 83 | 703–704 | 85.867–85.965 |
| C1 后（含额外操作影响） | 118 | 91 | 755 | 99.684–99.723 |
| S1 后 | 118 | 92–93 | 753–755 | 99.223–99.273 |
| C2 后 | 118 | 93 | 755 | 90.719 |
| S2 后 | 118 | 93 | 755 | 98.852–98.875 |
| C3 后 | 118 | 93 | 755 | 90.641–99.453 |
| S3 后 30–60 秒 | 118 | 93 | 755 | 98.980–99.043 |
| S3 后 150–180 秒 | 118 | 93 | 755 | 99.359–99.422 |

八个窗口的 GDI 均为 118。USER 和 handles 在首次打开阶段升高，后续分别大致保持
93 和 755，并未恢复初始值；private memory 在约 90.6–99.7 MiB 之间波动。
各窗 CPU 中位数均为 0。这些有限观察没有呈现每轮 GDI 持续增长，不能证明长期
无泄漏，也不能将初始台阶归因于某个模块。共享 Shell 资源及轮询未发现的短命进程
不在这个结论内。原始字节单位、工作集、CPU 和逐窗覆盖详见分析收据。
末个 150–180 秒窗口相对基线的中位数差为 GDI 0、USER +10、handles +52、
private bytes +14,159,872，不能把“后续大致持平”写成“全部回到基线”。

分析收据绑定 48 个输入，复核哈希无变化；另独立核对六轮 UI 引用与原图，并重算
八个窗口的 GDI/USER/handles/private/working-set 范围与中位数，均与收据一致。
另由独立复核 `picker-final-review-001` 绑定 68 个文件，不导入分析器而直接重算
区间与全部数值，结果逐字段吻合；固定覆盖窗的最大采样起点间隔为 1.002 秒。
该复核同样保留 strictProtocolPassed=false，没有覆盖初次审计失败。
`nativeAcceptancePassed` 保持 false。此结果支持保留两项修复继续验证，不支持将
辅助进程原型推广为默认实现。

六轮协议 `picker-case-protocol-001.json` 固定 C1、S1、C2、S2、C3、S3，使用同一父进程
代际、同一媒体和真实选中至少 30 秒。媒体为 41,698,926 字节的 `sample.flv`，SHA-256
`b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`，独立 CLI 扫描为
172,504 个 Tag、0 条诊断。每个辅助进程必须绑定 PID/创建 FILETIME，使用保留句柄
确认实际退出。成功轮还需实际扫描、Close File、空态和独立源文件关闭证据。

初始基线静置 60 秒。每轮静置锚点为“辅助进程实际退出 UTC”和“观察到父窗口空态
UTC”的较晚者；固定统计窗为锚点后 30–60 秒，末轮另外报告 150–180 秒。覆盖检查
分别保留完整 0–60 秒和末轮 0–180 秒；只纳入完全落在窗口内的查询和 CPU 区间，
最大采样间隙为 2 秒。缺少事件时间、身份、窗口覆盖或真实结果时保留 failed/incomplete，
不得补造时间、补零计数或静默换轮。最后还需正常关闭父程序，确认所有已观察代际退出。

GDI/USER 仅采父进程。实际子进程退出证明其进程所有资源回收，不证明所有共享 Shell
缓存已回收；六轮比较也不能证明不存在任何泄漏。轮询可能漏掉未发现的短命后代。

独立只读分析器 `helpers/analyze-picker-rounds-002.py` 已准备，21 项纯测试通过：
保留 100 ns 时间精度、排除跨界查询和 CPU 区间、拒绝缺失起止覆盖、超过 2 秒的
采样缺口、代际错误或缺失指标。001 分析器遗漏了真实退出后 probe 会从宽限期
样本中省略 root 的合法格式；独立复核发现后以 002 修正，001 及其收据保留。002
补充退出前缺失、PID 复用、退出后重新 live 等负控，并只读重放历史 687 条原始采样，
正确区分 681 条父进程存活、1 条实际退出和 5 条省略 root 的宽限期样本；这只验证
分析器格式兼容性，没有把历史异常退出改判为原生通过。
分析器只使用真实登记的逐轮事件和原始 OS 采样，
未知输入时间不补值；用户确认的驻留时长与工具计时分开报告。其定量检查结果不自动
成为 GUI 验收结论，原始 UI、实际视图、源文件/扫描事实和手动确认仍需独立复核。

生产六轮开始前另准备 `helpers/analyze-picker-rounds-003.py`，保留 002 与协议 001
原件。003 的 37 项纯测试通过（原有 21 项及新增 16 项），新增以下四份独立收据
的检查：S1/S2/S3 的 sourceClosed 分别在该轮 empty-ready 后开始，S1/S2 必须在
下一次 Open 输入前完成，S3 必须在最终关闭输入前完成；另有独立
`source-closed-final.json`，在 S3 静置锚点后至少 180 秒才开始，并在最终关闭输入及
父进程实际退出前完成。四份收据须使用不同路径和不同内容哈希，均绑定同一媒体的
路径、字节数、SHA-256 与只读独占打开事实。使用 helper 的真实 `completedUTC`，
不把未知时间补写为 finishedUTC。真实 `final-close.json` 绑定父代际、关闭输入开始
及工具返回时间和原始 UI 证据；后者可以晚于 OS 退出。

这些检查仅证明各次独占读取时刻的事实，不追溯证明所有历史句柄均不存在；分析器
不设资源增长通过阈值，`nativeAcceptancePassed` 始终为 false。最终调用和证据要求
见外部 `picker-analysis-preparation-003/USAGE.md`。本轮结果保存为独立
`picker-review-001`，额外代际导致的失败原样保留。

## 后续工作与保留边界

- 若要取得严格原协议通过结论，应另建明确编号的独立运行，先核对预注册视图，
  每次空态确认后再进入下一轮，确保每轮对应唯一子进程；保留本轮失败及偏差记录。
- 默认实现仍为 Wails 选择器；默认 PID 19680 的 exit 1 未归因，后续复现须捕获同一
  代际的日志，不从原型焦点或 stdout 证据替它补写根因。
- 当前资源观察不等于长期或跨版本无泄漏，也不能替代另一轮严格验收或默认推广决策。

## 关键收据

以下路径均相对 NEW；更多原始输入、日志和逐项 SHA 在对应收据中。

| 路径 | SHA-256 |
| --- | --- |
| `ci-baseline/receipt.json` | `93f4bdcd6c82cab61960038fccd168532930012b44dde9c08b95d8b56ac2cf5c` |
| `core-checks-002/receipt.json` | `7d9be2acbb10f788010c9ec3db8f5fda0c2ae602c09aa85bb9cf468692b548e1` |
| `fixed-picker-checks-001/receipt.json` | `8d30d6991d2d846c790ef773736dd3475c59e6867b68a13edd8af5f614cfad51` |
| `fixed-picker-checks-002/receipt.json` | `c98e65491cd970b744318c1ec02d7da354b0290de3c41f5457ddd613f2346c74` |
| `picker-ipc-v2-checks-001/receipt.json` | `13bfaee80deb7244e5b78e1a58f55a006fa767cc80043a3b6e1f223110e30c9c` |
| `codec-checker-review-002/receipt.json` | `9c7e27af795ee5206d36a9a97d8168d79bd6c2144e5f703a802111f4dab67786` |
| `python-all-001/receipt.json` | `41b8d17b519b1910c6299c25844cb32c73c0ca1816d9f498d6aeb323e641ed66` |
| `codec-tools-ax-final-002/receipt.json` | `861d9abed2ed964f399601e762a8808eaf977c9ff60b4b6a748968fcee730e8e` |
| `default-stage-review-001/summary.json` | `79c1043f14e49476b71f503c3fdf9edd2f862d750f43a57d20575b4475fb0671` |
| `default-stage-review-002/summary.json` | `b1539d20ef63b813f4e0d6cbd9e47f008db8448729c397e9c3f55bb6bd1716b6` |
| `default-run-002/checks/native-save-index-001.json` | `2adfc25a8e4f7a3bba4e27da0550a2a55dbacdf214ca1ca343b49134eac26165` |
| `default-run-002/checks/codec-coverage-matrix-001.json` | `76cce78c15adaa8d66dc650a468eaeed200dfd688dbfea262536f10631368525` |
| `default-final-review-001/summary.json` | `358e3defe8d34f31209132f401c42d2e4435cf2277ef10801b143164bd1493c5` |
| `codec-fixtures/result.json` | `9dcaeff268f8a6eaaa28ce68e5bc66879f29236a679d27e77b05114dabca6efe` |
| `diagnostic-review-001/summary.json` | `e5ff50c74db1a079e5d453fbb19acc411466a66c7cf829cad344c3ae778b3e66` |
| `fixed-picker-native-002/build-receipt.json` | `a110f3d609b42b415ed3fe1f956d50477c6807d3082d75f1655823c163b8e9e0` |
| `fixed-picker-native-003/build-receipt.json` | `20ab98e78b88ff5bb2bde954503c203e8d3b6d9ff09adb5cb4f3bd9ab8ecb228` |
| `picker-run-001/identity.json` | `6c56ef8267224c4a5b4e803200d4e9870b28de0b405160d0642807b2ecf5e850` |
| `picker-run-001/samplers-ready-001.json` | `5889ca0f308e9ec2131212b4171b22ab46135c18953be6e831ef403bef0ba0b3` |
| `picker-run-001/resources/summary.json` | `6edefe15805456495be4c5fe3616300990bfc06952c147681c8509729e524270` |
| `picker-run-001/gui-counts.summary.json` | `f4f01f578cf6f70cab97309c78edfa23faa326b421db35ac4263f43d9f1bd1b5` |
| `picker-run-001/source-closed-final.json` | `b5f174c9ff567e5d3f7d6b2a279a81d11cb165c4911788593ea1e4a61a117dbe` |
| `picker-run-001/final-close.json` | `ff62c089a1048fd22e7dc3c309ab240770ddafc3f2a87507ba0ae4cf863c52aa` |
| `picker-run-001/extra-helper-user-attestation-001.json` | `96b8a56488f782ae80cb7517b35f60111ea425b30777cc0162e8076c017d56d9` |
| `picker-review-001/summary.json` | `439d3ea4822813f1a94e7ad4756013ac7eca6acea7e718de5e686c82f9ff7b4c` |
| `picker-final-review-001/summary.json` | `670b8e1518616d65ff2cc4a6d3c878913309e4fcd5a2bb4b769fcb8ccee73c01` |
| `picker-final-review-001/inputs.json` | `318ad991d0ad30606788fecb15a2ad0e99c49401e751206f458233fc0b13942b` |
| `fixed-diagnostic-native-002/build-receipt.json` | `442509a0a5466b6207a769f0f2cedd3e642a8bb5cefc043416d2f714031ba6d3` |
| `fixed-diagnostic-run-001/resources/summary.json` | `11d46c1af54c1d8da7729dbe1304138527f688dae55db4bad0ccb2b8ae99cea7` |
| `fixed-diagnostic-run-001/resources/samples.jsonl` | `6a1476045c4988392abbbdd6645e33b08ea4678c8e5db85322cf7afc09e3f7f2` |
| `fixed-diagnostic-run-002/resources/summary.json` | `f337bbcc1536f43789497f80f4ce2b143b9213441cb0527cc2d8b96c8d77ace3` |
| `fixed-diagnostic-run-002/resources/samples.jsonl` | `4e265a75da4300274659d96759fa63070c24816bb4dd8041e6ec0122dd81ec43` |
| `fixed-diagnostic-review-001/summary.json` | `50e255c56d8b841ec14e051be0fa597c065cc82ce8c28c5f97b50480615cd491` |
| `fixed-diagnostic-native-003/build-receipt.json` | `eda681e07ae0f1d9f6a7e07428080828357750734a0cf483a0f27351ed757c8f` |
| `fixed-diagnostic-run-003/resources/summary.json` | `95581eea8fb903b44f57499ff525a8396ba050542392a94256acc7a2a7f6d48b` |
| `fixed-diagnostic-review-002/summary.json` | `06fbcb0c578c582201d9d32b6f264e5955e8504a45ada025fea55cc02b68480d` |
| `picker-case-protocol-001.json` | `e8bc0888d6bfccd3273529df0c669e6296c08ab566210f875c2dd6f9a5b5fa25` |
| `picker-fixture/binding.json` | `91026b5adf03145d2f963465d44944ff25d356e1fd3e5151ae76f88de58f63f2` |
| `picker-analysis-preparation-002/receipt.json` | `d4c69479d265f6d375d469bbb865e06d778a578552e044e43a9825d504926a17` |
| `picker-analysis-preparation-002/historical-format-replay.json` | `667e8973decf70d47453a9882b52c9df02b53ba8fc2278b4f4e3a591a6710a1f` |
| `picker-analysis-preparation-003/receipt.json` | `9d5085557c8e13d2137bc18e561126b5df712f5ad128fe6f328dddf7f5bd14a5` |

独立诊断审计绑定 2,960 个输入，并确认审计前后未变化；其
`auditChecksPassed=true` 只表示审计自洽，`nativeAcceptancePassed=false` 明确保留未通过状态。
