# H.264 VUI 与 MP3 深层详情验收

日期：2026-10-07。设计和预算见 [ADR-023](decisions/023-deeper-codec-details.md)。
上一阶段媒体曲线与 Linux 回归固化已整理为本地提交 `01d157a`。
本轮增加选中 Tag 的深层详情，不改变默认 CLI detail v1 或 Summary v1/v2。

## 使用与边界

桌面重新打开 FLV 并选择 AVC 配置/SPS 或 MP3 Tag，即可看到新增字段。
CLI 显式启用新合同：

```bash
GOTOOLCHAIN=go1.27.0 go run ./cmd/muxray detail --schema-version 2 --tag 1 --pretty sample.flv
```

H.264 增加 SPS VUI 的 SAR、色彩、时序、NAL/VCL HRD、bitstream restrictions
及 SPS 尾位检查。基础 SPS 与 VUI 完整性分别报告，未知字段不填零；CLI 保留
跨防竞争字节的分段位映射，桌面沿用 SPS 摘要展示并可从整个 SPS 定位 Hex。
时序/HRD 均为码流声明，不等于实测帧率或码率；未增加完整 PPS、slice 或图像解码。

MP3 检查当前 Tag 内至多 64 帧和 1 MiB 覆盖范围；识别首帧 Xing/Info/VBRI，
检查受保护帧的头部末 16 位与 side-info CRC。保留未检查范围，free-format
不猜码率和帧长。Xing/Info 声明总帧数与 Tag 内实际帧数分开；CRC 不验证
音频主数据、bit-reservoir 或可解码性。两类详情都保持既有取消、源变化及展示预算。

## 样本与独立核对

用户同意生成样本；本轮没有业务录制样本。所有媒体及原始证据位于忽略目录
`.cache/codec-detail-20261007/`，生成命令、哈希和预期值分别保留在 `generate.py`、
`commands.json`、`source-hashes.json`、`expectations.json`。

| 样本 | 验证事实 |
| --- | --- |
| `h264-sar-bt709-hrd.flv` | 160×96、SAR 4:3、BT.709；时序 1/50，NAL HRD 声明 128000 bit/s、CPB 256000 bit |
| `xing-vbr-single-tag.flv` | 同一 Tag 的 10 个物理 MPEG 帧，Xing 声明 9 帧 |
| `info-cbr-single-tag.flv` | 同一 Tag 的 10 个物理 MPEG 帧，Info 声明 9 帧 |
| `xing-vbr-tail-truncated-single-tag.flv` | FLV 结构完整，第 10 个 MPEG 帧少 17 字节；9 个完整边界，244 字节未决范围 |
| `crc-match-mismatch-structural.flv` | 两个结构完整帧，CRC 分别匹配和不匹配；零填充主数据，不作为可解码样本 |

前三个正常 FLV 及对应两个原始 MP3 均经 FFmpeg `-xerror` 解码返回 0。
截断变异与 CRC 结构样本不宣称可播放。CRC 固定向量为头后两字节 `90 64`
加 32 字节零 side-info，预期 `0x576f`；第二帧存储值改成 `0x576e`。

`validate-cli.py` 使用独立 FFmpeg trace 和原始字节预期，核对 38 个 VUI 字段
及源位映射、30 个 MP3 帧边界和 Xing/Info 声明/TOC 范围；8 份 v1/v2 文档
均通过 Schema。当前二进制默认输出逐份等于显式 v1；旧合同另由未改动的 v1
golden 回归约束。截断样本 v2 返回部分 JSON/退出 2，v1 保留原首帧头语义/退出 0。
结果见 `cli-validation-result.json`。VBRI 使用核心结构测试，未提供自然编码样本。

独立复核发现并修复 Xing 字段边界截断被包装成源 I/O 错误的问题；五个截断点
与真实 ReaderAt 错误对照通过，记录在 `review/probe-after-vbr-fix.log`。

## 自动验证

- 根模块 `go test -race ./...` 与 `go vet ./...` 通过。
- desktop 模块 `go test -race -tags webkit2_41 ./...` 与对应 vet 通过。
- 前端 150 项测试、TypeScript 检查、Vite 生产构建通过。
- Python 工具测试 14 项运行、1 项按条件跳过，其余通过；JSON 合同正负向检查通过。
- SPS VUI fuzz 10 秒、194009 次执行；MP3 frame sequence fuzz 15 秒、1279016 次执行通过。
- CLI Windows amd64 与 macOS arm64 交叉编译通过；不代表两平台原生桌面验收。
- 新增完整/截断 v2 Schema 和 golden；旧 v1 Schema/golden 未修改。
- 两个深层 fuzz 入口已加入 CI 配置，远端 CI 尚未触发。

完整帧详情测试使用独立结构帧 fixture，Summary/曲线原有首帧头 fixture 保持不变。
关键日志为 `root-race-final.log`、`root-vet-final.log`、`contracts.log`、
`production-final-build.log`；MP3 fuzz 原始日志位于 `.cache/mp3-deep-fuzz.log`。

## Linux 原生六轮

生产构建：`GOTOOLCHAIN=go1.27.0 ./desktop/build-production.sh`。
二进制 `desktop/build/bin/MuxRay`，SHA-256：
`641a6bbc37c3ca530b15299af688dd9342935b14e81e69d594a93a39cfbb3e15`。

沿用本机 Go 1.27.0、Node 22.22.2、Wails v2.14.0、GTK/WebKitGTK 环境。
正式 `tools/desktop-native/linux_smoke.py` 在独立 Xvfb、D-Bus、XDG 目录及内存
GSettings 中操作真实 GTK/WebKit。每轮执行原生打开 → 详情 → Hex → 原生保存
→ 关闭文件 → 窗口正常退出；未注入 WebView JS 或模拟 IPC。

| 证据目录 | 内容 | 耗时 |
| --- | --- | --- |
| `native-final-default` | 默认结构样本、AVC NAL Tag 4 | 14.65 s |
| `native-final-h264` | Tag 1 的 VUI/HRD，109 个详情字段 | 12.26 s |
| `native-final-xing` | 10 个物理帧与 Xing 声明 | 10.52 s |
| `native-final-info` | 10 个物理帧与 Info 声明 | 10.73 s |
| `native-final-truncated` | 9 个完整帧、尾帧截断诊断与未决范围 | 8.91 s |
| `native-final-crc` | match/mismatch、CRC 源位置及诊断 | 8.91 s |

六轮导出均通过快照 Schema、源字节和新增语义检查；源哈希未变化、关闭后源 FD
全部释放、父进程退出 0、已观察应用子进程消失，无强制清理或残留本轮进程。
`verify-native.py` 固化字段断言，结果为 `native-validation-result.json`。
只读检查确认系统辅助技术和屏幕阅读器开关仍为关闭。

复现单轮（输出目录必须不存在）：

```bash
/usr/bin/python3 tools/desktop-native/linux_smoke.py \
  --source "$PWD/.cache/codec-detail-20261007/h264-sar-bt709-hrd.flv" --tag 1 \
  --output-dir "$PWD/.cache/codec-vui-native-new"
```
