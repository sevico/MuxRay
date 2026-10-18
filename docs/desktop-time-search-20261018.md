# 长文件时间戳检索与 Windows 原生验收（2026-10-18）

ADR-033 已实现并完成一轮默认 Windows 生产构建的完整功能验收：从零开始三批
检索，发现尾部回退，进入趋势、AAC 详情和 Hex，实际保存 JSON，关闭源文件并
正常退出。该结论不扩展为长期稳定性、自然慢盘取消或历史 Windows 故障修复。

## 实现与边界

- 新增 `StartTimeSearch / GetTimeSearch / CancelTimeSearch`，复用 document 串行
  worker、递增请求 ID、取消和通信恢复。只读既有索引，不调用源 `Stat/ReadAt`。
- 每批最多 4096 个 Tag、64 条观察；前驱按 `(baseType, StreamID)` 保留最多
  512 项，确定性 LRU 淘汰。跨批延续比较；淘汰后的缺失前驱在所有筛选中显示未知。
- 每文档最多八个不可变续查状态。成功重开才丢弃旧状态，取消或失败不提交进度。
- 命中先加载所在 256 Tag 趋势窗，再选中详情。新的选择、关闭、换源及较新请求
  阻止迟到结果覆盖；检索期间阻止其他读取入口互相抢占。
- 快照仍沿用既有 v3/v4 合同。时间戳检索页不导出；本轮导出的是定位后已加载的
  趋势、详情与 Hex。普通趋势仍有独立的 4096 Tag 前驱回查上限。

完整语义见 [ADR-033](decisions/033-bounded-timestamp-observation-search.md)。

## 构建与样本身份

此前任务试用与 Hex 导航改动已提交为 `0e4ac15781be3d8a1ac3aa03c2e4231a3e2c3fec`。
本轮 EXE 在该提交加本功能工作树改动上构建，不能称为干净提交产物。原生验收前
记录了所有修改及新增源码文件的 SHA-256；之后仅补充说明文档和验收收据。

使用 `desktop/build-production.ps1`，Wails v2.14.0，Windows amd64 production，
实际生成绑定和前端资产；离线使用本机锁定依赖，没有 overlay 或 `pickerprocess`。

本地证据根目录：

```text
C:\Users\swk01\Documents\Codex\2026-09-09\computer-use\outputs\muxray-time-search-20261018
```

| 产物 | SHA-256 |
| --- | --- |
| `native-001/MuxRay.exe` | `deda36892609e73540bb660e64de8ec8f0f40b4358321b7e826139649e389cc1` |
| `fixtures/time-search-long.flv` | `bc6940d415183220f503309eefacbfc3b26b57ed0e3aa476da24c0bda852515a` |
| `time-search-snapshot.json` | `9268207b67e1490b1cc6baa4c1ad0d74f188bc3c143ba6b3df2a77a1489d9668` |

样本由 [time_search_fixtures.py](../tools/desktop-native/time_search_fixtures.py)
生成：190013 byte、10000 个完整 FLV Tag，独立字节遍历核对边界、时间戳和前驱。
这是合成的封装时间观察，不声称为真实损坏录制或可播放的 AAC 音频。

## 自动验证

| 检查 | 结果 |
| --- | --- |
| 根模块 `go test ./...` | 通过 |
| `go test -race ./internal/timesearch ./internal/session ./internal/document ./internal/desktop` | 通过 |
| 上述四包 `go vet` 及新增 `TestTimeSearch / TestBindingSurface` | 通过 |
| desktop 模块 `go test ./...`、`go vet ./...` | 通过 |
| 前端 `npm run typecheck`、`npm test` | 通过，237/237，新增 16 项 |
| 默认生产构建（含绑定生成、前端编译、Go 编译） | 通过 |
| 原生导出 `snapshot_check.py --source ...` | schema、512 byte Hex、256 个趋势点与源一致 |

测试覆盖跨批前驱、稀疏轨道、64 行无丢重、512 项淘汰、严格大间隔阈值、原始
回绕、部分索引、元数据零源读取、深拷贝、游标过期及取消重开、排队取消、替换、
源变化、关闭、截止时间，以及丢失响应恢复和迟到导航。原生流程不声称捕获到了
4096 条元数据操作进行中的取消竞争。

Wails 生成的 `models.ts` 保留生成器原样（新增空行含 tab），普通 `git diff --check`
会报告这 12 处行尾空白；手写源码和文档检查通过。未手改绑定，以保持 CI 重新
生成后一致。本轮未推送或触发远端 CI。

## 原生流程

默认“DTS 回退”、阈值 1000 ms，从头检索并两次点击继续：

| 批次 | 实际检查范围 | 扫描数 | 结果 |
| --- | --- | ---: | --- |
| 1 | `[0, 4096)` | 4096 | 无回退，明确提示未到末尾 |
| 2 | `[4096, 8192)` | 4096 | 无回退，累计范围 `[0, 8192)` |
| 3 | `[8192, 10000)` | 1808 | 命中 Tag 8192，到完整索引末尾 |

Tag 8192 的前驱来自上一批：Tag 8191 / DTS 163820 ms；当前 DTS 163720 ms，
差值 -100 ms。点击命中项后：

1. 趋势窗口为 `[8192, 8448)`，256 点，回查 Tag 8191 一条；选中点为 -100 ms。
2. AAC raw 包详情选择 Tag 8192，字段范围 `[155672, 155676)`，32 bit。
3. 字段定位打开 Hex `[155672, 156184)`，前四 byte 为 `af 01 aa bb`。
4. “返回页首导出”进入真实保存对话框，写出 72029 byte 的 v3 快照；独立检查通过。
5. 关闭文件后显示空状态；主进程仍活着时以 `FileShare.None` 独占只读打开源，
   成功并确认源 SHA-256 未变。
6. 通过标题栏关闭窗口；提前持有进程句柄的观察器记录 PID 15696 正常退出，
   exit code 0，无强制终止，随后窗口列表中已无本轮程序。

进程存活时间为 2026-10-18 12:23:21–12:27:23（北京时间）。工具对原生打开
对话框的缓存元素索引有一次失效；刷新后按实际截图点击文件名框，读回完整路径
后正常打开。没有重启应用或重复功能轮次。

证据包括 `native-plan.json`、`build.log`、`build-receipt.json`、`fixtures/fixtures.json`、
`native-001/01-open-ui.txt` 至 `10-closed-ui.txt`、实际快照及 `snapshot-check.json`、
`native-001/source-closed.json` 和 `native-001/exit.json`。界面文本是操作时保存的
可访问性树；截图已在操作会话中检查。

本轮按预定范围完成后停止。历史默认 exit 1、可选选择器严格复验 0/6 和 Shell
GDI 引用问题继续保留在 [Windows 稳定性记录](desktop-windows-stability-20261017.md)，
不因本次正常退出而关闭。
