# File Store OIApp

官方 File Store 的 `app.code` 为 `file-store`。它作为普通 OIApp 在选定 Host 的 OIC 上执行。服务器端运行 AList，首版验收使用 AList v3.66.0 的 Local 挂载与 WebDAV。Windows 和 Linux 使用各自运行包中的 `app.yaml` 和 Go worker。[构建与部署指南](../../../guides/service-files.md) · [App 源码](../../../../apps/file-store/)

## 路径与文件引用

| 值 | 解释 |
| --- | --- |
| `path`、`dest` | 执行 Action 的 Host 上，相对于该实例允许的源目录或目标目录的本地路径。生产文件的 App 和上传方需要访问同一份原件。 |
| `store_path`、`store_dir` | 所配置存储范围内的根相对路径和目录，可用于跨会话查找。存储路径与本机路径分别解释。 |
| `file_ref` | 包含 `store`、`file_id`、`size_bytes` 和 `sha256`。前两项定位一份不可变原件，后两项用于取回时校验。对象及还原引用所需的元数据保存在后端。 |

OIC 通过 `app_config.file-store` 为实例配置本机 `root`、逻辑 `store`、挂载根 `webdav_url`。认证账户使用 `username` 和 `password_file`。密码文件为根目录外的绝对路径，内容为单行 UTF-8 密码，只对运行账户开放。凭据由 worker 访问 AList 时使用。

调用者须有权使用对应 Service 成员。worker 根据实例的 `store` 与 OIS 提供的 Service ID、`access_identity` 隔离对象。在这一范围内，上传回执及原件保存在后端，lookup 和 get 可以在另一 Host 上重新解析。同一挂载中的其他身份不能只凭 `file_ref` 取得该文件。

## 四项 Action

| Action | 契约 |
| --- | --- |
| `put_file(path, store_path?)` | 打开允许目录内的源文件，流式上传并计算字节数和 SHA-256。省略目标时生成唯一存储路径。默认对已占用路径报告冲突。完整原件与查询元数据写入且可查回后，返回实际 `store_path` 和 `file_ref`。后端异步受理时仍须确认写入完成。 |
| `list_directory(store_dir, limit?, cursor?)` | 根目录传 `.`。按名称排序返回一层子项的 `name`、`kind`（`file` 或 `directory`）、`store_path`。`limit` 默认 100，范围 1–200。`next_cursor` 非空时可续页，目录发生变化导致 `STALE_CURSOR` 时从首项重新列举。 |
| `lookup_file(store_path)` | 在指定存储范围内按完整路径精确定位，按读取权限返回对应 `file_ref`。目标路径缺失时报告未找到。 |
| `get_file(file_ref, dest)` | 下载到目标 Host 的允许目录。先写临时文件，校验长度和 SHA-256，再提交到目标位置并返回本机根相对路径。返回值包含 `path`、`size_bytes` 和 `sha256`。目标存在时报 `ALREADY_EXISTS` 并保留原文件。 |

上传需要可读写的单个 AList 实例及同目录重命名、排他锁的支持。后端以最终文件名标记可见原件，同一逻辑路径不能覆盖。如在提交阶段收到 `COMMIT_UNCONFIRMED`，先使用预定 `store_path` 查回回执，再决定是否以相同路径和原始字节重试。下载目标目录所在文件系统须支持同目录硬链接。[存储提交与恢复](../../../../apps/file-store/README.md)

## 两台 Host 如何交接

前提是 A、B 的实例指向同一后端挂载和逻辑 `store`，调用处于同一 Service 与消费身份范围。A 获准写入，B 获准读取对应对象。按目录浏览还需对该目录有列举权限。发布者可以将两个 Host 的不同成员选入同一个 [Service](../service.md)。

1. A 的生产者在本机生成 `captures/83.png`。Agent 调用固定到 A 的 `put_file`，传入该路径及 `shots/host-a/83.png`。上传成功后获得文件引用。
2. 有引用时，直接调用 B 的 `get_file`，例如取回到 `inbox/83.png`。只知道完整存储路径时先调用 B 的 `lookup_file`。只知道目录时先 `list_directory`，再用所选条目的 `store_path` 查引用并取件。
3. B 校验原件并返回本地路径后，可把该路径交给能读取同一目录的其他 App。文件在存储中保留期间，B 可在 A 离线后取件。反向传递时交换两个实例的读写角色与授权。

文件原件沿 A、存储后端、B 传输。OIS 路由调用与回执，供 Agent 看图的 MCP 图像沿已有结果通道返回。多张截图并发时，通过上传回执或请求标识确认指定画面。
