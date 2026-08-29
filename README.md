# MuxRay（码流透镜）

MuxRay 是一个面向 FLV 和音视频码流的跨平台诊断工具。项目当前处于 P0/P1 技术基线阶段，优先建设可复用、可测试的纯 Go 解析核心与 CLI。

## 当前范围

- 有界 byte/bit 读取与统一源范围 `Span`；
- FLV Header、PreviousTagSize 和 Tag 索引扫描；
- 稳定、版本化的 JSON 输出和诊断代码；
- 正常、边界、损坏样本的 unit、Golden 与 fuzz 测试。

Wails 桌面界面、AMF0、H.264/AAC 详情和实时 RTMP/HTTP-FLV 将在核心数据模型稳定后分阶段加入。

## 开发

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/muxray inspect sample.flv
```

`inspect` 默认输出单行、确定性的 `schemaVersion: 1` JSON；加入 `--pretty` 可获得两空格缩进格式。扫描发现结构错误时仍会输出报告并以状态码 2 退出。公开字段定义见 [`docs/schemas/flv-scan-report-v1.schema.json`](docs/schemas/flv-scan-report-v1.schema.json)，诊断码见 [`docs/diagnostics-v1.md`](docs/diagnostics-v1.md)。

首版 scanner 默认且最多索引 1,000,000 个 Tag，诊断默认保留 1,024 条、允许范围为 2–65,536 条。至少保留两条，才能同时提供首个问题和“诊断已截断”标记。CLI 采用流式 JSON 编码，不复制完整 Tag DTO，也不把整份报告缓冲到内存。更高 Tag 数量将在分块/分页索引完成后开放，避免连续 slice 扩容突破内存预算。

项目许可证尚待确定；在许可证落定前，不应把代码作为已授权的正式发行版分发。
