# 长媒体 AVC 配置定位与原生验收：2026-10-01

桌面详情已改为使用扫描期间建立的 AVC 配置候选索引。打开 40 分钟样本后，
第一次详情请求直接选择尾部 Tag 172497，即可找到 Tag 0 的有效配置；不需要
先访问配置 Tag 或运行媒体概览。此次请求只访问 1 条候选记录、解析 1 个配置，
上下文读取 54 byte，所选 NAL 完整且没有详情诊断。

设计见 [ADR-020](decisions/020-indexed-desktop-avc-context.md)。
[前次语料回归](desktop-corpus-20260930.md) 中 8,192 条回溯预算耗尽的结果保留为
历史证据；本次验证同一文件的相同尾部 Tag，确认距离限制已解除。

## 实现与边界

默认 scanner 与 CLI 保持结构扫描。桌面通过可选的同步 committed-record observer，
对完整、未过滤且至少两字节的视频 payload 探测两个字节，记录 AVC sequence-header 候选 ID。
候选 bitset 在首次命中时分配，最多 125,000 byte；一百万 Tag 硬上限内，新增
探测读取最多 2,000,000 byte。扫描不解析 avcC，不缓存长度宽度。

每次详情仍按源顺序向前寻找最近可用配置，重新读取并验证候选原始字节。
取消或 I/O 失败后的覆盖边界单独记录，不把未观察区间当作没有配置。
配置切换、反向选择、损坏候选和部分扫描沿用最近可用配置规则。
32 个配置解析、1 MiB 上下文读取和三秒协作截止时间继续生效；解析器与展示
预算也继续保留。任意阻塞 ReaderAt 仍不能被截止时间强制终止。

详情增加派生字段 `lookupMethod=candidate_index`，沿用已有通用字段格式，
没有新增 binding、DTO envelope 或 schema 版本。`scannedTags` 计数实际访问的
候选记录，不再把跳过的逻辑 Tag 距离计为工作量。CLI 的原有输出合同不变。

## 构建与样本身份

基于本地提交 `45d83b2` 加本次修改执行 `desktop/build-production.ps1`，完成
Wails binding 生成、TypeScript/Vite 构建及 Windows 生产构建。生成的 binding、
`desktop/go.mod` 与 `desktop/go.sum` 无差异。生产程序为
`desktop/build/bin/MuxRay.exe`，11,414,016 byte，SHA-256：

`57ced707984292e97158ec9d093a64e024e33bde2d6f26f36822b90b726594d7`

原生进程 PID 为 43632，创建 FILETIME 为 `134353284543244149`。验收使用原生
文件选择、窗口控件、Hex 定位和保存对话框，未使用浏览器 mock 代替生产窗口。

样本为 `testdata/private/corpus-long-20260930/generated-long-avc-aac-2400s.flv`，
41,698,926 byte、172,504 Tag，SHA-256：

`b46e8df2e90eb661f4d830ba99d185fc7aeb3db94bc5f78102d09aaf6ca3e427`

它是已有配方生成的 AVC/AAC 媒体，清单位于
`testdata/manifests/longmedia/generated-long-avc-aac-2400s.sample.json`。
它不代替真实长录制语料。

## 原生流程结果

| 操作 | 实际结果 |
| --- | --- |
| 打开文件并完成结构扫描 | 172,504 Tag，结构诊断 0；没有运行 Summary 或趋势分析 |
| 首次详情直接跳转 Tag 172497 | `found`、`candidate_index`；来源 Tag 0、NAL 长度宽度 4；`scannedTags=1`、`candidates=1`、`bytesRead=54` |
| 检查所选 Tag | payload `[41698203,41698388)`、185 byte；DTS 2399960 ms、CTS 40 ms、PTS 2400000 ms；`complete=true`、`truncated=false`、诊断 0 |
| 检查 NAL 边界 | 长度字段 `[41698208,41698212)` 为 `00 00 00 b0`，声明 176 byte；NAL `[41698212,41698388)`，首字节 `01`、type 1、nal_ref_idc 0 |
| 点击配置长度字段源位置 | Hex 跳到 `[33,545)`；byte 33 为 `ff`，bit 6、7 高亮，`lengthSizeMinusOne=3` |
| 选择前部 Tag 2，再返回 Tag 172497 | 两次均找到 Tag 0 的四字节配置，尾部结果和首次选择一致 |
| 跳转尾部长度字段 Hex | `[41698208,41698720)` 的 512 byte 窗口以 `00 00 00 b0 01` 开始，与原始文件一致 |
| 原生保存诊断快照 | 成功保存 `long-tail.snapshot.json`，6,507 byte；详情与 Hex 为已载入，Summary 与趋势明确省略 |
| 关闭文件 | 界面回到空状态；随后独占只读打开源文件成功；源文件及生产 EXE 哈希未变 |
| 原生 Alt+F4 退出 | 保留进程身份的只读观察确认根进程及观测到的 6 个 WebView2 子进程全部退出，退出码均为 0 |

1 条候选、54 byte 是上下文查找的工作量计数，不是耗时或总文件 I/O 基准。
退出采样只覆盖关闭后的阶段，不用于宣称全流程资源峰值下降。

## 自动检查与证据

| 检查 | 结果 |
| --- | --- |
| 根模块 `go test -count=1 ./...` | 通过 |
| 根模块 `go test -race -count=1 ./...`、`go vet ./...` | 通过 |
| `desktop/` 模块 test、race、vet | 通过 |
| 前端 `npm test` | 141/141 通过 |
| 生产构建 | 通过 |

新增测试覆盖超过 8,192 条距离的尾部首次选择，1/2/4 字节配置切换与反向重复
选择，损坏候选跳过和 32 候选预算，bitset 字边界及硬上限，探测资格与读取
上限，观察缺口、失败和取消的已提交前缀，源文件变化以及阻塞 Close 后恰好
关闭一次并释放索引。scanner/session 测试同时验证默认入口没有额外 payload
读取、各提交分支的回调、错误优先级和已有结构诊断保留。

初轮新增 Close 测试误把已取消扫描的 Wait 结果预期为 nil，修正为
`context.Canceled` 后全量重跑通过；初轮失败日志保留，没有改动产品取消语义。

已有 `snapshot_check.py` 使用 64 MiB 源读取上限复核快照通过。另一份独立脚本
完成 90 项检查：遍历原始 FLV framing，确认唯一 AVC 配置为 Tag 0，并从源字节
核对 avcC 长度宽度、尾部 NAL 边界、时间戳、Hex 和快照省略项；三份带时间戳
的原生可访问性记录证明尾部、前部、尾部的选择顺序与结果。查找计数取自
原生观察及导出结果，脚本没有独立跟踪运行时 I/O。该脚本首轮遗漏 High Profile
avcC 扩展，补齐并验证本样本扩展后通过；两轮结果均保留，这不是产品解析失败。
关闭与退出另有 26 项检查通过，包括空态、独占打开收据、源/程序哈希、进程
PID 与创建时间身份，以及原始采样记录和退出汇总的一致性。

本地证据目录：

`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-avc-index-20261001`

- `production-build.log`、`build-identity.json`：生产构建与运行身份。
- `go-test-final.log`、`go-race.log`、`go-vet.log`、`desktop-*.log`、
  `frontend-test.log`：自动检查；`go-test.log` 保留初轮测试预期失败。
- `001` 至 `014` 观察 JSON：打开、首次尾部、配置来源、Hex、反向选择与原生保存。
  有截图的记录保留截图；仅含可访问性树的记录不能当作截图证据。
- `long-tail.snapshot.json`：实际保存的有界诊断快照。
- `review-long-tail-002.py`、`review-long-tail-002.json` 及同前缀
  `snapshot-check.json`：独立源字节复核及既有快照检查结果；`001` 保留复核脚本
  初轮未覆盖 High Profile 扩展时的失败。
- `015-close-action.json`、`016-closed-empty.json`、`close-and-identity-check.json`：
  关闭过程、空态、独占打开及哈希复核。
- `review-close-exit-001.py`、`review-close-exit-001.json`：关闭与退出的独立复核。
- `017-exit-windows.json` 是退出动作刚完成时的即时窗口列表，仍可包含正在退出的
  窗口；`018-exited-windows.json` 与 `exit-probe-001/summary.json` 才是最终退出证据。

媒体、生产二进制、截图与本机运行证据留在忽略目录或仓库之外，不加入源码提交。
