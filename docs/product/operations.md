# loom-oi 运行手册

本手册面向当前 alpha 实现的 `ois`、`oic` 与 `oi`。OI 负责发现、授权和 TCP 通道；SSH 登录、Bash 执行及业务结果由原生服务负责。首个目标是通过 OIC 访问已有 SSH/Bash 服务，真实场景验收尚未完成，以下示例不构成已验证部署或容量承诺。

职责与安全边界见[项目理念](../concept/loom-oi.md)，连接、状态与故障语义见[整体架构](../architecture/alpha-v0.7.md)。

## 构建与部署准备

使用 Go 1.27.3，在完整仓库根目录构建；仓库内的 `third_party/websocket` 是必需依赖。

```sh
CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...
go version -m bin/ois
```

PowerShell 先执行 `$env:CGO_ENABLED='0'`，再执行同一 `go build` 命令，查看构建信息时使用 `bin/ois.exe`。以下命令假定 `bin` 已加入 PATH，也可使用二进制的实际路径。交叉编译通过 `GOOS`、`GOARCH` 选择目标平台；构建成功不证明实际运行兼容。三个程序均支持 `version` 和 `help`；常规退出码为 0 成功、1 运行或 API 失败、2 参数或配置错误。

可信中继主机运行 OIS，业务服务所在环境运行 OIC，消费者运行 `oi` 与原生客户端。OIC 主动出站连接 OIS；SSH 等目标服务须事先安装、启动并由业务方维护。OIS 是可信中继；需要业务端到端保密时，继续使用 SSH/TLS 等原生加密及身份校验。

## 证书、凭据与 OIS

两个 OIS 监听器共用服务器证书，最低 TLS 1.3。服务器证书 SAN 必须覆盖实际访问的域名或 IP。OIC 使用客户端证书：OIS 同时验证其签发 CA 和已登记的 SPKI SHA-256 指纹，不以证书 CN/SAN 代替 Access 身份。

| 示例文件 | 用途 |
| --- | --- |
| `ois.pem`、`ois-key.pem` | OIS 服务器证书链与私钥 |
| `ois-ca.pem` | OIC、消费者验证 OIS 的 CA |
| `oic-ca.pem` | OIS 验证 OIC 客户端证书的 CA |
| `lab-a.pem`、`lab-a-key.pem` | Access `lab-a` 的客户端证书链与私钥 |

私钥仅保存在使用它的主机，并限制文件权限。OIS 配置示例 `ois.json`：

```json
{
  "api_listen": "0.0.0.0:7443",
  "access_listen": "0.0.0.0:7444",
  "tls": {
    "cert_file": "ois.pem",
    "key_file": "ois-key.pem",
    "client_ca_file": "oic-ca.pem"
  },
  "store": {"driver": "sqlite", "sqlite_path": "/var/lib/oi/ois.db"},
  "log_level": "info"
}
```

配置是最大 1 MiB 的 UTF-8 JSON；未知字段、重复键、尾随内容会被拒绝。相对文件路径以配置文件目录为基准。`api_listen` 面向消费者与管理 API，`access_listen` 强制 OIC 客户端证书认证。按实际访问范围配置防火墙。

`store.driver` 必须显式选择 `sqlite` 或 `postgres`，不能同时填写两种后端字段，也不会在失败时自动切换：

- SQLite 使用本地磁盘，不使用网络共享文件系统。OIS 使用 WAL、`synchronous=FULL`、增量 vacuum，并独占 `<数据库路径>.lock`。
- PostgreSQL 需预先建库及账号。将 `store` 改为 `{"driver":"postgres","postgres_dsn":"postgres://oi:REPLACE_ME@db.example:5432/oi?sslmode=verify-full"}`，配置服务器证书信任并保护含口令的文件。`synchronous_commit=off` 会被拒绝；advisory 锁限制同库只有单个 OIS，不提供主备选举。

```sh
ois init --config ois.json --subject admin
ois serve --config ois.json
```

`init` 输出首个管理令牌，仅此一次返回明文，应安全保存。已有库缺少有效管理令牌时，可在 OIS 停止后补发；已有有效管理令牌则拒绝。`serve` 取得存储锁、检查模式、递增 epoch、恢复配置后才监听。升级若提示模式待迁移，先备份并停止 OIS，再执行 `ois migrate --config ois.json --yes`；启动不会自动迁移。

日志是 stderr 上的 JSON 行。令牌、票据和鉴权头不应进入日志或工单；数据库口令会被日志脱敏，原配置文件仍包含秘密。`SIGINT`/`SIGTERM` 触发受控停止，新请求被拒绝，现有通道以 `Shutdown` 中止；OI 不保存可续接的业务会话。

## 登记、授权与 OIC 配置

管理终端设置地址、CA 与管理令牌。以下是 POSIX shell；PowerShell 使用 `$env:OI_URL='https://ois.example:7443'` 等对应赋值。

```sh
export PATH="$PWD/bin:$PATH"
export OI_URL='https://ois.example:7443'
export OI_CA_FILE='/etc/oi/ois-ca.pem'
export OI_TOKEN='替换为管理令牌'

oi admin access create lab-a --pin-cert lab-a.pem
oi admin policy create alice-ssh --subject alice --selector 'lab-a/ssh' --actions discover,open
oi admin token create --subject alice --scope consumer
oi admin config
```

环境变量避免令牌直接出现在命令参数中，但仍须保护终端历史、进程环境与本机账号。管理令牌只能调用管理 API，消费令牌也不能管理配置。策略分别授予 `discover` 与 `open`；可见不代表可访问。选择器可为精确 Exposure ID 或 `lab-a/*`，按必要范围授权。

`--pin-cert` 计算证书公钥指纹，也可用：

```sh
openssl x509 -in lab-a.pem -pubkey -noout | openssl pkey -pubin -outform der | sha256sum
```

每个 Access 至多两个指纹。轮换时先登记新旧两个指纹，再让 OIC 换证重启，确认后移除旧指纹；删除旧指纹会关闭仍使用它的连接。

OIC 配置示例 `oic.json`，暴露已有 SSH 服务：

```json
{
  "access_id": "lab-a",
  "ois": {"url": "wss://ois.example:7444", "ca_file": "ois-ca.pem"},
  "tls": {"cert_file": "lab-a.pem", "key_file": "lab-a-key.pem"},
  "exposures": [
    {
      "name": "ssh",
      "protocol": "ssh",
      "description": "现有 SSH 服务",
      "target": {"address": "127.0.0.1:22"},
      "admission_mode": "best_effort"
    }
  ],
  "diag_listen": "127.0.0.1:7602"
}
```

```sh
oic check --config oic.json
oic run --config oic.json
```

目标地址只能由本地 OIC 配置提供，OIS 与消费者不能指定另一目标。主机名目标必须填写 `allowed_cidrs`，例如 `{"address":"ssh.internal:22","allowed_cidrs":["10.20.0.0/16"]}`；拨号时只选网段内的解析地址并直接连接该 IP。IP 字面量若填写网段，也必须落在网段内。名称须符合 `[a-z0-9][a-z0-9._-]{0,62}`，最终 ID 是 `lab-a/ssh`。`enabled` 默认 true；`protocol` 是协议标签，不会使 OIC 执行业务命令。

`oic check` 校验配置并输出目标 revision，不连接业务目标或证明业务可用。OIC 断线后以 1–30 秒指数退避重连，新 generation 不沿用旧通道。运行中只支持重载 exposures：支持信号的平台可发送 `SIGHUP`，也会按默认 2 秒周期检测变化；非法候选保留旧配置并报告 `config_error`。目标、协议或允许网段变化使旧目标绑定失效；只改描述或准入模式不终止已运行的流。Access、OIS 地址、客户端证书、诊断监听及预算等字段变化需要重启。

## 通过通道使用 SSH / Bash

消费者终端使用刚签发的消费令牌：

```sh
export OI_URL='https://ois.example:7443'
export OI_CA_FILE='/etc/oi/ois-ca.pem'
export OI_TOKEN='替换为 alice 的消费令牌'
oi discover
oi connect lab-a/ssh --listen 127.0.0.1:2222
```

保持转发程序运行，在另一个终端连接：

```sh
ssh -p 2222 -o HostKeyAlias=lab-a-ssh alice@127.0.0.1
ssh -p 2222 -o HostKeyAlias=lab-a-ssh alice@127.0.0.1 'bash -s' < ./job.sh
```

这里的 `alice` 是示例 SSH 账号，与 OI 主体名没有自动绑定关系。首次连接须通过可信渠道核对**目标 SSH 服务**的主机密钥，保留 SSH 身份校验；`HostKeyAlias` 让该目标不与其他本地转发端口混淆。SSH 处理登录、密钥、权限与 Bash 进程，OI 仅传输字节。

`--listen` 只接受回环地址，每个本地 TCP 连接建立一个通道。同机其他用户也可能连接此端口并使用这份 OI 授权，多用户环境需隔离；也可用 `oi connect lab-a/ssh --stdio` 将单通道交给支持 stdio/ProxyCommand 的原生客户端。`oi channel ID` 查询状态，`oi close ID` 主动关闭。

**断线、超时或 ABORT 后，业务结果可能未知。** Bash 命令可能已经执行，数据库可能已经提交；应通过业务系统核对结果，不能自动重跑脚本或重放载荷。`Completed` 只表示双向传输正常结束，不表示命令退出成功。`oi` 只在 Open 因 `RevisionMismatch` 被拒绝时重新发现并尝试一次，不是业务请求重试机制。

## 健康观测与诊断

SSH 不需要 HTTP 探针才能使用；没有 probe 时健康状态为 Unknown。需要健康准入的服务可在 exposure 中加入独立、无副作用的 HTTP(S) GET 探针：

```json
"probe": {
  "kind": "http_status_v1",
  "url": "https://service.internal:8443/health/ready",
  "allowed_cidrs": ["10.20.0.0/16"],
  "tls_ca_file": "probe-ca.pem",
  "ready_statuses": [200],
  "not_ready_statuses": [503],
  "interval_ms": 5000,
  "timeout_ms": 1000,
  "validity_ms": 15000,
  "jitter_ms": 500
}
```

这是 exposure 内的字段片段。kind、URL、网段和两个非空互斥状态码集合必填，状态码限 200–599；四个时间字段省略时使用示例默认值。interval/validity 范围 100–86400000 ms，timeout 为 50–30000 ms，jitter 为 0 至 interval/2。HTTPS 必须配置有效 CA，正常验证名称；不支持 URL query/userinfo/fragment、重定向、环境代理、自定义鉴权、Cookie、客户端证书或正文匹配。只读取有界响应头，不把探针扩为业务操作或任意命令。

`best_effort` 默认允许对真实 Unknown 做有界连接尝试；`require_ready` 必须有未过期 Ready。有效 NotReady 两种模式都拒绝；检查本身失败也拒绝，不能当作 Unknown 放行。probe 超时或网络错误不会延长样本寿命，也不会立刻替换尚有效的旧样本；到期后当前状态为 Unknown，历史 Ready 只供诊断。修改 probe 或 CA 内容会更新独立观测版本，取消旧检查及尚未放行的通道，已进入 Running 的流继续。目录 Ready、最终准入、通道打开和业务成功须分别判断。

OIS 统计使用 `oi admin stats`（`GET /v1/admin/stats`），存储状态用 `oi admin store`。OIC 的回环 `GET /stats` 包含连接、登记、epoch/generation、许可剩余时间、通道、字节、关闭原因和 `config_error`。Linux 提供描述符、RSS、CPU 计数，其他系统对应不可用值为 -1。

两端分别设置 `diag_listen` 和 `metrics_enabled:true` 后提供 `GET /metrics`，例如 OIS 用 `127.0.0.1:7601`，OIC 用 `127.0.0.1:7602`。默认不启用指标；OIS 回环诊断端口不提供管理 `/stats`。诊断是无 Bearer 认证的本地 HTTP，须依靠本机访问控制，不能直接暴露公网。

指标为 Prometheus 文本 0.0.4，最多 2 个并发导出、64 KiB 完整响应、200 ms 写预算；槽满 429，生成超限 503。OIS 诊断监听另有 200 ms 请求头/读取/写入期限，连接后长时间不发请求也可能被关闭；OIC 诊断监听读取请求头期限为 5 秒，metrics Handler 仍保持 200 ms 写预算。聚合 timeout 计数不能归因到某个特定请求，更不能据此重放业务。

`observation.exporter_active` 与 `exporter_youngest_active_age_ns` 是同次快照的当前占槽数与最年轻占槽者年龄，429 不占槽；它们不证明之后仍占槽或剩余写预算。`observation_backlog` 是 dirty 或 pending query 条目的并集，`dirty_marked` 只是未取走的标记，不等于目标不健康。指标不带实体高基数标签，计数随进程重启归零，最新状态可能合并中间事件；不是完整审计历史。

## 运行预算

可选 `limits` 覆盖下列默认值；时间单位为 ms。这些是实现默认配置，不是吞吐、容量或资源承诺。

| 参数 | 默认值 / 含义 |
| --- | --- |
| `n_ois` / `n_access` / `n_subject` | 64 / 32 / 16 个通道；Closing 仍占额度 |
| `max_local_channels` / `max_exposures` | OIC 本地 32 通道 / 128 暴露项 |
| `data_max` / `http_max` / `ctrl_max` | 32768 / 65536 / 262144 字节 |
| `page_default` / `page_max` / `desc_max` | 50 / 100 项；描述 2048 字节 |
| `prepare_ms` / `dial_ms` / `close_ms` / `retain_ms` | 10000 / 5000 / 5000 / 60000 |
| `heartbeat_ms` / `offline_misses` | 10000 / 3 次 |
| `license_ttl_ms` / `license_margin_ms` | 30000 / 2000；本地扣减许可余量 |
| `ping_ms` / `idle_ms` | 15000 / 600000；数据腿活性与无 DATA 空闲期限 |
| `token_ttl_ms` / `token_ttl_max_ms` | 30 天 / 90 天对应的毫秒数 |
| `write_queue` / `sqlite_busy_ms` / `store_probe_ms` | 64 / 5000 / 5000 |

`close_ms` 限制异常终止后的关闭等待，正常完成仍要排空双方 FIN，不应截断尾部。拨号和检查始终受原总准备期限约束，不逐阶段续期。

两端另有 `observation_limits`：`message_max=4096`、`pending_checks=min(32,n_ois)`、`query_slots=min(8,n_ois)`、`check_ms=query_ms=min(1000,prepare_ms)`、`write_ms=100`、`refresh_ms=1000`、`probe_workers=min(4,max_exposures)`、`probe_queue=min(32,max_exposures)`。字段必须为正数；消息不得超过 4096，write/refresh 不得超过 100/1000，控制槽数不得超过 `n_ois`，检查期限不得超过 `prepare_ms`，probe 数量不得超过 `max_exposures`。协商不能扩大 OIC 本地网络权限。

## 常用 API 与错误处理

API 使用 HTTPS JSON 和 `Authorization: Bearer ...`。优先使用 `oi`；自行集成时保留状态码与错误体 `{code,message,reason?,config_version?}`。

| 接口 | 主要用途 |
| --- | --- |
| `GET /v1/exposures?limit=&cursor=` | 获授权目录及 readiness；返回 `items,next_cursor` |
| `POST /v1/channels` | 体含 `exposure_id,expected_revision`，可带 `client_request_id`、`admission:"require_ready"`；返回通道与一次性 attach 票据 |
| `GET/DELETE /v1/channels/{id}` | 查询本人通道 / 关闭通道 |
| `GET /v1/channels/{id}/attach` | WebSocket `oi.tunnel.v1`，带鉴权与 `OI-Attach-Ticket`，禁止 URL 查询参数 |
| `GET /v1/admin/config` | `config_version,epoch,store_id,backend,run_id,maintenance,store_healthy` |
| `GET /v1/admin/stats`、`GET /v1/admin/store` | 运行统计 / 存储大小、健康与写失败 |
| `/v1/admin/accesses`、`/v1/admin/tokens`、`/v1/admin/policies` | GET/POST 列表与创建；Access/Policy 支持单项 GET/PATCH/DELETE，Token 支持 DELETE 吊销 |
| `PUT /v1/admin/maintenance` | 体含布尔字段 `maintenance`，暂停或恢复新 Open |
| `GET /v1/admin/audit?after=&limit=` | 按 seq 顺序读取审计 |
| `POST /v1/admin/store/maintain`、`POST /v1/admin/audit/prune` | 存储整理 / 经过摘要核验的审计修剪 |

Access、令牌、策略与维护模式的配置写必须携带 `If-Match: "<config_version>"`。`oi` 自动读当前版本，或使用 `--if-match N`。缺失返回 428，格式错误 400，过期返回 409 `VersionConflict`；先读回最新配置并确认意图，不覆盖他人的变更。审计修剪与存储整理不改变配置版本，采用各自校验规则。

| 结果 | 检查与处理 |
| --- | --- |
| 404 / 403 `Denied` | 目标不存在或不可见 / 缺少 open 权限或令牌 scope 不符 |
| `AccessOffline` / `VersionUnsupported` | 检查 OIC 登记、CA、指纹与控制版本；新 OIC 不自动降级到旧控制 v1 |
| `TargetAddressNotAllowed` / `TargetConnectFailed` | 检查本地网段许可 / 目标服务与网络，不能改成任意目标绕过 |
| `TargetNotReady` / `ReadinessRequired` / `ReadinessCheckFailed` | 明确 NotReady / 缺有效 Ready / 检查无可靠决定；三者不是同一结论 |
| 429 | 通道额度或独立 metrics 槽已满；按实际入口区分 |
| 503 `StoreBusy` / `StoreUnavailable` / `Maintenance` | 写入排队饱和 / 存储不健康 / 禁止新 Open；读回状态再决定管理操作 |
| `PrepareTimeout`、传输中断、客户端超时 | 保留 channel/错误信息，通过业务系统核实结果，禁止自动重放载荷 |

撤销策略、令牌或 Access 会终止受影响通道，已发送字节无法撤回。存储故障时新 Open 与管理写拒绝，存量通道继续受原许可和生命周期约束；修复后由探测恢复。写满/只读不能靠成功的只读查询判定恢复，必须恢复实际可写。OIS 重启使旧通道、票据和 generation 失效，OIC 重新登记；消费者须新建连接并自行恢复业务会话。

## 备份、恢复与维护

备份数据库之外还须保存部署配置、证书和必要审计，私钥及导出文件不可公开。备份前记录 `oi admin config` 的 epoch 与配置版本。

**SQLite 备份**允许 OIS 运行：

```sh
ois backup --config ois.json --out /backup/ois-backup.db
```

命令生成一致副本，核对完整性、模式和 meta 后定名，并输出 SHA-256 等元数据；副本与元数据一起保存。不要直接复制运行中的 `.db` 并丢弃 WAL。PostgreSQL 使用部署方的 `pg_dump -Fc` 或 `pg_basebackup`，OI 不代为执行物理备份或时间点恢复。

**恢复时停止 OIS**，确保没有其他实例持库：

1. SQLite：`ois restore --config ois.json --from /backup/ois-backup.db --min-epoch 42 --yes`。`42` 仅为示例，应填写事故前实际观察到的 epoch。输入须为独立完整副本，旁边有非空 WAL 会被拒绝；可读原库与备份 `store_id` 不同也拒绝。旧库与 WAL 保留为 replaced 文件，不应手工混入恢复后的库。
2. PostgreSQL：先用 `pg_restore` 恢复到配置的数据库，再执行 `ois restore --config ois.json --external '已恢复的备份标识' --min-epoch 42 --yes`，登记恢复事实。
3. 恢复结果进入维护模式；启动 OIS 后新 Open 返回 `Maintenance`，管理与重新登记仍可用。核对并补做备份点之后的令牌吊销、策略删除、Access 禁用等变更，它们不会自动恢复。
4. 核对完成后执行 `ois admin resume` 或 `oi admin maintenance off`。计划维护也可先用 `oi admin maintenance on` 禁止新 Open。

**后端迁移**只支持离线导出、校验导入：停止 OIS → `ois export --config sqlite.json --out ois.export.jsonl` → `ois import --config postgres.json --in ois.export.jsonl --yes` → 切换配置并启动。目标必须为空，不要先 `init`；导入在单事务内校验数据与摘要，保留 store_id、epoch、配置版本和审计序号。源库不被修改，不支持双写、自动回切或在线迁移。导出文件含令牌摘要与配置，按凭据保管。

**数据操作完成与清理完成分开判断。** `init/migrate/maintain/export/import/restore` 可能已提交数据，却因关闭连接或释放锁失败而非零退出，并报告 `operation completed but cleanup failed`。保留已输出令牌或文件，读回数据库状态和锁持有者；不能把非零退出当作回滚并盲目重复操作。

审计默认不自动删除。先 `oi admin audit export --out audit.json`，再 `oi admin audit prune --export audit.json`；服务端核对规范化摘要，不一致则不删除。SQLite 可在低峰执行 `oi admin store maintain --vacuum-pages 10000 --checkpoint truncate`，长读事务可能使 checkpoint 返回 busy；停机时可用 `ois maintain`。PostgreSQL 此接口只支持 `--optimize`（ANALYZE），其他维护由数据库工具负责。

当前不提供自动主备、存量通道迁移、Unix socket 转发或业务执行器。配置默认值、局部测试和构建成功，都不能代替实际平台、SSH/Bash 场景、整体验收、容量与恢复耗时验证。
