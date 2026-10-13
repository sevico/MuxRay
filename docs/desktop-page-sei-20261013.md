# MP3 分页详情与 H.264 SEI 验收

日期：2026-10-13。功能与回归工具提交 `c85d102`，辅助工具修正提交 `cc0b2d8`，
分页选择器可访问性修正提交 `d16c4eb`。
后续回归时间预算提交 `09b6020` 只调整测试入口，不改变生产代码。
本记录区分自动检查、独立命令行对照和真实 Linux 桌面操作；结构制样不作为业务录制或解码质量证据。

## 交付范围

- MP3 详情按 Tag 内原始帧号分页，每页最多 64 帧、1 MiB 覆盖、64 KiB 实际读取；支持上一页、下一页、精确选帧和所选 side-info。完整帧汇总事件直接打开对应帧页；汇总独立保留的 256 条事件不限制详情继续翻页。
- 同 Tag 的主数据物理来源历史随分页延续；跨页、跨 Tag 来源链接可打开原始帧页及 Hex。远页每次请求最多推进 16 页，未达到目标时显示实际进度并支持显式继续；文档最多缓存 64 个紧凑检查点。
- AVC 详情 v5 解析 SEI 消息边界、类型、长度和 trailing bits，展开 `recovery_point` 与 `user_data_unregistered` 的 UUID、最多 64 字节十六进制预览。未知类型保留源范围并标明未解释语法；跨防竞争字节的字段可逐段定位 Hex。
- SEI 每 NAL 最多读取 64 KiB、保留 64 条消息，每次解析累计 256 KiB、256 条消息。桌面所选 Tag 启用此深度，参数回放和批量检索不扫描 SEI；旧 AVC CLI v1–v4、MP3 CLI v1–v3 保持兼容，快照仍为 v3。

语义、取消/源变化约束及完整预算见 [ADR-030](decisions/030-mp3-frame-pages-and-sei.md)。
物理主数据来源不等于 bit-reservoir 重建，恢复点声明不保证独立解码；本轮未增加图像/音频解码、
SEI picture timing/HRD 时序求值或帧级时间戳。普通帧摘要仍可能受桌面 512 字段预算省略。

## 固定构建

从 `d16c4eb2aab05e95e53067dd33794d1260bcc607` 建立独立干净检出：
`/home/swk/.codex/worktrees/muxray-pages-sei-final/MuxRay`。
通过 `desktop/build-production.sh` 构建 Linux 生产桌面，通过 `go build -trimpath` 构建 CLI；
构建后检出状态为空，生成 bindings、Go 模块及依赖锁文件无差异。

| 产物 | SHA-256 |
| --- | --- |
| Linux `MuxRay` | `35de1510f1c938be74679f8eebd86c79f32e02d33061f9fa7b95a3eba4bff52c` |
| CLI `muxray` | `bee7474a6132edec13dbe57a41abaf495559e1fa5cafe1d6e070877d77eb3e77` |
| MP3 分页 / SEI expectations | `166bb8c91c789e0071f01748e700a2de42e18641b33c9365d329c86e1005acf1` |
| 既有完整 codec expectations | `c7b1ee8ca009d75b22006b8cfa81ec5d0bbede2c7621176c8e8c852daeb1fb5d` |

CLI 构建信息记录相同 revision 且 `vcs.modified=false`。
新样本位于 `.cache/page-sei-samples-trial-b`，既有样本位于 `.cache/full-codec-samples-20261012-c`；
构建、验证日志和 CLI 结果哈希索引为 `.cache/page-sei-final-evidence/build-and-cli.json`。

## 自动检查与独立对照

- 根 Go 模块全部测试、race 测试和 vet；桌面模块 WebKitGTK 4.1 race 测试和 vet 均通过。
- 前端 typecheck、生产构建及 189 项测试通过；25 项 browser fixture 合同测试通过，后者不计作真实浏览器界面验收。
- codec 回归工具 92 项测试通过；原生辅助工具最终运行 145 项，其中 4 项既有平台相关跳过，其余通过。
- 全部登记 JSON Schema、golden 与负向合同检查通过；v5 新增闭合 Schema 和负向检查。

同一固定 CLI 完成以下独立对照：

| 分组 | 核对内容 | 本地结果 |
| --- | --- | --- |
| SEI v5 与旧版隔离 | 13 类 SEI、180 个源位字段、52 次 v1–v4 兼容检查 | `.cache/ps-release-cli/result.json` |
| 完整 AVC / MP3 汇总 | 14 类 H.264、72 个 slice、14 个 PPS、2,148 个源位字段、2 个 H.264 反例；12 类 MP3 汇总、347 条事件 | `.cache/ps-release-full-cli/result.json` |
| 基础 v1/v2 | 5 场景、38 个 H.264 trace 字段、源位与默认等于 v1 | `.cache/ps-release-base-cli/result.json` |
| PPS/slice v3 | 60 个 slice、315 个源位字段、20 个多 slice Tag 及 3 个反例 | `.cache/ps-release-v3-cli/result.json` |

SEI 独立结构样本包含恢复点、UUID、未知消息、多消息、扩展类型/长度、EPB、截断、
独立消息/字节预算以及累计预算；负向控制验证错误值、源跨度和虚构覆盖会被拒绝。
Go 单测另覆盖缺少尾位、非法对齐等异常。
MP3 分页样本覆盖超过 64 帧及 1 MiB、原始帧号、跨页/跨 Tag 来源和停止条件。
开发期定向 fuzz：SEI 12,395 次、MP3 分页 278 次执行通过；不据此声称穷尽所有码流。

## 原生验收

最终 **16 轮全部通过**，统一使用上表干净构建的生产二进制：

| 分组 | 轮数 | 核对内容 | 本地结果 |
| --- | ---: | --- | --- |
| MP3 汇总帧联动 / 依赖 | 3 | 汇总 100/100/56 三页，frame 255 → page 192；跨 Tag、跨页物理来源与精确选帧 | `.cache/ps-release-mp3/result.json` |
| MP3 长页 | 1 | 37 次真实下一页操作，page 2560 / frame 2600，超过 1 MiB 后的原始帧号、side-info、依赖来源 | `.cache/ps-release-long/result.json` |
| H.264 SEI | 4 | recovery 源位置、跨 EPB UUID 源片段、短 UUID、字节预算；8 条消息及 24 个源位字段 | `.cache/ps-release-sei/result.json` |
| 既有基本 codec | 6 | 基本流程、H.264 VUI、Xing、Info、截断、CRC | `.cache/ps-release-base/result.json` |
| 既有 PPS/B slice | 2 | PPS prefix、B 帧多 slice 与原始参数集引用；21 个 prefix 字段对照 | `.cache/ps-release-v3/result.json` |

长页轮在 1,200 秒预算内以 1,175.37 秒完成；frame 2600 位于 byte 1,084,288，
帧范围 3,336 bit。511 字节回指由 frame 2598 的尾部 130 字节和 frame 2599 的
381 字节主数据槽组成，跨越的帧头/side-info 不合并进来源；当前帧槽单独列出。
后续回归默认及 CI 显式预算已增加至 1,800 秒，只为操作与验证留余量，未修改产品预算或断言。

16 个应用均正常退出、释放源文件句柄，源文件哈希不变。SEI UUID 和截断轮另各记录一次
私有验收环境 `SIGTERM` 清理：目标在应用启动前创建，不在已观测应用进程集合；
未采集其命令，具体进程名不作确定结论。其余轮无清理信号，不能将整套称为“零清理信号”。
时序与进程身份审计见 `.cache/ps-release-sei/process-evidence.json`。

新增 8 场景的 **31 份快照和 57 份 UI 工件**已独立重读，完成 808 次字段映射比对、
3,159 个字段源范围边界检查，重新核对保存收据、源文件和工件哈希。
37 个长页及跨页场景的选项树逐页核对原始帧号集合，未缺项、重复或重编号。
结果见 `.cache/ps-release-recheck.json`；16 轮构建身份和结果索引汇总在
`.cache/page-sei-final-evidence/native-final.json`。

本机系统“辅助技术”和“屏幕阅读器启用”开关在验收前后均为 `false`，未改变用户配置。
已将固定二进制复制回 `desktop/build/bin/MuxRay`，SHA-256 与上表一致；最终 CLI 位于
`.cache/page-sei-release-cli`。后续仅文档和测试入口变更不需要重建该产品二进制。

## 回归入口与平台边界

`tools/codec-regression/paged_codec_oracle.py` 生成独立结构样本与原始位期望；
`verify_paged_codec.py` 验证 CLI v5、旧版隔离和原生导出内容；
`native_paged_codec.py` 执行真实分页、汇总联动和 SEI 源位置操作。
命令见 [回归工具说明](../tools/codec-regression/README.md)。
已有 Linux codec CI job 已接入相同入口；本次未运行远端 CI。

所有原生轮使用私有 Xvfb/D-Bus、仅内存 GSettings，不写入系统辅助功能配置。
验证包含 GTK 打开/保存、前端按钮、源位置与 Hex、快照 Schema、源文件不变、句柄释放和正常退出。
Linux 结果不代表 Windows/macOS 原生验收通过，Windows 辅助选择器取消/退出仍保留原平台边界。

## 保留的失败证据

`.cache/psn-a` 保留首轮汇总联动通过及首次长页失败。后者已完成全部 37 次翻页，
到达 page 2560，但 AT-SPI 的 combo 子节点失效，辅助工具在选择帧 2600 前因空节点抛错。
这轮不计为通过。`cc0b2d8` 改为最多五次重新获取完整选项树，任一缺失子节点使整次获取作废，
并核对精确选项名称；不会跳过节点后按移位下标点击，也不会重试可能已执行的选择动作。
三项工具回归覆盖中间节点失效、持续失效和错误身份/失败动作。
长页测试预算增加为 1,200 秒，以容纳 37 次原生操作及之后的选帧、来源导航与导出；产品预算不变。

随后 `.cache/psn-select-b` 至 `psn-select-j` 保留短轮诊断：重取节点、清缓存、展开菜单及 Selection
接口均未得到有效选帧结果。截图显示可见菜单已有帧 64 起的新选项，但可访问树中仅预览项有效，
其余 64 项引用为空；不据此断言 WebKit 内部根因。所有临时缓存、键盘、截图依赖和 Selection
试验代码已撤回，正式工具仍使用精确选项名称、一次原生动作及选后精确帧断言。

隔离 A/B 仅给前端 select 增加页起点 key，使页码变化时重建控件。相同 `cc0b2d8` 辅助工具从
原版的 page 64 选帧失败变为成功：65 个可访问选项完整，frame 100 的两份快照各有 64 个
正确帧头/源范围和 62 个准确 side-info 字段，程序正常退出且源句柄释放。证据为
`.cache/psn-select-remount-probe/independent-check.json`。此最小产品修复已提交为 `d16c4eb`；
最终生产构建与探针二进制 SHA-256 完全相同，正式 16 个场景已统一用干净构建重新验收通过。
