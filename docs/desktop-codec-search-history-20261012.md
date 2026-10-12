# H.264 批量证据检索与 MP3 跨 Tag 来源定位验收

日期：2026-10-12。平台：Linux amd64，GTK 3.24.41、WebKitGTK 2.52.6
（webkit2gtk-4.1），Go 1.27.0、Node 22.22.2、Wails 2.14.0。

功能实现与回归工具提交：`441d2b1745b5f28af631395af478417ea472cbd1`。
范围、预算、请求生命周期及兼容性约定见
[ADR-027](decisions/027-bounded-codec-search-and-mp3-sources.md)。

## 产物身份

使用该提交的独立干净 worktree 构建，构建后工作树仍干净，生成绑定与提交一致。
九轮使用固定二进制副本，不在验收期间覆盖产物。

| 产物 | 本地路径 | SHA-256 |
| --- | --- | --- |
| 桌面程序 | `.cache/sh-bin/MuxRay` | `67da3b84416a2eedf05e4406e7fa0bd0785f2d86e71237301d95a021a5b58286` |
| CLI | `.cache/sh-bin/muxray` | `4c6b7cd5a0ef98f430cba7762dff810e832cb1a1fd0fec0fc44e44f203d9174e` |

`desktop/build/bin/MuxRay` 已更新为同一桌面产物。干净构建记录为
`.cache/search-history-regression/clean-build.json`，验收总记录为
`.cache/search-history-regression/acceptance.json`。这些本机证据目录不提交到 Git；
重新生成及执行命令见 [桌面说明](../desktop/README.md)。

## 回归检查

以下 Go 检查均设置 `GOTOOLCHAIN=go1.27.0`。

- 根模块 `go test -race ./...`、`go vet ./...` 通过；桌面模块
  `go test -race -tags webkit2_41 ./...`、对应 `go vet` 通过。
- 前端 176 项测试、TypeScript 检查、Linux 生产构建通过。
- 编解码器回归工具 53 项测试通过；桌面原生工具测试 127 项通过、4 项因平台跳过。
- `tools/validate_contracts.py` 通过；CLI v1/v2/v3 和诊断快照 v1/v2 合同保持兼容。
- 干净构建 CLI 对照已有样本：H.264 核对 315 个源映射字段、60 个 slice 头，
  包括 I/P/B、20 个多 slice Tag 和 3 类负例；MP3 核对 13 类样本的 6,925 个
  源映射字段，各例默认输出与显式 v1 一致。

Go/前端测试额外覆盖批次游标、结果上限、短 AVC 包头配置屏障、部分证据不能
推导负向结论、过期/丢失回复、取消/超时/关闭/源变化，以及 MP3 的参数/CRC/
side-info 中断、每 Tag 64/65 帧边界、来源上限和不可选帧。执行日志和哈希保存于
`.cache/search-history-regression/checks/`；CLI 对照结果位于同目录下的
`clean-cli-v3/`、`clean-cli-side/`。

## 独立样本

由 FFmpeg/libx264/libmp3lame 生成编码流，再作明确的封装及结构变异。
这些是可复现测试样本，不是业务录制。`.cache/sh-samples-final/expectations.json`
保存原始字节 oracle；生成器不读取产品结果作为期望值。重复生成的 14 个媒体
文件及期望文件哈希完全一致，记录在
`.cache/search-history-regression/repeated-generation-final.json`。

同一 27 帧 MP3 分别封装为单 Tag、多 Tag 和音视频交错，逐帧回指窗口哈希一致。
选中的流帧 26 回指 338 byte，覆盖两个前序物理槽位。H.264 原样本自然包含
非 IDR 的 I slice（Tag 12、22），另有声明变异、未知 NAL、截断、部分 IDR
证据、缺配置及 792 Tag 分页样本。五个正常封装样本另经 FFmpeg 严格解码通过；
变异样本用于检查未知/边界处理，不视为有效码流。

## 九轮原生结果

每轮使用独立 D-Bus、Xvfb 和应用配置目录，`GSETTINGS_BACKEND=memory`。
通过真实 GTK/WebKitGTK/Wails 界面操作，比较实际保存的 JSON、原始文件位字段、
Hex 窗口及高亮范围。MP3 五轮与 H.264 四轮分两组并行，组内顺序执行。

| 场景 | 结果 | 秒 | 验收要点 |
| --- | --- | ---: | --- |
| MP3 合并 Tag | PASS | 103.45 | 帧 26；338 byte 回指；两个前序槽位与当前槽位 |
| MP3 拆分 Tag | PASS | 77.31 | 回溯到正确来源 Tag/帧；窗口与合并样本一致 |
| MP3 音视频交错 | PASS | 100.40 | 视频不打断音频物理来源；窗口仍一致 |
| MP3 音频编码中断 | PASS | 62.38 | 不跨越 AAC Tag；unknown，缺少 10 byte |
| MP3 缺少历史前缀 | PASS | 58.61 | unknown，缺少 10 byte；只保留当前槽位 |
| H.264 分页差异检索 | PASS | 88.78 | 792 Tag、4 批、96 条命中；上一批及来源跳转 |
| H.264 未知证据 | PASS | 44.17 | 1 页、1 条未知记录；源坐标正确 |
| H.264 部分 IDR 证据 | PASS | 52.86 | 全部筛选 1 页、30 条；部分覆盖保留正向 IDR |
| H.264 缺少配置 | PASS | 53.48 | 1 页、30 条未知；不猜测 NAL 长度 |

分页差异检索的实际范围是 `[0,256)`、`[256,512)`、`[512,768)`、
`[768,792)`，分别命中 32、31、31、2 条，读取 1,901、1,908、1,908、190 byte。
累计 7 页、157 条结果的关系、IDR、DTS 和精确源位置均对照独立 oracle；
另核对上一批重访。完成 4 次 H.264 来源 Hex 点击及 Tag 详情导出，11 次 MP3
来源 Hex 点击及 5 次来源帧跳转。返回 MP3 目标帧后重新查询与导出仍一致。

所有九轮源文件哈希保持不变，关闭文件后源 FD 已释放，应用均正常退出。
拆分 MP3、缺配置 H.264 两轮清理了遗留测试辅助进程（SIGTERM），没有强制
终止应用。验收后全局 `toolkit-accessibility`、`screen-reader-enabled` 均为 false。

完整逐轮证据位于 `.cache/sh-final/mp3/` 和 `.cache/sh-final/avc/`。
汇总时再次读取实际快照和界面证据，重新运行语义校验并核对产物/源哈希。
本轮没有在原生 GUI 中强制制造取消竞争；取消和丢回复恢复的结论来自 Go/前端测试。

## 保留的失败与边界

保留初次样本生成未满足多前帧窗口的目录，以及原生工具适配失败目录：
`.cache/search-history-regression/samples*`、`native-trial*`、
`.cache/sh-trial3`、`.cache/sh-trial4`、`.cache/sh-avc-trial`。
适配问题包括 WebKitGTK select 不提供 AT-SPI Selection 接口，以及 aria-label
覆盖按钮可见偏移文本。前者改为键盘选择并等待真实状态；后者在产品来源按钮
增加坐标说明，由 AT-SPI description 逐行核验，没有删减断言。

H.264 检索仅分析 NAL 头；MP3 来源仅说明物理主数据槽位。两者都不证明完整
图像或音频可解码。Windows/macOS 原生行为及既有 Windows 辅助选择器问题未由
本轮 Linux 验收确认。
