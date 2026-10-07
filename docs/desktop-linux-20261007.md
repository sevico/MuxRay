# Linux 桌面构建与原生功能验证：2026-10-07

Linux Mint 22.3 / Ubuntu 24.04 基础环境已完成生产构建、自动检查及一轮真实
GTK/WebKit 功能流程。测试使用独立 Xvfb 显示器和 D-Bus 会话中的生产二进制，
通过 AT-SPI 操作实际界面与原生文件对话框，没有注入合成 JS host 或替换 Go 文件读取。
这不是物理显示器、Wayland、长期资源稳定性或 Windows 原型的验收。

## 本次改动与环境

- 新增 `desktop/build-production.sh`，固定 Wails v2.14.0，以 `webkit2_41` 构建。
  默认使用 Go 1.27.0，保留调用者的显式 `GOTOOLCHAIN`，不写全局 Go 配置。
- 该版本 Wails 在 Linux 上生成的五个绑定文件内容与已提交文件一致，但会赋予
  执行权限；脚本构建成功后将生成文件权限统一为 0644，继续保留生成物差异检查。
- `desktop/go.mod` 将原型代码已直接引用的 `golang.org/x/sys` 归入直接依赖；
  版本仍为 v0.46.0，没有升级依赖或修改窗口、解析与请求协议。
- 新增 Ubuntu 24.04 非 GUI CI：生产构建、生成物核对、前端测试、Go race/vet。
  原 Windows job 保持不变。新增远端 CI 尚未在本轮实际触发。

本机使用 Go 1.27.0、Node 22.22.2、GTK 3.24.41、WebKitGTK 2.52.6。
用户安装了 `libgtk-3-dev`、`libwebkit2gtk-4.1-dev` 与 `xvfb`；构建入口不自动安装
系统包。显式工具链版本可避免本机旧 Go 尝试获取裸 `go1.27` 时的失败。
构建和使用命令见 [桌面开发说明](../desktop/README.md#linux-构建)。

## 自动检查

以下检查在本机通过：

- Linux 生产构建，包括最终 `./desktop/build-production.sh` 入口；动态库解析无缺失。
- 根模块 `go test ./...`、`go vet ./...`，均显式使用 Go 1.27.0。
- 桌面模块 `go test -race -tags webkit2_41 -count=1 ./...` 和 `go vet -tags webkit2_41 ./...`。
- 前端类型检查、Vite 生产构建、141 项 TS 单测与 25 项模拟宿主合同测试。
- `go mod tidy -diff` 无差异；绑定内容与 `go.sum` 无差异，绑定文件无执行位。
- Bash 语法、CI YAML 解析与 `git diff --check`。

## 原生功能流程

样本由现有 `summary_fixtures.py` 生成：`summary-mixed.flv`，497 byte、20 Tag，
包含 AAC/AVC/MP3 封装与时间戳边界。这是确定性结构样本，不宣称媒体可播放。
源 SHA-256：`f94ef706dc53f2004d0a38c17037a3b200cbffb32a92ce33c851381e5ef3eda2`。

| 步骤 | 实际结果 |
| --- | --- |
| 打开选择器后取消 | 返回空态，可再次打开；没有创建媒体会话 |
| 重新打开样本 | 完成 497 byte / 20 Tag 扫描，与 CLI 一致 |
| 分析媒体 | 导出中的全部 112 项概览指标与独立 CLI JSON 逐项一致，包括缺失值 |
| 选择 AVC Tag 3 | 显示 AVC Video 详情、`candidate_index` 配置来源及 NAL 字段 |
| Hex 与趋势 | 导出中的 byte 109 起 388 byte 与源完全一致；20 个趋势点通过源校验 |
| GTK 原生保存 | 实际写出 `native.snapshot.json`，21,516 byte；界面显示已保存，Schema 与源证据校验通过 |
| 关闭文件 | 界面回到空态；本进程原有源 FD 39 消失，源大小与 SHA-256 不变 |
| 关闭窗口 | 向任务窗口发送正常 `WM_DELETE_WINDOW`，父进程退出码 0；两名已观察的 WebKit 后代随后均不存在 |

Linux 源释放使用 `/proc/<pid>/fd` 的设备号与 inode 核验，没有套用 Windows 的
独占打开语义。进程消失检查不等于逐个观察了所有短命后代，也不代表无资源泄漏。

## 本地证据与限制

本轮本地证据目录为 `.cache/linux-acceptance.CXdmc5/`（被 Git 忽略）：
`validation.json`、原生 accessibility 树、截图、CLI 输出、源释放与退出记录、
`snapshot-check.json`、`independent-summary-check.json` 及测试辅助脚本。

- 生产二进制 SHA-256：`e978de3ffad5a4471a12432017872408ba22a104dc585c7f71e8b2feffdfba26`。
- 原生快照 SHA-256：`958ac5fddd740db4778ee23ee47075a854e8ad71822802a489fdd6a314bd8381`。
- 父进程 PID 1011821；观察到的 WebKit 子进程 PID 1011914、1011916。

独立显示器中存在 EGL/DRI3、GIO/虚拟文件系统服务警告；窗口与上述流程完成。
本轮不评价硬件加速、物理桌面集成或这些警告在其他环境中的行为。
AT-SPI 对 WebKit 输入框的 EditableText 接口未实现，设置 Hex 偏移 0 的尝试失败；
实际验收的是已显示的偏移 109，未把失败动作计为通过。

准备阶段的辅助探针曾意外写入系统辅助功能设置，已恢复已知原值，用户确认屏幕
阅读器原本关闭并保持关闭。正式 MuxRay 验收改用 `GSETTINGS_BACKEND=memory`
启动独立 D-Bus/Xvfb，会话内的 XDG 数据、配置和缓存目录也指向本轮临时目录。
后续复现应沿用该隔离方式，避免测试工具改变用户配置。

尚未覆盖慢 I/O 扫描取消、大文件、真实录制语料、多轮资源浸泡、Wayland、macOS
及 Linux 分发包。Windows 辅助选择器取消退出与六轮验收仍见
[原型记录](desktop-picker-process-20261007.md)，没有被本次 Linux 结果替代。
