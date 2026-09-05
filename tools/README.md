# 开发工具

本目录用于保存确定性制样、Golden 更新、差异比较和性能基准辅助工具。Golden 更新必须人工审阅。

`validate_contracts.py` 使用固定版本的 Draft 2020-12 validator 检查公开 Schema 本身以及已登记的 Golden/manifest 实例。CI 会从 `requirements-schema.txt` 安装该开发期依赖；MuxRay 二进制不依赖 Python。

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
