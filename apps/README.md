# 独立 oiapp

这里收录随项目维护的独立 App。每个目录都是完整源码包：YAML、worker 实现、依赖、构建入口、测试和使用说明放在一起，可以整体复制到主工程之外构建和交付。

| App | 目标平台 | 构建入口 | 运行包 |
| --- | --- | --- | --- |
| [Bash](bash/README.md) | Linux | App 目录下 `sh ./build.sh` | `app.yaml` + `runtime/oiapp-bash` |
| [PowerShell](powershell/README.md) | Windows | App 目录下 `./build.ps1` | `app.yaml` + `runtime/oiapp-powershell.exe` |

两个 App 各有独立 `go.mod` / `go.sum`，不依赖主工程的 Go 包、`go.work` 或本地 `replace`。根目录的构建脚本只负责组合交付，普通 `go test ./...` 不会进入这些独立模块；测试应在对应平台逐个模块运行。CI 将 App 源码复制到仓库之外再构建和测试。

## OIC 与 App 的边界

OIC 从 YAML 读取 Action、输入 Schema 和 runtime，托管 worker、适配输入、转发调用与取消、向 OIS 登记状态。App 自己实现能力并持有执行状态；Bash / PowerShell 的命令、终端、会话、输出缓冲和子进程回收均在各自 App 内。OIC 不导入 App 代码，也不识别 Bash 或 PowerShell 命令。

接入边界是 JSONL worker 协议与 YAML 声明，不是共享 Go 包。当前两个 App 各自维护所需协议类型和进程管理代码；公共 SDK 或依赖发布机制不属于此次重构。第三方 App 可以使用其他语言和独立仓库，无须修改 OIS/OIC 或注册到这个源码目录。完整规则见 [App runtime](../docs/spec/reference/app-runtime.md)，各 App 的 README 自带最小协议、配置与调用说明。

## 使用与迁移

构建完成后，在目标机器执行：

```text
oic --config /path/to/oic.yaml load /path/to/app/app.yaml
oic --config /path/to/oic.yaml apps
```

`load` 不编译源码、不安装依赖。相对 runtime 路径按 YAML 所在目录解析；部署时仅需要 YAML 与对应 runtime，构建工具不必安装在受控机器上。访问凭据和连接地址留在 OIC 配置中。

从旧 `oiapp-terminal` 迁移时，先构建新 App，卸载原 App，再加载新路径；两个 `app.code`（`bash` / `powershell`）、Action 名、参数和 worker 协议版本保持不变，OIS 的固定绑定无需修改。卸载会结束旧会话，无法将它们迁移到新 worker。仅替换二进制后重复 `load` 不会热更新。

本次仅交付这两个 App，CU 尚未实现。
