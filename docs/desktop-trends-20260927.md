# Tag 趋势与异常定位：2026-09-27 验证记录

## 状态

本轮 ADR-018、趋势窗口、异常定位及验收已完成。本地自动测试、下列合成浏览器
交互和 Windows 生产原生五样本流程均通过；两次原生构建的覆盖范围分别记录。

## 范围与口径

趋势展示连续的源 Tag 窗口，UI 每窗 256 条，API 最多 512 条。按相同类型及
Stream ID 找最近前驱，前窗回查最多 4096 条，无源 Stat/ReadAt 和完整索引复制。
窗口、部分索引与前驱未知状态分别呈现。DTS 不排序、不展开回绕；载荷声明与
实际字节分别显示；时间观察项不是新增解析诊断，也不代表解码帧或码率。

图中点、窗口 Tag 选择和观察项可定位现有详情；Tag/Payload Hex 使用实际范围，
不依赖详情可解析。趋势窗口和已完成 Summary 保留，普通 Tag 跳转退出筛选。
5 秒通信等待、显式重试以及关闭/换文件/源变化后的旧回复隔离保持有效。

## 本地证据

`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-trends`

浏览器合成宿主仅验证前端交互；Windows 生产程序另行验证真实文件选择和
Wails→Go→详情/Hex 链路。CI 配置的本地等效检查不冒充远程 GitHub Actions 执行。

## 最终生产产物身份

以 `build-identity-release.json` 为最终生产构建记录：

| 项目 | 记录 |
| --- | --- |
| 产物 | `C:\Users\swk01\go\src\MuxRay\desktop\build\bin\MuxRay.exe` |
| 目标 / 长度 | Windows amd64 / 12859392 byte |
| SHA-256 | `200CE9D7F61C12365E92C574AB2A1D20A763BA6CE3B62ECDADDC2BEB0CEA8AFB` |
| 构建记录时间 | `2026-09-27T15:35:08.1975273Z` |
| 命令 / 返回码 | `wails build -clean` / 0 |
| 内嵌前端资源 | `index-BpScZ2dD.js`、`index-CV99v9p3.css` |

本次 release 完成 bindings、tsc、Vite 和生产应用构建，devtools 为 false。
原生 `002–037` 使用 verified 构建 `66DEF7DB…`（11325440 byte），详测 mixed；
`038` 起使用上述 release，验证其余四样本和关闭。两版最后的源码差异为 SVG
命中区 CSS 显式设置 `pointer-events: fill`；构建命令/参数也可能不同，不能将
二进制长度差异归结为 CSS。更早 identity/screenshot 仅保留迭代历史，不作为
最终产物身份。release 的密集点选择与大坐标 Hex 已重新进行浏览器复核。

## 自动测试与 CI

| 检查 | 本地结果与证据 |
| --- | --- |
| 根 Go 模块 `go test ./...` / `go test -race ./...` / `go vet ./...` | 通过；`root-test.log`、`root-race.log`、`root-vet.log` |
| desktop Go 模块同三项检查 | 通过；`desktop-test.log`、`desktop-race.log`、`desktop-vet.log` |
| 前端 `npm test` | 128/128，通过；`frontend-test-verified.log`，包含趋势超时/迟到隔离、精确大整数、Tag/Hex 交错请求回归检查 |
| 前端类型检查及生产构建 | verified 构建通过，最后 CSS 修复后再次完成 release 构建；`native-build-verified.log`、`build-identity-release.json` 与上述最终资源身份 |
| JSON 契约验证 | 通过；`contracts.log`，现有 Tag/detail/AAC/AVC/MP3/Summary 合同仍有效 |
| Python 所有 `*fixtures_test.py` | 20/20，通过；其中趋势新增 6 项，独立解析 FLV 字节并核对上下文预算、跨窗前驱与拒绝覆盖 |
| 合成 host 契约测试 | 16/16，通过；筛选 8 项、趋势 8 项，包含脚本独立注入、512 点上限、精确超大坐标和延迟会话身份 |
| PowerShell gate 与 SummaryRecovery 自测 | Windows PowerShell `5.1.26100.9549`、PowerShell `7.6.5` 均通过；只验证工具元数据、收据顺序和拒绝错误输入，不是本次原生 GUI 结论 |

工具检查的工作目录、原样命令、退出码和输出保存在 `tool-validation-final.log`。
主要复现入口为：

```powershell
# 仓库根目录
python -B -m unittest discover -s tools/desktop-native -p '*fixtures_test.py' -v
# desktop/frontend
node --test test/browser-filter-fixture.test.mjs test/browser-trend-fixture.test.mjs
```

CI 新增独立的 `desktop-windows` 非 GUI job，包含 npm ci/test/build、合成 host
契约检查、desktop Go test/vet、Python fixture 测试和 PowerShell 5.1/7 自测。
有 gcc 时运行 desktop race；不可用时会明确输出跳过通知。本机 desktop race
已通过。YAML 解析及与 HEAD 对照确认既有根模块两个 jobs 未改动；新增远程 job
尚未推送或执行，因此本记录不声称 GitHub Actions 已通过。

## 合成浏览器已验证范围

以下通过真实浏览器交互观察，宿主数据与响应由 `browser-fixture.mjs` 构造，
不调用 Go parser、不接触原生源文件。普通趋势模式与真实 fixture 使用相同的
类型、DTS、StreamID 和大小事实，但源 offset 特意超过 JavaScript 安全整数。

| 场景 | 已观察结果 | 证据文件 |
| --- | --- | --- |
| 连续窗口与跨窗前驱 | `[0,256)` 256 点，下一窗 `[256,300)` 44 点；第二窗回查 `[0,256)` 256 条；保留重复、回退、2001 ms 正差、独立 StreamID 及 uint32 最大 DTS | `browser-first-window.txt`、`browser-second-window.txt` |
| 阈值边界 | 阈值设为 2001 后，差值恰为 2001 的 Tag 258 不再算大间隔；条件为严格大于阈值 | `browser-threshold-equality.txt` |
| Summary、详情和 Hex | ready Summary 保留；从趋势异常定位 Tag 257，显示 DTS 4900、前驱 255、差 -100，并进入详情；Payload Hex 精确定位实际 9 byte | `browser-anomaly-detail.txt`、`browser-anomaly-hex.txt` |
| 有界回看 | 起点 4097，回查 `[1,4097)` 恰 4096 条；Video 4097 前驱 1 可用，Audio 4098 与 Other 4101 显示预算未知；窗口内 4099 恢复已知前驱 | `browser-sparse-context.txt` |
| 载荷截断及单点绘图 | 2 Tag 窗口中，Tag 1 实际 2 / 声明 10 byte；声明位置补空心点后可见；Hex 仅选择实际 2 byte | `browser-truncated.txt`、`browser-truncated-accepted.png`、`browser-truncated-hex.txt` |
| release：密集点准确选择 | 窗口 `[9007199254740993,9007199254741249)` 的首点被准确选为 Tag 9007199254740993；详情显示同一 ID，DTS 1860、差40、offset 909727124728840293 | `browser-dense-hit-release.txt` |
| release：超大坐标 Hex | 同一 Tag 的 payload 从 909727124728840304:0 开始、72 bit、结束 909727124728840313:0；Tag 跳页回复到达后新的 Hex 选择仍保留 | `browser-huge-hex-release.txt` |
| 超时与显式重试 | 首次趋势请求失去响应后出现通信失败及“重试趋势查询”；显式重试返回 `[0,256)` 的256点 | `browser-timeout.txt`、`browser-timeout-retry.txt` |
| 关闭后迟到回复隔离 | 发出延迟请求、loading 时关闭文档；超过1.8秒的宿主延迟后仍为无文档页面，没有恢复旧趋势 | `browser-delayed-close.txt`，结合本次操作者动作顺序记录 |

浏览器最后复核同时检查实际 computed style：命中区 `pointer-events` 为 `fill`，
宽度约 3.112941176；`browser-hit-style-release.json` 同时保存实际加载资源和
前两点的精确 label；真实点击和 Hex 操作也通过。旧 verified 文件保留前次
复核，accepted 截图保留声明点修复时的画面；不将其身份混写成 release。
静态空页面本身不证明等待时长；迟到隔离
结论同时依据操作者请求、关闭和等待过程，以及自动化迟到回复回归测试。

本轮浏览器验证发现并修复三处问题：

1. 从趋势选 Tag 后立即定位 Hex，较晚返回的 GetTags 会清空新的 Hex 选择。
   现在保留到达的索引页，并按较新的读操作意图隔离旧详情/Hex 副作用；两项
   控制器交错请求回归测试及最终超大坐标 Hex 复核通过。
2. 单点轨道的声明大小路径只有起笔位置，没有可见线段，导致实际2/声明10时
   看不到声明10。现在声明大小有独立空心点，实际字节仍用实点，不填补缺失数据。
3. 密集点的命中区域相互覆盖，可能选到相邻 Tag。现在命中宽度随窗口点间距
   有界收窄，装饰点和线不拦截点击，并明确设置命中矩形 `pointer-events: fill`
   避免键盘焦点描边扩大点击范围、覆盖邻点；release 首点选择及精确 Tag ID 复核通过。

## Windows 生产原生验收

通过真实 Windows 文件选择器、生产 Wails IPC、Go 索引/详情和 Hex 完成以下
五样本流程。样本位于证据目录 `fixtures-001`，完整元数据与 SHA-256 在
`fixtures.json`。表内数字为证据文件名前缀，均保留捕获 UTC、MuxRay 窗口身份
和 UI 树；有截图的观察还可核对实际画面。

| 样本 / 构建 | 实际通过的覆盖 | 证据前缀 |
| --- | --- | --- |
| mixed，300 Tag / verified | 6496 byte；Summary ready 覆盖300 Tag，类型153/143/2/2。首窗256点，次窗44点、前驱回查256条；原始DTS与同类型DTS差可切换。阈值2001后观察项为8个，排除等值差的258。进入详情、字段Hex、返回趋势和关闭后，状态与范围保持正确 | `007–018`、`024–027`、`032–037` |
| mixed AVC / verified | 趋势异常定位257，DTS4900、前驱255/DTS5000、差-100；真实AVC详情与上下文完整。字段Hex选择 `[5601,5610)`、72 bit，实际窗口 `[5601,6113)`；可返回趋势 | `019–024` |
| mixed AAC / verified | 趋势定位8，前驱6/DTS20，raw AAC 封装详情保留；字段Hex `[236,240)`、32 bit，实际窗口 `[236,748)`，返回趋势后继续操作 | `027–032` |
| mixed MP3 / verified | 趋势点选择16，前驱15/DTS152；固定头显示128 kbit/s、44100 Hz、singleChannel。字段Hex `[436,441)`、40 bit，实际窗口 `[436,948)`；Summary及返回链接保留，随后关闭回空态 | `033–037` |
| sparse，4354 Tag / release | 65400 byte；`[4097,4353)`256点，回查`[1,4097)`4096条。Tab聚焦趋势点、Enter准确选择4097，前驱1可用；选择4098明确预算未知，不当作零差；差值图不绘制未知差。末窗`[4353,4354)`1点、回查1条；EOF`[4354,4354)`0点、回查0条；关闭 | `040–053`，关键 `045–051` |
| structures，7 Tag / release | 128 byte；仅3个结构观察项：Tag1/3 PreviousTagSize不一致，Tag6缺失。未先读取详情即可从趋势直接打开Tag6 Hex，选择及实际窗口均为`[115,128)`、13 byte/104 bit，没有推测不存在的尾字段；关闭 | `055–061`，关键 `057–059` |
| truncated，2 Tag / release | 41 byte；载荷图实际2与声明10的实心/空心点同时可见，保留部分索引提示。Tag1同时为Payload截断/PreviousTagSize缺失；不依赖详情，直接Payload Hex精确选择`[39,41)`、2 byte/16 bit，实际字节`AF 00`、EOF；关闭 | `063–070`，关键 `066–068` |
| empty，0 Tag / release | 13 byte；`[0,0)`0点、前驱`[0,0)`0条、0观察项；三个趋势分页按钮和Tag定位select禁用，明确显示已到索引末尾；没有上个文件的趋势、详情和Hex残留 | `072–074` |
| 最终关闭 / release | 关闭最后文档后回无文档空态，没有残留结果 | `076-release-final-closed` |

mixed 详测在最后 CSS 调整之前完成，release 未重复全套 codec 流程；其后原生
趋势点选择采用 Tab/Enter，密集鼠标命中由 release 浏览器实际点击复核。上述
范围不扩展为解码/播放验证，也不声称所有额外输入组合均已原生测试。

截图和 UI 树可能在一次采集中略有先后：例如 `035` 的截图已显示 MP3 Hex，
但该次树仍有“未选择字段范围”；`036` 的后续树确认 `[436,441)`及实际窗口。
这里使用连续证据组合，不把单个文件名当作状态完成证明。

`010–030` 共21份 JSON 已整理：14张内嵌截图按实际 `image/jpeg` 和 JPEG 文件头
校验后原字节抽取为同名前缀的 `.jpg`，没有图像重编码；`screenshots[].url`
仅替换为同目录图片路径，其它解析后的状态保持相同，7份无截图 JSON 未改动。
`image-extraction-010-030.json` 保存截图ID、路径、MIME、长度、图片SHA-256、
JSON转换前后SHA-256、捕获时间与窗口身份；提取脚本为 `extract-010-030.py`。
后续截图同样应按实际MIME、捕获时间及所属窗口审计，不混用旧构建或其它窗口。
