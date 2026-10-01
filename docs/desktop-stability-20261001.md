# 扫描成本、真实长录制与连续稳定性验证：2026-10-01

本轮接续 `112df8b` / ADR-020。扫描版本对比和真实长录制的离线检查已完成；
**连续原生 5 轮回归与新语料原生验收尚未完成，等待 Windows 桌面解锁。**
下面的离线结果不替代原生资源稳定性结论。没有发现需要修改产品代码的功能异常。

## 扫描版本对比

比较 `45d83b2` 与 `112df8b` 的不可变 Git 归档，使用同一 Go 1.27.0、同一
测试探针、同机同源。每次在新进程中通过实际 `document.Open` 至 `Wait`
调用测量，使用原始 `os.File`；进程启动、记录摘要与关闭排除在计时之外。
先读取全源哈希、每版本预热一次，再运行 10 对交替顺序 AB/BA 的测量。
这明确是操作系统缓存预热后的结果，没有驱逐系统缓存。

源文件为已有 40 分钟生成样本，41,698,926 byte、172,504 Tag，SHA-256：

`b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`

| 指标 | 修复前 `45d83b2` | ADR-020 `112df8b` |
| --- | ---: | ---: |
| 测量样本数 | 10 | 10 |
| 中位扫描时间 ms | 594.477 | 706.259 |
| 均值 ms | 596.667 | 706.066 |
| 最小–最大 ms | 586.299–607.557 | 681.237–717.155 |
| P95 ms（nearest rank，n=10 时等于最大值） | 607.557 | 717.155 |
| 单独计数样本 ReadAt 调用数 | 345,010 | 405,012 |
| 单独计数样本请求/返回 byte | 2,587,573 | 2,707,577 |

中位数差为 111.782 ms；逐对倍率中位数增加 18.820%。额外 60,002 次读取、
120,004 byte 恰为 60,000 个视频 NALU、1 个配置和 1 个 EOS 各读取两个字节，
符合 ADR-020。计数探针单独包装源并调用同一 `document.start`，其耗时排除在
速度分布之外；计数是应用层读取量，不能解释成物理磁盘流量。

全部 24 次预热、计时及计数运行都完整扫描相同 172,504 Tag，结构诊断 0；
每条 Tag 及派生完整性标志、诊断摘要一致。独立复核重新计算统计、源视频 Tag
数量，并核对所有原始探针结果、进程退出、归档与输入哈希，通过。
测试前停止了本任务的下载、ffprobe、编译和其他测试；系统其他后台负载未隔离。
结果表明本样本增加约 0.11 秒扫描成本，没有发现扫描语义回归，不能据此推断
冷盘速度、其他介质、UI 耗时或持续内存表现。

## 真实长录制样本

选用 Marcel 与 Christian 的 Datenspuren 2024 会议录像
[《Die FSFW Schul-, Uni- und Makersticks》官方页面](https://media.ccc.de/v/ds24-468-die-fsfw-schul-uni-und-makersticks)。
页面及源 MP4 元数据标明 CC BY 4.0。录像长 2113.808 秒（35 分 13.808 秒），
H.264 High、720×576、25 fps，含 B 帧；德语 AAC-LC、48 kHz 双声道。
原始 MP4 从 [FAU 的 CCC 公开镜像](https://ftp.fau.de/cdn.media.ccc.de/events/datenspuren/2024/h264-sd/ds24-468-deu-Die_FSFW_Schul-_Uni-_und_Makersticks_sd.mp4)
下载，80,071,948 byte，SHA-256：

`9cab12ab223a6f7c7b42ba4873b0258fc24e75c0f36708a5b7732a8f50140f4d`

通过已安装 FFmpeg 7.1.1 的 `-c copy` 重封装，保留完整视频和音频，移除容器
全局元数据并将负 DTS 平移为非负值；没有重编码、裁剪、循环或拼接。
派生 FLV 为 81,206,458 byte，SHA-256：

`f84dcf05fb0bbed72c8ccdc05b9824ecee53b79b610e4022b9d5d8e70bdd4727`

源与 FLV 每流编码 payload 的 SHA-256 和包数相同，记录在独立 streamhash/
ffprobe 证据中。样本是**真实录像派生的 FLV**，不宣称原始直播 FLV 或完整
解码/播放验收。元数据清单见
[`derived-real-ds24-fsfw-2114s.sample.json`](../testdata/manifests/stability-20261001/derived-real-ds24-fsfw-2114s.sample.json)。
媒体保存在本地忽略目录 `testdata/private/corpus-real-long-20261001/`，不提交字节。

独立 FLV framing 结果为 151,934 Tag：52,845 个 AVC NALU、99,086 个 AAC raw、
各 1 个配置以及 1 个 AVC EOS；239 个声明关键帧。没有发现结构错误、每流 DTS
回退、配置变化或无效 NAL 边界。样本没有的异常不算已覆盖。

| AVC 定位 | Tag ID | 事实 |
| --- | ---: | --- |
| 配置 | 0 | payload 55 byte，四字节 NAL 长度声明在 byte 33、bit 6–7 |
| 首部 NALU | 2 | DTS 0、CTS 80、PTS 80；SEI 755 byte 和 IDR 121 byte |
| 中部 NALU | 75966 | payload `[38594996,38595041)`；DTS/PTS 1056920、CTS 0；NAL 正文 `[38595005,38595041)`、36 byte、type 1 |
| 尾部 NALU | 151926 | payload `[81206263,81206296)`；DTS 2113760、CTS 40、PTS 2113800 |

尾部长度字段 `[81206268,81206272)` 声明 24 byte，NAL 正文
`[81206272,81206296)`，type 1。上下文来自 Tag 0，距离远大于旧 8,192 条限制。

本轮从 `112df8b` 构建 CLI，SHA-256：

`8b692f735a3e4e1cde95a24f2380a3d895ac04a377b05111dafdfee63894d46e`

完整 inspect、桌面相同预算的 Summary v2，以及 AVC 配置、AAC 配置、首/中/尾
五份 detail 全部退出 0、stderr 为空。真实源核对与公开 Schema 检查单独保留。
CLI 的 AVC 查找仍用原有策略，不能用这些结果代替桌面候选索引的原生验收。

七份 JSON 均通过各自 Schema，inspect 的 151,934 条记录逐条与独立源字节
一致，头/中/尾 NAL 范围也一致。Summary 无扫描或分析诊断；238 个闭合 GOP
为 1000–10000 ms，平均 8839 ms。视频 PTS 按解码顺序回退 25,634 次，与 B 帧
重排事实相符，不能当作 DTS 异常或损坏。AAC 配置的 AAC-LC/GA 字段已解析，
仍有 24 个尾部 ASC 位保留而未解释，不宣称所有扩展语法均已解析。

## 连续原生验证状态

预先记录计划为同一生产进程的 5 轮“打开 → 尾部详情 → Summary → Hex →
关闭”，每轮关闭后至少静置 30 秒，统计固定窗口最后 10 秒；另检查空态、
源独占只读打开、源哈希及最终自然退出。生产 EXE 身份与 ADR-020 验收一致：

`57ced707984292e97158ec9d093a64e024e33bde2d6f26f36822b90b726594d7`

准备阶段能观察到空窗口；开始 `resources-001` 时截图已是黑屏和指纹图标。
虽然可访问性树仍返回旧空态，原生操作随即停止并请求用户解锁。
采样器以 `operator_stop` 结束，没有进行第一轮，也未记录目标自然退出。
该 run **不计入连续稳定性通过证据**；解锁后需要另建 run 完成全部连续流程。
新真实录制的原生头/中/尾详情、Summary、Hex、快照保存和关闭也待此后完成。

## 工具检查与证据

新增 `scan_compare.py` / Go 注入探针用于真实 Document 版本对比；新增
`resource_cycles.py` 独立核对连续资源原始记录。后者检查全程采样间隙、身份、
阶段、资源聚合和退出，分别报告固定静置窗口和活动峰值，不自动判断是否泄漏。
审阅发现活动阶段的采样间隙原先未参与完整性判定，已修复并补回归测试；
缺少周期的真实 `resources-001` 被正确拒绝，没有生成通过报告。

CI 已加入两种工具的单测；与既有长媒体、资源采样、快照检查器按同一入口运行，
57/57 通过。JSON 合同、登记清单和既有负例校验通过。
本轮尚未修改产品代码，因此沿用 ADR-020 已记录的 Go/race/前端/生产构建结果，
不把历史测试混称为本轮新执行。复现命令见
[原生验收工具说明](../tools/desktop-native/README.md)。

本地证据根目录：

`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-stability-20261001`

- `plan.json`、`build-identity.json`、`cli-identity.json`：计划与程序身份。
- `scan-comparison-prepared/`、`scan-comparison-run-001/`：两提交归档、构建及全部测量。
- `scan-comparison-independent-review-001.json`：独立统计与输入核对。
- `real-remux-result.json`、`real-*-ffprobe.json`、`real-*-packet-streamhash.json`、
  `real-framing-facts.json`、`real-framing-facts-v2.json`、`real-source-provenance.json`：
  真实录制的来源、重封装及独立事实。
- `real-cli-runs.json`、`real-inspect.json`、`real-summary-v2.json`、`real-detail-*.json`：
  本轮 CLI 命令、结果和退出记录。
- `real-cli-independent-check.json`：七份输出的 Schema、逐 Tag 源字段及独立分析核对。
- `locked-interruption.json`、`001-baseline-empty.json`、`resources-001/`：锁屏中断，
  不代表已通过原生流程；图像与可访问性文本之间的不一致也保留。
- `ci-evidence-tools.log`、`contracts.log`、`resource-cycles-tests-*.json`：工具/合同检查。

第一次尝试的 45 分钟录像下载过慢，未完成文件和镜像探针保留；最终选用上面
完整下载的 35 分钟录像，不把失败下载当作已登记媒体。
