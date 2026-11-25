# Service 与 File Store 首版设计（封板快照）

2026-11-23 设计封板，以 beta `e40e35b7` 为实现基线。目标是通过 Service 组织跨 App、跨 Host 的能力，并沿用已有的 OIApp 加载、登记和执行流程。本文保留封板时的使用示例与实现取舍。研发及现场验收待执行。

## 从登记到调用

1. 提供方在选定的 Host 上配置并加载普通 OIApp。OIC 读取 app.yaml、启动 worker，待就绪后向 OIS 登记 App 及其 Action。Browser、Filesystem 和官方 file-store 使用同一套通道和 worker 协议。
2. 发布者从目录选择具体来源的 Action，组合成一份 Service。一个 Action 可以被多份 Service 复用。Service 管发布范围，MCP 负责工具发现与调用；新版领域模型移除 vMCP。
3. Agent 连接 Service 的 MCP 入口。OIS 检查调用者权限与成员关系，沿固定通道将所选 Action 派给相应 OIC。worker 返回业务结果，OIS 通过 MCP 交给 Agent。

例如，workbench 可以发布 Host A 上的 computer-use.screenshot 和指定 file-store 实例的上传 Action。截图由 Host A 的 worker 执行，file-store 的 worker 使用部署方配置的存储。filesystem.read_file 同样按固定来源执行，从该实例配置的根目录读取文件。

## 已确认的验收标准

用户请 Agent 看一眼 Host A 的屏幕。Agent 实际读到为本次请求截取的图像，并能准确说出屏幕内容。验收时将回答与当时画面核对。截图用于检验整条链路；Service 只负责组合和发布 Action。

## 交付范围与实施留项

- **图像交付。** 以 Host A 当次桌面截图验证上传、读回和看图。beta 的 filesystem.view_image 已能返回 MCP 图像，真实桌面采集和目标 Agent 的视觉回答仍需验收。
- **App 联动。** 首版目标包括上传、列目录、按路径查找和取回。Agent 按需调用。同机 App 在共同允许的目录内传根相对路径，跨 Host 使用文件引用。首验接通一个实际后端，部署时选择具体后端。
- **兼容迁移。** Service 内的发布名必须唯一。迁移要保留原 Tool 的消费侧契约、固定来源及可信访问范围。旧管理端的退场时间尚未指定。

## 目标领域模型与 API

Host 承载 OIC 和本机 App 实例，Channel 标识连接 OIC 的固定通道。App 提供 Action。OIS 以 (channel, app, action) 定位具体来源。Service 是新增的发布对象，其成员引用具体来源，同一 Action 可进入多份 Service。建议每个成员有 Service 内唯一的对外名，默认采用 Action 名，冲突时由发布者指定别名。MCP 把这些成员呈现为 Tool。file_ref 与可选的限时上传目标均由 File Store 定义和解释。

| 使用者 | 建议接口与行为 |
| --- | --- |
| 管理者选取来源 | 沿用 GET /api/v1/hosts、GET /api/v1/hosts/{id}/apps、GET /api/v1/actions。App 清单、登记、就绪状态仍由 OIC 提供。 |
| 管理者发布 | 新增 GET /api/v1/services 和 GET、PUT、DELETE /api/v1/services/{id}。PUT 一次提交成员、来源、访问规则和 MCP 发布选项；建议启用 MCP 后即返回入口。新的管理流程由 Service 直接引用 Action，替代先写全局 Tool 再写 vMCP 的两步操作。 |
| Agent 调用 | 建议沿用 /mcp/{id} 地址形式，由 Service ID 解析发布范围；tools/list、tools/call 和图像回执沿用 MCP。OIS 检查访问权限、成员关系和实际来源可用性。 |
| 已有 REST 调用方 | 建议保留 POST /api/v1/exec，并接受 service、action（Service 内发布名）、arguments；过渡期可将旧 vmcp、tool 字段映射到同一范围。 |
| OIC 与 App | Agent 依次调用普通 Action 时沿用现有连接、load、invoke/result/error 流程。迁移时须兼容原 worker 对可信 vmcp_id、身份和授权范围的依赖。 |

例如，PUT /api/v1/services/workbench 可以发布下列成员。actions 成员在发布时可以从登记目录取得描述和输入 Schema，并随配置保存。旧配置迁移时应保留原 Tool 的发布名、描述和消费侧 Schema，这些字段可能与登记值不同。

```json
{
  "actions": [
    {"name": "capture_a", "source": {"channel": "host-a", "app": "computer-use", "action": "screenshot"}},
    {"name": "put_from_a", "source": {"channel": "host-a", "app": "file-store", "action": "put_file"}},
    {"name": "list_store_a", "source": {"channel": "host-a", "app": "file-store", "action": "list_directory"}},
    {"name": "lookup_on_a", "source": {"channel": "host-a", "app": "file-store", "action": "lookup_file"}},
    {"name": "get_on_a", "source": {"channel": "host-a", "app": "file-store", "action": "get_file"}},
    {"name": "view_on_a", "source": {"channel": "host-a", "app": "filesystem", "action": "view_image"}}
  ],
  "publish": {"mcp": true},
  "access": {"credential_env": "WORKBENCH_TOKEN"}
}
```

建议同时覆盖首次启动导入的 beta YAML 和已运行实例的持久 JSON。按原 vMCP 的 ID、Tool 绑定和访问策略生成 Service，保留原发布名、描述、输入契约和权限。旧管理端 /tools、/vmcps 的写入切换以及具体兼容期尚待决定。

## 官方 file-store 与联动（实现建议）

建议随版本发行独立的 `apps/file-store` 包，包含 `app.yaml` 与 Go worker，由部署方在选定 Host 的 OIC 加载。实例配置可读取和可写入的本地根目录、逻辑存储名、后端位置与凭据引用。首版建议先打通一种真实后端。例如用 WebDAV 对接 AList 的可写挂载，部署方须确认目标挂载及账号允许上传。已挂载的 NAS 目录或直接对接云桶也是候选，后端由部署场景决定。[AList WebDAV 文档](https://alistgo.com/zh/guide/webdav.html)

### 上传、浏览存储目录、按路径查找和取回

| 首版 Action | 输入和结果 |
| --- | --- |
| `put_file(path, store_path?)` | `path` 相对于执行实例的本地可读目录。`store_path` 是目标存储范围内的根相对路径，省略时由 File Store 生成唯一的存储路径。worker 流式上传并计算长度和 SHA-256。后端确认写入完成且可按路径查回时，返回实际的 `store_path` 和 `file_ref`。建议首版对已被占用的目标路径返回冲突。 |
| `list_directory(store_dir, limit?, cursor?)` | 按调用者的目录列举权限读取存储根下指定目录的直接子项。返回 `entries`，每项含 `name`、`kind`（文件或子目录）和 `store_path`，可附已有的大小和修改时间。文件条目只展示已完成上传且调用者可见的对象。若同次列举即可取得已保存的 `file_ref`，可随条目带回。仍有下一页时返回 `next_cursor`。 |
| `lookup_file(store_path)` | 在实例配置的存储范围内按完整路径精确查找。按与下载相同的权限检查对象，找到时返回 `file_ref`（含字节数和 SHA-256），指定路径缺失时返回未找到。查询结果可直接交给另一个有权读取同一存储的实例。 |
| `get_file(file_ref, dest)` | worker 按可信调用范围与实例配置检查存储和对象的读取权限，流式下载到本机允许的目标目录。先写临时文件，核对引用中的长度和摘要，随后提交到指定路径并返回根相对路径。建议默认保留已有同名文件，冲突时报告给调用方。 |

建议 `file_ref` 包含 `store`、`file_id`、`size_bytes` 和 `sha256`。前两项定位同一存储范围内的一份不可变原件，后两项供接收方核对字节。存储路径让调用者在新会话里重新找到文件。对象键与还原引用所需的元数据由 File Store 随对象保存在后端，使同一存储的其他授权实例在源 Host 离线后也能查回同一份引用。各实例持有各自的后端凭据，读取按可信身份和允许的对象范围授权。

### Host A 的截图如何验收

1. Host A 上提供能访问当前桌面的截图 Action。Agent 通过 `capture_a` 取得本次采集的原件路径，例如 `captures/83.png`。原件位于截图 App 可写且 File Store 可读的目录。beta 的 Browser 截取其专用 Chromium 页面，桌面采集需单独提供并实测。
2. Agent 调用 `put_from_a`，传入这个本地相对路径，也可指定一个便于日后查找的存储路径，例如 `shots/host-a/83.png`。A 上的 File Store 把原图存入配置的后端，成功后交回存储路径和文件引用。其他 Action 产生的文件也可以走相同步骤。
3. 为验证保存的确是这一次的原件，Agent 将引用交给 `get_on_a`，读回到另一处，例如 `received/83.png`。该目录同时位于 A 上 Filesystem 的可读根内。Agent 用 `view_on_a` 取得这份文件的 MCP 原生图像，描述当时屏幕上可核对的内容。只看屏幕时，也可以直接消费截图 Action 返回的图像。

### 未来从 Host A 传给 Host B

当 A 和 B 的 File Store 配置到同一个实际存储范围，发布者可以在同一 Service 中将 A 的 `put_file` 发布为 `put_from_a`，将 B 的 `get_file` 发布为 `get_on_b`。调用者获准使用两个成员。A 的实例有权写入，B 的实例有权读取对应对象，各自的本地根目录独立配置。

Agent 先调用 `put_from_a` 存入 A 上的文件，再把返回的 `file_ref` 和 B 上的目标相对路径传给 `get_on_b`。原件依次经过 A、存储后端和 B。B 校验并写入后，即可把返回的本地路径交给 B 的其他 Action。文件在后端保留期间，B 可以晚于 A 上线取回。两台 Host 只需各自能访问后端。反向传递时调换发起上传和接收下载的实例。

如果 B 的调用者只知道约定的存储路径，可以通过 Service 已发布的 B 端 `lookup_file("shots/host-a/83.png")` 重新取得文件引用，再交给 `get_on_b`。手里已有上传回执时可以直接取回。查找 Host 本机目录中的文件由对应 Host 的 Filesystem 提供。

如果 B 只知道共享存储目录，可以调用 B 端已发布的 `file-store.list_directory("shots/host-a/")` 浏览一层文件和子目录，再挑选一项。文件条目带有 `file_ref` 时可直接取回，否则只对选中的 `store_path` 调一次 `lookup_file`。同一目录中可能有并发请求的多张截图。核对指定请求时应使用该次上传回执，或按约定的请求标识从目录或文件名中选出对应项。目录修改时间和文件修改时间仅供浏览。

若 A 和 B 使用独立的存储范围，仍须有一方能够访问对方授权的传输端点。以支持预签名上传的后端为例，可以由 B 的 File Store 为指定对象签发限时上传目标，由 A 的 File Store 将本机文件直传该目标。B 确认对象落盘后从自己的后端取回。凭据留在各自实例，这属于可后续增加的联动方式。[阿里云 OSS](https://help.aliyun.com/zh/oss/user-guide/upload-files-using-presigned-urls) · [腾讯云 COS](https://cloud.tencent.com/document/product/436/68284)

原件通过 App 与存储后端之间的连接传输。OIS 按固定来源派发普通调用、转发业务回执，并沿已有图像链路向 Agent 交付预览。Agent 顺序调用即可完成上述流程。

## 首版交付清单

| 交付项 | 具体改动 |
| --- | --- |
| OIS 发布模型与配置 | 实现 Service 及其成员、固定来源、访问规则和 MCP 发布配置。迁移 beta 的首次启动配置和持久数据。 |
| OIS 管理与消费接口 | 实现 Service 管理 API，让 MCP 发现和调用、REST 执行和权限检查都按 Service 解析成员。迁移时验证旧入口、工具名、输入契约以及 worker 收到的可信访问范围。 |
| 官方 File Store | 发行可由 OIC 加载的 App 包及配置示例，接通一个实际后端，交付上传、列目录、按路径查找和取回。文件引用及查询元数据由后端保存，使授权实例可以独立定位原件。 |
| 真实桌面与读图 | 在 Host A 接入能访问交互桌面的截图 Action，让它产生 File Store 可读的原件，复用 Filesystem 的 MCP 原生图像能力。 |
| 端到端验证 | 发布一份 workbench，验证保存后按引用、完整路径和目录找到同一份文件。以 Agent 根据读回图像准确描述 Host A 当次画面作为整条链路的验收结果。 |
