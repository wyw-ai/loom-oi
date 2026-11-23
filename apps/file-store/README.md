# File Store OIApp

`file-store` 在 OIC 所在的 Host 上读取和写入本地文件，通过 WebDAV 与独立部署的文件服务传输原件。首版对接 AList v3.66.0 的本机存储驱动。共享实例须连接同一 AList 服务进程和挂载，后端提供 WebDAV 排他锁、完整文件可读及同目录重命名。多副本 AList 需要共享锁语义，其他存储驱动需分别验证。

## 构建与加载

Go 1.26。Windows 在此目录执行 `./build.ps1`，Linux 执行 `sh build.sh`。在目标 Host 加载生成的 `dist/file-store-windows/app.yaml` 或 `dist/file-store-linux/app.yaml`。目录内含清单和目标平台的独立 worker。

OIC 配置示例（本地根须已存在）：

```yaml
app_config:
  file-store:
    root: 'D:\oi\workspace'
    store: team-files
    webdav_url: 'https://files.example/dav/oi/'
    username: oi-files
    password_file: 'D:\oi-private\dav-password.txt'
```

`password_file` 为本机允许根以外的绝对路径，保存一行 UTF-8 口令，文件访问权限交给 OIC 运行账户。直接运行 worker 时还可选择 `password_env` 指定环境变量名，两个来源互斥。OIC 有子进程环境白名单，正式部署使用 `password_file`。没有认证的挂载可以同时省略用户名与凭据字段。账号可见的挂载路径决定 `webdav_url`。使用 HTTPS 或可信的本机隧道承载凭据。上传方需要 WebDAV 读写、建目录、上传、同目录重命名、清理暂存文件和排他锁，读取方只需要列举、读取。

## 文件 Action

| Action | 行为 |
| --- | --- |
| `put_file(path, store_path?)` | 从本地根内的普通文件流式上传。省略存储名时生成唯一路径。返回 `store_path` 及含 `store, file_id, size_bytes, sha256` 的 `file_ref`。已占用路径返回 `ALREADY_EXISTS`。 |
| `list_directory(store_dir, limit?, cursor?)` | 使用 `.` 列存储根，使用相对目录列一层。返回 `entries`、`next_cursor`。目录条目变化时重新列第一页。 |
| `lookup_file(store_path)` | 按完整逻辑路径查回已提交的 `store_path, file_ref`。 |
| `get_file(file_ref, dest)` | 校验调用范围及后端元数据，流式读回到本地根内的临时文件。长度与 SHA-256 一致后用同目录硬链接无覆盖地提交新路径。目标目录须支持硬链接（如本地 NTFS 或 ext4）。 |

Host A 和 B 采用相同的逻辑存储、实际 WebDAV 目录、Service ID 与可信调用身份时可交接文件。不同 Service 或身份的文件保存在独立后端子目录，包括使用不同消费凭据引用的调用。A 上传后，B 可直接取引用、按路径查找，或列目录再选择。原件位于 `oi-v1/<scope>/files/<store_path>`，查询元数据在同一范围的 `meta/<store_path>`。这些目录由 File Store 管理写入。

上传先写同目录唯一暂存文件，核对可见长度后，在挂载上的短期提交锁内确认目标未占用、保存元数据、重命名原件，再查回回执。最终文件名是可见性标记。收到异步受理时仅在有界等待中确认实际完成后返回成功。MOVE 期间失去响应或取消时仍会尝试独立查回结果。`COMMIT_UNCONFIRMED` 的错误详情含 `store_path` 和预期引用，可先按路径查找，再用相同路径及相同原始字节恢复。不可见的暂存文件不能作为成功回执。单个锁覆盖一个调用范围内的短提交，以处理驱动间大小写和路径别名差异；源端读取与上传流不占用这把锁。

运行 `go test ./...` 可验证并发无覆盖、跨实例发现和读回、作用域隔离、摘要、失败恢复及目录游标。
