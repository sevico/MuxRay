# 完整普通 H.264 头与 MP3 全帧汇总验收

日期：2026-10-12。功能提交 `c084c0b`；基本验收样本修正提交 `21aa9a8`。
本记录区分产品、独立命令行对照和原生桌面结果；生成媒体不作为业务录制样本。

## 交付范围

- `detail --schema-version 4`：AVC 普通 NAL 1/5 的完整 slice 头，包括参考列表修改、加权预测、参考标记/MMCO、CABAC/QP/QS、去块滤波与条件 FMO 字段。PPS 支持有界 FMO 0–6、可选 transform/scaling-list、第二色度 QP offset 与 trailing bits。
- `summary --schema-version 3`：跨已索引 Tag 顺序汇总全部可确认的 MP3 帧边界、样本数、精确分数时长、参数/码率变化及未确认字节。统计预算与事件保留预算独立；同 Tag 后续帧也参与统计。
- 桌面默认接入上述深度，完整性与省略分别显示；MP3 事件分页和 H.264 源字段可定位到 Hex，并随既有诊断快照 v3 导出。旧 CLI 详情 v1–v3、汇总 v1/v2 合同保留。

精确语义与资源限制见 [ADR-029](decisions/029-full-codec-headers-and-frame-summary.md)。完整头部不代表图像可解码；MP3 汇总不验证音频载荷、CRC 或 reservoir。slice 数据、DPB 重建、SEI 扩展和扩展 NAL 解码不在此次范围。

## 固定构建

从功能提交建立独立干净检出：
`/home/swk/.codex/worktrees/muxray-full-codec-acceptance/MuxRay`。
通过 `desktop/build-production.sh` 构建生产桌面，通过 `go build -trimpath` 构建 CLI；
构建后 Git 状态为空，Wails bindings、Go 模块及依赖锁文件没有差异。

| 产物 | SHA-256 |
| --- | --- |
| Linux `MuxRay` | `0e8c1fe05bf6f0a364cd6a05bd9c48d250cb0b8baf855ccdca6c95572a889bc0` |
| CLI `muxray` | `e5c0f7860daf04648ccd5d95e57714aac193e580b683dc91d923cb089a983fe6` |
| 独立样本 expectations | `c7b1ee8ca009d75b22006b8cfa81ec5d0bbede2c7621176c8e8c852daeb1fb5d` |

样本目录为 `.cache/full-codec-samples-20261012-c`；构建身份、日志哈希和 CLI 结果索引
保存在 `.cache/full-codec-final-evidence/build-and-cli.json`。基本回归采用后续 `21aa9a8`
中的测试样本修正，产品二进制始终为上表同一文件；该提交只修改两个 Python 测试文件。

## 自动检查与独立对照

干净检出完成以下检查：

- 根 Go 模块全部 race 测试及 vet；桌面 Go 模块 WebKitGTK 4.1 race 测试及 vet。
- 前端 typecheck/生产构建及 180 项测试。
- 80 项 codec 工具测试；142 项原生辅助工具测试，其中 4 项既有平台相关跳过。
- 全部公开 JSON Schema、登记 golden 和负向合同检查。

`verify_full_codec.py` 使用独立 FFmpeg trace 和作者制样时保存的原始位期望，验证
14 个 H.264 正向场景、2 个反例、72 个 slice、14 个 PPS、2,148 个字段值与源位映射；
另验证 12 轮 MP3 CLI 汇总、347 条事件，覆盖同 Tag 后续变化、混合采样率、截断、
free-format、非法头、统计预算与保留上限。结果位于 `.cache/fc-cli-final/result.json`。

旧版对照也使用同一 CLI：v1/v2 五场景和默认等于 v1 通过；v3 比较 60 个 slice、
315 个字段映射及 3 个负例通过。分别见 `.cache/fc-cli-legacy-final/result.json` 与
`.cache/fc-cli-prefix-final/result.json`。这些 CLI 结果不替代原生桌面操作。

开发期定向 fuzz：H.264 完整头部 188,405 次、MP3 全帧汇总 26,085 次执行通过；
这不是穷尽所有码流的证明。回归入口及 CI job 已提交，远端 CI 本次未运行。

## 原生验收

最终 **23 轮全部通过**，均使用上表固定生产二进制：

| 分组 | 轮数 | 核对内容 | 本地结果 |
| --- | ---: | --- | --- |
| 完整 H.264 | 14 | PPS scaling、FMO 0–6、I/P/B/SP/SI 与 FMO change cycle；479 个桌面字段值及源位、实际尾字段→Hex | `.cache/fn-h264-final-a/result.json` |
| MP3 全帧汇总 | 1 | 300 帧、独立保留 256 事件、100/100/56 三页；frame 255→byte 106423 | `.cache/fn-mp3-final/result.json` |
| 既有基本 codec | 6 | 基本流程、H.264 VUI、Xing、Info、截断、CRC | `.cache/fn-base-final-b/result.json` |
| 既有 PPS/B slice | 2 | PPS prefix、B 帧 multi-slice 与原始参数集引用 | `.cache/fn-v3-final-a/result.json` |

H.264 每轮三份快照，共 42 份，完成独立再次读取；全部 14 个程序退出码为 0，
源文件句柄释放，按 PID 与启动时间检查所有本轮进程均已消失。
复核见 `.cache/fn-h264-final-a-recheck.json`。
MP3 五份快照独立再次核对，见 `.cache/fn-mp3-final/independent-recheck.json`。
该 MP3 样本的精确累计时长为 `47987/6125` 秒，参数变化 4 次，其中保留上限之后的变化
仍进入统计。全帧汇总没有受详情查看器的 64 帧上限影响。

23 轮均为程序正常退出，无产品进程强杀。H.264 B-slice 和既有 PPS 各记录一次
私有测试环境的 `SIGTERM` 清理，目标不属于产品进程树；不能将此描述为“全套零清理信号”。
最终身份汇总见 `.cache/full-codec-final-evidence/native-final.json`。

本机两个系统辅助功能开关在验收前后均为 `false`，未改变用户配置。
已将固定二进制复制回 `desktop/build/bin/MuxRay`，CLI 位于 `.cache/muxray-full-codec`，
复制后 SHA-256 与固定构建一致。

所有原生轮在私有 Xvfb/D-Bus 中运行，使用仅内存 GSettings。实际执行 GTK 文件选择、
原生保存、前端按钮、源位置跳转、导出后独立 Schema/源字节核对、文件句柄释放和正常退出。
不直接调用宿主 API 替代界面操作。记录程序、源文件、快照及脚本哈希；失败记录保留。

Linux 结果不代表 Windows/macOS 原生验收通过；Windows 辅助选择器原生取消/退出的问题
仍需 Windows 环境处理。

## 复核发现与保留的失败证据

- 同 ID SPS 更新后，旧 PPS 的 FMO/色彩/位深验证结果可能被复用。已使相关 full PPS 失效，
  同声明重复出现保持可用；旧 prefix 路径保持兼容。原失败字节已加入独立回归。
- 已知 SPS 为 8-bit 时，PPS 的 QP 下界此前仍使用最宽范围。已补充 SPS 位深约束，
  并将反例固化到 generator/checker。
- 第一次 MP3 原生试运行等待了 WebKit 不暴露的分页纯文本，保留在 `.cache/fn-mp3-trial-a`。
  辅助工具改为检查真实可操作事件按钮的精确 ID 序列；导出页的起点、总数和每条事件仍独立核对。
- 旧基本回归的结构 IDR 样本在新解析深度下缺少匹配的 QP/deblock 尾部。
  `.cache/fn-base-final-a` 保留失败；`21aa9a8` 修正等长 NAL 为 `65 b8 4a 80`，
  独立 CLI v4 确认完整头部，其他 Tag 字节保持原样，严格 completeness 断言没有放宽。
- 可选浏览器 smoke 未运行成功：本机没有监听 `127.0.0.1:3456` 的 CDP proxy。
  前端自动测试和上述真实 GTK/WebKit 验收单独报告，不把这一项计入通过。
