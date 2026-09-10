# Windows 受控原生验收

这组工具从新 checkout 生成独立的验收程序和结构样本，用真实 Windows 文件选择器、Wails IPC 和会话生命周期复现取消及阻塞退出。正常产品代码不含 gate；工具只通过 Go `-overlay` 替换本次构建读到的 `session.go`，不会修改仓库 Go 源文件。

样本是确定性的 **493 byte、32 个空 Script Tag** FLV，仅验证容器 framing，不是可播放媒体或有效 AMF。SHA-256 为 `9758dc3635a5e05a780ffe4cd40659b243d2d23a4a9e704762add90fcd4c34fb`。仅这个 run 的绝对样本路径会在第 23 次 `ReadAt`、offset 163 处阻塞，之前已有 10 个完整 Tag。gate 故意忽略取消，直到同一进程身份的 release 收据发布；这是受控 I/O 实验，不能称为正常生产程序的自然慢盘实测。

## 前提

- Windows amd64、PowerShell、Go（符合两个 `go.mod`）、CGO C 编译器、Node（符合 `desktop/frontend/package.json`）及 npm 已在 PATH；运行桌面程序还需要 WebView2 Runtime。
- Go 模块和锁定 npm 包已在本机缓存。工具强制 `GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`，执行 `npm ci --offline --ignore-scripts --no-audit --no-fund`。冷缓存会失败；首次准备依赖需在允许联网的开发环境分别为根模块、desktop 模块执行 `go mod download`，为前端执行 `npm ci --no-audit --no-fund`。工具不下载工具链或私有媒体。
- 仓库自带已生成的 Wails bindings。本工具无需运行 Wails CLI；它每次重新安装锁定的前端依赖并执行 `npm run build`，不会默默嵌入旧 `dist`。
- `Repository` 和 `RunDirectory` 均须绝对路径。每个 run 必须使用不存在的**仓库外**新目录，防止生成的 overlay Go 文件进入正常构建；取消、退出、非 GUI 验证各用一个 run。使用本地 NTFS 或支持硬链接原子发布的文件系统，不支持 junction/symlink，也不覆盖、重置或删除历史证据。
- 同一 run 只启动一次验收 exe、只打开一次该 run 的样本。再次打开或启动会因已有收据失败；请构建新 run。构建时不要同时修改源代码。

以下示例可直接在新 checkout 使用，替换路径即可（空格路径受支持）：

```powershell
$repo = 'C:\work\MuxRay fresh'
$verify = 'C:\work\native acceptance\verify-001'
$cancel = 'C:\work\native acceptance\cancel-001'
$exitRun = 'C:\work\native acceptance\exit-001'
$scripts = Join-Path $repo 'tools\desktop-native'

& "$scripts\verify.ps1" -Repository $repo -RunDirectory $verify
& "$scripts\build.ps1" -Repository $repo -RunDirectory $cancel
& "$scripts\build.ps1" -Repository $repo -RunDirectory $exitRun
```

`verify.ps1` 不启动 GUI：在独立测试 overlay 中运行带 race 检查的真实文件会话测试，核对完整样本 32 Tag、gate、`running → canceling → canceled`、保留 ID 0–9、source Close、会话 Close 后拒绝分页，以及文件可独占打开。它不能代替以下原生交互验收。

## 原生取消

1. 手动或经已授权的 computer-use 工具启动 `$cancel\MuxRay-io-acceptance.exe`；点击“打开 FLV”，通过原生文件选择器选择 `$cancel\native-controlled.flv`。
2. 等待 `entered.json`，确认界面仍为扫描中。可用下面的 Status 查看 gate 收据。
3. 运行 MarkCancel 后，立即在真实窗口点击“取消扫描”。等待并截图确认“正在取消”，此时不要释放 gate。

```powershell
& "$scripts\control.ps1" -RunDirectory $cancel -Action Status
& "$scripts\control.ps1" -RunDirectory $cancel -Action MarkCancel
# 现在点击真实窗口的“取消扫描”，观察“正在取消”。
& "$scripts\control.ps1" -RunDirectory $cancel -Action Release
```

4. 等待并截图确认 `已取消`、10 Tag 的部分索引、无结构诊断；取消标记会使结果不完整。核对页中 ID 0–9，之后运行：

```powershell
& "$scripts\check.ps1" -RunDirectory $cancel -Scenario Cancel -ObservedState canceled -ObservedTagCount 10
```

`check.ps1` 校验 run、二进制、fixture、overlay 身份，收据同进程且顺序正确、source Close 成功，以及 fixture 可独占打开。`ObservedState/ObservedTagCount` 是操作者实际读到的 UI 结果，**不是脚本自动测到的状态**；请保留对应截图。MarkCancel 仅记录操作意图，不证明按钮确实被点击。最后通过真实窗口关闭文件及程序。

## 阻塞中退出

1. 启动 `$exitRun\MuxRay-io-acceptance.exe`，选择该 run 的 fixture，等待 gate 与扫描中界面。
2. 在一个独立的 PowerShell 进程执行 watcher（后台启动脚本时使用隐藏窗口）。watcher 先校验 exe 路径和进程开始时间、持有句柄，然后写 `exit-watch-armed.json`；它只观察，不发送关闭或终止进程。

```powershell
& "$scripts\watch-exit.ps1" -RunDirectory $exitRun -TimeoutSeconds 60
```

3. 确认 `exit-watch-armed.json` 已出现，在另一个 PowerShell 执行 MarkExit，随即通过真实窗口的关闭按钮或 Alt+F4 退出。**不要 Release。**

```powershell
& "$scripts\control.ps1" -RunDirectory $exitRun -Action MarkExit
# 随即操作真实窗口退出，等待 watcher 返回。
& "$scripts\check.ps1" -RunDirectory $exitRun -Scenario Exit
```

`check.ps1` 要求退出码 0、没有 release/released/closed 收据、原文件可独占打开，默认记录操作意图到 OS 退出不超过 6000 ms。产品阻塞退出预算是 3 秒；这里的 6 秒检查上界还包含操作者或 computer-use 输入分发时间，不能解读为精确宿主耗时。可以用 `-MaxExitMilliseconds` 显式选择 1000–15000 ms 的观测上界，必须在报告中记录该值，不能为掩盖失败而改变历史记录。若需要准确描述按键到退出的区间，应另外保存紧接实际按键前的 UTC 时间并与 `exit-observed.json` 比较。

gate 仍阻塞时没有 orderly source Close；进程退出后由 OS 回收句柄。它与释放 gate 后正常 Close 是两个不同验收结论。超时的 watcher 会留下失败收据并保留进程供调查，不自行 kill。

## 证据和失败

`build-record.json` 保存 run ID、源文件/前端输入和产物 SHA-256、工具模板身份、版本与 overlay 参数。控制及检查入口重新核对产物；收据绑定 run ID、PID 与每次进程生成的 token。JSON 先在同目录写完再原子发布，拒绝覆盖已有文件，读者不会看到半条收据。

保留整个 run 和截图，尤其是 `entered.json`、操作意图、`released.json`/`closed.json` 或 `exit-observed.json`，以及最终 `acceptance.json`。`verification.json` 只表示非 GUI 测试通过。构建或校验失败会保留已有产物；没有成功记录的 run 不算通过，请修复原因后使用新目录。工具不会加入交互 CI，也不修改私有样本、正常生产 exe 或既有 ADR。
