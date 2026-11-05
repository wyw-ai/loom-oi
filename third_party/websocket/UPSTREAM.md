# 固定上游来源

本目录保留 `github.com/coder/websocket v1.8.15` 模块身份和 ISC [LICENSE.txt](LICENSE.txt)。64 个上游文件从已缓存的 Go module ZIP 逐字复制，未修改或删除；逐文件长度与 SHA256 见 [UPSTREAM.json](UPSTREAM.json)。本地新增文件见 [PATCHES.md](PATCHES.md)。

- 上游 Git：`https://github.com/coder/websocket`，tag `refs/tags/v1.8.15`，commit `9c8faadccd1b679e811a79ce506f8a10237251ad`。
- ZIP：81,554 字节，SHA256 `4f1d890bc5131f335737bbb6241e3958758c8774b3e6d1c905f713f604af6209`；解包 64 文件、215,501 字节。
- 上游模块内容：`h1:6B2JPeOGlpff2Uz6vOEH1Vzpi0iUz20A+lPVhPHtNUA=`。
- 上游 go.mod：`h1:NX3SzP+inril6yawo5CQXx8+fk145lPDC6pumgx0mVg=`，原 `go 1.23`，无额外模块依赖。

根模块保留 `require ... v1.8.15` 及原 go.sum 记录，以 `replace github.com/coder/websocket => ./third_party/websocket` 引用此固定副本。上述 h1 只标识未修改的上游内容，不是本地补丁后的摘要；发布来源必须另外包含本目录全部文件的实际 Git/blob 或 SHA256。禁止以更新 module cache 代替仓库补丁。

上游模块 ZIP 未包括其开发脚本提及的其他嵌套模块；不运行会下载工具或安装系统组件的 `ci/test.sh`。保留这些脚本仅为原件保真。受支持的原生构建平台沿 OI 原范围；JS/wasm 的新控制帧入口只提供明确不支持的错误，不能宣称浏览器能发送该帧。

升级时须固定新版本/许可证及完整文件清单，重审原 `writeFrame` 锁、mask、取消和错误路径，并运行原上游测试及本地补丁测试。不得只移动版本号或把本地新增代码排除在测试统计之外。
