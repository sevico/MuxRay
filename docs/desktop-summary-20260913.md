# 桌面媒体概览 v2：2026-09-13 验证记录

## 当前状态

ADR-016、保留文档的 Summary 读取任务、精确投影、Wails 绑定、媒体概览
面板及事件分页已实现。自动检查、同预算 CLI 样本、内嵌浏览器 smoke
和下述 **Windows 原生验收场景通过**。生产程序完成文件选择、概览、
事件分页、详情/Hex 导航、源变化恢复和规模检查；独立 Summary gate
完成取消等待及阻塞读取期间关闭。本轮未发现需修改产品代码的问题。
原生覆盖范围与未单独观测的场景在下文列明。

生产构建使用 Wails v2.14.0，命令：

```powershell
cd desktop
wails build -webview2 error -nopackage -trimpath
```

`MuxRay.exe` SHA-256：
`69725dc0790f4bde945e85cf77d35bdf30c46c30f44efddf29a0fafc57fc6197`。
嵌入 assets 为 `index-BqhcflmK.js`、`index-RCKVWXtQ.css`。首次沙箱构建
因 Vite 无法读取上级目录失败，以已授权的本地构建权限重跑后成功。
运行环境为 Windows amd64、Go 1.27.0、Node 24.4.1；其他 OS 未作桌面验收。
验收对应实现提交 `a6a8b33`；原生交互期间未重新构建，结束后再次核对
生产 exe 哈希一致。两个验收窗口均正常退出，未遗留文件占用。

## 功能与边界

- 明确点击“分析媒体”后，使用已有索引和同一只读文件句柄执行 v2 分析。
- Summary 与 Detail/Hex 共用单 worker、递增 ID，独立 120 秒协作截止时间。
- 完成概览含 112 项精确字符串/null 指标，初始展示六张卡和四行时间轴。
- 关键帧、AAC、AVC、MP3、诊断分别分页，跟随实际 `next`，同时限制条数
  和响应字节数；观测数和保留数分开，诊断截断后未知总数为 null。
- 就绪概览跨 Tag/Hex 导航保留；重新分析、关闭和观测到源变化会清理。
- 取消、超时或 I/O 失败不发布半个 Tag 更新过的聚合结果。取消扫描后
  可以单独分析已提交前缀，明确标注“部分索引”。
- 声明关键帧不等同于已验证 IDR；MP3 仅覆盖首帧头；基础 A/V 范围偏差
  不代表解码播放同步。完整预算与协议见 [ADR-016](decisions/016-retained-desktop-summary.md)。

## 自动检查

| 检查 | 结果 |
| --- | --- |
| 根模块 `go test -count=1 ./...`、`go vet ./...`、CGO-disabled `go build ./...` | 通过 |
| 根模块 `go test -race -count=1 ./...` | 通过 |
| desktop 独立模块普通测试、vet、race | 通过 |
| 前端 Node 测试、typecheck、production build | 97/97 通过；构建通过 |
| Python JSON 合同检查（含现有 v1/v2 golden） | 通过，jsonschema 可用 |
| Python 离线工具测试 | 14 项，13 通过、1 条件跳过 |
| Hex/Summary fixture 测试 | 9 项通过 |
| PowerShell 5.1/7 六种 gate 模式自测 | 通过 |
| Summary/Scan overlay race | 通过 |

新增测试覆盖进度等价、精确大 ID、响应复制、按字节缩短的分页、ready
缓存跨读取保留、queued/active 取消与替换、截止时间、源变化锁存、实际
关闭所有权和绑定错误隐藏。前端覆盖 600 次轮询边界、Start/Cancel 响应
丢失、旧会话响应隔离、事件导航和未知值。生成绑定保留 Wails 的真实输出；
其中空白行自带制表符，普通源文件的 diff/gofmt 检查通过。

## 同预算确定性样本

用 `tools/desktop-native/summary_fixtures.py` 生成最终
`work/summary-fixtures-003`，`summary_check.py` 调用本地 CLI v2 并比对
独立声明的期望值。七个文件共 **122 条断言通过**，保留 CLI 原始 stdout、
stderr、退出码和 SHA-256。正常结果退出 0，含语义/结构问题结果退出 2，
均按预期检查；不是可播放媒体的解码测试。

| 样本 | 主要证据 |
| --- | --- |
| mixed / change | 20 Tag，497 byte；音频编码切换 2 次；AAC/AVC 配置各变 1 次；MP3 参数/码率各变 1 次；音频/视频/MP3 区间码率 1565/2200/1538 bit/s |
| timing | 6 Tag；A/V 起点和终点极值偏差均 10 ms，重叠 30 ms |
| retention | 1660 Tag；关键帧 1100→1024、AAC/AVC 配置各 130→128、MP3 帧头 300→256；观测计数继续准确累加 |
| diagnostics | 300 个损坏 MP3 首帧头，诊断保留至 256 并有截断标记 |
| truncated | 21 Tag，510 byte；实际可用索引/结构诊断按期望比对 |
| many-tags | 200000 Tag，约 3 MB；原生扫描进度、概览和末 Tag 导航通过 |

## 浏览器 smoke

用仓库 `desktop/frontend/test/browser-host.mjs` 在 loopback 服务本次生产
dist，保留生产 CSP，使用明确的合成宿主；通过 Codex 内嵌浏览器交互。
这是实际浏览器 UI 检查，不执行原生文件选择器或 Go ReaderAt。

14 组最终值断言通过，覆盖：最大 uint64 / 小于 -2^53 的精确显示、负
int64 时间戳、部分索引、HTML 文本转义、布局宽度、37 条短页的下一页和
历史回退、未知诊断总数、诊断点 Hex、跨页 Tag 105 详情、导航保留概览、
关闭清理、丢失 Start 恢复、取消不明时禁用读取与再次确认、源变化清理、
pending 时 Close。浏览器比对辅助代码最初两处分别把对象键顺序和 CSS
箭头误当实际 DOM 文本，修正比较方法后重验通过；不是产品失败。

测试可从任意工作目录复现：

```powershell
node C:\work\MuxRay\desktop\frontend\test\browser-host.mjs
# 打开输出的 loopback URL，按需附加 ?smoke=summary-short-page
# 其余：summary-lost-start、summary-cancel-lost、summary-source-changed、summary-pending
```

## Windows 生产程序验收

通过 computer-use 的真实窗口、Windows 文件选择器和 Wails IPC 执行。
操作者首先打开 mixed 样本；后续文件由自动化在原生选择器内选择。
文件名输入框的 accessibility 索引仍偶尔失效，改用已观察到的文件行、
目录面包屑和截图坐标完成选择，无需修改产品或绕过原生入口。

| 场景 | 实际观察 |
| --- | --- |
| mixed 概览 | 20 Tag；音频/视频/脚本/其他为 12/6/1/1，AAC/MP3 为 7/5，编码切换 2，声明关键帧 3、平均 GOP 80 ms；AAC/AVC 配置变化 1/1、MP3 参数/码率变化 1/1；三种区间码率与 CLI 一致 |
| AAC 事件→详情→Hex | Tag 0 的配置为 AAC-LC、44100 Hz、双声道；详情一致；偏移 13 的点定位返回 `[13,497)`、484 byte |
| AVC 变化事件→详情→Hex | Tag 10 为 profile 66、level 11、NAL 长度字段 2 byte、176×144；详情一致；偏移 263 返回 `[263,497)`、234 byte |
| MP3 变化事件→详情→Hex | Tag 14 为 160 kbit/s、singleChannel，参数变化 true、码率变化 false；详情一致；偏移 385 返回 `[385,497)`、112 byte |
| 导航后保留与关闭 | 上述详情/Hex 导航均保留就绪概览；关闭后概览、事件及读取结果清除，重新启用文件选择 |
| retention | 关键帧显示观测 1100/保留 1024，验证前两页 100+100、第二页从 Tag 360 开始；AAC、AVC 各 130/128，尾页各 28 条；MP3 为 300/256，三页 100+100+56，返回起点 100；尾页禁用下一页 |
| timing | A/V 首尾极值偏差均 10 ms、GOP 40 ms；音频/视频区间码率 800/2200 bit/s；视频 PTS 10–50 ms；展开指标的 accessibility 树确认范围交集 30 ms |
| diagnostics | 索引完整、媒体分析部分完成；诊断观测总数显示“未知（已省略）”，实际保留 256；三页 100+100+56，最后一条 `MEDIA_DIAGNOSTIC_LIMIT_REACHED` 可见 |
| truncated | 21 Tag、510 byte，明确显示部分索引/部分分析；`MEDIA_INCOMPLETE_TAG_SKIPPED` 指向 Tag 20、偏移 508；Hex 只显示 `[508,510)` 的 `AF 00`，到 EOF |
| source_changed | change 副本就绪后将 mtime 增加 2 秒；从事件发起 Hex 触发失效，清除概览与结果页、禁用读取并提示关闭重开；重开并重新分析恢复就绪；验收结束并关闭后精确恢复原 mtime，内容 SHA-256 不变 |
| 200000 Tag | 观察到扫描中和完成状态，概览覆盖 200000 Tag；事件直达 Tag 199999 的 MP3 详情，索引定位到起点 199900 的最后 100 条；Hex `[2999998,3000018)`、20 byte，概览保留 |
| 大物理文件 | 既有 `mp3-large.flv` 为 1,082,131,440 byte、66 Tag；概览完成，Tag 65 详情可读；事件偏移 1,073,742,812，Hex `[1073742812,1073743324)`、512 byte，下一窗口可用，概览保留 |

事件 Hex 入口是原始位置的点标记，不伪造字段范围。上表的大文件和大索引
是结构样本；没有把本次交互耗时当作吞吐量、内存峰值或解码性能基准。
大物理文件 SHA-256 为
`d5da1ed883197c2f7e560020010997ec0f895ca6ce33c31c9a5c24f33e7c825c`。

## Summary 阻塞读取取消与关闭

按 [工具步骤](../tools/desktop-native/README.md) 使用独立的
`work/summary-native/close-001`，run ID 为
`7d5d3964e0214e51a72f51b7d699b463`。进程 PID 26976，token 为
`26976-1789274744473518100`；这是通过 Go overlay 注入 gate 的受控
I/O 实验，不能称为生产程序的自然慢盘实测。

1. 原生选择器打开本 run 的 497 byte 样本，完整扫描 32 Tag；分析前没有 entered 收据。
2. 实际点击“分析媒体”后，entered 收据确认 Summary、第 67 次 ReadAt、offset 24、已提交 32 Tag；界面进度停在 0/32。
3. 点击“取消媒体分析”，界面显示“取消尚未确认完成；正在等待底层读取结束”，关闭入口仍可用。
4. MarkClose 后实际点击“关闭文件”；界面显示正在关闭、保留 32 Tag 扫描信息，清空概览及结果页、禁用打开和新读取。Release 前无 closed 收据，独占打开源文件失败。
5. Release 后窗口恢复空状态。`check.ps1 -Scenario SummaryClose -ObservedState closing -ObservedTagCount 32` 通过：收据属于同一进程和 run，满足 `close-command < release < released < closed`，实际 Close 无错误，源文件可独占打开。

本次原生取消请求随后转入 Close，没有单独观测“取消完成后重新分析”的
终态。非 GUI `verify-summary-001` 和 `verify-scan-001` 的 race 已覆盖
实际 worker 关闭所有权、取消后的 10 Tag 前缀 Summary ready 且
ScanComplete=false；本次 20 万 Tag 生产窗口未单独执行扫描取消。

## 证据与限制

本地证据根目录为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use`。
`outputs/muxray-summary/` 保留 Go 日志、CLI 原始报告、浏览器断言、原生
`resume-*.json` accessibility 树和对应 PNG、`native-checks.json`、源文件
mtime 恢复记录、Release 前独占检查及独立审计记录。gate 的构建身份、
收据和 `acceptance.json` 位于 `work/summary-native/close-001`。

主要原生证据：mixed 001–031，分页 039–060，截断 065–072，源变化
080–095，gate 106–113，timing 117–123，诊断 128–139，大索引 146–157，
大物理文件 168–177。旧 Hex/MP3 验收证据保持原样。

工具有两类观测限制：点击后的首份 accessibility 树偶尔仍是上一帧，
以随后的稳定捕获和截图确认；字段定位按钮在树中只暴露名称，位置数字
需看截图。辅助断言曾误匹配分拆的“覆盖 20 Tag”文本、误设末页起点为
199999、从按钮 accessibility 名称查找大偏移数字，均保留失败记录并用
稳定树或截图更正；不计作产品失败，其中末页断言曾导致 Node 会话重启。

未逐项原生展开全部 112 个指标和所有事件参数；关键帧分页仅检查前两页。
本次源变化流程没有单独检查“恢复原 mtime 后、不关闭仍保持失效”；该锁存
语义由自动测试覆盖。浏览器中的精确超大整数及丢回复注入也不冒充原生
文件系统场景。本记录不宣告正式发布或其他系统的桌面验收。
