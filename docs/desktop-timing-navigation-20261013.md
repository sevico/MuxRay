# MP3 帧号直达与 SEI 时序解析验收

日期：2026-10-13。功能与回归工具提交 `6cb0f786265b59162a8230e060b8cc17fb276e33`。
本记录分别列出自动检查、独立结构样本、Linux 原生操作与后台性能测量。
生成的结构样本用于验证字段和边界，不作为业务录制或解码质量证据。

## 交付范围

- MP3 支持输入 Tag 内从零开始的原始帧号、精确打开所在页，以及跨页上一帧/下一帧。
  远页每次请求最多推进 16 页，用户显式继续后才推进下一段；保留真实进度。
  非法输入、已确认结束和无法继续的边界分别反馈，不跳过损坏范围猜测帧起点。
- AVC CLI v6 和桌面所选详情解析 SEI `buffering_period` 与 `pic_timing`：
  NAL/VCL HRD 初始延迟、CPB/DPB 延迟、图像结构、完整/部分时钟标记及有符号 `time_offset`。
  字段保留原始位映射；跨防竞争字节的字段分段定位 Hex。
- 新深度的参数回放保存固定大小的 SPS 时序摘要及选择证据，支持跨 Tag 缓冲周期激活，
  以及同包后续可信 slice 为前置 SEI 选择上下文。缺失、冲突、多图像或不明确边界保留未知，
  不猜测最近 SPS。原始 SPS 和选择依据都可定位。
- CLI 默认版本仍为 v1，旧 AVC v1–v5 和 MP3 v1–v3 不增加解析深度；快照仍为 v3。
  既有页、检查点、SEI 读取/消息、参数回放及桌面 512 字段预算不扩大。

完整合同见 [ADR-031](decisions/031-direct-mp3-and-sei-timing.md)。本轮不计算 HRD/DPB
播放时间线、不转换 FLV DTS/PTS，不解码音视频正文；时序上下文诊断表示分析不完整，
不能单独证明媒体损坏。

## 固定生产构建与独立对照

从上述提交建立干净独立检出，使用 Go 1.27.0 和 `desktop/build-production.sh` 构建
Linux GTK 3 / WebKitGTK 4.1 生产桌面；CLI 使用 `go build -trimpath`。
构建后检出状态为空，生成绑定、模块文件与依赖锁文件没有差异。
CLI 构建信息包含对应 revision 和 `vcs.modified=false`。

| 产物 | SHA-256 |
| --- | --- |
| Linux `MuxRay` | `b226d80a63cc5ddab0b1e7ace63c7577da2a90e6fe21f513ca39ed42257d2836` |
| CLI `muxray` | `b01211a85d45e22c78adcf9d169b2380b7605e04c490c69be6626405f59ac9be` |
| 本轮 expectations | `bbcf8988ee9a05217868d6740585419d87b1c581d7508e0fb574922f786c2d34` |
| 旧版对照 CLI | `bee7474a6132edec13dbe57a41abaf495559e1fa5cafe1d6e070877d77eb3e77` |

同一固定 CLI 通过 20 类独立 SEI 时序样本，核对 461 个字段及原始位映射；
对照前次固定 CLI 的 100 组 v1–v5 输出逐字节一致。
样本覆盖单/双 HRD、VCL HRD、无 HRD 时钟、宽字段、跨防竞争字节、
截断、非法值、消息预算、跨 Tag 激活、SPS 替换和同包 SEI/slice 上下文。
结果位于 `.cache/tn-release-cli/result.json`；构建与日志哈希索引位于
`.cache/timing-navigation-final-evidence/build-and-cli.json`。

## 自动回归

- 根 Go 模块全部测试、race、vet，以及桌面模块 WebKitGTK 4.1 race、vet 通过。
- 前端 typecheck、生产构建与 199 项测试通过；另有 25 项 browser fixture 合同检查，
  后者不计作真实浏览器操作。
- codec 工具全套 101 项检查通过，其中 timing 工具 9 项包含值、源映射及遗漏字段负控；
  原生工具全套 146 项中 4 项平台相关跳过，其余通过。
- 全部登记 Schema、golden 和负向合同检查通过。v6 完整性检查拒绝缺 HRD 延迟、
  缺 CPB 条目、时钟个数与 `pic_struct` 不符等虚构完整结果；旧 v5 投影不泄漏新类型。
- 核心/上下文测试覆盖参数替换、解析深度隔离、冷暖一致、多图像歧义、预算和截断；
  定向 SEI timing fuzz 最终执行 63,274 次通过，不据此声称穷尽所有码流。

最终检查日志位于 `.cache/timing-navigation-work/`。

## Linux 原生验收

最终 **8 轮通过**，全部使用上表同一固定生产二进制：

| 场景 | 轮数 | 核对内容 | 本地结果 |
| --- | ---: | --- | --- |
| MP3 帧号直达 | 1 | 输入 2600，三次有界请求，实际进度 1024 → 2048 → 2624，精确选择 frame 2600 | `.cache/tn-release-mp3-direct-rerun/result.json` |
| MP3 边界步进 | 1 | 63 → 64 → 63，跨页后保持原始帧号、所选 side-info 和来源 | `.cache/tn-release-mp3-boundary/result.json` |
| SEI 时序与来源 | 3 | 双 HRD 延迟、跨 Tag 缓冲周期来源、跨防竞争字节的有符号偏移；实际点击来源并核对 Hex | `.cache/tn-release-sei/result.json` |
| SEI 不完整/预算 | 3 | 缺上下文、截断、消息预算，不补造缺失字段 | `.cache/tn-release-sei/result.json` |

MP3 直达的每次推进分别为 16、16、9 页；两次准备态后才显示目标帧详情。
frame 2600 位于 byte 1,084,225，范围为 3,336 bit。两轮合计 8 份快照，
完成 372 次 side-info 映射比较；准备态快照不虚构目标帧语法。
SEI 六轮的 12 份快照完成 272 次时序字段比较、744 个源范围边界检查。
独立复核重新读取保存文件，校验 Schema、保存收据、源文件哈希、字段原始位和实际 Hex，
包含三次有哈希收据的 SEI 字段来源点击。
两组逐份复核结果分别保存在 `.cache/tn-release-mp3-recheck.json` 和
`.cache/tn-release-sei/independent-recheck.json`；MP3 另核对了 1,284 个源范围边界。
另一份八轮联合审计保存于 `.cache/tn-release-independent-recheck.json`：20 份快照、
25 份 UI、644 次字段值/映射/原始位串比较、2,028 个源范围边界全部通过。
该审计还按保存的真实 UI 核对跨页后的 64 项原始帧号和当前展开帧，并将首次失败单列排除。

八个应用均正常退出、释放源句柄且没有剩余所属进程，源文件哈希不变。
直达复跑与跨防竞争字节轮各有一次验收环境 `SIGTERM` 清理：两个目标创建均早于各自应用，
且不在已观测应用集合。没有命令身份记录，不能断言具体辅助进程名称，也不将整套称为
“零清理信号”。结果索引为 `.cache/timing-navigation-final-evidence/native-final.json`。
两次清理的时间与进程身份边界见 `.cache/tn-release-cleanup-boundaries.json`。

验收前后系统“辅助技术”和“屏幕阅读器启用”均为 `false`。
已将固定二进制复制回 `desktop/build/bin/MuxRay`，复制后 SHA-256 与上表相同；
CLI 位于 `.cache/timing-navigation-release-cli`。后续验收文档变更不影响产品构建。

## 后台性能基线

独立干净检出在原生操作启动前执行 `performance_baseline.py`，每组 3 次、每次目标
200 ms，以下为中位数。记录的环境为 Linux x86-64、Intel Core i7-13700H、Go 1.27.0；
原始环境、三次测量、命令和输出哈希保存在
`.cache/timing-navigation-release-performance/result.json`。这些数值是本机基线，不是跨机器门槛。

| 场景 | 耗时/次 | 源读取/次 | 读取次数 | 分配字节/次 | 分配次数 |
| --- | ---: | ---: | ---: | ---: | ---: |
| MP3 到 frame 2600，文档检查点冷 | 46.97 ms | 94,800 B | 5,455 | 20,927,342 B | 360,786 |
| MP3 到 frame 2600，文档检查点暖 | 2.07 ms | 2,312 B | 133 | 844,750 B | 12,213 |
| SEI 时序含 SPS/HRD 配置解析 | 32.21 μs | 78 B | 16 | 24,240 B | 378 |
| SEI 时序已有参数上下文 | 15.39 μs | 30 B | 3 | 6,912 B | 130 |
| 紧凑时序上下文复制 | 2.94 μs | 0 B | 0 | 5,072 B | 14 |

MP3 冷路径为 3 次请求、41 页，暖路径为 1 次请求、1 页；runner 校验固定读取量和页/请求数，
防止通过漏做工作得到更快结果。这里的冷仅指文档检查点为空，没有清空操作系统页缓存。
样本为已打开的本地文件和已完成的索引，不包含创建、扫描、打开、依赖解析、传输或 UI。
SEI 使用内存 ReaderAt，包含配置的路径和已有上下文的路径分开记录。
`B/op`、`allocs/op` 是累计分配，不是 RSS 或峰值存活内存；原生自动化的耗时也不是解析延迟。

## 回归入口与平台边界

独立造样、CLI 对照、原生操作、性能采集命令见
[回归工具说明](../tools/codec-regression/README.md)。新增 Linux CI job 使用同样的入口，
对照基线固定为功能之前的 `0e853bebd90f12658c1123f8703fb247cbf61530`；本轮未运行远端 CI。

Linux 原生入口使用独立 Xvfb/D-Bus、仅内存 GSettings。Linux 结果不代表 Windows/macOS
原生验收通过，Windows 辅助选择器的取消/退出问题仍需 Windows 环境完成原生验证。

## 开发期审查记录

独立复核促成三类修正：同 SPS 不同图像不能混用 picture-timing 上下文；v6 Schema
必须约束完整结果的条件字段及数组长度；v5 投影必须隐藏 v6 新枚举。
这些反例已纳入正式测试或负向合同，并在固定功能提交前通过。
造样时还纠正了 CPB 大小顺序及 NAL/VCL 初始延迟宽度；正式 expectations 的哈希如上。
早期性能采集与并行测试重叠，仅用于检验工具入口，未用于本节数值。

首次 MP3 直达轮在 280.23 秒时因收尾的“关闭文件”AT-SPI action 未收到回复而失败。
虽然已完成三阶段和 frame 2600 字段/来源导出，缺少完整关闭、FD 释放及正常退出证据，
因此不计入上述八轮。失败目录 `.cache/tn-release-mp3` 完整保留。
失败时的可访问性树可能不完整，不能用于推断 DOM 或关闭 RPC 执行阶段；
随后出现的信号退出日志也不能作为先因。
同一二进制和原始工具在独立目录无改动复跑通过（98.25 秒），边界轮通过（149.55 秒）。
这次未复现不足以确定原失败属于应用、WebKit 或自动化通信；没有放宽断言，
也没有在失败会话内重试可能已执行的动作。
