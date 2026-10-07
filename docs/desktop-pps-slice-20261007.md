# PPS/slice 前缀与编解码器回归验收

日期：2026-10-07。设计见 [ADR-024](decisions/024-bounded-pps-slice-prefix.md)，
复现命令见 [回归工具说明](../tools/codec-regression/README.md)。

## 本轮结果

桌面所选 AVC Tag 增加 PPS 必要字段，以及 NAL 类型 1/5 的 slice 头前缀：
I/P/B 类型、首宏块、帧号、条件 POC 字段及所引用 SPS/PPS 的原始位置。
CLI 使用 `detail --schema-version 3 --tag <id>`；默认 v1、AVC/MP3 v2、
Summary v1/v2 和旧 Schema/golden 未修改。

`prefixComplete` 只覆盖当前支持的前缀，不代表完整 slice 头或图像可解码。
PPS 扩展、FMO、后续参考列表/预测权重语法及图像正文仍在范围外。
每 slice 最多读 256 字节、每包累计 64 KiB；参数重放最多 8,192 个 Tag 和
1 MiB 实际读取。缺失、损坏替换、EOS、编码切换或预算不足不会借用旧参数。
可靠的 NAL 长度与参数上下文分别显示状态，长距离查找不因此伪装成功。

## 固化与独立对照

此前缓存内的生成与验收脚本已移为正式 `tools/codec-regression/` 工具。
生成、v1/v2 CLI、v3 CLI、六轮原生和两轮新前缀验收具有独立入口，要求新输出
目录并保存失败证据。Linux CI 接入同一流程；slice fuzz 也加入根模块 CI。
本轮没有推送或触发远端 CI。

样本为本机 FFmpeg 生成的测试媒体，不是业务录制文件。正常 H.264/MP3 样本
有独立解码检查；截断变异和零主数据 CRC 结构样本不宣称可播放。
两次独立生成的 9 个媒体文件和 `expectations.json` 哈希一致；跨 FFmpeg
版本仍允许合法字节差异。生成媒体未进入 Git。

v1/v2 五样本通过 10 份 Schema，38 个 VUI 字段及源 bit 映射一致，默认 CLI
输出等于显式 v1。v3 对照两份 H.264 的 **60 个 slice、20 个多 slice Tag、
315 个映射字段**，覆盖 I/P/B、PPS 和条件 POC；缺失参数、坏配置替换和截断
三个变异均返回预期部分结果。验证器要求所有支持的条件字段存在，删除字段
和篡改源位置的反例必须失败。

## 自动检查

- 根模块和 desktop 模块 race、vet 均通过；desktop 使用 `webkit2_41`。
- 前端 150 项测试、TypeScript/Vite 生产构建通过，生成 bindings 无差异。
- 编解码器工具 16 项、Linux smoke 工具 7 项测试通过。
- 所有 JSON 合同及正负例通过；新增三份独立 v3 golden。
- 核心测试覆盖源顺序、多 slice、同 ID 参数替换、POC 0/1/2、场图像、
  colour plane、redundant、EPB 映射、截断、取消/源 I/O 错误和累计预算。
- `FuzzSlicePrefixes` 15 秒、626457 次执行无崩溃；该项证据为工具输出，
  没有单独落盘日志。

## Linux 原生八轮

使用最终生产构建 `desktop/build/bin/MuxRay`，SHA-256：
`de22cacf5fedd8a76c45bfcb96131a5f51fe430e8ea7421a9de49f686fa760f9`。
每轮在私有 Xvfb/D-Bus、内存 GSettings 中执行真实 GTK 打开、详情、Hex、
原生保存、源文件关闭和窗口退出；未注入 WebView JS。

| 场景 | 结果 | 耗时 |
| --- | --- | --- |
| 基础结构样本 | 有效最小 slice 前缀，50 个字段 | 27.88 s |
| H.264 VUI/HRD | 配置完整，135 个字段 | 25.47 s |
| MP3 Xing | 帧边界及声明核对通过 | 16.30 s |
| MP3 Info | 帧边界及声明核对通过 | 15.96 s |
| MP3 尾帧截断 | 正确显示部分结果 | 17.73 s |
| CRC match/mismatch | 正确显示不匹配诊断 | 13.28 s |
| PPS 配置 | 11 个前缀字段及源 bit 一致 | 17.72 s |
| B 帧多 slice | 两个 slice 共 10 个编码字段、不同首宏块和参数集引用一致 | 16.83 s |

八轮导出均通过快照 Schema 和源字节检查；源哈希不变、源 FD 释放，应用退出
码为 0，无观察到的应用子进程或本轮所属进程残留。Info 一轮由测试框架向一个
非应用辅助进程发送 SIGTERM；记录保留于 `harnessCleanupSignals`，没有将
应用强杀算作成功。最终验证器增加必须观察主应用进程身份的门槛后，再次检查
全部八份原生证据通过。只读确认系统辅助技术、屏幕阅读器开关仍为关闭。

早期 `native-final` 的基础轮应用正常退出，但 wrapper 错把测试环境自身清理
判为失败；证据保留。最终八轮使用重建后的同一二进制，结果独立保留。

所有本机证据位于忽略目录 `.cache/codec-regression-portable/`：

- `samples/`、`samples-final/`：生成命令、工具版本、原始 FFmpeg 日志及预期。
- `cli-release/result.json`、`cli-v3-release/result.json`：最终 CLI 对照。
- `native-final-2/result.json`、`native-v3-final/result.json`：六轮加两轮结果。
- `tool-checks-final/native-revalidation.json`：最终工具重新核查八轮及脚本哈希。
- `tool-checks/repeat-generation.json`：两次生成身份比较。
- `checks-root-final/`：race/vet、前端、构建、合同日志及二进制哈希。

本轮原生结论仅覆盖 Linux。Windows 辅助选择器取消退出问题及 Windows/macOS
原生验收不在本轮完成范围内。
