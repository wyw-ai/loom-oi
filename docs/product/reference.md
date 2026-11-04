# P1配置与接口参考

描述已实现的P1产品；规范性语义以[B1](../architecture/p1/b1-protocol.md)–[B4](../architecture/p1/b4-baseline.md)为准，两者冲突时以规格为准并视为缺陷。所有配置文件为UTF-8 JSON，严格解码：未知字段、重复键或尾随内容都会使加载失败。

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
| `close_ms` | T_close | 5000 | OIS关闭宽限：腿与控制连接的关闭握手至多等待T_close，对端未应答即断开传输；OIC与`oi`等待OIS关闭腿的上限 |
| `retain_ms` | RETAIN | 60000 | Closed记录与幂等记录保留 |
| `heartbeat_ms` / `offline_misses` | H/OFFLINE_MISSES | 10000/3 | OIS心跳与离线判定 |
| `license_ttl_ms` / `license_margin_ms` | L/LICENSE_MARGIN | 30000/2000 | OIS下发L；OIC本地扣减余量 |
| `ping_ms` | PING | 15000 | OIS数据腿ping；两个周期无任何收发即断开 |
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
| `diag_listen` | 可选，只接受回环地址；提供`GET /stats` |
| `log_level` / `limits` | 同OIS |

**revision**：`rv_`＋SHA-256后小写base32的前26个字符；输入为按键排序、无空白的JSON`{"protocol":…,"target":{"address":"<规范化host:port>","allowed_cidrs":[<规范化、去重、排序的网段>]},"transport_profile":"tcp"}`，主机名转小写、IP取规范写法。同一配置在任何OIC实例与重启后得到同一revision。

## 4. HTTP API

Bearer令牌格式为`oi_<token_id>_<secret>`。管理令牌（scope `admin`）只能调用`/v1/admin/*`，消费令牌（scope `consumer`）只能调用其余消费API，越界为403 `Denied`。

### 4.1 消费者（api监听器）

| 接口 | 成功 | 说明 |
| --- | --- | --- |
| `GET /v1/exposures?limit=&cursor=` | 200 `{items, next_cursor}` | `limit`默认50、上限100；按exposure_id排序；只含获discover授权的项；`readiness`恒为`unknown` |
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

写操作必须带`If-Match: "<config_version>"`：缺少428 `PreconditionRequired`，格式错400，过期409 `VersionConflict`且响应体含当前`config_version`；成功返回`{config_version}`与新`ETag`。存储不可用503 `StoreUnavailable`，写队列满503 `StoreBusy`（可重试）。

### 4.3 access监听器（OIC）

`GET /v1/access/control`（子协议`oi.control.v1`）与`GET /v1/channels/{id}/attach`（提供方腿）。TLS握手要求证书链到`client_ca_file`且指纹属于已启用Access；消息格式见[B1§4–5](../architecture/p1/b1-protocol.md)。

### 4.4 错误体

非2xx响应为`{"code": "<B1§7错误码>", "message": "…", "config_version": n?}`。HTTP状态按B1§7；另有`VersionConflict` 409、`PreconditionRequired` 428、`StoreBusy` 503、`Shutdown` 503（OIS停止中）、`Internal` 500。ABORT原因码见B1§7，`Completed`为两方向FIN后的关闭原因。

## 5. 命令行

| 命令 | 说明 | 退出码 |
| --- | --- | --- |
| `ois serve --config F` | 运行OIS | 0正常停止，1启动/运行失败，2用法 |
| `ois migrate --config F [--yes]` | 显示备份提醒；`--yes`才执行待迁移 | 0/1/2 |
| `ois init --config F [--subject S] [--ttl-seconds N]` | 空库建表并输出首个管理令牌（JSON一行） | 0/1/2 |
| `oic run --config F` / `oic check --config F` | 运行OIC／校验配置并输出revision | 0/1/2 |
| `oi [--ois URL] [--ca F] [--server-name N] [--data-max N] <cmd>` | 令牌取自`OI_TOKEN`（或`--token`），`OI_URL`、`OI_CA_FILE`为默认值 | 0成功，1 API错误或通道中止（stderr输出错误体JSON），2用法 |
| `oi discover` / `oi channel ID` / `oi close ID` | 消费者操作，输出JSON | 同上 |
| `oi connect EXPOSURE --listen ADDR` | 回环转发；stdout输出`{"listening": addr}`，每个连接结束在stderr输出`{"code", "bytes_up", "bytes_down"}` | 同上 |
| `oi connect EXPOSURE --stdio` | 单通道接标准输入输出；中止时退出码1 | 同上 |
| `oi admin …` | 见`oi help` | 同上 |
| `version` | 三个程序均支持 | 0 |

## 6. 诊断计数

OIS `GET /v1/admin/stats`：`channels{preparing,open,closing,closed}`、`tickets`（未消费票据）、`accesses_online`、`relay_bytes`、`max_buffered_bytes`（在途DATA峰值）、`store_healthy`、`epoch`、`process{goroutines,open_fds,rss_bytes,heap_bytes,cpu_ms}`。OIC `GET /stats`：`connected`、`registered`、`epoch`、`generation`、`license_remaining_ms`、`channels`、`relay_bytes`、`max_buffered_bytes`、`config_error`、`closed{原因:次数}`与同样的`process`。`open_fds`、`rss_bytes`、`cpu_ms`只在Linux上提供，其他系统为-1。
