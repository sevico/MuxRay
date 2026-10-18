# MuxRay · 码流透镜

**从媒体概览到 Tag、字段与原始字节的 FLV 分析工具。**

MuxRay 帮助音视频开发者检查 FLV 封装、时间戳和编解码器信息。通过桌面界面查看
码率与 GOP 曲线、查找时间戳变化，再从观察项进入详情和 Hex，保留可核对的诊断结果。
项目提供 Windows 桌面版、命令行工具和纯 Go 解析核心，源文件只读，媒体不上传。

[下载桌面版](https://github.com/sevico/MuxRay/releases/latest) ·
[使用指南](docs/preview-guide.md) ·
[命令行指南](docs/cli-guide.md) ·
[反馈问题](https://github.com/sevico/MuxRay/issues)

## 主要功能

| 能力 | 可以做什么 |
| --- | --- |
| FLV 结构检查 | 查看 Header、Tag 索引、载荷边界与结构诊断，按类型、DTS 或 Tag ID 定位 |
| 编解码器详情 | 查看 AMF0 元数据、AAC 配置、AVC/H.264 的 SPS/PPS、NAL、slice 与 SEI，以及 MP3 帧头和 side-info |
| 媒体概览与趋势 | 观察音视频码率、封装声明的 GOP、Tag 大小和时间戳变化 |
| 批量检索 | 分批查找 DTS 回退、重复与大间隔，以及 H.264、MP3 和 SEI 相关信息 |
| 字段与 Hex 联动 | 从字段、诊断或检索结果定位原始字节及位范围 |
| JSON 导出 | 保存当前已加载的分析结果，便于复核与协作 |

## 下载与开始使用

当前桌面发行版为 **[v0.1.0](https://github.com/sevico/MuxRay/releases/tag/v0.1.0)**，
提供 Windows x64 便携包，解压即可运行。

1. 确认已安装 Microsoft Edge WebView2 Runtime。
2. 下载 [Windows x64 ZIP](https://github.com/sevico/MuxRay/releases/download/v0.1.0/MuxRay-v0.1.0-windows-x64.zip)，
   可使用同页的 [SHA256SUMS.txt](https://github.com/sevico/MuxRay/releases/download/v0.1.0/SHA256SUMS.txt) 核验文件。
3. 完整解压后运行 `MuxRay.exe`，点击“打开 FLV 文件”。
4. 扫描后查看概览、趋势或 Tag；选择字段进入 Hex，需要时导出诊断 JSON。

桌面包不含 CLI；命令行工具可按[构建说明](docs/cli-guide.md#构建与运行)从源码生成。
Linux 桌面可按[开发文档](desktop/README.md#linux-构建)构建，本次发行不提供 Linux 或 macOS 桌面包。

## 使用说明

MuxRay 一次分析一个本地文件，专注结构与语法检查，不提供播放、修复、转码或实时流接入。
大文件采用有界索引和分批检索；阅读结果时请留意扫描完整性及当前覆盖范围。
导出保存当前已加载内容，可能含元数据文本和有限原始字节，分享前请检查。

完整的平台要求、导出范围和 Windows 使用注意事项见[支持范围与使用说明](docs/preview-scope.md)。

## 开发

解析核心使用 Go，桌面基于 Wails、React 和 TypeScript。Go 版本要求见 [`go.mod`](go.mod)。
在仓库根目录可以运行：

```sh
go test ./...
go vet ./...
go run ./cmd/muxray inspect --pretty sample.flv
go run ./cmd/muxray summary --schema-version 3 --pretty sample.flv
```

桌面是独立 Go 模块，构建、依赖及开发命令见[桌面开发说明](desktop/README.md)。

| 文档 | 内容 |
| --- | --- |
| [桌面使用指南](docs/preview-guide.md) | 打开文件、查找观察项、详情/Hex 与导出 |
| [命令行指南](docs/cli-guide.md) | inspect、tags、summary、detail 和版本化输出 |
| [JSON schemas](docs/schemas/README.md) | 机器可读的报告与快照格式 |
| [架构决策](docs/decisions/README.md) | 解析、索引、读取与桌面交互设计 |
| [开发工具](tools/README.md) | 样本校验、测量与回归工具 |
| [发行说明](docs/releases/v0.1.0.md) | 当前版本功能、下载与运行要求 |

欢迎通过 [Issues](https://github.com/sevico/MuxRay/issues) 提交建议或反馈。
问题报告请附版本、系统环境及复现步骤；涉及媒体文件时，优先提供可公开分享的最小样本。

## 许可证

MuxRay 采用 [MIT 许可证](LICENSE)。第三方组件遵循各自许可，详见发行包中的
`THIRD-PARTY-NOTICES.txt` 和 `license-inventory.json`。
