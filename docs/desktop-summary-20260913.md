# 桌面媒体概览 v2：2026-09-13 验证记录

## 当前状态

ADR-016、保留文档的 Summary 读取任务、精确投影、Wails 绑定、媒体概览
面板及事件分页已实现。自动检查、同预算 CLI 样本和内嵌浏览器 smoke
通过。**真实 Windows 完整交互验收尚未完成**：生产程序已启动，原生
选择器已打开，但 computer-use 无法可靠更新该对话框的输入焦点。
这不是已确认的产品错误，也不把浏览器合成宿主视作原生验收通过。

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
| many-tags | 200000 Tag，约 3 MB；用于后续原生进度/取消验收 |

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

## 原生验收待完成项

生产 exe 已成功启动并显示空状态，点击“打开 FLV 文件”后选择器可见。
自动化 `set_value` 返回 `element 126 is not available in cached app state`；
刷新窗口身份后，键盘/坐标操作也未得到可确认的文件名焦点。已请求操作者
在现有选择器打开 `summary-mixed.flv`，保留当前窗口继续，不猜测输入结果。

下一段原生交互应覆盖 mixed 数值/事件参数、AAC/AVC/MP3 到详情与 Hex、
retention 分页、timing、truncated、源变化并关闭重开、20 万 Tag 进度/取消，
以及现有 1,082,131,440 byte 的 `mp3-large.flv`。这些不能用 CLI 结果替代。

独立 `Summary` gated exe 已构建于 `work/summary-native/close-001`，尚未
启动，不存在 native SummaryClose 完成收据。待生产交互结束后按
[工具步骤](../tools/desktop-native/README.md) 启动此 run：完整扫描 32 Tag，
分析的第 67 次 ReadAt / offset 24 阻塞，观察取消/Close/槽位占用，释放后
确认实际 Close 和独占文件。不要覆盖该 run，也不要在启动后重复打开。

非 GUI `verify-summary-001` 和 `verify-scan-001` 已通过 race：Summary
阻塞时关闭立即清缓存并拒绝新读，释放后才 actual Close；取消扫描前缀
10 Tag 的 Summary ready 且 ScanComplete=false。它们不代替待完成的
原生点击和窗口观察。

本地证据位于工作区 `outputs/muxray-summary/`，包括 Go 检查日志、CLI
原始报告、浏览器断言/DOM/截图、生产空状态及选择器截图和 gated build。
旧 Hex/MP3 验收证据保留。本记录不宣告正式发布或其他系统的桌面验收。
