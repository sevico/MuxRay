# Windows 候选发行包

`candidate.py` 将生产构建、第三方通知采集、便携 ZIP、构建信息和 SHA-256 校验文件串成一条命令。工具只生成 **candidate**，不创建 Git tag，不上传 GitHub Release，不覆盖已发布附件。

## 准备与运行

需要 Windows x64、Git、PowerShell、Python 3.11+、仓库指定的 Go 工具链、Node.js 24 和 Wails CLI（当前为 `v2.14.0`）。先安装锁定的前端依赖：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0
Push-Location desktop/frontend
npm ci --no-audit --no-fund
Pop-Location
```

准备并提交 `docs/releases/v<版本>.md`，第一行必须为 `# MuxRay v<版本>`。版本使用不带 `v` 前缀的 SemVer，例如 `0.2.0-dev`。说明文件应描述该版本的实际变更及使用限制；候选工具不推断发布资格。

在**干净 checkout** 的仓库根目录运行，提交号必须是该 checkout 的完整 SHA。输出必须是新目录，仓库内输出只能放在已被忽略的 `.cache/` 下：

```powershell
$releaseCommit = git rev-parse HEAD
python -B tools/release/candidate.py build --version 0.2.0-dev --commit $releaseCommit --output .cache/release-0.2.0-dev-run1
python -B tools/release/candidate.py verify --assets .cache/release-0.2.0-dev-run1/assets
```

生产构建前、构建后和采集后均检查 HEAD 与工作区。未知版本、dirty 工作区、构建修改已跟踪文件、错误平台/tag、旧 EXE 的许可证库存、遗漏通知、越界路径和个人绝对路径都会使命令失败。失败目录保留用于排查；修复后换一个新输出目录，不覆盖证据。

生成绑定目录由 `.gitattributes` 固定为 LF，与 Wails 的输出一致，避免 Windows checkout 使用 CRLF 后产生仅换行转换导致的 dirty 状态。此规则不忽略绑定文件，也不放宽源码检查；失败信息会列出最多 20 项相对路径，方便定位真正的构建改动。

许可证和源文本按 UTF-8 显式读取。Actions 构建步骤将 Python 的默认编码警告提升为错误，使依赖系统区域设置的文本读写立即失败，避免本地 UTF-8 环境掩盖英文 Windows 上的编码错误。

`assets/` 中只有候选 ZIP、`BUILD-INFO.json` 和 `SHA256SUMS.txt`。ZIP 包含桌面 EXE、MIT 许可证、使用说明、该版本说明、构建身份、第三方通知、许可证库存及原始通知副本。不分发 CLI、WebView2 Runtime、安装器或本地媒体。程序需要预装 Microsoft Edge WebView2 Runtime。

其余 `build.log`、`license-audit/`、中间 EXE 和校验收据是本地证据，可能包含机器路径，**不要将整个输出目录上传**。公开附件使用固定白名单；ZIP 会在临时目录实际解压，并逐项核对大小、SHA-256、许可副本及清单引用。

## 许可证采集边界

每次从刚构建的 EXE 执行 `go version -m`，核对 Windows 生产 tag、Go 版本和实际链接模块；重新生成当前 production Go 包图和前端 bundle 模块图。前端 chunk 完整字节必须存在于该 EXE 中，库存记录该 EXE 的 SHA-256，不能把仓库中另一份候选清单当作当前许可证据。

`collect_notices.py` 延续首版实际二进制许可审查，包括 Go 标准库源内通知、Wails vendored 组件、内嵌 JS runtime、WebView2 Go loader、React/scheduler、Vite polyfill 和 Unicode 数据通知。`Unicode-LICENSE.txt` 是首次发行核对并保留的 Unicode License V3 原文，来源为 [Unicode 官方许可](https://www.unicode.org/license.txt)，保留原版权年份。

当前摘录规则已经审查的依赖为 Go `1.27.2`、Wails `v2.14.0`、go-webview2 `v1.0.22`、uniseg `v0.4.7`；升级这些依赖时工具会失败，需复核源内许可与数据版本后更新规则。出现新的前端运行时依赖、未知模块许可、缺少源文件或嵌入内容变化也会失败。此约束有意保留，避免泛化采集器静默漏掉通知。Wails runtime 的许可元数据差异继续保存在库存中供发行者复核，不能把库存视为法律背书。

`license-inventory.json` 是逐文件可核验的归属证据；合并通知必须覆盖每份原文。采集脚本拒绝 `python -O`，入口会清除 `PYTHONOPTIMIZE`，不允许跳过这些核对。

## 验证与发布流程

```powershell
python -B -m unittest discover -s tools/release -p 'test_*.py' -v
```

测试覆盖源身份、dirty/untracked 文件、SemVer、平台/tag、EXE 与许可清单错配、缺少许可、许可被修改、路径越界、私有路径、虚假验证状态、额外 ZIP 文件、校验和以及正常打包/解压。测试只生成临时假 EXE；真实生产候选的成功需要执行上面的完整命令。

`BUILD-INFO.json` 中 CI、原生验收和安全扫描固定记录 `not-run-by-this-tool`。版本字段来自本次构建传入的 linker 参数，实际窗口/导出 JSON 版本仍需原生验收。文件校验说明字节一致，不能替代来源信任或代码签名，也不证明 EXE 没有缺陷。

GitHub Actions 的 **Release candidate** 工作流只支持手动启动，输入版本与完整提交号。它执行同一候选命令并仅上传 `assets/` 为 Actions Artifact，权限仅 `contents: read`。下载后可运行 `verify` 再检查。GitHub Artifact 本身不是公开发行。

正式发行前，发行者应将该 SHA 的 CI、安全扫描及**这个 EXE SHA-256** 的原生验收分别核对，审核版本说明和许可，再显式决定发布。不要把“candidate 构建成功”改写成“全部门禁通过”，也不要用新附件覆盖已发布版本。
