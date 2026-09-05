# 可取消扫描与 Tag 分页第一阶段

本阶段对应 [ADR-008](decisions/008-cancellable-chunked-index.md)。已有扫描、
AMF0/AAC/AVC/MP3 详情以及 media-summary v1/v2 合同不变。

## CLI

```text
muxray tags --start 0 --count 256 --pretty sample.flv
muxray tags --start 256 --count 256 sample.flv
```

`tags` 首先完成容器扫描，再输出请求页；每次调用都会重新扫描。省略 `start`
时为 0，省略 `count` 时为 256；显式 count=0 非法，上限为 4,096。
`--max-tags`、`--max-diagnostics` 与 `inspect` 的扫描预算一致。

新文档标识为 `documentType: "flv-tag-page"`、`schemaVersion: 1`，由
[独立 Schema](schemas/flv-tag-page-v1.schema.json) 定义。

| 字段 | 含义 |
|---|---|
| `source.sizeBytes` | 声明源大小，不含路径 |
| `scan.bytesScanned` / `complete` | 本次扫描的逻辑游标与完整性，不是本页的读取量 |
| `page.start` / `limit` | 请求的全局起点与页容量 |
| `page.total` | 已提交的索引条数，可能只是结构损坏/超限后的前缀 |
| `page.next` | `start + tags.length`，不大于 total |
| `tags` | 最多 limit 条，ID/Span 保持全局位置 |
| `diagnostics` | 整次扫描的有界诊断，不按页过滤 |

`start == total` 返回 `tags: []`，`next == total`；`start > total` 是参数错误。
页内顺序保持源顺序，不支持 filter/sort。跨字段算术和 ID 连续性由 Go 测试保证，
Schema 负责形状、字段闭合及数值/数组硬边界。

退出码：0 成功（可含 warning）；2 容器不完整或有 error，仍输出可用页；
1 打开/读取/写出失败；64 参数错误；130 上下文取消或 Ctrl+C。
调用方遇到 130 必须丢弃 stdout，包括看似已完整的 JSON；不自动输出取消后的部分页。

## 核心接口与迁移

```go
scan, err := flv.ScanContext(ctx, source, size, flv.Options{})
// err 可能为 context.Canceled / DeadlineExceeded 或 I/O 错误；
// 此时 scan.Complete 为 false，已提交的记录仍可读取。
index := scan.Index()
tag, found := index.Get(1024)
page, pageErr := index.Page(1023, 256)
for id, record := range index.All() {
    // break 会立即停止迭代；记录为值拷贝。
    _, _ = id, record
}
_, _, _, _, _ = err, tag, found, page, pageErr
```

`ScanContext` 不填充 `ScanResult.Tags`，也不在返回时铺平成连续数组。
新消费者使用 `Index()` 的 `Len/Get/Page/All`。旧 `Scan` 仍保留 flat slice，
手工构造的 `ScanResult{Tags: ...}` 也可通过 Index 访问；读取旧 slice 视图期间
调用方不得修改原 slice。`report.New` 是明确的完整 DTO 物化入口，大扫描应使用
`WriteScanJSON`，分页输出使用 `NewTagPage` / `WriteTagPageJSON`。

块内最多 1,024 条记录，按需分配，预算尾块仅分配剩余容量；记录不包含 payload。
100 万条硬上限继续生效，达到上限且仍有数据时保留原有 tag-limit 诊断。
Page 分配独立记录副本，Get 和 All 也返回值拷贝。返回后的分块索引支持并发只读；
本阶段不允许扫描期间查询或修改它，也没有快照/后台工作线程。

取消检查位于结构读取前后、Tag 之间和返回前。中途取消的 Tag 不提交；真实源截断
仍保留原有部分 Tag 语义。`bytesScanned` 是最后接受的逻辑游标，可能已经越过
最后提交 Tag 的末端，不代表读取的物理字节数或可直接续扫的检查点。
取消本身不产生 FLV corruption 诊断，先前已发现的真实诊断保留。

CLI 还将上下文传入汇总遍历、AVC 反向配置查找，并用 `cursor.WithContext`
覆盖详情/汇总的实际源读取、用输出包装器覆盖写入。旧的 analysis API 不改变
原有 Tag 级部分结果口径。取消不能抢占正在阻塞的任意 ReaderAt/Writer，
也不会创建逐次读写 goroutine。已读入内存的有界解析/序列化可能在下次检查前继续。

## 验证与后续

已添加块边界、页界限/溢出/副本所有权、并发只读、读边界取消、I/O 部分结果、
旧/新扫描与汇总输出等价、跨块 AVC 自动上下文查找测试，以及三种分页 golden。
虚拟稀疏源验证超过 10 GiB 的声明源大小和 4 GiB 以上偏移，拒绝任何 payload
读取；这不是实际大文件、RSS 或真实语料验收。

```text
go test ./...
go vet ./...
go test -race ./...
go test ./internal/flv -run=^$ -fuzz=FuzzScanContextPage -fuzztime=30s
go test ./internal/flv -run=^$ -bench='Benchmark(ScanContextTags|TagPage)$' -benchmem
python tools/validate_contracts.py
```

下一阶段优先注册真实语料并完成大文件实测，再设计可观测进度/后台扫描会话、
磁盘索引或更高 Tag 上限。Wails GUI、在线输入、过滤排序不在本批范围。

### 本批本机验证（2026-09-05）

Windows/amd64、Go 1.27、Intel Core Ultra 7 255H 环境下，全量 unit、vet、
启用 CGO/GCC 的 `-race -count=1` 以及全部 JSON Schema 正反例校验通过。
新 `FuzzScanContextPage` 运行 30 秒、4,285,624 次执行通过；现有
`FuzzAnalyzeV2` 运行 5 秒、426,913 次执行通过。

`-benchmem -benchtime=200ms -count=2` 的分配结果：

| 测量 | 总分配字节/次 | 分配次数/次 |
|---|---:|---:|
| 分块扫描 10,000 个空 Tag（虚拟源） | 约 492,380 | 20 |
| 分块扫描 1,000,000 个空 Tag（虚拟源） | 约 48,060,520 | 993 |
| 10,000 条索引中取 256 条跨块页 | 12,288 | 1 |
| 1,000,000 条索引中取相同容量跨块页 | 12,288 | 1 |

这些是 Go 分配基准，不是 RSS/峰值工作集，也不代表磁盘扫描吞吐量。
取消验证使用确定性 context 注入、读取/输出中断测试；未声称完成手工 Windows
控制台 Ctrl+C 或其他操作系统的交互验收。跨平台 CI 已增加新分页 fuzz 项，
远端 CI 要在后续提交/推送后实际执行。
