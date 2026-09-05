# 首批语料与落盘大文件实测（2026-09-05）

本批验证 [ADR-008](decisions/008-cancellable-chunked-index.md) 的分块扫描和分页基础，
不改变产品 CLI、解析语义或公开 JSON 合同。被测核心为本地提交 `782664b`
（`feat: add cancellable chunked scans and tag pages`）；本报告及辅助工具属于后续批次。

当前结果：8 个小样本完成离线回归；1/2/10 GiB 非稀疏文件完成容器扫描实测。
这不是 P2/MVP 全量验收。真实语料清权、覆盖数量、跨平台实测和一次首次取消离群值仍未闭环。

后续已本地提交本批为 `f2d6c27`，并完成 [取消专项复测](cancellation-investigation-20260905.md)：
100 次独立进程未复现原异常，37 次真实 Windows Ctrl+C 事件符合退出合同；历史根因仍开放。
以下初测数据和结论保留原貌，不以复测覆盖离群记录。

## 语料身份与来源边界

[清单目录](../testdata/manifests/README.md) 新增 11 个互不重复的 SHA-256 身份：
根目录 8 个小样本，`largefile/` 下 3 个生成压力样本；`example-*` 不计数。
所有清单均为 `metadata-only`，媒体字节没有进入版本控制。

3 个外部文件来自 [FFmpeg 公开 FLV 样本目录](https://samples.ffmpeg.org/FLV/)。
站点 [使用说明](https://samples.ffmpeg.org/00-README) 允许按需下载单个样本，
但这不构成样本再分发授权，也不证明其中没有个人信息。按本次 web-access 来源核验流程，
仅记录公开来源、大小和哈希，保留本地副本；它们的 `rights` 为 `unreviewed`，
`privacy` 为 `not-reviewed`，原始制作者也尚未核实。
`originClass: real` 在此区分外部既有文件与本项目生成器，不等同于已清权生产录制。
因此本批新增的“已清权真实样本”数量仍为 **0**，不能将这 3 个文件计入正式验收目标。

3 个编码生成样本来自固定 lavfi 测试图/正弦波，没有采集输入；另有 2 个受控损坏派生文件。
生成器是本机 FFmpeg `7.1.1-full_build-www.gyan.dev`，配方保存在
[`prepare_corpus.py`](../tools/prepare_corpus.py)。这些样本及大文件统一限制为 `testing-only`，
隐私状态为 `no-sensitive-data`；没有在本批批准二进制再分发。
FFmpeg 二进制也没有进入仓库，项目自身许可证状态不变。

| 小样本 ID | 来源 | Tag 数 | 容器扫描与覆盖 |
|---|---|---:|---|
| `public-flv-mp3-44100-2008` | 外部 | 11,959 | 完整；MP3 44.1 kHz 双声道 + FLV1；无容器诊断 |
| `public-flv-mp3-22050-2010` | 外部 | 11,026 | 完整；MP3 22.05 kHz 单声道 + FLV1；offset 312 的 PreviousTagSize 不匹配 |
| `public-flv1-adpcm-zelda` | 外部 | 998 | 完整；FLV1 + ADPCM；验证未支持 codec 的显式降级 |
| `generated-avc-aac` | 生成 | 141 | 完整；H.264 160×90、B 帧、AAC 44.1 kHz、配置/NALU/EOS |
| `generated-aac` | 生成 | 96 | 完整；AAC 48 kHz 单声道、配置 + 95 个 raw Tag |
| `generated-mp3` | 生成 | 78 | 完整；MP3 44.1 kHz 单声道、128 kbit/s 首帧头 |
| `derived-aac-truncated` | 派生 | 96 | 不完整；末尾删除 2 字节，offset 17,996 的 PreviousTagSize 截断 |
| `derived-mp3-bad-previous` | 派生 | 78 | 完整且 warning；首个 PreviousTagSize 置零，offset 442 |

`validity` 与来源分类独立：外部的 22.05 kHz 文件本身就是损坏样本。
不支持 FLV1/ADPCM 深层分析时，summary v2 正确保留未分类诊断和 `complete: false`；
这不是完整解码成功。上述 8 个小样本也不构成全部 codec/边界情况覆盖。

## 小样本验证口径

[`corpus.py`](../tools/corpus.py) 不联网、不调用任何 Golden/清单更新接口，执行：

1. 核对清单 Schema、唯一 ID/SHA-256、文件大小与哈希；结束时再次核对身份。
2. 用独立 Python 固定字段遍历校验全部 Tag 的 ID、位置、大小、DTS、标志、StreamID、
   Span、payload 完整性及 PreviousTagSize；不复用 Go 解析器作为 oracle。
3. 每个文件重复 3 次 `inspect`，要求原始 JSON 字节一致；用公开 Schema 校验扫描、分页、
   summary v2 和已支持的 detail 输出，并核对清单最小期望及诊断计数/首锚点。
4. 比较起始页、跨 1,024 条块边界的页（文件够长时）和 EOF 空页；保持全局记录不变。
5. 对容器完整文件抽取每种已支持 packet 类型的首个详情；本批共检查 11 个详情变体，
   不是逐个 Tag 深层解析。截断容器不调用详情或 ffprobe 差分。
6. 对完整文件使用本机 ffprobe 7.1.1，逐包比较类型、位置、payload 大小和 DTS/PTS。
   配置/EOS Tag 不等于媒体包；H.264 CTS 按有符号 24 位计算。
   另核对 AAC/MP3 采样率与声道、H.264 SPS 显示尺寸、summary 媒体 Tag 计数。

唯一已解释差异是派生 MP3 的非法 PreviousTagSize：ffprobe 默认丢弃第一包，得到 77 包；
MuxRay 保留全部 78 个 Tag 并给出 warning。使用 `-flv_ignore_prevtag 1` 后 78 包完全一致。
[`corpus-differences-v1.json`](../tools/corpus-differences-v1.json) 只允许这个样本哈希、
工具版本及确切位置 `13` 的包被省略；其他字段、位置、版本或样本变化均失败，
没有通用“容许差一点”的规则。两种工具的恢复语义不同，不据此判定某一方有 bug。

完整机器可读记录见 [`corpus.json`](validation/20260905/corpus.json)：
`verified` 为 8，`missing` 为空。3 个生成文件及 2 个派生文件已在另一个新目录重新生成，
5 个哈希均与清单一致；这些副本不增加样本计数。

## 大文件环境与方法

本机 Windows 11 Home China 10.0.26200、amd64、Go 1.27.0；
Intel Core Ultra 7 255H，16 个逻辑处理器；物理内存 33,752,997,888 字节；
YMTC PC411 1 TB SSD、NTFS、平衡电源计划。测试运行在共享开发机，未隔离后台负载或清空系统文件缓存。

被测二进制 SHA-256（本机构建，仅用于追溯）：

| 二进制 | SHA-256 |
|---|---|
| MuxRay CLI | `cad91d1471cf221c52719160fe479ff3b020aa945b012f41a17652640c336023` |
| 最终实测 largefile 工具 | `538194e7de9e87116a5ead2d3b79f2036bfc6938d206cc3b35c0a7e3e12d6c4d` |

[`largefile`](../tools/largefile/main.go) 以顺序 Write 实际写入精确的 1、2、10 GiB，
不使用 Seek/Truncate 或稀疏分配；1 MiB 缓冲反复写入确定性非零填充。
三个文件分别具有 100,000 个 Script Tag，DTS 为 `id × 40`，无音视频声明。
它们只有合法容器 framing，payload 不是有效 AMF，也不是编码媒体。
大小/哈希记录在 [largefile 清单](../testdata/manifests/largefile/)；
生成后整文件读回 SHA-256 全部匹配，Windows 属性均为 `Archive`，无 SparseFile/Compressed 标志。

每种大小使用独立进程：扫描预热 1 次后采样 10 次，全部到达 EOF、索引 100,000 条；
比较首/中/末 Tag 的生成公式事实；取消另行预热 1 次后采样 10 次，并保留首次取消值。
中位数使用偶数两项平均，p95 使用 nearest-rank，10 次采样时即最大值。原始 values 全部保留。

扫描只读取容器结构，不读取 payload。每轮 `ReaderAt` 实际返回 **1,500,013 字节**，
与文件大小无关；这也不是 SSD 的物理读取量，操作系统可能额外缓存/预读。
不可将“10 GiB ÷ 耗时”称为磁盘吞吐量，不能把本次测试当作完整解码或 GUI 内存验收。

Go 分配量来自 MemStats 差值；retained heap 是索引保持存活、GC 后的进程 Go 堆。
工作集和扫描阶段观测到的进程生命周期峰值使用 Windows
[GetProcessMemoryInfo](https://learn.microsoft.com/en-us/windows/win32/api/psapi/nf-psapi-getprocessmemoryinfo)
返回的 [PROCESS_MEMORY_COUNTERS_EX](https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-process_memory_counters_ex)，
不是拿 Go 堆数冒充 RSS。工作集采样点在 GC 后；峰值计数由操作系统维护。
非 Windows 工具目前输出 `workingSetSource: unavailable` 和零值，不构成该平台的 RSS 测量。

## 实测结果与限制

| 实际大小 | 扫描 median / p95（ms） | 峰值工作集（MiB） | 首次取消（ms） | 预热后取消 p95（ms） | 原始记录 |
|---|---:|---:|---:|---:|---|
| 1 GiB | 1,034.18 / 1,352.25 | 12.99 | 0.0611 | 0.0024 | [JSON](validation/20260905/largefile-1gib.json) |
| 2 GiB | 614.79 / 751.67 | 12.89 | 0.0247 | 0.0022 | [JSON](validation/20260905/largefile-2gib.json) |
| 10 GiB | 550.14 / 730.17 | 13.31 | 0.0374 | 0.0305 | [JSON](validation/20260905/largefile-10gib.json) |

相同 Tag 数下，10 GiB 与 1 GiB 的 GC 后工作集中位数差为 98,304 字节（0.09375 MiB）。
每次扫描分配量中位数分别为 4,829,864 / 4,829,528 / 4,829,640 字节，
分配次数中位数为 125 / 122 / 122；三者读取 256 条跨块页均为 12,288 字节、1 次分配。
这些数据支持“当前索引不会随不读取的 payload 等比例占用内存”，
但只覆盖 100,000 Tag；不能外推为百万 Tag 或所有真实文件的资源验收。

扫描时间非单调且轮间波动明显，没有控制缓存、调度或后台负载，
所以本批仅保留初始观察记录，不设置性能回归门槛，也不声称大文件更快。
本次峰值和工作集差值低于暂定 256 MiB/64 MiB 目标，但不是所有平台、工作负载的通过结论。

取消在第 100 次实际读取后的确定性边界，由另一 goroutine 调用 `cancel()`；
计时从调用前的时钟读数到 ScanContext 返回，验证 `context.Canceled` 和非完整的部分索引。
它不包含操作系统 Ctrl+C 事件分发时间，也不测量任意阻塞 ReaderAt 被抢占的能力。
最早用 Go 时钟得到过全零的微小间隔，故取消计时改用 Windows
[QueryPerformanceCounter](https://learn.microsoft.com/en-us/windows/win32/api/profileapi/nf-profileapi-queryperformancecounter)，
不把低分辨率零值当作零延迟。

必须保留的异常：高精度初测中，10 GiB 进程的第一次取消为 **607.1234 ms**，
超过暂定 500 ms 目标，其余 9 次小于 0.04 ms。
[该次完整原始记录](validation/20260905/largefile-10gib-initial-cancellation.json) 未删除或修改，
旧记录没有 `cancelWarmups`/`coldCancelMs`，首次取消当时包含在 `cancelMs.values` 中。
随后工具补充单独取消预热，同时继续报告首次值；上表为三个新进程的完整复测，未复现该异常。
目前没有证据确定它来自系统调度、初始化还是其他原因，**不能宣告取消延迟目标验收通过**。
下一批应优先收集多次新进程首次取消及调度/时间线证据，再判断是否需要改核心。

## 验证完成项与下一步

本机全量 `go test ./...`、`go vet ./...`、启用 CGO/GCC 的
`go test -race -count=1 ./...` 通过；7 个 Python 工具单测、全部 Schema/Golden 正反例、
11 个清单身份检查、8 个实际小样本验证通过。本批没有重跑上一批记录的所有 fuzz 时长。

CI 增加离线工具单测和全部清单身份校验，不下载公开媒体、不要求 FFmpeg，
也不在共享 CI 自动写入 13 GiB。跨平台 Go 测试配置保留，但本批未推送，不能报告远端 CI 已通过。
辅助进程采用 argv、单次 30 秒和双输出上限，并在超时/超限时清理本次启动的进程；
这只是开发工具保护，不是产品级不可信进程沙箱或 P3 外部工具集成。

复现命令与限制见 [tools/README.md](../tools/README.md)。当前媒体保留在被忽略的
`testdata/private/corpus-20260905/` 和 `testdata/artifacts/largefile-20260905/`，
后者实际占用约 13 GiB；没有为报告删除媒体，也没有上传媒体。

后续依次处理：首次取消离群值、继续获取并审清真实语料、补足原交接约定的
48 个 MVP 样本和 20 个真实样本及覆盖矩阵，再评估可观测进度/后台扫描会话。
提高 Tag 上限、磁盘索引、Wails GUI、在线输入仍需后续设计；本批没有实现这些功能。
