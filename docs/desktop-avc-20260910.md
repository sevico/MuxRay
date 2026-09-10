# AVC 桌面详情与 Windows 原生验收（2026-09-10）

本批以 `91675f3` 为基线，完成 [ADR-014](decisions/014-bounded-desktop-avc-context.md)、
有预算的 AVC 上下文读取与桌面详情，以及生产 Windows 窗口和受控 AVC 阻塞关闭验收。
所有本次 MuxRay 进程均已通过原生窗口正常退出；没有推送远端。
此前 AMF0/AAC 与扫描取消、阻塞退出记录仍见
[上一批验收](desktop-details-20260910.md)，本批不将其计作重新执行。

## 实现与边界

- 提取 `internal/avccontext`，CLI 使用零搜索限额，保留既有完整索引回溯策略。
  桌面经 `session.Tag` 读取单个已提交记录，不复制整份索引、分页数组或诊断。
- 每次桌面查找至多 8,192 个前置记录、32 次配置解析、累计 1 MiB 源读取。
  实际返回字节计入预算，含所选包探测与重复读取；读前检查整个请求。
  找到、前缀穷尽、预算耗尽分别为 `found`、`missing`、`budget_exhausted`。
  已达上限但恰好穷尽前缀不误报预算耗尽；尚有工作被预算拒绝才报告限制。
- 回溯和所选 Tag 投影共享原文档 worker、3 秒合作式截止和读前/读后 Stat 检查。
  不重开文件、不创建第二个 worker，也不增加跨请求配置缓存。
  阻塞 I/O 返回前，Close 仍等待实际工作结束。
- AVC 详情包括 FLV 包头、有符号 CTS、DTS/PTS、avcC 声明、SPS/PPS 基础概要、
  原始参数集范围、NAL 长度/头/范围和配置来源。1/2/4 字节长度均支持；不猜四字节。
  来源长度声明显示准确的原始 2-bit 位置，派生值显示无单一原始位置。
- 去除 emulation-prevention bytes 后的多段 RBSP 映射不压平成连续 Span。
  首批保留参数集整体原始范围，尺寸、标识符等摘要不伪造字段位置。
  不解码 slice、视频画面或扩展深层 VUI；FLV keyframe 声明不证明 IDR。
- AVC 解析预算为 64 诊断、64 个 avcC 参数集条目、256 NAL、每参数集 EBSP 正文
  64 KiB（不含 NAL 头）、4,096 RBSP 映射项；包内 SPS/PPS 受 NAL 数量上限约束。
  另有 512 展示字段、64 展示诊断、256 UTF-8 byte 预览、256 KiB 实际 JSON 上限。
  原有八个 Wails 方法及 DTO 外壳保持兼容，新增 `avcVideo` kind；真实生成绑定无差异。

## 自动检查

所有下列已执行命令均成功。日志与命令、退出码、UTC、日志 SHA-256 索引保留在
本机证据目录的 `integration-checks.json`、`cli-verification.json` 等文件中。

| 检查 | 结果 |
| --- | --- |
| 根模块，无 CGO | `go test -count=1 ./...`、`go vet ./...`、`go build ./...` 全部 exit 0 |
| 根模块 race | `go test -race -count=1 ./...` exit 0，含 CLI goldens 和原 AMF0/AAC 回归 |
| 独立 desktop 模块 | `go test -count=1 ./...`、race、vet 全部 exit 0 |
| 前端 | `npm.cmd test` 62/62；真实绑定生成后的类型检查、生产构建成功 |
| Wails | `wails generate module`、`wails build -webview2 error -nopackage -trimpath` 成功 |
| 新上下文/文档测试 | 1/2/4 长度、配置切换/损坏回退、FramingComplete 语义、三种预算边界、跨索引块、超大 ID/offset、计数溢出、取消、I/O 错误、部分索引、前序配置读阻塞 Close、读取期间源变化均通过 |
| 新 AVC 投影测试 | 8 组顶层测试及子用例，覆盖时间精度、EPB 映射、原始跨度、预算/JSON、损坏帧、取消、源错误、opaque slice 不读取；专项 race/vet 成功 |
| CLI 合成样本 | 16 项通过；含负 CTS、显式 override、300 NAL 与 33 候选仍遵循 CLI 原有策略，以及截断索引拒绝 |
| 受控 AVC 非 GUI | `verify.ps1 -GateMode AVCDetail` 的真实文件 race 测试通过 |
| 工具兼容 | Windows PowerShell 5.1 与 PowerShell 7 的 `self-test.ps1` 均通过，含三种 gate 参数及场景错配拒绝 |

初次验收缺少 Python `jsonschema`，独立 schema 命令未执行，Python 单元测试因三个
模块无法导入而 exit 1；原始 `python-tools-tests.log` 保留该失败记录。随后按仓库
`tools/requirements-schema.txt`，用 `python -m pip` 为实际运行测试的 Python 3.13.5
安装了 `jsonschema==4.25.0`，安装与执行均使用
`C:\Users\swk01\AppData\Local\Programs\Python\Python313\python.exe`。

复验 `python -B tools/validate_contracts.py` exit 0，21 个登记的 Schema 实例、11 个
语料清单身份及负向契约检查均通过；`python -B -m unittest discover -s tools -p 'test_*.py'`
exit 0，14 项中 13 项通过，1 项非 Windows guard 测试按 Windows 平台条件正常跳过。
安装与复验日志、退出码另存 `outputs\muxray-python-schema`，不覆盖初次证据。
全局 `python -m pip check` 另报告两项既有 LangChain 版本冲突，涉及的包未在本次
安装中变更，与本项目依赖无关；该全局检查不计作通过。本次没有修改 CLI schemas、
goldens 或 Python 依赖版本声明，并已补充本地安装说明。
新的标准库 AVC 样本生成器已实际运行并由原生流程与 CLI 核验。
扩展后的浏览器 smoke 脚本通过语法检查，但 Browser 工具连接失败，未实际执行；
它不计入通过项。下述 Windows 原生流程实际执行，使用真实文件选择器、Wails IPC
和生产二进制，没有模拟宿主。未重新进行长时间 fuzz、跨平台 GUI、安装器或签名验收。

## 生产窗口结果

生产程序为 `desktop/build/bin/MuxRay.exe`，SHA-256：
`5c73a4fbf25a98fef34303c3604c2014c0f4f52e2ffd79dfd494243d13899ccc`。
原生验收 PID `32532`，程序开始于 `2026-09-10T14:53:39.9030691Z`。
环境为 Windows amd64、Go 1.27.0、Wails 2.14.0、Node 24.4.1、npm 11.4.2。
构建使用本机已有 Go/npm 依赖缓存，未下载媒体。

样本由 `tools/desktop-native/avc_fixtures.py` 在仓库外新目录生成；是合成语法样本，
不宣称可播放。`fixtures.json` 记录各 Tag 的准确偏移、时间戳和预期。

| 文件/选择 | 实际结果 | 主要证据前缀 |
| --- | --- | --- |
| details Tag 0 | 缺配置，CTS −12、DTS 5、PTS −7；保留 opaque NAL 范围与缺长度诊断 | `01-missing` |
| Tag 1 / 3 | Baseline 176×144；High Profile coded 320×192、display 320×180；EPB 计数 1/2、映射分段 2/3，摘要没有伪造原始位置 | `02-baseline`、`04-high-profile` |
| Tag 2 | 1-byte 长度，来源 Tag 1，声明位置 56:6，PTS 28，两条 NAL | `03-length1` |
| Tag 4 | 2-byte 长度，来源 Tag 3，声明位置 140:6，PTS −20 | `05-length2-negative` |
| Tag 6 | 越过不可用的 Tag 5，来源仍为 Tag 3；扫描 3 Tag，解析 2 候选 | `06-skip-bad-config` |
| Tag 8 | 切换到 4-byte 长度，来源 Tag 7，声明位置 280:6，PTS 132 | `07-length4` |
| Tag 9 | NAL 声明 20 byte，仅剩 2 byte，显示 `AVC_NAL_PAYLOAD_TRUNCATED` 和准确可用范围 | `08-nal-overrun` |
| Tag 10 / 11 | EOS 无需配置；非 AVC 视频明确不支持 | `09-eos`、`10-unsupported` |
| Tag 12 | 300 NAL 触发 256 NAL 解析上限及字段省略；显示 `AVC_NAL_UNIT_LIMIT_REACHED`，窗口仍可切换/关闭 | `11-nal-budget` |
| truncated Tag 13 / 8 | 14 Tag、1 条容器诊断；拒绝末尾 2/10 byte payload，完整 Tag 8 仍成功回溯并显示详情 | `13-partial-rejected`、`14-partial-prefix` |
| budget Tag 33 | `budget_exhausted`，扫描 33 记录、解析 32 候选、读取 292 byte；显示专用预算诊断，不推断缺配置、不猜 NAL 长度 | `15-context-budget` |
| large Tag 66 | 67 Tag，容器完整；尾 Tag offset 1073742870，payload offset 1073742881；回溯来源 Tag 0，66 记录/1 候选/48 byte，4-byte 长度，PTS 988 | `16-large-scan`、`18-large-tail` |
| change 副本 | Tag 2 先成功；仅 mtime +2 秒且内容哈希/长度不变后，Tag 4 与原 Tag 2 均被 `source_changed` 拒绝；关闭重开后 Tag 2 恢复 | `20-source-before` 至 `24-source-reopened` |
| 正常关闭/退出 | 普通文件、大文件、变化副本关闭后均可独占只读打开；生产程序通过 Alt+F4 正常退出 | `normal-close.json`、`large-close.json`、`source-close.json`、`production-exited.json` |

大文件实际长度为 **1,073,742,902 byte**，含实际顺序写入的 1,024 MiB 填充 payload，
`Get-Item` 属性为 Archive，SparseFile=false。另经 `Get-FileHash` 完整读回确认哈希，
不是仅检查写入时的增量摘要。该检查验证大偏移和保留源后的按需读取；67 个 Tag
不是巨大 Tag 数量压力测试，结构扫描也不代表逐字节读取或解码 1 GiB 音视频。

| 合成文件 | SHA-256 |
| --- | --- |
| avc-details.flv / avc-change.flv（1933 byte） | `7ea10e84f93947fbda935b22dfe6baa5a1dda3596cead4afbc6b3ce1eb24168b` |
| avc-truncated.flv（1946 byte） | `a3fec4089d243672c3a97022b3293cbcdff2ed70253831ff542cb1357623e1e5` |
| avc-budget.flv（732 byte） | `5fe53955af9344d85830c426124615c0d0b1932ace06d265c8700130f347b4ed` |
| avc-large.flv（1073742902 byte） | `8273685ba12cc63bb00bfa1868f986c0d9f69fd827e07f028f6508d88337763a` |

## 受控 AVC 阻塞关闭

独立 `AVCDetail` overlay 构建只对本 run 样本加入 gate，不进入生产源码。
样本为 504 byte、32 Tag，首 Tag 是孤立 AVC NALU；无前序 sequence header。
扫描全部 32 Tag 后仍没有 `entered.json`。点击 Tag 0 后，在 **第 67 次 ReadAt、
offset 24** 阻塞所选包的首次上下文探测。它验证 AVC 任务生命周期；前序候选
读取阶段的阻塞/取消/源变化由确定性文档测试另行覆盖，不冒充第二个原生 gate。

原生 run ID `e4ef23179c214a1789ce8ece3d99181f`，PID `34520`。
受控 exe SHA-256 `a1a6708cb5d989a263ae75fd799a6d8a35319800bdfca93b23199973ccfa9ecb`；
样本 SHA-256 `8b9d282048f76e6cc7fa6821a83ce8c02726a2dd91592b8da493b421ba4cdda9`。

1. 在原生窗口观察详情读取中；连续 pending 后显示“等待已超时”和重试按钮，窗口仍响应。
2. MarkClose 后实际点击“关闭文件”，看到“正在关闭”、32 Tag 扫描信息、新打开禁用，
   详情被清空。Release 前没有 closed 收据，独占打开失败，源仍被保留。
3. Release 后先捕获关闭中过渡帧，随后看到空状态；生成 closed 收据，独占打开成功。
4. `check.ps1 -Scenario DetailClose -ObservedState closing -ObservedTagCount 32` 通过，
   随后经原生 Alt+F4 退出。ObservedState 是操作者实测，不是校验脚本自动读取 UI。

| 收据 | UTC |
| --- | --- |
| entered | 2026-09-10T15:13:55.4382589Z |
| close-command | 2026-09-10T15:14:38.5363392Z |
| release | 2026-09-10T15:14:58.9613264Z |
| released | 2026-09-10T15:14:58.9994022Z |
| closed | 2026-09-10T15:14:58.9999048Z |

同 run、PID、process token 的收据及二进制/fixture/overlay 身份均通过校验。
截图 `25`–`30` 对应上述阶段；`29-controlled-released` 是释放后的过渡帧，
`30-controlled-empty` 才是关闭完成。受控 gate 故意忽略取消，不是自然慢盘测量。

## 证据位置与后续

本机工作根目录：`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use`。

- 证据：`outputs\muxray-avc`，含截图、原生可访问文本、日志、JSON 收据副本和哈希清单。
- 生产样本：`work\avc-fixtures-001`。
- 非 GUI 验证：`work\avc-native\verify-avc-001`。
- 原生受控构建及原始收据：`work\avc-native\close-avc-001`。

截图与文本互补；长字段页的 accessibility document_text 有截取上限，不能从
`11-nal-budget-ui.txt` 推断完整 512 行。预算诊断另有原生可见截图，精确字段/JSON
上限由自动测试确认。未将二进制、约 1 GiB 样本或本机路径证据作为项目源文件提交。

下一增量可优先补齐 MP3 桌面详情，以复用现有详情生命周期；若目标是可点击字段联动
Hex，应先定义多段 SourceSpans 选择契约，再扩展深层 RBSP 字段。源检查仍是可观察
Stat 变化检查；同身份、大小、mtime、模式均未变化的原位修改不在其保证范围内。
