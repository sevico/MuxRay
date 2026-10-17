# SEI 时序回归基线重新登记与 Windows CLI 对照

日期：2026-10-16。本轮修复 `linux-timing-navigation` 工作流无法取得旧基线的问题，
并在 Windows 本机重新完成当前 CLI 与新登记基线的兼容性对照。本轮没有运行 GUI
或远端 CI，不替代 Linux/Windows 原生界面验收。

## 基线身份

检查时仓库为完整、非浅克隆，HEAD 与 `origin/main` 均为
`f4c64cd8289516fa3338383cf6ed0f89b2a75ff3`。工作流原先引用的
`0e853bebd90f12658c1123f8703fb247cbf61530` 无法由本地对象库解析，
`git cat-file -t` 退出 128；原有 `git archive` 步骤因而无法完成。

本次新登记 [timing_baseline.json](../tools/codec-regression/timing_baseline.json)：

| 身份 | Git 对象 |
| --- | --- |
| 新基线提交 | `bc799d6afebc7206d3c7bac1e6a8cc49dc1c41c1` |
| 新基线源码树 | `0d060c10e60e3c0c6ed299a398353b1ead3f9920` |
| 引入 timing v6 的功能提交 | `bff653bf74307a0249759a5c485a29276c69a73e` |
| 本轮被比较的当前提交 | `f4c64cd8289516fa3338383cf6ed0f89b2a75ff3` |

新基线是功能提交的直接父提交，其 CLI 支持 AVC 详情 v1–v5，尚未包含 timing v6。
**没有证据证明它与不可用的旧提交源码等同。** 本次结果只确认新基线与上表当前提交
在规定样本上的输出兼容性；2026-10-13 等历史报告保留原构建身份、二进制哈希和结论，
不追溯改记为新基线，也不由缺失对象直接推断历史改写原因。

## 工作流保护

归档基线前执行 [check_timing_baseline.py](../tools/codec-regression/check_timing_baseline.py)，
验证完整对象 ID、提交类型、固定源码树、功能提交的第一父关系，以及功能提交可从
当前 HEAD 到达。任一检查失败即停止，不回退到 HEAD 或自动选择另一个版本。
成功或失败收据保存在该 CI job 的独立证据目录，现有 `always()` artifact 步骤会保留它。
该脚本仅执行本地 Git 读取，不获取缺失对象或联网。

七项新增测试以真实临时 Git 历史覆盖有效基线、缺失提交、错误源码树、非祖先功能、
误选已包含功能的版本、可变引用别名及只输出已验证提交；缺失提交用例还检查失败
收据与禁止覆盖。连同九项既有 timing 工具测试，共 **16 项通过**。

## 本轮对照结果

使用 Go `1.27.0 windows/amd64` 和 Python `3.13.5`。两个源码快照均由固定提交归档到
当前仓库之外的全新目录，再以 `go build -trimpath` 构建。归档构建本身不含 VCS 元数据，
修订身份由归档命令、归档 SHA-256 和外部收据绑定。Go proxy 与 checksum 查询关闭。
生成器和校验器沿用现有实现，没有放宽断言或替换期望数据。

- 20 个独立生成的 SEI timing 样本通过 v6 schema、上下文、部分状态与预算校验。
- 461 处字段及原始位映射通过。
- 同一批样本的 **100 份 AVC CLI v1–v5 输出逐字节一致**。
- 两个二进制、参与验证的工具源码在运行前后哈希均一致。
- 本轮覆盖 CLI 兼容性；未执行窗口操作、音视频正文解码或远端 CI。

| 产物 | SHA-256 |
| --- | --- |
| 新基线 Windows CLI | `cf201858ff3f3ef9561ad397cb9db6cfda2c37d336f09f1f3d7cd02d1b31b54e` |
| 当前 Windows CLI | `10e4b13d8a9becfcc772b5f247bec495cac0c4039a53124f864b2c70c19ea62b` |
| `verification/result.json` | `eb82d6ff6b59523b3aa0dbdc0e4e445de826f96566781761b3b4999b0010adfd` |
| 总收据 `receipt.json` | `93f4bdcd6c82cab61960038fccd168532930012b44dde9c08b95d8b56ac2cf5c` |
| 基线检查 `baseline-validation.json` | `6aa01676807e299af1cbac8440d3635a3079b77d70b402654328d3dfc4f35af1` |

外部证据目录为
`C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-windows-stability-20261016\ci-baseline`。
原始命令、stdout/stderr、源码归档、二进制、样本及逐份输出全部保留；未覆盖旧证据。
复现入口见 [codec 回归说明](../tools/codec-regression/README.md#direct-mp3-frames-and-sei-timing-v6)。
