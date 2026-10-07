# 开发工具

本目录用于保存确定性制样、Golden 更新、差异比较和性能基准辅助工具。Golden 更新必须人工审阅。

`validate_contracts.py` 使用固定版本的 Draft 2020-12 validator 检查公开 Schema 本身以及已登记的 Golden/manifest 实例。CI 会从 `requirements-schema.txt` 安装该开发期依赖；MuxRay 二进制不依赖 Python。

本地首次运行 Schema、corpus 工具或 Python 单元测试前，在仓库根目录安装依赖：

```powershell
python -m pip install --disable-pip-version-check -r tools/requirements-schema.txt
```

安装和后续验证必须使用同一个 Python 解释器。使用 `python -m pip`，避免单独的
`pip` 指向另一套 Python 或 Anaconda 环境；若使用虚拟环境或解释器绝对路径，安装与运行均保持一致。

## Windows 原生桌面受控验收

[`desktop-native`](desktop-native/README.md) 从新 checkout 离线构建独立的 Go overlay 验收程序，自动生成 493 byte / 32 Tag 的结构 FLV，无需私有媒体。真实窗口选择文件后，在十个已提交 Tag 之后阻塞读取，用于重复验证取消保留部分索引与阻塞中有界退出。提供非 GUI race 验证、gate 控制、进程退出观察和收据检查；每次使用新目录并保留身份与哈希，不覆盖旧证据。需要已准备的 Go/npm 依赖缓存，原生按钮操作需由操作者或 computer-use 执行，不自动进入 CI。

同目录还提供三种生产验收辅助工具，完整命令见 [桌面验收工具说明](desktop-native/README.md)：

- `resource_probe.py` 用 PID、创建 FILETIME 和 exe 路径绑定已打开的生产进程，分别记录宿主、观察到的 WebView2 后代和其他后代的资源；`mark` 标记操作阶段，`stop` 只停止采样。输出目录必须是绝对新目录，后台采样应使用隐藏窗口。采样工作集可能重复计入共享页，private bytes 不是物理内存，采样最大值不是完整生命周期峰值；退出观察不代替源文件关闭验收。
- `long_media.py` 用已安装的 FFmpeg 和本地 lavfi 生成默认 40 分钟 H.264/AAC，要求绝对新目录，600 秒生成截止、256 MiB 文件预算，并保留失败产物。它是有效编码的**生成媒体**，不能计入真实录制样本；精确配方、工具/脚本哈希、独立 Tag 计数及 DTS 锚点随 run 保存。同版本不同 FFmpeg 构建也可能产生不同字节。身份清单位于 `testdata/manifests/longmedia/`，不进入小语料的 32 MiB/50,000 Tag 流程。
- `snapshot_check.py` 校验不超过 2 MiB 的诊断 JSON 及可选源证据，默认源上限 16 MiB；长样本须显式加 `--max-source-bytes 67108864`（64 MiB）。可设上限最多 1 GiB，使用至多 1 MiB 读取缓冲、最多 100 万 Tag 头及 120 秒协作式源校验预算，不能强制打断已阻塞的文件 I/O。

桌面 CI 从 `tools/requirements-schema.txt` 安装校验依赖，单独执行
`long_media_test`、`resource_probe_test`、`snapshot_check_test`，不运行 FFmpeg
长媒体生成、真实语料或 GUI。资源工具的 Windows 自测仅使用测试自己启动的辅助进程。

## 离线语料验证

`corpus.py` 核对清单身份、独立 FLV framing oracle、重复 JSON、分页、summary v2 和
支持的 detail 变体，并校验这些输出的 Schema。可选 ffprobe 进行逐包和流参数差分。
根目录小样本清单与 `manifests/largefile/` 分开，默认不选中大文件或 `example-*`。

在仓库根目录运行（以下为 PowerShell 示例，FFmpeg/ffprobe 需已安装）：

```powershell
go build -o .cache/muxray.exe ./cmd/muxray
python -B tools/validate_contracts.py
python -B -m unittest discover -s tools -p 'test_*.py'
python -B tools/prepare_corpus.py --output-dir testdata/private/corpus-local --ffmpeg (Get-Command ffmpeg).Source
python -B tools/corpus.py --media-dir testdata/private/corpus-local --muxray .cache/muxray.exe --ffprobe (Get-Command ffprobe).Source --allow-missing
```

生成命令产生 3 个固定编码样本及 2 个受控损坏样本，不会下载外部媒体。
当前身份按 FFmpeg `7.1.1-full_build-www.gyan.dev` 登记，同版本已在新目录复现；
其他构建不保证逐字节相同。生成哈希不匹配即失败，保留文件供审查，不自动改清单或 Golden。
不覆盖现有文件；`--only-derived` 可从身份匹配的 AAC/MP3 父文件仅重建派生项。

外部文件需按清单的公开 `sourceReference` 逐个取得、保存为 `file.name`，并遵循清单的
许可/隐私状态；媒体只放在被忽略的本地目录。完整 8 项验证需要去掉 `--allow-missing`。
该选项只允许显式返回 `missing`，缺失项不会计入 `verified`，退出成功不等于全语料通过。
省略 `--ffprobe` 会跳过外部差分，结果中的比较数为 null；不得称为已完成 ffprobe 验证。

限制：单文件 32 MiB、独立遍历最多 50,000 Tag、每次子进程 30 秒，stdout/stderr 各 16 MiB；
重复次数默认 3、允许 1–10。路径只接受指定目录中的 basename；哈希在验证前后检查。
子进程使用 argv，不经 shell；ffprobe 只允许 file/pipe 协议。超时或超限会报错并清理本次启动的
进程。工具用于明确选择的本地开发程序，不替代产品级进程隔离或任意恶意子进程树沙箱。

`corpus-differences-v1.json` 是人工审阅的差异允许清单，严格绑定样本哈希、工具版本与确切包位置。
任何新差异都必须调查，不能为让测试变绿自动扩大允许范围。

## 实际落盘大文件

`largefile` 是独立 Go 开发命令，不是产品 CLI/API。生成器顺序写满文件，使用确定性非媒体 payload，
不做稀疏分配；目标文件必须不存在。中断/失败留下的部分文件不会被自动删除或覆盖。
大小上限 12 GiB，Tag 数上限沿用核心的 100 万；测量要求至少 100 Tag。

以下三个生成命令会占用 **13 GiB** 磁盘空间，需预先检查可用空间：

```powershell
go build -o .cache/largefile.exe ./tools/largefile
New-Item -ItemType Directory -Force testdata/artifacts/largefile-local
.cache/largefile.exe generate --file testdata/artifacts/largefile-local/framing-1gib.flv --size-bytes 1073741824 --tags 100000
.cache/largefile.exe generate --file testdata/artifacts/largefile-local/framing-2gib.flv --size-bytes 2147483648 --tags 100000
.cache/largefile.exe generate --file testdata/artifacts/largefile-local/framing-10gib.flv --size-bytes 10737418240 --tags 100000
Get-FileHash -Algorithm SHA256 -LiteralPath testdata/artifacts/largefile-local/framing-10gib.flv
.cache/largefile.exe measure --file testdata/artifacts/largefile-local/framing-10gib.flv --tags 100000 --runs 10
```

测量前将各文件整文件读回的大小/SHA-256 与对应清单核对；同样分别测量 1 GiB 和 2 GiB。
生成器输出哈希不代替落盘后的读回检查。测量验证 Tag 数及首/中/末记录事实，不重新读取所有 payload。
默认分别预热扫描和取消 1 次，再各测 10 次；保留首次取消及所有原始数值。`--runs` 允许 1–100，
正式比较至少 10 次。输出包括 median/p95、Go 分配量、GC 后保留堆、工作集、读取量及请求 256 条页的分配。
峰值工作集是本进程计数；非 Windows 当前返回 `unavailable`，不能当作 0 字节 RSS。

这些是固定 Tag 数的容器压力文件，不是有效 AMF 或真实音视频；扫描耗时不能换算成全 payload 读取吞吐量。
首次取消与预热后结果必须一起审阅，不能用预热隐藏超标样本。
本机数据、已知 607 ms 离群值与后续事项见
[`首批实测报告`](../docs/corpus-baseline-20260905.md)。工具不自动覆盖该报告或历史原始记录。

## 首次取消分阶段实验

`largefile cancel-probe` 每次进程只做一次取消；`--pre-scans 0–11` 控制此前完整扫描次数，
`--prime-clock` 控制测量时钟是否预读。单独初始化的 observer 记录时钟、取消、调度握手与扫描返回阶段。
`cancel_trials.py` 默认启动 30 个独立子进程（允许 1–100），单次时限 45 秒、双输出各 1 MiB。
它记录二进制哈希、输入大小、每轮完整数据及失败项，不把旧进程中的重复调用当作首次取消。

```powershell
go build -o .cache/largefile-cancel.exe ./tools/largefile
python -B tools/cancel_trials.py --tool .cache/largefile-cancel.exe --file testdata/artifacts/largefile-20260905/framing-10gib.flv --trials 30 --output .cache/cancel-new-run.json
.cache/largefile-cancel.exe cancel-probe --file testdata/artifacts/largefile-20260905/framing-10gib.flv --pre-scans 11 --trace .cache/cancel-new-run.trace
```

`--output` 和 `--trace` 只创建新文件，拒绝覆盖历史证据。trace 上限 8 MiB，包含本机构建路径，
只应留在忽略目录中；若本机已安装 trace 解读工具，可用 `go tool trace -d=parsed` 检查。
`--legacy` 可对上一批未改动的 measure 二进制运行原协议，保留首次取消及全部预热后结果。
不要覆盖旧二进制后称其为未改动对照；本次旧版哈希记录在专项报告中。
输入文件须预先与 largefile 清单的完整大小/哈希核对；试验内检查大小/修改时间没有变化，不重复扫描全部 payload。

## Windows Ctrl+C（显式运行，不自动进入 CI）

`ctrlc_windows.py` 只在 Windows 运行，要求本机已有 CLI 与至少 1 GiB 的本地压力文件。
它创建独立隐藏控制台和专属 Job，核对控制台仅包含本次进程后发送真实 `CTRL_C_EVENT`。
不使用用户终端，不以 Ctrl+Break 代替，不接受任意 PID。退出或失败时只清理本次进程；
如果系统不允许建立隔离控制台/Job，测试失败，不回退到共享控制台。

```powershell
go build -o .cache/muxray-cancel.exe ./cmd/muxray
python -B tools/ctrlc_windows.py --muxray .cache/muxray-cancel.exe --file testdata/artifacts/largefile-20260905/framing-10gib.flv --runs 3 --output .cache/ctrlc-new-run.json
```

默认每项 3 次（允许 1–10），覆盖正常 version、4 种命令的扫描取消与 inspect/tags 输出取消；
可用 `--case inspect-output` 等选择一项。helper 单次最长 25 秒，输出采集有上限，
不联网、不打开可见交互窗口。测量包含事件分发、退出和管道排空，不代表纯核心取消时间。
JSON 即使完整，exit=130 时也必须丢弃。阶段判定、权限边界、原始数据和历史未定位异常见
[`取消专项报告`](../docs/cancellation-investigation-20260905.md)。

## Codec depth regression

[`codec-regression/README.md`](codec-regression/README.md) documents local sample
generation, independent FFmpeg/source-bit comparison for AVC v2/v3 and MP3 v2,
and six baseline plus two PPS/slice Linux native acceptance rounds. All runs
require fresh evidence directories and record commands, hashes and failures.
