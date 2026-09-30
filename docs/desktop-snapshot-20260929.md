# 桌面诊断快照 JSON：2026-09-29 验收记录

本轮 ADR-019、桌面导出、原生保存与异常恢复验收已完成。下列本地自动检查、
最终生产程序三种样本，以及三个独立受控故障场景均通过。验收进程已退出。

## 实现范围

[ADR-019](decisions/019-desktop-diagnostic-snapshot.md) 定义独立的
`desktopDiagnosticSnapshot` v1 合同；CLI 的既有 JSON 合同不变。
扫描终态后，“导出诊断 JSON”冻结当前已载入的有界证据，通过 Windows 原生
保存对话框写入 JSON。导出不重新读取 payload，不启动分析，不复制全部索引。

报告保留源文件名、扫描覆盖、最多 100 条保留结构诊断、选中 Tag、匹配的已完成
详情与 Hex、已有 Summary 概览及当前一页事件、当前趋势窗口及局部观察项。
坐标采用十进制字符串；缺失部分有明确 `omitted` 原因。趋势最多 512 点、事件页
最多 100 条、Hex 最多 4096 实际字节，编码后总大小上限为 2 MiB。
报告可能包含 AMF 字段文本和原始媒体字节，但不含源绝对路径。

详情与 Hex 分别保留一个成功结果，按精确请求 ID 配对；新同类请求使该缓存失效，
新 Summary、关闭和源变化清理两类缓存。既有读取查询仍只接受当前读取请求。
这使先看详情、再看 Hex 时能够导出两份已完成证据，且不改变读取 worker 协议。

保存只接收证据选择 ID，目的路径由原生对话框取得。源路径、原始句柄身份及其
硬链接别名受到保护；符号链接目标被拒绝。先创建同目录独占临时文件，检查
write/sync/close，再复查目标身份并 rename/replace；发布前失败保留已有目标。
保存响应超时后按同一 ID 查询，手动确认不会重写文件或再弹出对话框。
关闭文档后已经冻结的快照可以继续保存，状态仍标注原文件。

## 本地证据与构建身份

证据根目录：

`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-snapshot`

真实 Windows 交互使用 Wails、WebView2 和原生文件对话框。首次观察遇到系统锁屏；
用户解锁后才执行本表中的验收。锁屏画面不作为产品验收证据。

| 产物 | SHA-256 | 长度与范围 |
| --- | --- | --- |
| 首次生产构建 | `8D7A434B7B7551D6CB51BB855387D484AA210F3215053D6E90DC68EEF94B8556` | 11408384 byte；mixed 完整证据导出 |
| 最终生产构建 | `6483DD0C53A1565E2E0A00082A8BD36DD2943715B84E6D3EF072B8737AD1B865` | 11408896 byte；mixed、截断载荷、空索引三样本 |

最终产物位于 `desktop/build/bin/MuxRay.exe`，使用统一入口
`desktop/build-production.ps1`，实际命令为
`wails build -webview2 error -nopackage -trimpath`。未生成安装包。
最终前端 assets 为 `index-B3RheiEb.js`、`index-BaQSi16Z.css`；完整输入身份见
`native-final-001/production-build-identity.json`，构建日志同目录保留。
首次构建之后补齐了无效趋势起点的错误码映射；不能将两次二进制混称为同一产物。

收尾时 `native-final-001/production-identity-final-check.json` 确认 187 个输入
文件哈希全部一致、最终 exe 哈希未变化。`processes-final-check.json` 确认所有
本轮 MuxRay 与受控验收进程已退出。`acceptance-summary.md` 是原生证据清单，
`final-checker-invocations.json` 记录最终三份生产报告的独立检查全部通过。

## 自动检查

| 检查 | 本地结果及证据 |
| --- | --- |
| 根模块普通测试、race、vet | 全部通过；`root-test.log`、`root-race.log`、`root-vet.log` |
| desktop 模块普通测试、race、vet | 全部通过；`desktop-test.log`、`desktop-race.log`、`desktop-vet.log` |
| 前端控制器测试 | 141/141；`frontend-test.log`；含初始回复丢失、查询失败/手动确认、延迟响应、关闭与新会话隔离 |
| TypeScript 与 Vite 生产构建 | 通过；最终 `production-build.log` |
| Wails bindings 重复生成 | 离线 `wails generate module` 返回 0；生成文件和 Go 模块文件共 7 份哈希均不变，见 `bindings-reproducibility.json` |
| JSON Schema、golden 与负例 | 通过；`contracts.log`；包括快照身份、精确坐标、大小上限、省略项及局部观察口径 |
| 确定性结构样本测试 | 20/20；`fixtures.log` |
| 合成 host 契约测试 | 25/25；`synthetic-host.log`；包括保存新增 9 项，不启动浏览器 |
| 保存 overlay 生成器自测 | 17/17；`save-harness-test.log`；包括输入漂移、拒绝覆盖、Go 语法及收据身份 |
| gate 与 SummaryRecovery 工具自测 | PowerShell 5.1.26100.9549、7.6.5 均通过；`gates-powershell.log`、`gates-pwsh.log` |
| 实际导出检查器复核 | `checker-review.py/json`：4 项正例与 7 项反例通过；修复了伪造 Tag 序号和缩短 Hex 的漏检；最终 mixed 检查结果见 `mixed-check.json` |

Go 测试覆盖完整/最小/部分快照、无额外 I/O、原始文件身份、请求配对、未知前驱、
严格大于间隔阈值、最大组合与硬字节上限；保存测试覆盖取消、捕获/写入失败、
源路径/硬链/符号链保护、目标变更、替换成功、未见请求封存、重复请求只写一次、
文档关闭后继续保存和 shutdown 提交栅栏。另行只读审查未发现阻断性问题。

Windows 非 GUI CI 统一到 `.github/workflows/desktop.yml`，包含 bindings 漂移检查、
上述桌面检查及相同生产构建入口；根模块原有多平台、Schema 和 fuzz jobs 保留。
本轮没有推送或执行远程 GitHub Actions，也没有执行浏览器 UI smoke。
合成 host 测试不能冒充浏览器或原生对话框验收。
`ci-static-check.json` 另确认两个 YAML 可解析、根模块原有 jobs 未变化，桌面
只保留一个 Windows job。手写文件通过 `git diff --check`；Wails 生成的 models
保留生成器本身的空白格式，以可重复生成的哈希为准，不手工改写生成物。

## 原生生产程序实测

| 场景 | 实际结果与证据 |
| --- | --- |
| mixed 完整证据 | 300 Tag、6496 byte；完成 Summary，趋势窗口 `[256,300)`、delta、阈值 2001；选 Tag 257 查看 AVC，再看 Payload Hex，保存 `mixed-native.json`，59324 byte。`mixed-save-success.json` 记录 UI 成功状态 |
| 完整证据的字节核对 | 独立 Schema 通过；详情请求 2、Hex 请求 3、Summary 请求 1；Hex 从 offset 5601 读取 512 byte，与源逐字节一致；100 条关键帧页、44 个趋势点、8 条局部观察全部保留，`omitted=[]`。Tag 258 的差值恰为 2001，不计为大间隔 |
| 取消保存 | 第二次打开保存窗口后 Escape；最终稳定截图 `native-final-001/003-handoff-canceled-settled.json` 明确显示“已取消保存”，当前 300 Tag 及 Summary 保留 |
| 最终构建：截断载荷 | `trends-truncated.flv` 的 Tag 1 声明 10 byte、实际 2 byte。趋势→Payload Hex→保存 `native-final-001/truncated-native.json`，2407 byte；报告 `scan.complete=false`，Hex offset 39、实际 `AF 00`，未补零，未请求部分明确 omitted；`truncated-check.json` 通过源字节核对 |
| 最终构建：空索引 | 13 byte 合法 FLV、0 Tag，直接保存 `native-final-001/empty-native.json`，1382 byte；`scan.complete=true`，6 个未请求/不可用部分明确 omitted，不虚构详情、统计或趋势；`017-empty-saved.json` 和 `empty-check.json` 分别保留 UI 与校验结果 |
| 最终构建：完整主流程复测 | `041–047` 重新完成 300 Tag Summary → Tag 257 AVC 详情 → Hex `[5601,6113)` 的 512 byte → 趋势 `[256,300)`、delta、阈值 2001 → 原生保存。`mixed-final-native.json` 为 59324 byte，生成于 `2026-09-29T01:07:42.2312452Z`；选择为 detail 2/Hex 3/Summary 1、关键帧页 100 条，`omitted=[]`。`047-final-mixed-saved-stable.json` 的文本与截图均显示成功，最终检查器核对 Schema、源 Tag 与 Hex 通过 |

完整 mixed 报告 SHA-256 为
`4ff5559b6a8ea0a5a6e0b45bc26ab221588f6c1c6024712bf6358603009a1f6b`。
其源内容 SHA-256 为
`58c47ee6b21dad16b0ce4802f8c5c4aed6b2a99c8347972d4f522a5e4382f3f7`。
实际打开的是历史趋势样本目录中的同名文件，与本轮 `fixtures-001` 的样本
哈希一致；报告只保存 basename，不用生成时间或文件名冒充内容哈希。
最终复测报告 `mixed-final-native.json` 的 SHA-256 为
`2c3027889b6a7bb22374105f93426271015508d1edf95036cafd5cc0bfa5c28f`。

## 受控保存恢复实验

三个独立 run 使用 `save_recovery_build.py` 从相同产品源码及 assets 生成 Go overlay，
通过真实 Windows 保存对话框触发。它们的 exe 仅供验收，不是可分发生产程序；
普通 `save.go` 不含 gate。每次构建的命令、源/前端/工具/overlay/fixture 哈希和实际
exe 身份单独记录。收据必须和实际 UI 观察结合，不把收据当成截图结论。

| 场景 | 已观察结果与证据（位于 `native-final-001`） |
| --- | --- |
| 初始 Save 回复丢失 | `lost_reply_002` 使用 8 秒延迟；`026-lost2-recovered-before-return.json` 已显示保存 1566 byte，紧接检查初始回复尚未释放，`ui-recovery-observation.json` 记录 `initialReturnReleased=false`；`completed.json` 与后来的 `return-released.json` 保持同一 run/进程/请求。迟到回复后 `027` 状态稳定，没有第二次保存对话框 |
| 写入失败及再导出 | `write_failure/write-failed.json` 记录目标 I/O 前注入失败；`031-write-failed-summary-retained.json` 显示失败提示，同时 20 Tag/497 byte 文档与已完成 Summary 保留；没有创建新目标，源哈希不变、没有残留 snapshot 临时文件。同会话新请求再导出成功 11299 byte，见 `033` 及 `write_failure/retry-check-final.json` |
| 冻结后关闭源文档 | `close_during_save` 在写入前等待；`038-close-gate-empty-pending.json` 已是空文档，仍标注原文件且等待保存。实际顺序为 entered `00:59:55.4575639Z` → 空页观察 `01:00:07.530Z` → release `01:00:19.617528Z` → completed `01:00:19.6889741Z`，均属同一 run/进程且未超时。`040-closed-source-save-stable.json` 的文本与截图均显示保存 1566 byte、打开入口恢复启用；`report-check-final.json` 通过，报告保留冻结时 `scan-1` 的 20 Tag/497 byte、`closed:false`，不会把保存后状态写回快照 |

第一轮 `lost_reply` 的稳定截图在初始回复释放后才取得，不能区分查询恢复和原调用
返回，因此只保留为历史证据，不计入恢复通过结论。第二轮重新创建独立 run，
以截图后的“初始回复尚未释放”事实确认恢复路径。写失败原生场景使用新目的文件；
已有目标在写失败时保留的结论来自 host 单元测试，不冒充原生覆盖。
收尾的 `receipt-order-final-check.json` 另行核对了两条成功受控恢复链的
run/进程/请求身份与时间顺序，结果均通过。

## 限制

本轮使用确定性结构样本，不证明媒体可播放或完整语料覆盖。快照是当前已载入
证据的有界导出，不是可重新打开的项目文件、全文件时间线或完整 CLI Summary。
源一致性基于已有变化锁存，不声称在保存时重验全文件；不保证识别同大小同时间戳
原位改写、恶意目录竞态、断电或强制中断阻塞文件系统操作。受控失败不是自然慢盘
实测。安装器、签名及发布验收仍在本任务范围之外。
