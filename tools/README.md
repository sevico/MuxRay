# 开发工具

本目录用于保存确定性制样、Golden 更新、差异比较和性能基准辅助工具。Golden 更新必须人工审阅。

`validate_contracts.py` 使用固定版本的 Draft 2020-12 validator 检查公开 Schema 本身以及已登记的 Golden/manifest 实例。CI 会从 `requirements-schema.txt` 安装该开发期依赖；MuxRay 二进制不依赖 Python。
