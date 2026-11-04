# 配置与接口参考

描述当前工作分支已实现的接口，包括尚待正式验收的G2观测、准入与导出。规范性语义以[B1](../architecture/p1/b1-protocol.md)–[B4](../architecture/p1/b4-baseline.md)及[已采用G2契约](../architecture/p2/g2-observation.md)为准；实现状态见[TASK-007](../../spec/TASK-007-p2-g2-readiness/README.md)，不得把本参考作为发布通过证明。所有配置文件为UTF-8 JSON，严格解码：未知字段、重复键或尾随内容都会使加载失败。

<a id="ois-config"></a>
## 1. OIS配置

```json
{
  "api_listen": "0.0.0.0:7443",
  "access_listen": "0.0.0.0:7444",
  "tls": {"cert_file": "ois.pem", "key_file": "ois-key.pem", "client_ca_file": "oic-ca.pem"},
  "store": {"driver": "sqlite", "sqlite_path": "/var/lib/oi/ois.db"},
  "log_level": "info",
  "limits": {"prepare_ms": 10000}
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `api_listen` / `access_listen` | 是 | 两个监听地址（部署A：同一进程）。api只做服务端TLS；access强制客户端证书 |
| `tls.cert_file` / `tls.key_file` | 是 | 服务器证书与私钥（PEM），两个监听器共用 |
| `tls.client_ca_file` | 是 | 签发OIC客户端证书的CA（PEM） |
| `store.driver` | 是 | `sqlite`或`postgres`，不自动切换 |
| `store.sqlite_path` | SQLite | 数据库文件；锁文件为同目录`<path>.lock` |
| `store.postgres_dsn` | PostgreSQL | pgx DSN，例如`postgres://oi:…@db:5432/oi?sslmode=verify-full`；日志中密码被隐去 |
| `log_level` | 否 | `debug`/`info`（默认）/`warn`/`error` |
| `limits` | 否 | 覆盖下表默认值 |
| `observation_limits` | 否 | 有界观测控制预算，见§7；OIS与OIC取更严格值 |
| `diag_listen` / `metrics_enabled` | 否 | 默认无诊断监听且不导出指标；显式回环地址与`true`一起启用`GET /metrics`，见§8 |

相对路径以配置文件所在目录为基准。

<a id="limits"></a>
## 2. limits（B4§3）

| 键 | 符号 | 默认 | 使用方 |
| --- | --- | --- | --- |
| `n_ois` / `n_access` / `n_subject` | N_OIS/N_ACCESS/N_SUBJECT | 64/32/16 | OIS；计Preparing、Open与Closing通道（释放于Closed） |
| `max_local_channels` | MAX_LOCAL_CHANNELS | 32 | OIC |
| `data_max` | F | 32768 | OIS（经welcome告知OIC）；`oi --data-max`须不大于它 |
| `http_max` / `ctrl_max` | HTTP_MAX/CTRL_MAX | 65536/262144 | OIS请求体与控制消息上限；OIC控制消息上限 |
| `page_max` / `page_default` | PAGE_MAX | 100/50 | 目录与审计分页 |
| `max_exposures` / `desc_max` | MAX_EXPOSURES/DESC_MAX | 128/2048 | 登记上限（与Access的`max_exposures`取小）；OIC本地也校验描述长度 |
| `prepare_ms` | T_prepare | 10000 | OIS准备期限与票据有效期 |
| `dial_ms` | T_dial | 5000 | OIC；实际为min(T_dial, 剩余准备时间) |
| `close_ms` | T_close | 5000 | OIS关闭宽限：Closing通道与腿、控制连接的关闭握手至多等待T_close，对端未应答即断开传输；OIC与`oi`中止通道后等待OIS关闭腿的上限（正常完成时等待OIS转发完两个FIN后关闭腿，不以T_close截断） |
| `retain_ms` | RETAIN | 60000 | Closed记录与幂等记录保留 |
| `heartbeat_ms` / `offline_misses` | H/OFFLINE_MISSES | 10000/3 | OIS心跳与离线判定 |
| `license_ttl_ms` / `license_margin_ms` | L/LICENSE_MARGIN | 30000/2000 | OIS下发L；OIC本地扣减余量 |
| `ping_ms` | PING | 15000 | OIS向数据腿发送WebSocket ping；连续2次未收到pong按断开处理（B1§5） |
| `idle_ms` | IDLE | 600000 | 两方向均无DATA的空闲超时 |
| `token_ttl_ms` / `token_ttl_max_ms` | TOKEN_TTL/TOKEN_TTL_MAX | 30天/90天 | 令牌默认与最长有效期 |
| `write_queue` / `sqlite_busy_ms` | WRITE_QUEUE/SQLITE_BUSY_MS | 64/5000 | 存储写队列；SQLite忙等 |
| `store_probe_ms` | STORE_PROBE | 5000 | 存储健康探测间隔 |

<a id="oic-config"></a>
## 3. OIC配置

```json
{
  "access_id": "lab-a",
  "ois": {"url": "wss://ois.example:7444", "ca_file": "ois-ca.pem"},
  "tls": {"cert_file": "lab-a.pem", "key_file": "lab-a-key.pem"},
  "exposures": [
    {"name": "http", "protocol": "http", "description": "T1 read-only HTTP",
     "target": {"address": "127.0.0.1:8080"}},
    {"name": "db", "protocol": "postgres", "admission_mode": "require_ready", "enabled": false,
     "target": {"address": "db.internal:5432", "allowed_cidrs": ["10.20.0.0/16"]}}
  ],
  "diag_listen": "127.0.0.1:7600"
}
```

| 字段 | 说明 |
| --- | --- |
| `access_id` | 本Access的ID；exposure ID为`<access_id>/<name>` |
| `ois.url` | `wss://host:port`，指向access监听器；`ois.ca_file`为校验OIS证书的CA；可选`ois.server_name` |
| `tls.cert_file` / `tls.key_file` | 本Access的客户端证书，其SPKI指纹须已登记 |
| `exposures[].name` | `[a-z0-9][a-z0-9._-]{0,62}`，同一配置内唯一 |
| `exposures[].target.address` | `host:port`；目标只来自这里，OIS与消费者都不能提供或改写 |
| `exposures[].target.allowed_cidrs` | 主机名目标必填：拨号时解析，只保留网段内地址并直接拨该IP；IP字面量若配置网段须落在其内 |
| `exposures[].protocol` | 应用协议标签（如`http`），参与revision |
| `exposures[].admission_mode` | `best_effort`（默认）或`require_ready` |
| `exposures[].description` | ≤`desc_max`字节，不参与revision |
| `exposures[].enabled` | 默认true；禁用项不登记 |
| `exposures[].probe` | 可选、本地HTTP(S)健康契约，见§7；缺省或删除后为Unsupported/Unknown |
| `observation_limits` | 有限控制与probe调度预算，见§7 |
| `diag_listen` | 可选，只接受回环地址；提供`GET /stats`；OIS同样支持回环诊断 |
| `metrics_enabled` | 默认false；true须同时设置`diag_listen`，增加`GET /metrics`，见§8 |
| `log_level` / `limits` | 同OIS |

**revision**：`rv_`＋SHA-256后小写base32的前26个字符；输入为按键排序、无空白的JSON`{"protocol":…,"target":{"address":"<规范化host:port>","allowed_cidrs":[<规范化、去重、排序的网段>]},"transport_profile":"tcp"}`，主机名转小写、IP取规范写法。同一配置在任何OIC实例与重启后得到同一revision。

## 4. HTTP API

Bearer令牌格式为`oi_<token_id>_<secret>`。管理令牌（scope `admin`）只能调用`/v1/admin/*`，消费令牌（scope `consumer`）只能调用其余消费API，越界为403 `Denied`。

### 4.1 消费者（api监听器）

| 接口 | 成功 | 说明 |
| --- | --- | --- |
| `GET /v1/exposures?limit=&cursor=` | 200 `{items, next_cursor}` | `limit`默认50、上限100；按exposure_id排序；只含获discover授权的项；`readiness`为`ready`/`not_ready`/`unknown`，`readiness_view`携带来源、期限与历史，见§7 |
| `POST /v1/channels` | 201新建／200幂等重放 | 体`{exposure_id, expected_revision, client_request_id?, admission?}`；`admission`只能为`require_ready`；响应含`attach{url, ticket, expires_in_ms}`，票据已用后为null |
| `GET /v1/channels/{id}` | 200 | 仅通道所属主体可见，否则404；含`close_reason` |
| `DELETE /v1/channels/{id}` | 202（改变状态）／200（已Closed） | 原因`ClosedByClient`；响应为当前通道视图 |
| `GET /v1/channels/{id}/attach` | 101 | WebSocket，子协议`oi.tunnel.v1`；`Authorization`与`OI-Attach-Ticket`头；URL不得带查询参数（400） |

### 4.2 管理（api监听器）

| 接口 | 说明 |
| --- | --- |
| `GET /v1/admin/config` | `{config_version, epoch, store_id, backend, run_id, maintenance, store_healthy}`；`ETag`为当前版本 |
| `GET /v1/admin/stats` | 见§6 |
| `GET /v1/admin/audit?after=&limit=` | 按seq升序的审计记录 |
| `GET/POST /v1/admin/accesses`，`GET/PATCH/DELETE /v1/admin/accesses/{id}` | 体`{access_id, enabled?, spki_pins, max_exposures?, labels}`；`spki_pins`至多2个小写hex SHA-256；PATCH中`null`或缺省表示不变，`[]`/`{}`表示清空 |
| `GET/POST /v1/admin/tokens`，`DELETE /v1/admin/tokens/{id}` | 创建体`{subject, scope, ttl_seconds?}`，响应含唯一一次的`token`；DELETE为吊销 |
| `GET/POST /v1/admin/policies`，`GET/PATCH/DELETE /v1/admin/policies/{id}` | 体`{policy_id, subject, exposure_selector, actions, enabled?}`；选择器为精确exposure ID或`<access_id>/*`；actions为`discover`/`open`子集 |
| `PUT /v1/admin/maintenance` | 体`{"maintenance": bool}`（必填）；配置写，需`If-Match`；审计`maintenance_entered`/`maintenance_resumed` |
| `GET /v1/admin/store` | `{backend, engine, healthy, maintenance, write_failure?, usage{db_bytes, wal_bytes, page_size, page_count, freelist_count}}`；存储不可用时也应答（`healthy:false`） |
| `POST /v1/admin/store/maintain` | 体`{checkpoint?: "passive"\|"truncate", incremental_vacuum_pages?: 0..10000, optimize?}`；响应`{before, after, checkpoint?{busy, log_frames, checkpointed_frames}}`；PostgreSQL只接受`optimize`，其余400 |
| `POST /v1/admin/audit/prune` | 体`{through_seq, sha256}`；删除`seq≤through_seq`的记录，仅当其规范化摘要等于`sha256`，否则400且不删除；响应`{through_seq, deleted, sha256}` |

配置写操作（Access、令牌、策略、维护模式）必须带`If-Match: "<config_version>"`：缺少428 `PreconditionRequired`，格式错400，过期409 `VersionConflict`且响应体含当前`config_version`；成功返回`{config_version}`与新`ETag`。存储整理与审计修剪不改变配置版本，不要求`If-Match`，分别返回整理结果与按摘要核验的修剪结果。存储不可用503 `StoreUnavailable`，写队列满503 `StoreBusy`（可重试）。

### 4.3 access监听器（OIC）

`GET /v1/access/control`与`GET /v1/channels/{id}/attach`（提供方腿）。新OIC只协商`oi.control.v2`，失败时不降级；新OIS支持v2及旧OIC的v1，v1条目始终Unknown。新OIC连接仅支持v1的旧OIS会明确报告`VersionUnsupported`。数据腿仍用`oi.tunnel.v1`。TLS握手要求证书链到`client_ca_file`且指纹属于已启用Access；原消息见[B1§4–5](../architecture/p1/b1-protocol.md)，v2见[G2§5](../architecture/p2/g2-observation.md#wire)。真实兼容组合与当前验证范围由G2-13记录，不从协议名称推导任意旧版支持。

### 4.4 错误体

非2xx响应为`{"code": "<错误码>", "message": "…", "reason": "<有限原因>"?, "config_version": n?}`。HTTP状态按B1§7及G2§6；另有`VersionConflict` 409、`PreconditionRequired` 428、`StoreBusy` 503（写队列或并发管理写超过`WRITE_QUEUE`）、`Maintenance` 503（维护模式拒绝新Open，B3§8）、`Shutdown` 503（OIS停止中）、`Internal` 500；路径存在但方法不支持时为405与`InvalidRequest`。

就绪拒绝区分503 `TargetNotReady`、412 `ReadinessRequired`和503 `ReadinessCheckFailed`。最后一项表示检查没有得到可靠决定，两种准入模式都拒绝；不能当作已知Unknown继续。总准备期限仍为504 `PrepareTimeout`，子检查不续期。准备后失败通过ABORT传播：`ReadinessRequired`为`0x0107`，`ReadinessCheckFailed`为`0x0205`；原其他值保持，`GET channel`可查询准确关闭原因。`Completed`只表示两方向FIN后的传输完成。

## 5. 命令行

| 命令 | 说明 | 退出码 |
| --- | --- | --- |
| `ois serve --config F` | 运行OIS | 0正常停止，1启动/运行失败，2用法 |
| `ois migrate --config F [--yes]` | 显示备份提醒；`--yes`才执行待迁移 | 0/1/2 |
| `ois init --config F [--subject S] [--ttl-seconds N]` | 空库建表并输出首个管理令牌（JSON一行） | 0/1/2 |
| `ois backup --config F --out PATH` | SQLite一致备份（OIS可运行），输出含SHA-256的JSON；PostgreSQL拒绝并提示`pg_dump` | 0/1/2 |
| `ois restore --config F (--from PATH \| --external LABEL) [--min-epoch N] --yes` | OIS停止时恢复SQLite副本或标记已由其他工具恢复的库；结果进入维护模式 | 0/1/2（无`--yes`为2） |
| `ois export --config F --out PATH` / `ois import --config F --in PATH --yes` | OIS停止时导出／导入到空库（见[运维指南§9](operations.md#backup)） | 0/1/2 |
| `ois maintain --config F [--checkpoint passive\|truncate] [--vacuum-pages N] [--optimize]` | OIS停止时执行存储维护 | 0/1/2 |
| `ois admin resume [--ois URL] [--token T] [--ca F]` | 对运行中的OIS退出维护模式；默认读取`OI_URL`、`OI_TOKEN`、`OI_CA_FILE` | 0/1/2 |
| `oic run --config F` / `oic check --config F` | 运行OIC／校验配置并输出revision | 0/1/2 |
| `oi [--ois URL] [--ca F] [--server-name N] [--data-max N] <cmd>` | 令牌取自`OI_TOKEN`（或`--token`），`OI_URL`、`OI_CA_FILE`为默认值 | 0成功，1 API错误或通道中止（stderr输出错误体JSON），2用法 |
| `oi discover` / `oi channel ID` / `oi close ID` | 消费者操作，输出JSON | 同上 |
| `oi connect EXPOSURE --listen ADDR` | 回环转发；stdout输出`{"listening": addr}`，每个连接结束在stderr输出`{"code", "bytes_up", "bytes_down"}` | 同上 |
| `oi connect EXPOSURE --stdio` | 单通道接标准输入输出；中止时退出码1 | 同上 |
| `oi admin …` | 见`oi help`；含`maintenance on\|off`、`store [maintain …]`、`audit export [--through-seq N] --out F`与`audit prune --export F` | 同上 |
| `version` | 三个程序均支持 | 0 |

## 6. 诊断计数

OIS `GET /v1/admin/stats`：`channels{preparing,open,closing,closed}`、`tickets`（未消费票据）、`accesses_online`、`relay_bytes`、`max_buffered_bytes`（在途DATA峰值）、`store_healthy`、`epoch`、`process{goroutines,open_fds,rss_bytes,heap_bytes,cpu_ms}`。OIC `GET /stats`：`connected`、`registered`、`epoch`、`generation`、`license_remaining_ms`、`channels`、`relay_bytes`、`max_buffered_bytes`、`config_error`、`closed{原因:次数}`与同样的`process`。`open_fds`、`rss_bytes`、`cpu_ms`只在Linux上提供，其他系统为-1。存储大小与写入失败类别见`GET /v1/admin/store`（§4.2）。

观测启用后，`observation`包含有界的probe任务/执行中/排队/到期待调度数、历史峰值与开始/完成计数，以及待决query/check/Open/等待者数。`probe_max_wait_ns`与`probe_oldest_due_ns`是纳秒时长，不是墙钟时间。OIS的`observation_backlog`统计dirty或正在query的条目并集，每条最多一项；`dirty_marked`只统计尚未被query取走的标记，两者不能互换。此对象是本地/管理诊断，不把实体标识加入指标标签。

<a id="observation"></a>
## 7. 健康观测与准入

在一个OIC exposure中加入下列`probe`对象。部署者应确认该GET接口无业务副作用；探针地址与业务`target`分别受本地许可约束。

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

`kind`、URL、允许网段及两个非空互斥状态码集合必须显式提供；状态码范围200–599。四个时间字段省略时采用示例默认值；interval/validity范围100–86400000 ms，timeout为50–30000 ms，jitter为0至interval/2。删除probe禁用观测，不用零期限表达无限。HTTPS正常验证名称与CA，当前实现要求显式提供有效`tls_ca_file`。只允许HTTP(S)、host/port/path；禁止query、userinfo、fragment、重定向及环境代理，不支持自定义鉴权header、cookie、客户端证书或正文匹配。只读取有界最终响应头（8 KiB），不额外drain正文；最多保留4 KiB预读缓冲。

匹配显式状态才生成Ready/NotReady样本。开始下一次probe、timeout、网络/权限/TLS错误或未列出的状态码只改变attempt，不替换尚有效的旧样本，也不延长其寿命。有效期从请求发起的本地单调时点计算；`observed_at`是收到结果的展示时间。到期或绑定失效派生Unknown，保留`last_reported`历史；不能拿历史样本重新授权。

`observation_revision`与业务目标`revision`独立。probe配置或CA内容变化递增观测版本、取消旧probe/check/Preparing并失效旧样本；OIC已处理READY并进入Running的流保持；尚未本地放行者取消。目标/协议/传输或授权变化仍执行原关闭规则。A→B→A也产生新观测版本，重新连接清空本代有效样本。

两端顶层`observation_limits`可覆盖下表；字段均为正数，不能为null。控制协商取两端更小值，probe网络权限不会由OIS扩张。

| 字段 | 默认值 | 上限/约束 |
| --- | --- | --- |
| `message_max` | 4096字节 | ≤4096 |
| `pending_checks` / `query_slots` | min(32, N_OIS) / min(8, N_OIS) | 各≤N_OIS |
| `check_ms` / `query_ms` | min(1000, T_prepare) | 各≤T_prepare；所有阶段仍受原总准备期限约束 |
| `write_ms` / `refresh_ms` | 100 / 1000 ms | 分别≤100 / 1000 |
| `probe_workers` / `probe_queue` | min(4, MAX_EXPOSURES) / min(32, MAX_EXPOSURES) | 各≤MAX_EXPOSURES；由OIC本地调度 |

目录`readiness_view`包含authority/source、目标和观测版本、generation、sample_seq、reason、保守`remaining_ms`、`last_attempt`和可选`last_reported`；不含探针URL或凭据。目录缓存通过挑战查询扣除整段RTT，只用于展示。Open在本地执行preflight与两腿到齐后的final检查：有效NotReady两模式都拒绝；Unknown仅best_effort有界尝试；require_ready必须有未过期Ready。HTTP请求的`admission:"require_ready"`只能收紧本地模式。

Commit-Snapshot以OIC最终检查固定本次健康决定。检查后至READY前的新NotReady只影响后续Open；本次仍复核授权、目标、代数、许可和保守剩余期。观测配置的本地取消可能让消费者先READY再ABORT，但OIC不放行载荷。目录Ready、检查允许、READY和业务成功是四件不同的事；`oi connect`遵循目标本地模式，当前CLI不增加请求侧准入开关。

<a id="metrics"></a>
## 8. 回环指标拉取

OIS/OIC显式配置`"diag_listen":"127.0.0.1:7600","metrics_enabled":true`后，在该监听器提供`GET /metrics`（不同进程使用不同端口）。格式为Prometheus文本0.0.4；默认关闭，仅回环，无远程监听或无限超时选项。并发抓取最多2个、完整响应最多64 KiB、单次写期限200 ms；槽满429，生成内容超限503，不将半份内容标成成功。诊断端口没有Bearer认证，应由本机访问边界管理。

固定指标覆盖probe等待/attempt耗时、样本产生/到期、dirty合并、query RTT/发布延迟、check耗时/原因、两种Open结果、关键控制耗时及导出结果。持续时间以秒输出，有限标签只表示result/reason/stage；不含Exposure、Channel、subject、URL、IP或错误文本。counter以进程为作用域，重启归零；最新状态可合并中间事件，不保证完整监控历史。配置期限与直方图桶不是生产SLO。
