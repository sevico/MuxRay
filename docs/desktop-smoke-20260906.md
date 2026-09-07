# 最小桌面壳验证记录（2026-09-06，09-07 完成复测）

## 已验证

- Windows/amd64，Go 1.27，Wails CLI/runtime v2.14.0，Node 24.4.1。
- 根模块纯 Go 全量 tests/build/vet，以及 GCC/CGO 全量 race。
- 新宿主的原生对话框注入测试：未启动、取消、错误、并发打开、窗口关闭时
  对话框迟到返回、关闭后禁止重新接纳；宿主逻辑 100 轮 race。
- 新宿主使用临时真实 FLV 文件进行后台扫描和 100/100/5 条分页；正常和
  PreviousTagSize 不匹配 warning / 截断 error 样本分开断言，关闭后禁用页面读取。
- 17 项前端控制器/数值/绑定 null 转换测试，TypeScript 严格检查和 Vite
  生产构建；Go 宿主 tests/race/vet；真实 Wails 绑定生成与 Windows 生产编译。
- Vite 升级并锁定为 7.3.6 后，npm audit 为 0 项已知问题。
- Chrome 中对生产 assets 使用与宿主相同的 CSP 进行 **模拟宿主** 检查：
  空态、100/100/5 条分页、uint64/int64 极值文本、诊断转义、部分结果、
  取消、关闭、取消文件选择，以及页面没有整窗横向溢出。

浏览器检查使用 `frontend/test/browser-smoke.mjs` 的合成数据，直接替代
`window.go`，没有打开、上传或修改用户媒体。它验证 React 与生成 JS
包装器的前端行为，不验证 WebView2 IPC、Wails runtime 注入或原生对话框。
测试只创建并关闭自己的后台 tab。

## 尚未通过的人工/原生验收

Windows Computer Use 通道返回：
`Computer Use native pipe is unavailable ... 系统找不到指定的文件 (os error 2)`。
重置 JS 通道后仍失败，不能执行真实窗口操作。Chrome 后台截图也超时；
浏览器检查仅有 DOM/交互断言，没有截图级视觉验收。

因此 **不能宣告原生桌面验收完成**。构建出的本地文件位于
`desktop/build/bin/MuxRay.exe`，待人工或可用的桌面通道依次确认：

1. 启动窗口正常显示，而不是空白页；WebView2 runtime 能加载。
2. 点击打开，真实文件选择器出现；取消不创建扫描任务。
3. 打开一个正常 FLV，查看 Tag 页和诊断页；关闭后再次打开。
4. 打开损坏 FLV，确认部分结果/结构诊断与打开失败有区分。
5. 在较慢扫描中取消，确认结束后保留部分结果；扫描中关闭窗口，确认进程
   能退出且文件不再占用。任意阻塞 I/O 不保证立即被强制打断。
6. 检查较窄窗口、高 DPI、长文件名和键盘焦点；确认 CSP 没有阻止 Wails IPC。

## 复现浏览器检查

先完成生产前端构建，并按 web-access 技能使本机 Chrome/CDP proxy 可用：

```powershell
cd desktop/frontend
$env:CDP_PROXY_URL = 'http://127.0.0.1:13456'
npm run test:browser
# 仅当 Chrome 支持后台截图时额外设置：
$env:MUXRAY_SCREENSHOT = '1'
npm run test:browser
```

本机端口 3456 落在 Windows 保留范围 3407–3506 中，故通过技能支持的
`CDP_PROXY_PORT=13456` 启动代理；没有更改 Windows 或 Chrome 安全配置。
代理端口不是应用配置，不写入生产包。可选截图输出到忽略的 `.cache/`。

本批不修改现有 CLI/JSON 合同，不提交媒体或二进制，不进行公开发行。
其他桌面系统、签名、安装器和完整第三方许可证/安全审查仍未验收。
