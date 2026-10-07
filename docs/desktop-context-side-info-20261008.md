# 长文件 H.264 上下文与 MP3 side-info 验收

日期：2026-10-08。范围与语义见 [ADR-025](decisions/025-avc-checkpoints-mp3-side-info.md)。

## 实现结果

- H.264 参数回放按完整 Tag 边界保存检查点，每文档最多 8 个、合计上限 4 MiB。
  每次仍限制 8,192 Tag / 1 MiB 和文档的三秒截止。桌面显示进度并通过
  “继续准备参数”推进；CLI v3 自动最多推进 128 段。
- 随机跳转复用最近的适用检查点。取消、超时、已过期请求和源文件变化不会提交
  新缓存；源变化或关闭清空缓存。坏配置和视频屏障仍阻止复用旧参数。
- MP3 v3 及桌面支持 MPEG-1/2/2.5 单双声道 side-info，包含 SCFSI、granule/channel、
  长短块条件字段及原始 bit 位置。依赖摘要描述同 Tag 物理主数据槽位，
  `before_tag` 不误报损坏；不解码音频或重建 reservoir。
- 桌面先显示全部保留帧的摘要，再展开前两帧 side-info；省略数量和截断标记
  明确显示。CLI v3 在原有帧数/字节预算内输出全部已检查 side-info。
- CLI v3 在扫描后和写 JSON 前检查原句柄 identity/size/mtime，变化时失败且
  不输出 JSON；v1/v2 保持原行为。不能识别保留全部元数据的原位改写。

## 自动检查

本机 Go 1.27.0、Node 22.22.2，Linux GTK 3 / WebKitGTK 4.1 生产构建通过，
生成绑定和桌面模块依赖文件无变化。

- 根模块和桌面模块 `go test -race`、`go vet` 通过。
- 前端 153 项测试、TypeScript 检查及生产打包通过。
- 编解码器回归工具 34 项、Linux smoke 工具 8 项测试通过。
- 新增 MP3 v3 的三个 golden、全部 JSON 合同与拒绝/接受反例通过。
  反例覆盖位宽、条件字段、有效/无效值、未知依赖及版本隔离。
- H.264 测试覆盖多段、零进度、部分配置读取、坏替换、8 项缓存淘汰、随机跳转、
  取消、超时、源变化及“已完成但发布前被取代”的请求。
- MP3 side-info fuzz 15 秒预算完成 126,220 次执行，无失败。
  日志：`/tmp/muxray-mp3-side-info-fuzz-HKYHxD.log`。
- 独立样本 oracle 检查 13 场景、144 帧、6,925 个源 bit 字段。
  包含自然编码的切换块、截断、非法 big_values/block_type、Tag 外历史、
  主数据长度矛盾及 CRC 匹配/不匹配。未修改编码样本经 FFmpeg 独立解码。
  重复生成的 25 个媒体文件及 expectations 内容完全一致。
- 旧 v1/v2 回归通过；AVC v3 对照通过 315 个字段、60 个 slice 头、
  I/P/B 类型及 20 个多 slice Tag。默认输出与 v1 相同。

CLI 验收二进制 SHA-256：
`0f78d3017ca11f922d7043387cb65f9b215e0dff2f9ed5cc0bd08b5ae41bfabc`。

## 十二轮原生验收

所有轮次使用同一生产二进制，SHA-256：
`693d6b936284aee2c324a7c2253a52167e218e2e8e5051bcc82cb16cdecd9fee`。

| 轮次 | 场景 | 结果 |
|---|---|---|
| 1–6 | 基础、H.264 VUI/HRD、MP3 Xing、Info、尾帧截断、CRC | 全部通过 |
| 7–8 | PPS 字段、B 帧多 slice 前缀 | 全部通过 |
| 9 | MPEG-1 stereo 两帧，含自然切换块 | 136 个 side-info 源字段匹配 |
| 10 | MPEG-2.5 mono | 32 个源字段匹配，明确省略另外 7 帧的 side-info 展开 |
| 11 | side-info 截断 | 65 个已读源字段匹配；未读字段缺省、依赖未知 |
| 12 | 20,003 Tag 长上下文、继续与随机跳转 | 三段推进及缓存复用通过 |

第 12 轮是由真实 avcC 和 B 帧多 slice 字节拼装的容器有效测试文件，
中间插入轻量 Tag，用于测回放边界，不宣称是自然录制或可解码长影片。

| 动作 | 段起点 | 完成边界 | 回放 Tag 数 | 参数回放读取字节 |
|---|---:|---:|---:|---:|
| 首次选择 Tag 20002 | 0 | 8192 | 8192 | 55 |
| 第一次继续 | 8192 | 16384 | 8192 | 20 |
| 第二次继续 | 16384 | 20002 | 3618 | 2 |
| 跳到 Tag 8193 | 8192 | 8193 | 1 | 2 |
| 重访 Tag 20002 | 20002 | 20002 | 0 | 2 |

表中字节数只统计参数回放，不包括独立配置定位、所选详情和 Hex 读取。
每个阶段均通过真实 GTK 导出快照；最终 B slice 字段和前置 SPS/PPS 原始引用匹配。
同一长文件的最终 CLI 也通过 v3 schema、10 个源 bit 字段和参数集引用检查，
累计回放 20,002 Tag / 77 字节，与桌面三段计数相符。
所有轮次均检查原生打开/保存、源位置 Hex、快照 schema、源文件不变、文件句柄释放、
应用正常退出 0 和应用子进程清理。没有强杀应用；第 1 轮在应用正常退出后，
单独对一个测试辅助进程发送了 SIGTERM，其余轮次无需该清理。

测试均使用私有 Xvfb/D-Bus 和仅内存 GSettings，未操作系统辅助功能配置。
收尾只读核对 `toolkit-accessibility=false`、`screen-reader-enabled=false`。

## 复现与证据

命令入口见 [编解码器回归说明](../tools/codec-regression/README.md)。
Linux CI 已加入同样的 oracle、CLI 及四轮新原生检查；CI 配置的 YAML 已检查，
远端 CI 尚未执行。本次没有重新执行 Windows/macOS 原生验收。

本机证据保存在 `.cache/side-info-regression/`，不提交生成媒体或临时日志：

- `cli-release-v12/result.json`、`cli-release-avc3/result.json`、`cli-release-mp3/result.json`、`cli-release-long/result.json`；
- `native-final-six/result.json`、`native-final-v3/result.json`；
- `native-final-context-side/result.json`，以及 `long-context/` 下五份阶段快照；
- `samples-expanded/expectations.json`、`reproducibility.json`。

根模块/前端/合同检查日志位于 `/tmp/muxray-side-final-*.log`，
桌面 race/vet 和构建日志分别为 `/tmp/muxray-side-desktop-*.log`、
`/tmp/muxray-side-production.log`。
