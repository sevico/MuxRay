# 首次取消与 Windows Ctrl+C 专项（2026-09-05）

上一批语料、工具与实测报告已本地提交为 `f2d6c27`
（`test: add corpus validation and materialized large-file baselines`），没有推送或提交媒体。
本批只增加诊断工具、测试和证据；`cmd/muxray`、`internal/` 和公开 JSON 合同没有修改。

结论：100 次独立进程对照未复现原来的 607.1234 ms 异常，37 次真实 Windows Ctrl+C
事件均按合同返回 `130`。尚无证据指向可复现的核心取消缺陷，因此没有改动解析核心。
**历史异常的根因仍未定位；“本批未复现”不等于“已修复”，也不是全场景延迟验收通过。**
原始 [607 ms 记录](validation/20260905/largefile-10gib-initial-cancellation.json) 完整保留，500 ms 暂定目标没有放宽。

## 对照与计时口径

复用 [首批环境](corpus-baseline-20260905.md) 和 `framing-10gib.flv`：
10,737,418,240 字节、100,000 Tag、确定性非媒体 payload。
本批完成后整文件读回 SHA-256 仍为
`0fc94ee61590a968f7b86f722dc085b125fc250d762906524f72a5012d9278e7`。
共享开发机没有隔离后台负载，未增加新的 13 GiB 文件或更改电源计划。

| 对照组 | 独立进程数 | median / p95 / max（ms） | 指标与原始数据 |
|---|---:|---:|---|
| 无完整扫描、测量时钟不预读 | 30 | 0.02625 / 0.0353 / 0.0582 | cancel → 扫描返回；[JSON](validation/20260905/cancellation/cancel-cold-lazy.json) |
| 无完整扫描、测量时钟预读一次 | 30 | 0.02595 / 0.0294 / 0.0718 | cancel → 扫描返回；[JSON](validation/20260905/cancellation/cancel-cold-primed.json) |
| 先完整扫描 11 次、测量时钟不预读 | 30 | 0.031 / 0.0434 / 0.0979 | cancel → 扫描返回；[JSON](validation/20260905/cancellation/cancel-after-scans.json) |
| 上一批未改动的 measure 二进制 | 10 | 0.03365 / 0.0575 / 0.0575 | 原 `coldCancelMs`；[JSON](validation/20260905/cancellation/cancel-legacy-controls.json) |

前三组每个新进程只执行一次取消，不丢弃首次值。第四组每个新进程按原命令预热扫描 1 次、
测量扫描 10 次，再记录首次取消及预热后的取消数组；全部原始字段仍保留。
第四组的时间窗口与前三组不同，不能据此计算版本加速比。
所有统计使用偶数项平均中位数与 nearest-rank p95，保留每个 trial，没有按耗时筛除样本。

新增 `largefile cancel-probe` 把读边界、取消 goroutine 唤醒、起始时钟调用、`cancel()`、
读取恢复、扫描返回、结束时钟调用拆开记录；独立 observer 始终先初始化，测量时钟可选择预读。
同原实验一样，在第 100 次真实 ReadAt 后通过握手触发另一 goroutine 取消。
90 个带埋点的 trial 都只保留 48 条已提交 Tag，读回 748 字节，无损坏诊断。

主要字段：

| 字段 | 起止点与限制 |
|---|---|
| `boundaryToWorkerMs` | 读取边界 → 取消 goroutine 开始计时，包含通知和调度 |
| `startClockCallMs` | 起始测量时钟调用前后，包含返回到 Go 的开销 |
| `cancelCallMs` | `cancel()` 调用前后 |
| `cancelToScanReturnMs` | `cancel()` 前 → ScanContext 返回，包含本次握手和调度，不是纯 CPU 时间 |
| `afterCancelToReaderMs` | `cancel()` 返回 → 读取握手释放后恢复执行 |
| `readerToScanReturnMs` | 读取恢复 → ScanContext 返回 |
| `returnClockCallMs` | 扫描返回后读取结束时钟的开销 |
| `legacyWindowMs` | 测量时钟两个内部读数之差，对照历史单窗口口径 |

源码审查确认 Windows `LazyProc` 首次调用会延迟查找过程地址；本批测得未预读起始时钟调用最大
0.0512 ms，不能用它解释历史的 607 ms。新增模拟时钟单测在起始读数后、`cancel()` 前注入
607 ms 虚拟间隔，验证该间隔进入旧窗口/时钟阶段，而不会被归入核心取消阶段。
这只是证明分类方法能区分计时与取消，**不是对历史异常原因的认定**。

observer 和埋点本身也会预热共享 runtime/系统路径，并增加开销，因此保留完全未改动的旧二进制对照。
三个带埋点组结果只描述本机、确定性读边界；不证明任意阻塞 I/O 可被抢占。

## 调度 trace 观察

另执行一次带 runtime trace 的正常样本，`cancelToScanReturnMs` 为 0.0338 ms。
取消 goroutine 从 Runnable 到执行约 0.005697 ms，扫描 goroutine 在实验握手通道上的
等待到恢复约 0.008676 ms。trace 与测量协议相符，但该次没有异常，不能据此推断原异常的调度原因。

本机 Go 安装未提供 `go tool trace` 可执行工具，因此从本机已有 `cmd/trace` 源码构建了
项目缓存内的解读程序，并以 `-d=parsed` 校验；没有联网下载、全局安装或打开 trace Web 服务。
trace 文件 11,776 字节，SHA-256 为
`658e201594ab47015ec695a0e3b558e4b38f6fe182da52ccf339a095d4857e28`。
它包含绝对构建路径，因此仅保留在被忽略的 `.cache/cancel-observation.trace`；
仓库保存 [不含路径的观察摘要](validation/20260905/cancellation/trace-observation.json)。

## Windows 真实 Ctrl+C 验证

本次用 web-access 查阅 Microsoft 一手说明后，采用真实 `CTRL_C_EVENT`，不以 Ctrl+Break
或直接调用 context 代替。该事件不能限定到指定进程组，因此测试使用独立隐藏控制台，
绝不向用户现有控制台发送事件。[GenerateConsoleCtrlEvent 文档](https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent)

安全边界：父进程先把等待握手的 helper 加入本次专属、kill-on-close 的 Job，再允许其创建目标 CLI。
helper 在启动前和发事件前核对控制台成员分别恰好为“自己”和“自己 + 本次 CLI”，否则拒绝发送。
helper 只在 CLI 创建后设置自身忽略 Ctrl+C，避免子进程继承忽略属性。
隐藏窗口、成员检查和继承处理依据
[进程创建标志](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags)、
[控制台成员列表](https://learn.microsoft.com/en-us/windows/console/getconsoleprocesslist) 与
[SetConsoleCtrlHandler](https://learn.microsoft.com/en-us/windows/console/setconsolectrlhandler)。

正常退出后确认控制台只剩 helper；超时/超限/失败时只清理本次持有的进程句柄和 Job。
没有按进程名称批量终止，没有 AttachConsole 到已有终端。
Job 的退出清理语义见
[JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_basic_limit_information)。

| 场景 | 最终矩阵每项次数 | 验证条件 |
|---|---:|---|
| `version` 正常退出 | 3 | 不发送信号，exit=0，无 stderr |
| `inspect` / `tags` / `summary v2` / `detail` 扫描阶段 | 各 3 | exit=130，stdout 为空，明确的 scan input 取消消息 |
| `inspect` / `tags` 输出阶段 | 各 3 | exit=130，已有 stdout，明确的 write 取消消息与丢弃提示 |

扫描阶段用子进程至少 1,000 次读操作且尚无 stdout 作为触发条件；
最终还检查 stderr 中的扫描取消位置，而非仅凭计时猜测阶段。
输出阶段等到第一批字节再发事件，随后立即继续排空管道，避免测试器制造永久写阻塞。

[首个 smoke](validation/20260905/cancellation/ctrlc-first-smoke.json)、
[初始完整矩阵](validation/20260905/cancellation/ctrlc-matrix.json) 和
[加强阶段断言后的完整矩阵](validation/20260905/cancellation/ctrlc-matrix-verified.json) 全部保存。
合计 37 次取消均为 `130`、无强杀；另有 6 次正常退出对照。
最终 18 次取消的“发事件 → 进程退出并排空输出”中位数为 4.13965 ms，p95/max 为 12.0628 ms；
全部 37 次中最大为 14.2151 ms。该口径包含信号分发、调度、进程退出和测试器的管道处理，
不与上面的核心 API 计时直接比较。

6 次 `tags` 输出阶段取消都得到了语法完整的 JSON，但仍为 exit=130。
原因是已进入的写操作可以把字节写完，随后上下文包装器检查到取消。
这符合既有 [Tag 分页合同](tag-index-v1.md)：调用方必须优先检查退出码，130 时即使 JSON 完整也丢弃。

本批没有手工按键/Windows Terminal UI 验收，也没有测量任意阻塞 ReaderAt/Writer、所有深层解析路径、
跨平台信号、系统高负载尾延迟或后台会话取消；自动注入真实控制台事件不能替代这些范围。

## 可追溯性、验证与后续

| 被测二进制 | SHA-256 |
|---|---|
| 旧 largefile 对照 | `538194e7de9e87116a5ead2d3b79f2036bfc6938d206cc3b35c0a7e3e12d6c4d` |
| cancel-probe 工具 | `0df189c23e04bc2e3dbdba1d6bd0e749b10ae6bbd8edde1b08f714c509ee5d06` |
| CLI（核心未修改） | `f497bc998b2fc2946ca82cf5d5ba282379ef4431cdb26e4292cae7bf7f1a2f36` |

完整 unit、vet、CGO/GCC race 和 JSON 合同验证通过。Python 单测包含时限/输出限制、共享控制台拒绝、
自有 Job 清理、失败保留及统计检查；Windows 下非 Windows API 拒绝测试按平台跳过。
现有 CI 的工具测试发现规则会覆盖新增单测；真实控制台矩阵仍是显式运行的本地 smoke，远端 CI 未运行。
复现命令见 [工具说明](../tools/README.md)。历史原始数据未覆盖，媒体和含构建路径的 trace 不入库。

本次专项形成可复现工具和明确边界，可以作为一个独立批次收尾；历史离群值继续列为开放观察项。
再次出现超标时，应连同首次值、分阶段时间线和同次调度证据保留，不能只重跑到通过。
后续可单独评审 ADR-009（进度/后台扫描会话），将会话级取消、输出丢弃和资源释放纳入设计；
本批没有开始实现该功能，也没有提高 Tag 上限或宣告取消性能验收完成。
