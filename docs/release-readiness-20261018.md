# Windows 稳定版发行准备（2026-10-18）

> 历史记录：本文保留 2026-10-18 发行准备阶段的核查过程与当时结论。
> MuxRay v0.1.0 已于同日公开发布；当前下载与版本信息见
> [v0.1.0 发行说明](releases/v0.1.0.md)。下文中的候选构建、待决事项及发布状态
> 均属于准备阶段记录，不代表当前发行状态。

本轮目标是公开发行 Windows x64 portable 稳定版，质量门禁不因发行请求而降低。
**目前仍未通过稳定版门禁，尚未发布正式版本。** 默认构建继续标识为 `0.1.0-dev`；
不能将本地预览 ZIP 改名为稳定版。平台、能力及数据边界沿用[预览范围](preview-scope.md)。

## 本轮已完成的准备

- 维护者明确选择 [MIT](../LICENSE)，补入项目许可证；第三方软件保留独立许可。
- [第三方通知](../THIRD-PARTY-NOTICES.txt)及[许可清单](../license-inventory.json)
  根据实际 Windows EXE、Go 包图和嵌入前端收集，覆盖 18 个第三方 Go 模块及实际前端组件。
  不分发 WebView2 Runtime、Microsoft loader/bootstrapper 或用户源媒体。
  Wails JS runtime 的 package.json 标 ISC、模块根 LICENSE 为 MIT 的元数据差异已保留；
  不据此编造缺失的许可文本。清单绑定下述 EXE，后续改变依赖或构建必须重新核对。
- 将最低 Go 工具链更新到 1.27.2；升级 x/net 0.60.0、x/text 0.42.0，
  连带 x/sys 0.48.0、x/crypto 0.57.0。source-map-js 升至 1.2.2。
- CLI、窗口标题和诊断 JSON producer 共用构建版本；Windows 生产脚本支持严格验证的
  `-Version` 参数。版本注入不改变报告 schema，不自动打标签或发布。
- 新增 Security 工作流：核心和 Windows 桌面生产 tags 的 Go 漏洞扫描，以及包含开发
  依赖的 npm audit。网络错误不能算通过；govulncheck 使用有失败退出码的 text 模式。
- 新增外部退出采集器 `tools/desktop-native/capture_exit.py`，收集实际进程身份、
  stdout/stderr 和 OS 退出码。它不会吞掉产品错误或强杀产品；原始日志可能含本机路径，
  只保留本地，分享前需检查。完整性、截断、超时与持久化分别记录。

## 本地安全与回归验证

此前 Go 1.27.0 命中 13 项标准库记录，桌面依赖另外命中 x/text 记录；扫描分为模块、
导入包与可达符号层级，不把每条模块命中称为已验证的利用路径。升级后：

| 检查 | 结果 |
| --- | --- |
| 核心 Go 源码 govulncheck | No vulnerabilities found |
| 桌面生产 tags Go 源码 govulncheck | No vulnerabilities found |
| 实际 Windows EXE 二进制 govulncheck | No vulnerabilities found |
| npm audit（包括开发依赖） | 0 项；此前 source-map-js 的 high 项已消除 |
| Go 核心、桌面测试、vet 与 race | 通过 |
| 前端测试、typecheck、production build | 237 项测试通过；构建通过 |
| JSON 冻结合同验证 | 通过；未调整已有 schema |

漏洞数据库检查结果仅针对本次版本与当时数据库，不承诺永久不存在漏洞。
退出采集器 17 项子进程/失败负控测试通过；Windows 构建参数自测在 PowerShell 7/5.1 通过。
Linux 主窗口定位工具已兼容带版本标题，并保留 PID 与唯一窗口约束。该工具的 Linux
专用完整测试需由 Linux CI 执行，不把 Windows 上不可用的 `/proc`/信号用例算通过。
以上本地结果不替代本次提交的远端 CI；远端结果应按完整提交号在 Actions 中核对。

## 一次有界原生观察

默认生产 EXE：SHA-256
`18c7f66b6b5e73893ba4b828400df9e601eb308d8e4ffcac25a1162cd9318e32`，
Go 1.27.2，`desktop,wv2runtime.error,production`，不含 `pickerprocess` 或 overlay。
该候选从 `df7a3fe` 加本轮依赖与构建身份改动构建；本地记录保留源码 manifest 与 diff，
发行时仍必须绑定最终提交。

预先限定一轮取消、成功打开、详情/Hex、保存、关闭文件与退出；不以重复成功次数
消除旧异常。实际 PID 3848、创建 FILETIME `134367950994143964`：

- 默认文件对话框取消后恢复空会话。随后按完整文件名打开旧现场的 `case-a/sample.flv`；
  未声称完整重建旧现场的视图和输入时序。
- 文件 41,698,926 byte、172,504 个 Tag，容器索引完整、结构诊断 0。
- Tag 0 AVC 配置详情可读，Hex 实际窗口 `[24, 536)` 共 512 byte。
- 原生保存 19,457 byte JSON v3，schema 合法；Hex 与真实源字节一致，未加载部分如实省略。
- 关闭文件恢复空会话；程序仍活着时源文件可用 `FileShare.None` 独占只读打开，
  SHA-256 前后均为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。
- 点击窗口关闭，持有进程句柄确认 **exit 0**，最终窗口列表为空。两输出流均确认 EOF，
  未截断、无缺失或超时；stdout 48 byte、stderr 64 byte，无本轮失败栈。

本轮原生观察使用采集器第一版；之后针对慢磁盘造成管道背压的工具修正单独通过测试，
不能倒填为原生观察已经使用了新工具。实际本轮没有采集 I/O 错误。

句柄分类采样没有通过完整性门禁：95 个活样本均有对象类型索引 56 无法复制查询
（Windows error 50）。GDI/USER 等独立字段有值，但不把不完整的句柄分类报告写成
资源稳定性通过，也不从启动到对话框后的计数差推算每轮泄漏速率。

## 稳定版仍被什么阻塞

[10 月 16 日默认生产运行](desktop-windows-stability-20261016.md)确有 OS `exit 1`，
缺少同代错误输出和调用栈。本轮没有复现，**未证明已经修复或界定触发条件**。
辅助选择器原型的 MoveFocus 错误不能自动套用到这次默认运行；原型仍不进入默认构建。

Shell 图像列表/GDI 保留另列为长期重复对话框风险：独立原生宿主也能复现，但尚无
可安全用于产品的释放点，不强制释放 Shell 私有对象。它与默认异常退出分别报告。

下一步只聚焦取得可归因的默认失败材料：用退出采集器运行实际触发流程，保留同代
错误、调用栈和输入时刻；出现失败立即停止该轮，修复可证实路径后做定向复验。
若无法复现，继续保留待决状态，不无限追加相同成功流程来换取“稳定”结论。
稳定版需要完成这一风险评估及最终提交的 CI、构建和原生证据后才能公开发布。
