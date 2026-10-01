# 真实语料与长媒体桌面回归：2026-09-30 至 2026-10-01

本轮在 ADR-019 完成后补充生产桌面的媒体回归与资源观测，不改变产品读取、
分析或导出协议。ADR-019 实现及合同说明已提交为 `6357de6`。
10 份样本已完成原生打开、浏览、快照保存与关闭，最终生产进程自然退出。
资源数据来自两个分开的采样窗口，包含一次长时间观察空档，不能视为连续基线。

## 构建与证据身份

从 `6357de6` 的产品源码重新执行 `desktop/build-production.ps1`，
`wails build -webview2 error -nopackage -trimpath` 成功，未生成安装包。
生产程序 `desktop/build/bin/MuxRay.exe` 为 11,408,896 byte，SHA-256：

`6483dd0c53a1565e2e0a00082a8bd36dd2943715b84e6d3ef072b8737ad1b865`

与 ADR-019 最终验收产物逐字节相同。CLI 由同一源码
`go build -trimpath -o .cache/muxray-corpus-20260930.exe ./cmd/muxray` 构建，SHA-256：

`e00744df4fdbaea632c31e89778989d717833c2f7c9ed51fe139812b17825424`

本地证据根目录为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-corpus-20260930`。
其中 `production-build.log`、`original-eight-cli.json`、`external-*` 保留构建、
小语料和新增外部样本检查；`native-001/` 保存本次真实 Windows 原生窗口、
文件选择/保存对话框、导出文件、关闭后的独占打开检查和资源采样。
UI 文本及可用截图随观察 JSON 保留，不能用 CLI 结果替代原生观察；
没有截图的观察只能称为原生可访问性状态核对。

## 语料与独立基准

| 样本 | 大小 / Tag 数 | 应观察到的范围 |
| --- | --- | --- |
| generated-avc-aac | 49,722 B / 141 | AVC 与 AAC；AVC NALU Tag 2 的 CTS 为 80 ms |
| generated-aac | 18,000 B / 96 | AAC-LC 48 kHz 单声道；配置与 raw 包 |
| generated-mp3 | 33,861 B / 78 | MPEG-1 Layer III，44.1 kHz 单声道，首帧声明 128 kbps |
| derived-aac-truncated | 17,998 B / 96 | 最后 PreviousTagSize 只剩 2 byte；最后 payload 完整，扫描不完整 |
| derived-mp3-bad-previous | 33,861 B / 78 | 首个 PreviousTagSize 实际 0、应为 429；结构警告不妨碍完整索引 |
| 11-04-2008 | 11,097,438 B / 11,959 | AMF0、MP3 与未支持的 FLV1；容器完整，Summary 语义覆盖为部分 |
| IFTlhxwwkKE | 2,297,322 B / 11,026 | AMF0 缺对象结束标记、22.05 kHz MP3 与 FLV1；首个 PreviousTagSize 不一致 |
| zelda | 601,526 B / 998 | 未支持的 FLV1 / ADPCM；原始 Hex 仍可读 |
| Enigma_Principles_of_Lust-part | 512,000 B / 608 | 外部 AVC/AAC 截断前缀，末 Tag 607 payload 声明 2,780 B、实际 1,195 B |
| generated-long-avc-aac-2400s | 41,698,926 B / 172,504 | 40 分钟生成编码媒体；跨越关键帧保留上限，尾部索引仍可定位 |

前八项使用已有清单，当前 CLI、Schema、独立 framing、三次确定性输出、分页及
可用详情检查通过。完整扫描样本另做 ffprobe 包/流差分；已有人工审阅的差异
规则保持不变。截断 AAC 的 CLI detail 和 ffprobe 包比较按既有工具边界跳过，
不能称为这些检查也通过；桌面保留前缀详情另由原生流程验证。

新增外部文件来自 [FFmpeg samples 的 FLV 目录](https://samples.ffmpeg.org/FLV/)，
逐个下载的 [原始文件](https://samples.ffmpeg.org/FLV/Enigma_Principles_of_Lust-part.flv)
SHA-256 为 `a0b6d22bd69054d1e7082de11a825e72739e3cdd3409a54349365bffcd3a40fb`。
清单位于 `testdata/manifests/desktop-20260930/`，媒体只留在本地忽略目录。
`corpus.py` 单独选择该目录后全部预期通过：608 Tag、不完整扫描、
`FLV_TAG_BODY_TRUNCATED` 位于 512,000，Summary 明确跳过不完整 Tag。
其 ffprobe 仅独立识别 H.264 Main 426×240、AAC-LC 22,050 Hz 双声道，未做完整
包相等比较。末 Tag DTS 为 11,678 ms；元数据声明的约 210.21 s 属于原文件，
不能作为本截断前缀的实际覆盖时长。CLI 不完整索引拒绝 detail 是既有合同。

四个外部样本的再分发和隐私审查仍未完成；登记身份不等于已获准入库。
本轮没有提交媒体字节，也没有补齐真实 AVC/AAC 长录制这一缺口。

## 长媒体验证

`long_media.py` 使用固定 FFmpeg 配方生成 160×90、25 fps、GOP 50、两个 B 帧的
H.264 和 48 kHz 单声道 AAC，编码线程各为 1。两个新目录的完整生成分别耗时
19.882 s / 19.860 s，大小、独立 framing 和 SHA-256 完全相同：

`b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`

这只证明本机 FFmpeg `7.1.1-full_build-www.gyan.dev` 构建下的两次复现，不能保证
其他版本或构建逐字节相同。生成样本不会算作真实录制。新清单位于
`testdata/manifests/longmedia/`，不自动进入有 32 MiB / 50,000 Tag 限额的小语料工具。

本地 `testdata/private/corpus-long-20260930/` 保存生成计划、argv、工具和样本
哈希、独立 framing、第二次复现检查、ffprobe 及当前 CLI 检查证据：

- 全量 ffprobe 解码计数为 60,000 视频帧、112,501 音频帧，stderr 为空。
- 同桌面五项保留预算的 CLI Summary v2 完整扫描/分析 172,504 Tag，无诊断。
- 关键帧观测 1,200、保留 1,024、省略 176；1,199 个闭合 GOP 均为 2,000 ms。
- 最后保留关键帧 Tag 147056、DTS 2046000；首个未保留关键帧 Tag 147200、
  DTS 2048000、payload offset 35586515，仍存在于原 Tag 索引。
- 最后 AVC NALU 为 Tag 172497、payload offset 41698203；最后 AAC 为
  Tag 172502、payload offset 41698829；Tag 172503 为 AVC EOS。

ffprobe 的格式时长为 2399.960 s，最大原始 DTS 为 2400059 ms，来源分别受尾部
EOS 与 AAC 覆盖影响。它们不应被强行写成相等，也不是播放器同步验收。

## 原生生产程序矩阵

生产进程 PID 15952 顺序执行各样本，使用真实打开和保存对话框。十份导出已
独立复核通过；每份检查 Schema、会话/请求身份、源文件清单与完整哈希、原始
Tag 顺序/头字段、Hex 实际字节、Summary 与独立 framing/CLI 的一致性。
`native-001/checks-review/` 保存可复现检查脚本、精确命令、退出码、逐项结果
及 `01-10-review-matrix.json/md`。标准检查合计 494/494 通过，另有语义/字节
边界复核；第十份显式使用 64 MiB 源校验上限，53 项标准检查全部通过。

Hex 区间采用 `[起点,终点)`，包含窗口内实际原始字节，允许跨越 Tag 边界。
API 上限为 4 KiB；本轮生产界面实际使用固定 512 byte 窗口，EOF 时返回剩余字节。

| 样本 / 导出前缀 | 原生结果 | 已保存 Hex | JSON 大小 |
| --- | --- | --- | --- |
| 01 AVC/AAC | Summary 完整；DTS 0 筛选与 141 点趋势；Tag 2 AVC context found、CTS 80 ms | `[112,624)`，512 B | 52,943 B |
| 02 AAC | Summary 完整；DTS 0 筛选与 96 点趋势；Tag 0 AAC 配置 48 kHz 单声道 | `[24,536)`，512 B | 39,225 B |
| 03 MP3 | Summary 完整；Tag 0 MPEG-1 Layer III / 128 kbps / 44.1 kHz；未请求趋势明确省略 | `[24,536)`，512 B | 16,022 B |
| 04 截断 AAC | 筛选定位 Tag 95、趋势前驱 94；扫描和 Summary 不完整，但所选 AAC raw 区间完整 | `[17852,17998)`，146 B、EOF | 13,112 B |
| 05 错误 PreviousTagSize | 趋势定位 Tag 0；1 个结构警告，扫描与 Summary 仍完整，MP3 详情完整 | `[24,536)`，512 B | 36,275 B |
| 06 11-04-2008 | 11,959 Tag 完整索引；Summary 因 FLV1 部分覆盖；Tag 1 MP3 96 kbps / 44.1 kHz 双声道 | `[222,734)`，512 B | 16,146 B |
| 07 IFT | 11,026 Tag 完整索引、1 个结构警告；Summary 部分覆盖；Tag 0 AMF0 保留字段并报告缺对象结束标记 | `[24,536)`，512 B | 22,824 B |
| 08 zelda | 998 Tag 完整索引、Summary 部分覆盖；Tag 0 FLV1 明确不支持详情，Hex 与保存可用 | `[24,536)`，512 B | 11,662 B |
| 09 外部 AVC/AAC | 完整前缀 Tag 6 AVC context found；Tag 607 截断载荷拒绝详情，趋势保留 1195/2780 实际/声明长度与缺失 trailer；扫描和 Summary 不完整 | `[511829,512000)`，171 B、EOF | 14,077 B |
| 10 长 AVC/AAC | 172,504 Tag、Summary 完整；关键帧 1200/1024/176；可定位未保留关键帧 Tag 147200；尾部 7 点趋势和 Tag 172497 AVC framing，配置回溯预算耗尽 | `[41698715,41698926)`，211 B、EOF | 48,915 B |

04 的 payload 为 `[17852,17996)`，恰好 144 B；Hex 还包括残留的 2 B
PreviousTagSize。AAC raw 投影完整不代表对 access unit 做了解码，也不意味着
整份文件完整。07 的 AMF0 payload 为 `[24,312)`，独立有界 walker 确认外层
ECMA array 与 keyframes 对象都在 312 耗尽，缺少 object-end；已保留字段与源
位置正确。其 Summary 部分覆盖来自未支持的 FLV1，与 AMF 详情诊断分别记录。

08 的详情请求失败后再读取 Hex，旧详情请求已过期，因此本次导出实际为
`detail:null`、`omitted.detail=stale_request`；不能写成 `unavailable`，也不能
仅凭这个省略原因推断编码支持情况。此前保存的 `044` 原生截图/文本明确显示
“不支持详情”。未请求趋势的 03、06、07、08 均为 `trend:not_requested`。

09 的完整 AVC NALU 从最近配置 Tag 2 取得 4 byte 长度字段，独立核对一个 131 B
IDR NAL 的边界。末 Tag 607 载荷不完整，界面拒绝详情，并未生成“部分解析树”；
三次 512 B Hex 请求到达最后 171 B 窗口，下一窗口禁用，未补零。其导出中
失败的详情请求 3 在 Hex 请求 6 后同样为 `stale_request`。
保存后另查 Tag 3 的 AAC 配置：源字节 `AF 00 13 90`，ASC 为 AAC-LC、22,050 Hz、
双声道，字段与位位置正确；该次详情在 `055` 观察中，不混称为已保存在先前快照。

10 的尾部 AVC payload 为 `[41698203,41698388)`，185 B 完整；DTS 2399960、
CTS 40、PTS 2400000 ms。配置查找读过 8,192 条前序记录、0 个配置候选、5,700 B，
返回 `budget_exhausted`。详情为 `complete:false` / `truncated:true`，保留两条
诊断 `AVC_NAL_LENGTH_SIZE_UNAVAILABLE` 和 `AVC_CONTEXT_LOOKUP_LIMIT_REACHED`，
没有猜测 NAL 长度或边界。此处 truncated 表示解析预算耗尽，不能说源 payload 截断。
尾部趋势保留 Tag 172497–172503；EOS 相对前一视频 Tag 的 DTS 差为 0，
它与 Summary 的媒体时间轴统计口径分别解释，不视为彼此矛盾。
从 AVC 字段范围进入 Hex 后继续一窗，最终 211 B 精确到 EOF、下一窗禁用。
快照同时保留 Summary、部分 AVC 详情、Hex 及趋势，没有省略 section。

十项均完成 Close 后空态及源文件独占只读打开。01–04、08、10 的关闭截图已
查看；05–07、09 的关闭证据只有原生可访问性文本/控件树，不声称有对应视觉截图。
独占检查收据绑定源路径与 UTC，均晚于相应空态观察；其本身没有 PID 字段，
不能当作内核句柄归属的完整审计。`071` 保存最终长媒体关闭空态，`072` 记录
原生 Alt+F4 请求，`073` 确认目标窗口消失；资源工具另核对进程实际退出。
`sources-and-executable-after.json` 确认 10 份源文件和生产 exe 的大小/哈希均未变化。

## 资源测量口径

`resource_probe.py` 以 PID、创建 FILETIME、exe 路径绑定目标并保留进程句柄，
每 500 ms 采样、每 1 s 发现后代。MuxRay、观察到的 WebView2 和其他后代分别
统计 private bytes、working set、句柄数及 CPU；不按全系统进程名汇总。

private bytes 是私有提交内存，working set 求和可能重复计算共享页；CPU 100%
代表一个逻辑核，同时保留按本机 16 逻辑核归一化的值。采样最大值会漏过短峰，
未覆盖附加前的启动资源、GPU、系统缓存或未观察到的共享进程。
采样覆盖带原生自动化操作和界面观察的诊断流程，不是无人操作的稳态内存或
吞吐基准，也不能由单次顺序回归推断内存泄漏或长期性能分位数。
阶段标记记录操作请求和观察时间，跨阶段 CPU 区间可能包含前一阶段的工作；
按钮操作到稳定观察的时间上界不能当作解析耗时或性能分位数。

`resources-001` 因 Windows 读者暂时占用 `status.json` 的替换权限而提前失败，
保留为失败证据，不计入完整资源基线。采样器已补有限重试及状态写失败继续保留
样本的处理。产品运行不受该工具错误影响。

`resources-002` 于 9 月 30 日附加到正在保存首份快照的进程，收集 9,478 个样本，
因 `duration_limit` 结束，未覆盖最终长媒体关闭或进程退出。相邻采样间隔的
p95 为 0.500392 s，最大间隔为 5,005.432671 s（约 83 分 25 秒），出现在
UTC 15:55:07.391 至 17:18:32.865；原因未确定，不推断为休眠。
超过 1 s 的间隔共 7 个，其中仅该空档超过 2 s。不能宣称全程每 500 ms 连续采样。
附加时已有文件和保存对话框，首点总 private bytes 509.148 MiB、工作集
781.273 MiB，只是附加点观测，不是冷启动或空态基线。

该 run 的实际脚本版本已按仅涉及后续 PID 入参校验和口径说明的差异重建为
`sampler-source-reconstructed.py`，其 SHA-256 与 run manifest 完全匹配：
`d1dae842f22e549f28ce910f21c94e8d2e3b89da6867451187555eb937d03744`。
最终入库的工具版本另受 34 项测试验证，不将两者的源码哈希混称为相同。

10 月 1 日在同一个保留会话上另启 `resources-003`，精确脚本副本为
`sampler-source.py`，覆盖最终尾部 Hex、保存、Close 空态及原生 Alt+F4。
742.510 s 内得到 1,486 个样本，以 `target_exit_grace_complete` 正常结束。
MuxRay 及观察到的 6 个 WebView2 后代均记录退出码 0，未强制终止进程。
此结果仅证明已观察身份的退出；源关闭另有空态及独占只读收据。
初始 `sample10_resumed_hex_observed` 标签早于成功的 Hex 操作，后一个标记已明确
纠正；实际 Hex 结果以 `064`、`066` 原生观察为准，不从阶段名称推断 GUI 行为。

003 相邻间隔 p95 为 0.500381 s、最大 0.501000 s，没有超过 1 s 的间隔。
两段原始样本均独立重新计算逐进程分组总量、阶段/全段峰值和标记，结果与各自
summary 一致，无进程或进程树发现错误。独立复核文件为证据根目录的
`resources-002-independent-review-001.json` 和 `resources-003-independent-review-001.json`；
其中同时保留脚本及输入哈希。002/003 之间没有连续观测，不跨空档推断资源变化。

| 采样段 / 组 | private bytes 最大值 (MiB) | 工作集最大值 (MiB) | 句柄最大值 | CPU 最大值（单核为 100%） |
| --- | ---: | ---: | ---: | ---: |
| 002 / MuxRay | 167.488 | 165.965 | 1,408 | 117.7422% |
| 002 / WebView2 合计 | 522.617 | 761.660 | 3,338 | 132.0863% |
| 002 / 同时采样总量 | 641.004 | 900.742 | 4,702 | 165.4113% |
| 003 / MuxRay | 134.906 | 143.258 | 1,398 | 104.4206% |
| 003 / WebView2 合计 | 631.227 | 812.867 | 3,358 | 114.6492% |
| 003 / 同时采样总量 | 752.996 | 947.543 | 4,746 | 124.9863% |

各指标可能在不同采样点达到最大值，总量从每个采样点的进程之和计算，不是相加
各进程或分组的峰值。002/003 总 CPU 峰值换算为 16 核机器比例分别为
10.3381% / 7.8116%。以下关闭后短窗均值只用于复现对照，不表示 GC 已结束、
内存已收敛或长期空闲水平，也不能据两个时段的差值判断泄漏。

| 关闭后短窗 / 组 | private bytes 均值 (MiB) | 工作集均值 (MiB) | 句柄均值 | CPU 均值（单核为 100%） |
| --- | ---: | ---: | ---: | ---: |
| 002 首文件 / MuxRay | 155.602 | 132.266 | 1,338 | 19.384% |
| 002 首文件 / WebView2 | 309.651 | 561.462 | 3,284 | 1.422% |
| 002 首文件 / 总计 | 465.252 | 693.728 | 4,622 | 20.806% |
| 003 长文件 / MuxRay | 129.398 | 136.152 | 1,209 | 1.552% |
| 003 长文件 / WebView2 | 409.696 | 592.807 | 3,345 | 5.570% |
| 003 长文件 / 总计 | 539.095 | 728.960 | 4,554 | 7.121% |

002 取首个关闭空态最后约 5 s：sample 225–235、11 点、实际跨度 4.999858 s；
003 取最终关闭空态末 5 s 范围：sample 1431–1440、10 点、跨度 4.499761 s。
表内均为样本算术平均，不是按时间加权的持续测量。

## 自动检查与边界

| 检查 | 本轮结果 |
| --- | --- |
| 长媒体工具自测 | 6/6：新目录与拒绝覆盖、生成配方、输出/时间限制、失败产物保留、独立 framing 与保留边界 |
| 资源采样工具自测 | 15/15：身份复用、父子归属、缺失指标、CPU 精度、并发总量、阶段控制、采样预算、Windows 共享冲突，以及自有父子进程的真实 API/自然退出 |
| 快照检查器自测 | 13/13：默认 16 MiB / 显式源预算、1 MiB 分块、文件变化、读前后/最终 EOF 截止、Tag 序号与源偏移、截断边界、Hex 字节/长度/EOF、省略一致性 |
| CI 相同入口 | `python -B -m unittest long_media_test resource_probe_test snapshot_check_test -v`，34/34、exit 0；工作目录为 `tools/desktop-native` |
| CI 与示例静态检查 | YAML 可解析，Schema 依赖步骤位于测试之前；README 中 33 个 PowerShell 代码块语法可解析，未执行示例中的实际操作 |
| JSON 合同与语料身份 | 13 个登记清单、既有 Golden 与负例通过；媒体是否存在仍由独立语料检查负责 |

统一新工具测试证据在 `tool-tests/ci-evidence-001/`，记录命令、cwd、退出码、
日志、依赖版本以及测试文件/workflow 哈希。快照源校验扩大后，旧 mixed 原生
报告仍通过，512 byte Hex 及 44 个趋势点全部匹配源。

此次只增加验收工具、清单、CI 与文档，未更改产品代码。
已有 ADR-019 的 Go/前端/race 等检查见
[快照验收记录](desktop-snapshot-20260929.md)，不将历史日志重复称为本轮新测试。
本轮未推送，未运行远程 GitHub Actions；原生交互及本地媒体不进入 CI。

## 后续优先级

下一轮优先设计长媒体的有界 AVC 配置定位。此次尾部 Tag 的 payload 完整，
配置头也确实存在，但距所选 Tag 超过 ADR-014 的 8,192 条回溯预算，详情因此
保留 framing 并明确显示 `budget_exhausted`。这是当前设计的可用性限制；
不能把它描述成文件损坏，或通过猜测四字节 NAL 长度消除告警。

建议先以新 ADR 比较会话内有界配置索引与按需缓存，明确配置变化、损坏候选、
部分扫描、源变化失效、取消/关闭和内存预算，再实现尾部详情与原生回归。
已有 Summary 的保留列表会截断，不能直接假定它覆盖任意 Tag 的最近配置。
验收应包含远距离配置、配置切换、不同访问顺序，以及本轮 40 分钟样本尾部。

语料方面仍需另补具有明确使用依据的真实 AVC/AAC 长录制；当前生成媒体和
外部截断前缀分别验证了不同边界，不能替代这个样本。资源方面再安排独立的
短时连续重复打开/关闭观测，才适合判断资源回落趋势；本轮不作泄漏定论。
