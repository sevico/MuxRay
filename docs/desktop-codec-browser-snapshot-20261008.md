# MP3 逐帧浏览、H.264 对照与快照 v2 验收

日期：2026-10-08。设计与边界见 [ADR-026](decisions/026-desktop-codec-evidence-snapshot-v2.md)。

## 实现范围

- MP3 可在当前 64 帧 / 1 MiB 检查范围内选择完整边界的帧，只展开所选帧的
  side-info，也可返回前两帧预览。选择仍从 Tag 起点检查，保留前序主数据槽位历史。
  截断边界、未知帧长及预算外帧不可选；不解码音频或重建 reservoir。
- H.264 仅对照所选 Tag 的 FLV 声明、IDR NAL 观察和 slice 类型集合。
  正向观察可来自部分覆盖；负向结论要求完整可用的 NAL 头覆盖。
  原始声明、代表 NAL 和 slice_type 可定位 Hex；不新增读取或修改 Summary/GOP。
- 界面导出快照 v2，冻结当前详情和已加载的最多 64 点曲线页，记录指标和
  Summary 请求身份。未知值及省略原因明确保留，总量仍限制 2 MiB。
  导出不重读、不重新分析；默认/显式 v1 请求和原 v1 schema/golden 保持兼容。

## 自动检查

本机 Linux、Go 1.27.0、Node 22.22.2，GTK 3 / WebKitGTK 4.1 生产构建通过。
功能提交 `54d4866` 在干净 detached checkout 中重新构建成功，生成绑定与提交一致，
根模块、桌面模块和前端测试再次通过。干净构建的程序 SHA-256 与下方原生验收
程序完全一致；构建验证不依赖工作区的未提交文件或前端依赖目录。

- 根模块与桌面模块 `go test -race`、`go vet` 通过。
- 前端 163 项测试、TypeScript 检查及生产打包通过。
- 编解码器回归工具 39 项、Linux smoke/快照校验器 27 项测试通过。
  反例覆盖产物篡改、请求/Tag 身份、源位与高亮、曲线算术及不可能的 GOP 极值。
- MP3 覆盖帧 0、2、7、63、同 Tag 历史、原始源位、非法/截断选择、请求恢复与源变化。
- H.264 覆盖未知/保留 NAL、截断、部分参数上下文、多 slice、256 NAL 预算和零新增读取。
- 快照 v2 full/minimal golden、完整 JSON 合同及拒绝反例通过；原 v1 golden 未变。
  冻结测试在源变化后捕获、关闭文档后复核字节，确认未重新读取或改变既有快照。

生产二进制 SHA-256：
`6bee2f817065857fea46e933c313fa544c511bf12b7ceb950bd358dc3cee5ae9`。
CLI 对照二进制 SHA-256：
`19ae8e4fe7fae7b431845237890dece6a37569c30944093fa96ccf23ea001936`。

## 原生与源数据对照

使用既有可复现编码样本，不下载或上传媒体。原生流程运行在独立 Xvfb/D-Bus 中，
采用仅内存 GSettings，不操作系统辅助功能设置。每个通过轮次验证原生打开/保存、快照
schema、源文件未变、关闭后的 FD 释放、应用正常退出及子进程清理。

| 场景 | 结果 |
|---|---|
| 基础六轮：默认详情、H.264 VUI/HRD、MP3 Xing/Info、尾帧截断、CRC | 六轮全部通过 |
| PPS 与 B 帧多 slice 两轮 | 两轮通过，分别核对 11/10 个前缀字段 |
| MPEG-1 stereo、MPEG-2.5 mono、side-info 截断三轮 | 136/32/65 个源字段匹配 |
| 20,003 Tag 长上下文继续与随机跳转 | 重跑通过；三段到达 8192/16384/20002，回访复用检查点，末次重访不重放历史 |
| MP3 第三帧与末帧 | 帧 2 的 62 个字段、帧 14 的 70 个字段匹配；所有帧摘要及前序依赖保留 |
| H.264 多 slice B Tag | `inter_without_idr`，slice 集合 B，10 个前缀字段匹配 |
| H.264 IDR Tag | `keyframe_with_idr`，slice 集合 I，12 个前缀字段匹配 |

后三轮均核对快照 v2 的当前指标、Summary 身份和曲线页值。独立 FLV framing、
载荷大小、单调 DTS 与关键帧声明提供聚合 oracle，再用 CLI v2 Summary 交叉检查。
H.264 样本为 23 Tag 单窗口；MP3 样本为一 Tag 的 15 帧，曲线没有足够时间间隔，
正确保持未知码率。单位测试另覆盖非零 MP3 曲线和前端第二页冻结。

另补两轮 H.264 原生来源按钮验收：B Tag 点击声明/NAL/B slice 三个按钮；
IDR Tag 点击声明/NAL/IDR/I slice 四个按钮。逐按钮记录 UI 树及 SHA-256，
核对 Hex byte/bit/位长、八个位的原始值和范围高亮，七个来源按钮全部通过。
初末快照再次核对实际文件哈希、会话/请求/Tag 身份、源字节及曲线值。
两轮分别耗时 141.12 / 175.49 秒，保持 240 秒自动化总时限。

共 15 类场景、17 轮成功流程。收尾对 17 份最终快照重新进行 schema、请求/Tag
身份、源字节和实际产物哈希核对；每轮文件句柄均释放、应用正常退出 0，
没有在通过轮次强杀应用。部分测试辅助进程需要受控 SIGTERM，记录在各轮结果中。
长上下文重跑用时 213.04 秒，五份阶段快照均保留，包含随机回访和最终重访。
系统辅助功能配置收尾只读核对仍为 `toolkit-accessibility=false`、
`screen-reader-enabled=false`。

## 复现与证据

命令与样本生成方式见 [编解码器回归工具](../tools/codec-regression/README.md)。
Linux CI 已登记三项新功能的原生入口及快照校验器测试；远端 CI 未在本机执行。
本轮未重新执行 Windows/macOS 原生验收，也不代替物理显示器、Wayland/GPU 或
业务录制文件的解码质量验证。

本机日志位于 `/tmp/muxray-browser-codecs-*.log`；媒体、快照和进程证据保存在
`.cache/codec-browser-regression/` 与 `.cache/cb/`，不提交生成媒体或临时日志。
主要结果：`native-six/result.json`、`native-browse-evidence-r2/result.json`，
以及 `.cache/cb/v3/result.json`、`.cache/cb/context/cases.json`、
`.cache/cb/hclick/result.json`。`native-browse-evidence-r2/identity-recheck.json`
保存新增身份/哈希规则对原始产物的补充复核。
完整汇总为 `.cache/codec-browser-regression/acceptance.json`；干净提交构建记录为
同目录 `clean-build.json`。长上下文合并结果为 `native-context-combined.json`，
重跑的五份快照及退出证据位于 `.cache/cb/lr/`。
首轮新曲线流程因不可访问的等待文字超时，保留在
`.cache/codec-browser-regression/native-browse-evidence/`。修正测试等待条件后重跑，
未为该失败修改生产代码。
长上下文轮在三段解析及对应快照完成后超过 240 秒自动化总时限，失败与受控清理
保留在 `.cache/cb/context/long-context/`。已核对三段的源数据和进度，再用 420 秒
重跑完整五次导出/随机跳转流程；Linux CI 对此入口采用同样的显式总时限。
这是原生自动化流程预算，产品每次详情读取的三秒截止未改。
`.cache/cb/h264-click/` 是选择更精简的逐按钮 UI 验收方案前主动取消的工具尝试，
已清理且不计入通过结果；正式补验使用 `.cache/cb/hclick/`。
