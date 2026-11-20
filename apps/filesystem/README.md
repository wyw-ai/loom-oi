# Filesystem oiapp

Filesystem 通过 OIC 在一个固定本地目录中提供七个 Action。使用当前支持 `content.v1` 的 OIC 加载发行包中的 `app.yaml`，在 OIS 上为所需 Action 绑定 Tool 和 vMCP。实例默认只读。安装不同 `app.code` 的实例可分别绑定不同目录。

## 构建与安装

使用 Go 1.26 或更高版本，在模块根目录运行 `go test ./...`。通过 `go run ./cmd/package -os windows -arch amd64` 或 `go run ./cmd/package -os linux -arch amd64` 构建。Linux 还可选择 `-arch arm64`。`build.sh` 和 `build.ps1` 调用同一打包入口。

将 `dist/filesystem-<os>-<arch>/` 作为整体复制到受控端。`checksums.txt` 包含包内数据和可执行文件的 SHA-256 校验和。运行包已携带 worker，无需在受控机安装 Go、Python、Node、rg 或 shell。Linux 本地文件系统须支持同目录硬链接与重命名，Windows 修改目标须在本地 NTFS。避免部署到要求完整复制扩展属性、SACL、EA 或 POSIX ACL 的目录。

从 Windows 复制 Linux 包时，在 Linux 包目录运行 `chmod +x runtime/oiapp-filesystem` 恢复可执行位。可以运行 `sha256sum -c checksums.txt` 校验文件内容。

包内 `examples/oic.<os>.yaml` 演示 OIC 配置，`examples/ois.tools.yaml` 提供可加入 OIS 配置的 Tool 定义。把 `root` 改成已经存在的绝对目录，确认需要修改文件时才显式设置 `read_only: false`。改变实例配置后重启 OIC，再加载 App。

## 接口

| Action | 调用方式 |
| --- | --- |
| `list_directory` | 在一层目录中分页列出条目。续页传相同路径和上页游标。 |
| `find_files` | 用相对搜索目录的 `pattern` 匹配普通文件，支持 `**`。 |
| `search_content` | 用 `query` 查找逐行字面值或 Go 正则。`status.complete` 表示本次是否查全。 |
| `read_file` | 读 UTF-8 文本、字节范围和完整版本。大文件流式续读，版本为空。 |
| `write_file` | 按 `create` 独占创建，或用 `replace` 和 `expected_revision` 替换现有文件。 |
| `edit_file` | 传实际版本和一组 `old_text/new_text` ，在同一份原文中匹配并一次提交。 |
| `view_image` | 返回现有静态 PNG、JPEG 或 WebP 的 MCP 原生图像。读取小字时传方向校正后源像素单位的 `crop`，获得原像素无损 PNG。 |

路径推荐写成实例根相对路径。遇到版本冲突请重新读取，结果未知时先核对目标再发起新修改。临时文件以 `.oi-filesystem-` 加随机标识命名。进程异常退出后，管理员可核对具体残留项的归属后处理。完整接口和约束见独立模块中的 `contract/schemas.json` 与发行清单。

