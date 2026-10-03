# GDI 增长的原生调用边界：2026-10-03

后续 [Win32k ETW 提权与采集验证](desktop-gdi-etw-20261003.md) 已取得有效 ETL 并解码，权限阻塞解除；成功选择与对象校准尚未完成。本文下述权限拒绝保留为当时实验结果。

control、A、B 均重现取消预热后 `260 → 262 → 264 → 266`，末轮静置 180 秒仍为 266。A 在 native GetResult 前截断，B 加回 GetResult 与结果 Release，均跳过路径转换和媒体会话；这些下游调用不是本次增长复现的必要条件。实验仍包含真实选择器成功选择、dialog Release、关闭收尾和观察，**不能据此把分配时间或根因定位到 Show 内部**，也没有取得 GDI 类型或泄漏已修复的证据。

证据根：`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-gdi-boundaries-20261003`。下文证据名均相对于此根；前轮完整 picker-only 结果见[隔离记录](desktop-gdi-isolation-20261002.md)。

## 身份、流程与构建

| 实验 | PID / 创建 FILETIME | EXE SHA-256 |
| --- | --- | --- |
| 生产一致 control | `5436` / `134354631454149798` | `57ced707984292e97158ec9d093a64e024e33bde2d6f26f36822b90b726594d7` |
| A：native GetResult 前 | `27156` / `134354647796057617` | `4b186086ad39448f64e46482cbeda32308ee731101899f905e0188a8e42e5616` |
| B：native GetResult 后 | `7236` / `134354656150529297` | `b4fc2cd81289753c27695c85a23f2ae5acca49f80200737ebb4b355ae3626263` |

各进程在其协议/构建之后创建，完整身份和 EXE 路径保留各段 `identity.json`；采样不拼接。

长源为 `generated-long-avc-aac-2400s.flv`，41,698,926 byte，SHA-256 为 `b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`。先取消预热，再双击文件项成功选择三次；前两轮静置至少 60 秒，末轮至少 180 秒。

control 三轮均扫描 172,504 Tag、诊断 0、默认 Tag 0–99，随后关闭；未进入详情、概览、趋势或 Hex。A/B 各三次真实选择输入、返回空页及源文件独占只读全 SHA 均通过复核。A/B 不调用媒体 API，其源收据只证明可独占读取且内容未变，不证明应用媒体句柄释放。

构建 001 在离线模块元数据阶段失败，未编译；002 的 control 与生产一致，但 A 被 Go 1.27 的 GOMODCACHE overlay 限制拒绝。003 改用约 99 MB 的逐字节私有模块缓存，保留模块版本；10 条命令成功，control SHA 仍与生产一致，224 项原输入前后匹配，私有 CFD 最后恢复原字节。两次失败和复制路径过长的中断记录保留，不将准备失败计作产品故障。

## 已审定固定窗

| 阶段 | 统计窗（秒） | control GDI | A GDI | B GDI | USER 中位数（control / A / B） |
| --- | --- | ---: | ---: | ---: | ---: |
| 初始静置 | 30–60 | 118 | 118 | 118 | 83 / 83 / 83 |
| 取消预热 | 30–60 | 260 | 260 | 260 | 108 / 109 / 109 |
| 成功选择 1 | 30–60 | 262 | 262 | 262 | 109 / 105 / 107 |
| 成功选择 2 | 30–60 | 264 | 264 | 264 | 111 / 108 / 111 |
| 成功选择 3 | 30–60 | 266 | 266 | 266 | 111 / 109 / 110 |
| 第 3 轮晚窗 | 150–180 | 266 | 266 | 266 | 105 / 102 / 107 |

三组各六个固定窗和对应完整静置区间均有效，每窗有 30 份完全落窗的 GDI/USER 快照，GDI 最小、中位和最大值一致。首次 118→260 含初始化，不计逐次增长斜率；USER、普通内核句柄与 GDI 分别报告。已保存 GUI 收据均未落入静置区间，但这不是全局输入审计。

| 复核项目 | control | A | B |
| --- | --- | --- | --- |
| 资源样本 | 1,675 | 1,537 | 1,518 |
| 句柄样本 | 832 存活 + 1 退出 | 763 存活 + 1 退出 | 753 存活 + 1 退出 |
| 全程资源最大间隔 / 1 秒限值 | 0.831 秒，通过 | **1.795 秒，不通过** | 0.505 秒，通过 |
| 全程 GDI/USER 最大间隔 / 2 秒限值 | 1.005 秒，通过 | 1.741 秒，通过 | 1.002 秒，通过 |
| 全程资源覆盖 | 完整 | 不完整 | 完整 |
| 六窗及完整静置覆盖 | 全部有效 | 全部有效 | 全部有效 |
| 原生退出 / 各段观察到的 7 个进程 | 全部退出码 0 | 全部退出码 0 | 全部退出码 0 |
| formalProtocolCompleted | true | **false** | true |

A 唯一资源间隔异常在操作期（样本 246→247），未与固定窗或完整静置重叠。此前状态落盘耗时约 1.786 秒，不据此认定 GUI 是原因；整体不通过与局部固定窗有效分别保留。

原生退出 UTC：control `01:20:03.0304017`、A `01:46:21.7331545`、B `02:00:04.7016011`。正式审计及 SHA-256：

- control：`run-control-001/final-isolation-review.json`，`40581a7aa79f88145adc5f2139662f53806fee90f9e353e53dd8d6ce33f1b7f6`。
- A：`run-a-001/final-isolation-review.json`，`5fa839811fbd0c883d536142d94f66f962eba431f7c63dce9ce182c556518041`；分项判定见同目录 `final-verdict.json`。
- B：`run-b-001/final-isolation-review.json`，`c6837cb0dceb9fe942d91637aff463b6656d1bef00087d49ab89f7fa78448d84`；分项判定见同目录 `final-verdict.json`。

## 调用边界

| 版本 | 真实选择后保留的结果处理 | 状态 |
| --- | --- | --- |
| 完整 picker-only | GetResult、GetDisplayName、转换、free/Release；跳过媒体 API | 前轮已复核，不重复计入本轮 |
| A | 原 Go 层 GetResult 与第二次 GetOptions 保留；不调用 native GetResult | 三轮仍逐次 +2；覆盖限制如上 |
| B | native GetResult 后一次原 item Release；不调用 GetDisplayName | 三轮仍逐次 +2；正式采样与原生协议完成 |

A/B 保留 Show、外层 dialog Release、原 COM 初始化和 HRESULT 行为。B 与 A 相差 GetResult 和 item Release，完整路径还改变字符串处理和结果生命周期；Release 被调用不证明对象已销毁，差分不直接给出分配点。

## ETW 尝试与对象类型限制

独立完整 picker-only 进程 PID `5612`，创建 FILETIME `134354664483736113`，EXE SHA-256 `e1f638812ab786ea5dad59354e37f418fae1e5818c93efdf2f71c59b324f749f`。协议要求跟踪有效后才成功选源。`02:03:41 UTC` 启动跟踪返回 `-2147024891`（`0x80070005`，访问拒绝）；唯一会话的启动前及失败后查询均返回 `4201`，未建立可用跟踪会话，未生成 ETL 或 watchdog，也未成功选源。没有提权或重试。

三次独立 GDI/USER 见证为初始 `118/83`、取消预热后 `260/107`、失败尝试后 `260/107`。随后正常 Alt+F4 退出；资源探针保存 175 样本，根进程与六个观察到的 WebView2 子进程全部确认退出码 0，探针以 `target_exit_grace_complete` 无错误结束。证据为 `etw-traces/full-picker-001/receipt.json`、同目录查询收据及 `run-etw-001/resources/summary.json`。本次没有可解码事件，丢事件字段为未知，不能写为 0。

独立复核见 `run-etw-001/final-etw-review.json`（SHA-256 `b24e106dec597c80bf80c8767d141f66b2dc0bb51a461098b00fa0f2be40278d`）：资源最大间隔 1.004183 秒，根进程于 `02:04:17.5737375 UTC` 正常退出。前后 `260/107` 只记录取消和失败启动，不能作为成功选择没有增长的证据。`start.stdout.txt` 的中文输出有解码损失，访问拒绝判定依据保留的数值退出码，不依赖乱码文本。

本机已安装的 Win32k 提供程序元数据没有 `HandleType` 名称枚举，未校准数字不能称为 Bitmap、Font 或 Brush。**未知内核类型 56 不等于 GDI 类型。** 三组严格内核类型枚举仍不完整，但独立 GDI/USER 计数没有缺失。

下一步是在具有跟踪权限的环境中，按同一有界协议短时采集，再用已知 GDI 对象校准 `HandleType`。分析须按目标 OwnerProcessId 处理所有者变更与句柄转换，核对丢事件后才讨论存留对象；权限拒绝不等于零分配。规则及元数据见 [ETW 外部证据](C:/Users/swk01/Documents/Codex/2026-09-09/computer-use/outputs/muxray-gdi-boundaries-20261003/etw-preparation/trace-proposal.json)。

## 其余限制与收口

实验共享既有 profile、按顺序执行，未随机化。control/A/B 创建到附加分别有 26.152/38.215/36.590 秒未观测，初始窗不是启动瞬间；GUI/AX、对话框存续和收尾也可能随版本变化。不同进程的绝对初值不互相相减。

构建细节见 `boundary-overlays/build-execution-001..003/`，原始采样与 GUI/源收据保留各 run。结论限于本机、此源和操作条件；A 的全程资源覆盖不达标与有效固定窗并列保留，ETW 权限失败也已留证。不将 COM/HRESULT 源码问题预先认定为根因，不据此修改生产资源释放逻辑。
