# MP3 变化、SEI 批量检索与原生回归工具验收

日期：2026-10-15。功能与边界见 [ADR-032](decisions/032-codec-change-and-sei-search.md)。

## 本轮功能

MP3 按参数变化、码率变化、停止原因或全部观察项检索。游标保存 Tag 内已确认帧边界
和前帧比较信息，大 Tag 的第 2,600 帧之后仍可查找；预算耗尽只产生续查位置。
停止行保留来源，不提供虚构的帧详情跳转；普通 VBR 变化不判为损坏。

SEI 按全部消息、缓冲周期、图像时序、未知/不完整证据检索。跨批保留紧凑 SPS/PPS
及明确激活来源，同 Tag 剩余消息可以零读取续页。配置、EOS、不完整覆盖等屏障
清除相关上下文，不按“最新 SPS”猜测。消息及 SPS/选择依据均可定位原始字节。

两者共用单一读取 worker，每页最多 64 行、显式继续、可取消和恢复丢失的回复。
后端最多保留八个不透明游标。快照 v4 仅冻结当前已完成页，保持 v1–v3 合同不变。

## 固定构建与自动校验

生产程序从功能提交 `2422d77` 的干净独立工作树构建，重新生成 Wails 绑定后工作树
仍无差异。工具链为 Go 1.27.0、Node 22.22.2、Wails 2.14.0、GTK 3.24.41、
WebKitGTK 2.52.6，Linux amd64。生产程序 SHA-256：

```text
a81a2e5e7fa4ef1b2bdc00bcde8c2db3376f8c91253fab84bc0e1937bad4b156
```

构建修订由外部干净工作树记录固定，程序元数据未内嵌 VCS 修订。
本轮自动校验通过：

- 根模块及桌面模块的 race 测试与 vet；新检索覆盖取消、截止时间、源变化、关闭、
  续查失败不提交候选状态、游标淘汰、零读取 pending 页及预算回滚。
- 前端 221 项测试、类型检查和生产构建；实际生成的 Wails 类 round-trip 保留 v4 选择。
- 112 项 codec 工具测试通过；158 项 Linux helper 测试中 154 项通过、4 项按既有条件跳过。
- 全部 schema/golden 正反例校验；v1–v3 快照 golden 未修改。
- 六个独立构造的样本经过真实桌面 API，共 20 页、217 条筛选结果，与独立原始字节
  预期一致。此项尚不替代窗口操作。
- 20 个既有时序样本、461 处源位映射通过；100 份 CLI v1–v5 输出与既有基线逐字节相同。
- SEI 搜索 20 秒 fuzz 共 189,095 次执行通过。最大紧凑状态测试计量 388,766 字节，
  低于 512 KiB 测试上界；这是 Go 存储计量，不是进程 RSS。

验证记录位于本地 `.cache/codec-search-validation-20261015/`，API 页及核对结果位于
`.cache/codec-search-api-probe-20261015/`。来源清单由
`tools/codec-regression/codec_search_oracle.py` 独立生成，不使用产品输出作为期望。

## 工具提速与关闭取证

同一 1,251,029 字节源、62 个 side-info 字段、三次重复：

| 指标 | 原整文件位串 | 字段字节窗口 |
| --- | ---: | ---: |
| 每 62 字段中位耗时 | 13.6044 s | 0.000112742 s |
| 三轮访问源字节合计 | 232,691,394 | 261 |
| 单次调用 tracemalloc 峰值 | 82,001,854 B | 785 B |

此结果仅衡量 Python 回归校验工具，不代表媒体解析或界面提速；分开计量分配与耗时，
不设跨机器时延门槛。原始位串与整数值双重校验继续保留。
计量结果为 `.cache/source-field-benchmark-20261015.json`。

关闭失败会先保存原始动作及异常，再以独立进程保存两份进程/源 FD 观察和有界
UI 取证。取证进程八秒超时、节点不完整、读取异常、信号和回收均有明确记录。
定向测试验证了实际阻塞观察进程的超时终止与回收。该观察进程的终止与应用清理
分别记录，不重放关闭，不把失败改成通过，也不据此宣告已修复 AT-SPI 关闭根因。

## Linux 原生验收

使用工具提交 `7d4746e`，六轮首次运行全部通过，没有重试或修改断言。
每轮通过真实 GTK 文件选择、WebKit 界面操作和原生导出执行：

| 场景 | 检索页 | 比对行数 | 主要核验 | 耗时 |
| --- | ---: | ---: | --- | ---: |
| `mp3-late-change` | 3 | 1 | 第 2,600 帧参数变化；两次 preparing 后精确帧跳转，32 个 side-info 字段及 Hex | 122.05 s |
| `mp3-bitrate-stops` | 3 | 14 | 全部、码率、停止原因筛选；两次码率变化和五类停止 | 47.11 s |
| `sei-pagination` | 3 | 130 | 64/64/2 消息分页，无丢重；后两页零读取；从头重检换新请求 | 61.87 s |
| `sei-cross-256` | 6 | 4 | 三种筛选各跨 256+3 Tag；消息、SPS、buffering_period 选择依据定位 | 167.51 s |
| `sei-missing` | 1 | 1 | 缺失上下文保持 missing/incomplete，不虚构 SPS 或选择依据 | 35.02 s |
| `sei-budget` | 4 | 67 | 64 条已检查消息及覆盖缺口；未知、图像时序筛选都保留覆盖说明 | 49.33 s |

合计 20 个检索页、217 次筛选结果比对、225 处来源范围、8 次 Hex 操作和 32 个
side-info 字段；相同行在不同筛选下会重复出现，217 不是唯一消息数。
另有重检、帧导航、来源和最终导出，共保存 38 份快照。第二次从保存文件重新执行
schema、源字节、UI、分页语义及生命周期校验，全部通过。

六轮均确认源文件未变、关闭前持有源 FD、关闭后释放、退出码 0，且无应用所属
残留进程和强制清理。本次所有工具清理信号列表也为空。所有 GUI 操作均使用独立
Xvfb/D-Bus 与仅内存 GSettings，未写入系统辅助功能设置。本轮未出现关闭超时；
新增取证分支的阻塞和超时行为由定向工具测试验证。

原始结果目录为 `.cache/codec-search-native-20261015-a/`，独立复核为
`.cache/codec-search-independent-recheck-20261015.json`。身份摘要：

```text
expectations.json SHA-256:
13cd4e097360e310827bd4c74c9b6f124a3091f5fc342b31f787f3d890c81058
native result.json SHA-256:
78b292bbf56fb6f998c839b91ab66b94ae2f6495a3341f04d17c60334963eac1
```

## 复现与边界

```sh
GOTOOLCHAIN=go1.27.0 go test -race ./...
GOTOOLCHAIN=go1.27.0 go vet ./...
./desktop/build-production.sh
/usr/bin/python3 -B tools/validate_contracts.py
/usr/bin/python3 -B tools/codec-regression/codec_search_oracle.py --output-dir "$PWD/.cache/cs-samples"
/usr/bin/python3 -B tools/codec-regression/native_codec_search.py \
  --input-dir "$PWD/.cache/cs-samples" --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/cs-native" --timeout-seconds 900
/usr/bin/python3 -B tools/codec-regression/source_field_benchmark.py --repetitions 3
```

使用全新的输出目录保存每次运行；CI 的独立 `linux-codec-search` job 上传成功及失败
记录、源/二进制身份和原始取证。本文的本机验证不表示远端 CI 已执行。
构造样本验证结构、语法和来源，不验证音视频解码质量；未实现 HRD/DPB 时间线或
MP3 reservoir 重建。本轮 Linux 结果也不替代 Windows/macOS 原生验收，既有 Windows
辅助选择器取消退出问题仍须在目标平台单独处理。
