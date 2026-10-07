# Linux 回归固化与分页媒体曲线验收

日期：2026-10-07。范围：生产 Linux 桌面、生成媒体试用、分页媒体码率与声明 GOP
曲线；设计见 [ADR-022](decisions/022-bounded-media-curves.md)。本轮没有提供业务
录制，用户同意生成样本，因此以下全部标记为 generated，不计作真实业务语料验收。

## 实现与复现

完成媒体概览后点击“查看媒体曲线”，每 256 个源 Tag 汇成一点，每页 64 点。
提供音频、视频、MP3 覆盖码率及声明 GOP 平均/最长时长，点击点可定位闭合 Tag
和实际原始字节。翻页不读取源，也不受事件保留前缀限制。未知值不补零；源变化、
重分析、取消和关闭清除旧数据。既有 CLI 输出与诊断快照 v1 格式保持不变，后者
不导出曲线，界面已有明确说明。

正式原生回归命令已落到 `tools/desktop-native/linux_smoke.py`，并接入
`.github/workflows/desktop.yml` 的 Linux job：

```bash
GOTOOLCHAIN=go1.27.0 ./desktop/build-production.sh
/usr/bin/python3 -B tools/desktop-native/linux_smoke.py \
  --output-dir "$PWD/.cache/linux-smoke-new"
```

工具使用真实 GTK 打开/保存、WebKit/Wails IPC、详情/Hex、现有 Schema 与源字节
检查器，核验源 FD 释放、父进程退出 0 和已观察子进程消失。全会话默认 180 秒，
失败保留现场，按 PID/startTicks 清理本轮后代。完整依赖、输入上限和验证边界见
[工具说明](../tools/desktop-native/README.md#linux-生产程序-smoke)。本地命令已验证；
远端 GitHub Actions 尚未触发，不能把本地结果称为远端 CI 已通过。

## 环境与产物

- Linux Mint 22.3 / Ubuntu 24.04 基础环境，GTK 3.24.41、WebKitGTK 2.52.6。
- Go 1.27.0、Node 22.22.2、Wails v2.14.0，生产构建使用 `webkit2_41`。
- 生产文件：`desktop/build/bin/MuxRay`。
- SHA-256：`cf05d7c591a98b42570a29d0116d3af1cd519e5a292a442614484750fe50f3ab`。
- 本轮证据根目录：`.cache/linux-media-curves-20261007/`，媒体和原始证据不入 Git。

## 生成样本

`generate_samples.py` 和 `media/commands.json` 保留 FFmpeg 生成参数、命令及退出码。
120 秒样本拼接三段 40 秒、160×90、25 fps 的 H.264/AAC 媒体，目标视频码率分别
96/320/64 kbps，GOP 分别 25/100/50 帧。原样本经 FFmpeg 完整解码返回 0。
另一个副本从 Tag 4300 起将 DTS 减少 10000 ms，专门检验时间戳回退。
编码目标不作为容器覆盖码率的精确期望，拼接边界也不强行修正成整数秒。

| 样本 | 字节 / Tag / 曲线点 | SHA-256 |
| --- | --- | --- |
| `media/generated-bitrate-gop-switch.flv` | 3027585 / 8632 / 34 | `c2d7753d2dc2acad3c6c5141e700abb595d8291644efcb49205f7323d87e8276` |
| `media/generated-timestamp-reset.flv` | 3027585 / 8632 / 34 | `6196d4413cd0b857be8e7d8fc9eef3b6cca96a85d906533eaca4c371860ae70c` |
| 既有 40 分钟 `generated-long-avc-aac-2400s.flv` | 41698926 / 172504 / 674 | `50fafbfd48f680556107c4fabce01f5515dce1be1a704310a85da00c3b278786` |
| 既有 `generated-mp3-30s.flv` | 499066 / 1150 / 5 | `469f3bc39dcdfa7cfd15df578486a46e17f656bcf09a7d9d3b607f906c620e88` |
| `oracle/generated-config-barriers.flv` | 4205 / 265 / 2 | `aeb537b4062592698b08c8b1081ff1e46f3bbb95678959613c5ed79b059ebac3` |

40 分钟与 MP3 文件位于既有 `.cache/linux-extended-20261007/media/`，完整路径也保留
在 oracle 清单。配置样本是结构数据，复用有效 avcC，不能称为可播放录制。

## 数值与兼容性

`oracle/verification.json` 核对上述 5 个样本，共 749 点、20223 个标量字段，全部
与独立计算相等。Python 只读取原始 FLV framing 与已知编码前缀：离线分组相同
DTS，按相邻正向组计算 DataSize 覆盖，再归入闭合 Tag 的窗口；GOP 使用声明
关键帧配对并排除明确的时间戳/配置/EOS 边界。期望值不读取 Go 曲线结果生成。
Go probe 另逐项比较新旧完整 Summary、进度序列和所有 ReaderAt 偏移/长度，均相等。

- 变码率样本 70 个声明关键帧、69 个已闭合 GOP；最长 4080 ms 来自拼接时间戳。
- 回退样本音视频各回退一次，闭合 GOP 降为 68；窗口 16 声明 1、闭合 0，时长未知。
- 40 分钟样本声明 1200、保留 1024、闭合 1199。第 1025 个关键帧 Tag 147200
  所在窗口 575 及其后共 99 点继续正确，包含 176 次后续闭合。
- MP3 的 Audio 与 MP3 逐点覆盖相等，首帧头事件仅保留 256 不影响余下曲线。
- 结构补充样本的相同配置不截断 GOP，改变配置及 EOS 分别截断；7 个声明、4 次闭合。
  原变码率样本只有一个 AVC 配置头，所以不能单独证明配置切换行为。

该 oracle 只覆盖已知编码与固定 MP3 参数，不是通用损坏媒体解析器，也不证明 IDR、
解码图像或音画同步。算法和限制见 `oracle/report.txt`；命令及产物哈希见
`oracle/evidence.json`。

## 自动检查

- 根模块 `go test -race ./...`：16 包通过；`go vet ./...` 无诊断。
- desktop `go test -race -tags webkit2_41 -count=1 ./...`、对应 vet：通过。
- 前端 TypeScript、Vite 生产构建与 150 项测试：通过。
- 既有浏览器模拟宿主合同测试：25 项通过；它们不代替原生 UI。
- Python 原生工具：122 项运行，118 通过、4 项按平台条件跳过；新增 smoke 边界测试 6 项通过。
- JSON 合同检查保持通过，快照 v1 Schema/golden 未修改。

对应日志分别为 `core-race.log`、`core-vet.log`、`desktop-race.log`、`desktop-vet.log`、
`frontend-tests.log`、`build.log`、`browser-fixtures.log`、`native-tools.log`、`contracts.log`。

## 原生验证及边界

本轮曲线驱动使用 AT-SPI/X11 操作真实生产程序，不注入 WebView JS、不模拟 IPC。
逐页读取实际可访问控件的精确点名，与已独立校验的 Go probe 比较；再选点进入
详情/Hex、原生保存快照，检查源字节、Summary 保留、源哈希、FD 释放与正常退出。
曲线不进入快照 v1，因此曲线证据另保存在 UI 树、截图和 native-result 中。

最终原生结果全部通过；以下目录均位于本轮证据根目录：

| 目录 | 实际检查 | 耗时 |
| --- | --- | --- |
| `native-smoke-isolated` | 正式默认结构样本，Tag 4 详情/Hex、原生导出、关闭/退出 | 33.09 s |
| `native-switch-isolated` | 34 点全部五指标标签、末窗 GOP 定位 Tag 8484、详情/Hex/导出 | 55.41 s |
| `native-reset-isolated` | 34 点全部五指标标签，回退窗未知 GOP 正确保留，Tag 8484 联动 | 58.80 s |
| `native-long-isolated` | 全部 11 页视频码率/GOP 最长指标共 674 点；首页及末页另查全部五指标；末窗 673 定位 Tag 172356 | 178.59 s |
| `native-mp3-isolated` | 5 点全部五指标，音频/MP3 一致，视频/GOP 未知，末窗定位 Tag 1024 | 41.05 s |

五轮均有 `native-result.json` 与 `result.json`，运行中 EXE 哈希与上列生产二进制
一致。所有曲线轮次均验证 Summary 在 Tag/Hex 后保留、原生快照通过 Schema/源
核验、源哈希未变、关闭后源 FD 为空、父退出 0、已观察后代消失。均无强制清理，
最终剩余进程为零。`curve-progress.json` 保留按页按指标核对点数，`curve-ui.json`
和 `curve.png` 保留生产 UI 证据。

失败清理另用**专用测试程序，非产品二进制**验证：
`native-timeout-isolated` 在 30 秒整轮时限后如预期返回 1，总耗时 33.07 秒；
忽略 TERM 的自建子进程收到 KILL，按 PID/startTicks 再查无剩余进程。

过程中的失败没有删除或计作通过：

- `native-switch-001` 的五指标数值已相等，但驱动把 SVG 的 AT-SPI `toggle button`
  错当 `push button`，选点超时；只修正驱动角色。清理尾部还暴露孤儿进程变为
  zombie 的时序空隙，正式脚本补上最多两秒的收养/reap 等待。
- `native-reset-001` 的 AT-SPI Registry 初始化超时。只读检查另确认旧启动顺序
  `dbus-run-session → xvfb-run` 使应用在虚拟屏，而总线激活的辅助进程继承 `:0`。
  因此先前 `native-long-001` 主动中止，其数据不计最终验收。该隔离缺陷已修正为
  `xvfb-run → dbus-run-session`，并清除继承的 AT-SPI 定位环境变量；不单凭超时
  日志断言它是唯一失败原因。
- 修正后的 `native-long-isolated/helper-display-evidence.json` 从 `/proc` 实际核对
  MuxRay、D-Bus、AT-SPI launcher/Registry 均在 `:99`，全部最终轮次使用新顺序。
  `GSETTINGS_BACKEND=memory` 与独立 XDG 目录持续启用；末次只读确认系统
  toolkit-accessibility 和 screen-reader-enabled 仍为 false。
- 首次独立 oracle 未支持长样本 avcC 的已知 High-profile 扩展而停止；补充严格
  的已知 4:2:0/8-bit 扩展检查后重验通过，原失败日志保留，未修改产品解析器。

Wails 原样生成的 `models.ts` 保留生成器空白行制表符，以使重新生成的绑定能
逐字匹配 CI；其他改动通过 `git diff --check`，Go 文件均通过格式检查。

这些结果只证明本机 Linux X11/Xvfb 的有界流程，不替代物理 GPU、Wayland、长期
资源监测或真实业务语料验收。本轮不修改 Windows 选择器原型；此前 Windows
取消退出与六轮回收验收仍需 Windows 环境处理，也未完成 macOS 原生验收。
