# MuxRay 最小桌面版

Wails **v2.14.0** + React **18.3.1** + TypeScript **5.9.3**，构建工具 Vite
**7.3.6**。这是本地开发预览，不是已完成清权、签名或安装器验收的发行版。
架构边界见 [ADR-011](../docs/decisions/011-minimal-wails-shell.md)。
打开响应恢复协议见 [ADR-012](../docs/decisions/012-recoverable-native-open.md)。
文件所有权和有界详情协议见 [ADR-013](../docs/decisions/013-retained-document-details.md)。
AVC 上下文查找和准确源位置见 [ADR-014](../docs/decisions/014-bounded-desktop-avc-context.md)。

## 范围

- 原生 FLV 文件选择；取消选择不创建会话。
- 单文件后台扫描、200 ms 串行状态轮询、取消、关闭和宿主退出清理。
- 扫描结束后查看完整或部分 Tag/结构诊断，每页最多 100 条，不缓存历史页。
- 选择完整的 Tag 查看 AMF0 字段树、AAC 音频头、AudioSpecificConfig 或 raw 包范围，保留原始 byte/bit 位置和语法诊断。
- AVC Video 包头、有符号 CTS 与派生 PTS、avcC/SPS/PPS 基础摘要、NAL 长度及头字段，并说明所用前置配置的来源。
- 偏移、DTS、Tag ID 和总数保留十进制字符串；分页和比例计算使用 BigInt。
- 文件只读，不上传媒体；界面只显示文件名，不存储历史路径。

“扫描结束”不是“媒体语义全部正常”。不完整索引明确显示部分结果，结构
诊断与打开/读取/通信失败分开。仍使用核心的百万 Tag 上限和诊断预算。
MP3 桌面详情、hex、summary、曲线、导出、拖放、多文件、边扫描边分页尚未接入。

已接纳的扫描结束或取消后，桌面文档保留同一只读源句柄，直到关闭文件或窗口。
在 opening 阶段取消会取消打开过程；若文件已经打开但文档尚未发布，迟到的
文档会关闭并清理，不保留详情。界面会收到关闭状态，清理结束后才可重新打开。
完整的已提交前缀 Tag 可以解析；截断 payload、filtered Tag 和不支持的
编码会说明原因。AMF0/AAC/AVC 语法错误属于所选详情，不影响其他 Tag。
详情每次最多显示 512 字段、64 诊断和每段 256 字节 UTF-8 预览，实际
详情 JSON 不超过 256 KiB；预算或预览省略会明确显示。AAC raw 不进行音频解码。
解析预算与显示预算独立：AMF0 最多 32 层、4,096 节点、每个容器 4,096 项，
单段及累计保留字符串均不超过 64 KiB；AMF0/AAC 各最多保留 64 条解析诊断。

AVC NALU 使用同一文档的已提交索引反向查找最近可用 sequence header，明确显示
`found/missing/budget_exhausted/not_needed`、来源 Tag、原始长度声明位置与搜索计数。
每次最多查找 8,192 个前置记录、32 个候选配置和累计 1 MiB 的源读取（含所选包探测）；
查找与所选详情共享同一任务和协作截止。缺配置或达到查找预算时保留包头与 opaque
范围，不猜四字节 NAL 长度，也不提供手动覆盖或跨请求配置缓存。不可用的候选可跳过，
但更深 SPS 语义不完整本身不会否定已经完整的长度声明。

AVC 解析预算为 64 条诊断、64 个 avcC 参数集条目、256 个 NAL、每个参数集 EBSP
正文 64 KiB（不含 NAL 头），以及每次 4,096 个 RBSP 映射项。
包内 SPS/PPS 数量受 NAL 上限约束，全部结果仍独立受字段/JSON 展示预算限制。
FLV/avcC/NAL 的连续原始范围保留精确位置；DTS/PTS、尺寸和 SPS/PPS 摘要等派生字段
显示“—”，不会把去除防竞争字节后跨段的 RBSP 字段伪装成连续原始 Span。
NAL 只展开边界与头信息，不解码 slice 或视频画面；FLV keyframe 声明也不证明 IDR。

详情读取前后检查原句柄的文件身份、大小和修改时间，发现改变后要求关闭重开。
这不是不可变快照，不能识别所有同大小、同时间戳的原位修改。快速切换仅保留
最新选择，至多一个实际解析任务；前端 5 秒等待上限和后端 3 秒协作截止均
不能强制中断阻塞的系统读取。未结束的读取继续占用资源，关闭不会提前复用槽位。

详情确认每次响应后间隔 200 ms 串行查询；连续收到 25 次 pending 查询响应后
暂停自动确认，保留同一请求 ID 供手动重试，初次 Start 响应不计入这 25 次。
这是查询次数上限，不是详情必须在 5 秒内完成。Start 响应丢失时先查询原请求；
确认失败或超时也会暂停，不自动新建解析。快速切换 Tag 时，旧回复立即失效，
已发出的本地调用仍等到返回或 5 秒截止后再发送最新选择；切页、切换诊断页、
关闭文件或销毁界面则立即清除本地详情等待。只有宿主已确认可重试的解析失败，
用户选择重新读取时才递增请求 ID。

详情协议中的 `sessionId` 是不透明字符串，例如 `scan-1`；请求 ID、Tag ID 和
64 位位置使用规范十进制字符串。`GetDetail` 在尚未定位到当前已接纳请求时
返回前置错误，未知的 `tagId` 为空字符串；已接纳请求的结果保留准确 Tag ID。

关闭、取消、状态和 Tag/诊断分页调用均有 5 秒等待上限。关闭或取消的
响应丢失、报错或超时时，界面先查询当前会话确认实际结果，不自动重发
关闭/取消请求。确认前暂停新的分页读取；若状态查询也失败或超时，则
停止自动查询并保留“重试查询”入口。手动重试会先刷新状态，包括已结束
的会话，再恢复当前分页。迟到响应不能覆盖较新的操作或会话；控制器
销毁时释放本地等待和定时器。等待超时不等于宿主操作已被撤销。

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

受控 I/O 的 Windows 原生取消与阻塞退出验收使用
[可复现工具](../tools/desktop-native/README.md)。工具生成确定性结构样本，
用 Go overlay 构建独立验收程序，并为每次运行保留门控与资源关闭收据。
普通生产构建不包含门控；此工具不依赖私有媒体，也不代替自然慢盘测试。

前期自动检查和浏览器模拟检查见
[验证记录](../docs/desktop-smoke-20260906.md)。打开恢复协议和真实 Windows
窗口验收结果见 [2026-09-10 验收记录](../docs/desktop-native-20260910.md)。
取消/关闭的通信恢复、组合清理错误修复及最新构建结果见
[2026-09-09 回归记录](../docs/desktop-recovery-20260909.md)。
其余 IPC 等待上限和验收工具复现结果见
[2026-09-10 超时与工具记录](../docs/desktop-timeouts-tools-20260910.md)。
AVC 详情、1 GiB 文件尾部读取与阻塞关闭的本次实测见
[AVC 原生验收报告](../docs/desktop-avc-20260910.md)。

ADR-013、保留源文件的详情读取和 AMF0/AAC 完整原生流程见
[2026-09-10 详情验收记录](../docs/desktop-details-20260910.md)。

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
