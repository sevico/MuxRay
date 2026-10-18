# 命令行指南

MuxRay CLI 用于扫描本地 FLV、查询 Tag、生成媒体摘要及读取编解码器详情，
适合脚本处理和自动化分析。输出为版本化 JSON；桌面便携包不附带 CLI。

## 构建与运行

安装 [`go.mod`](../go.mod) 要求的 Go 版本，在仓库根目录执行：

```sh
go build -o muxray ./cmd/muxray
```

Windows 可指定 `-o muxray.exe`。也可直接运行源码：

```sh
go run ./cmd/muxray inspect --pretty sample.flv
```

下文使用 `muxray` 表示已构建且可从当前终端调用的程序。参数放在文件路径之前；
含空格的路径需加引号。使用 `muxray <命令> --help` 查看完整参数。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `inspect` | 扫描 FLV 容器并输出结构报告 |
| `tags` | 扫描后输出一个指定范围的 Tag 页 |
| `summary` | 汇总编码、时间戳、码率、配置变化及相应版本支持的帧信息 |
| `detail` | 按 Tag ID 读取 AMF0、AAC、AVC/H.264 或 MP3 详情 |

```sh
muxray inspect --pretty sample.flv
muxray tags --start 1024 --count 256 --pretty sample.flv
muxray summary --schema-version 3 --pretty sample.flv
muxray detail --tag 0 --pretty sample.flv
muxray detail --schema-version 6 --tag 2 --pretty sample.flv
```

Tag ID 从 0 开始；示例中的 Tag 必须实际存在，且详情版本须适用于其编解码器。
默认输出为单行 JSON，`--pretty` 使用两空格缩进。

## 容器扫描与分页

`inspect` 输出 `schemaVersion: 1` 的容器报告。`tags` 默认从 Tag 0 开始，
每页 256 条，`--count` 可设为 1–4,096；返回的 `next` 可作为下一次的 `--start`。
ID 和源位置不因分页而重新编号。每次 CLI 调用独立扫描文件。

索引默认及硬上限均为 1,000,000 个 Tag。诊断默认保留 1,024 条，可设置为
2–65,536 条；超出资源预算会明确报告部分结果。
详见 [Tag 分页接口](tag-index-v1.md) 与 [结构诊断代码](diagnostics-v1.md)。

## 媒体摘要版本

默认 `summary` 版本为 1，需要新字段时显式指定 `--schema-version`。

| 版本 | 范围 |
| --- | --- |
| 1 | AAC/AVC 时间戳、配置变化、声明关键帧/GOP、payload 码率和基础 A/V 时间范围偏差 |
| 2 | 在 v1 基础上加入 MP3 跨 Tag 时间线、首帧头参数/码率变化及音频编码切换统计 |
| 3 | 顺序检查已索引 Tag 中的 MP3 帧，提供完整帧数、样本数、精确分数时长、逐帧变化和未解析范围 |

码率只使用已有已知时长覆盖的字节，不推测最后未闭合区间的时长。
GOP 按 FLV 关键帧声明统计；原始 DTS 不自动展开回绕，A/V 时间范围偏差也不等于
解码后的音画同步。版本语义见 [Summary v2](media-summary-v2.md) 和
[完整头部与帧汇总设计](decisions/029-full-codec-headers-and-frame-summary.md)。

## 编解码器详情版本

默认 `detail` 版本为 1。各编解码器使用独立的 `documentType`，旧版本格式保持兼容。

| 版本 | 支持内容 |
| --- | --- |
| 1 | AMF0 ScriptData、AAC AudioSpecificConfig、AVC 配置与 NAL 边界、MP3 首帧头 |
| 2 | AVC SPS VUI；MP3 Tag 内多帧边界、Xing/Info/VBRI 及头部/side-info CRC |
| 3 | AVC PPS 与 slice 头前缀及参数来源；MP3 side-info 和主数据物理依赖窗口 |
| 4 | AVC 普通 NAL 1/5 的完整 slice 头和 PPS 扩展 |
| 5 | 在 AVC v4 基础上加入 SEI 消息、恢复点及 UUID 用户数据预览 |
| 6 | 在 AVC v5 基础上加入有 SPS 上下文的 SEI 缓冲周期与图像时序字段 |

AVC NAL 长度字段宽度默认从最近可用的 sequence header 获取，也可通过
`--avc-nal-length-size 1|2|4` 明确指定。读取与上下文回放都有预算，缺失或含糊的
参数保留未知。字段包含可定位的 byte/bit 源范围；截断或不支持语法会保留已确认内容。

这些结果用于语法检查，不执行图像/音频解码、MP3 reservoir 重建或 HRD/DPB 时间线重建。
完整字段、预算与格式定义见 [JSON schemas](schemas/README.md) 和 [架构决策](decisions/README.md)。

## 退出状态与自动化

| 状态码 | 含义 |
| --- | --- |
| 0 | 命令成功；仍应读取报告中的完整性和诊断字段 |
| 2 | 结构/分析问题、结果不完整或所选 Tag 不适用；是否输出 JSON 取决于命令 |
| 1 | 文件打开、读取、分析或输出等运行失败 |
| 64 | 参数或用法错误 |
| 130 | 操作被取消；丢弃未完成的标准输出 |

具体诊断状态取决于命令；`summary` 中影响完整分类的 unsupported、filtered 等
状态也会返回 2，不影响完整性的 warning 可与 0 共存。
`detail` 无法建立详情时可能仅向 stderr 输出说明。自动化调用应同时检查退出状态、
`schemaVersion`、报告覆盖范围及 `documentType`（若该格式提供），不要只以是否生成
JSON 判断任务完整性。
