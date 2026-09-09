# MuxRay 最小桌面版

Wails **v2.14.0** + React **18.3.1** + TypeScript **5.9.3**，构建工具 Vite
**7.3.6**。这是本地开发预览，不是已完成清权、签名或安装器验收的发行版。
架构边界见 [ADR-011](../docs/decisions/011-minimal-wails-shell.md)。

## 范围

- 原生 FLV 文件选择；取消选择不创建会话。
- 单文件后台扫描、200 ms 串行状态轮询、取消、关闭和宿主退出清理。
- 扫描结束后查看完整或部分 Tag/结构诊断，每页最多 100 条，不缓存历史页。
- 偏移、DTS、ID 和总数保留十进制字符串；分页和比例计算使用 BigInt。
- 文件只读，不上传媒体；界面只显示文件名，不存储历史路径。

“扫描结束”不是“媒体语义全部正常”。不完整索引明确显示部分结果，结构
诊断与打开/读取/通信失败分开。仍使用核心的百万 Tag 上限和诊断预算。
详情树、hex、summary、曲线、导出、拖放、多文件、边扫描边分页尚未接入。

关闭或取消的响应丢失时，界面先查询当前会话确认实际结果，不自动重发
关闭/取消请求。确认前暂停新的分页读取；若状态查询也失败，则停止自动
查询并保留“重试查询”入口。手动重试会先刷新状态，包括已结束的会话。

打开响应失败或 5 秒内未返回时，界面按原请求 ID 确认打开结果，不重新弹出
选择器。选择器仍打开则继续等待；已接纳的扫描恢复为当前会话。确认调用
也有 5 秒超时，失败后可“重试查询”。宿主只保留最新打开请求的结果，并
封存尚未送达的请求，避免迟到的打开命令创建无人管理的会话。

## Windows 构建

前提：根模块要求的 Go 版本、Node 22.18+（本机使用 24.4.1）、npm、
Wails CLI v2.14.0，以及已安装的 WebView2 Runtime。不要用 `@latest`
悄悄改变已审定的 Wails 版本。其他系统的桌面壳尚未验收。

```powershell
# 如尚未安装 CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0

# 从仓库根目录进入独立模块
cd desktop
wails build -webview2 error -nopackage -trimpath
.\build\bin\MuxRay.exe
```

构建会生成绑定，按 lockfile 安装前端依赖并嵌入生产 assets。
`-webview2 error` 不自动下载/安装 WebView2；缺少运行时应由用户安装。
`-nopackage` 不生成安装包，也不附带第三方媒体或原版图标。
不要直接用无 build tags 的 `go run .` 代替 Wails 的生产构建。

## 检查与开发

```powershell
# 在 desktop/ 下
wails generate module
npm --prefix frontend ci --no-audit --no-fund
npm --prefix frontend test
npm --prefix frontend run build
go test -count=1 ./...
go vet ./...
# 已配置 GCC / CGO 的 Windows 开发机
go test -race -count=1 ./...
```

提交 `go.sum`、`package-lock.json` 和 `frontend/wailsjs/go/` 的真实生成物；
不要手改生成代码。修改 Go DTO 后重新生成并检查差异。Wails 的指针 TS
声明使用可选字段，`src/normalize.ts` 将实际 JSON 的 null 和声明中的
undefined 统一为显式 null，并保持结构类型检查。

根目录仍可独立运行 `go test ./...`、`go vet ./...` 和 CGO-disabled build；
Go 的 `./...` 不跨越嵌套模块。没有 `go.work`，没有根模块 Wails 依赖。
前端控制器单测只用 Node 内置 test runner，不需启动浏览器或 Wails。
生产资产不提交；`bindings` 专用编译文件使全新 checkout 能先生成绑定，
正常宿主 tests/build 则必须先构建前端，避免把占位页面打进程序。

前期自动检查和浏览器模拟检查见
[验证记录](../docs/desktop-smoke-20260906.md)。打开恢复协议和真实 Windows
窗口验收的最新结果见 [2026-09-10 验收记录](../docs/desktop-native-20260910.md)。
取消/关闭的通信恢复、组合清理错误修复及最新构建结果见
[2026-09-09 回归记录](../docs/desktop-recovery-20260909.md)。

配置保留 `wails dev` 开发入口（热更新循环尚未验收；不作为生产验收路径）。Vite 只
监听回环地址。生产版本不启动 Vite，不加载远程内容，不放开绑定来源，
使用 CSP 并禁用默认文件拖入；不加入任意 URL 打开或文件写入接口。

关闭文件是立即的逻辑关闭；若底层 I/O 未结束，仍占用槽位。关闭窗口会
先取消扫描，最多等待 3 秒清理，超时后由进程退出和 OS 回收资源。这不
改变核心无法强制中断任意阻塞 ReaderAt 的事实。WebView2 自身可能维护
OS 管理的浏览器 profile；应用不把它用作扫描数据库。

## 依赖与发布边界

Vite 7.3.6 替换了本机旧缓存版本，参考维护者的
[Windows 路径修复公告](https://github.com/vitejs/vite/security/advisories/GHSA-fx2h-pf6j-xcff)。
2026-09-06 的 `npm audit` 返回 0 项已知问题，不代表完整供应链安全审计。
Wails API/构建选项核对使用本机 v2.14.0 源码以及
[官方 CLI 文档](https://wails.io/docs/reference/cli/)；官网可能展示更高版本，
不能据此自动升级本项目。公开发布前仍须审核 Go/npm/原生组件许可证与
安全公告、确定项目许可证、完成签名和安装流程验证。
