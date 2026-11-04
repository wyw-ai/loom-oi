# P1运维指南

适用于P1交付的`ois`、`oic`、`oi`。字段与接口细节见[参考](reference.md)；行为依据为[B1](../architecture/p1/b1-protocol.md)、[B2](../architecture/p1/b2-security.md)、[B3](../architecture/p1/b3-storage.md)、[B4](../architecture/p1/b4-baseline.md)。下文的默认值来自B4§3，是P1默认与测试值，不是SLO、容量或1C1G承诺。

## 1. 构建

```sh
# Go 1.27.3；纯Go，无需C工具链
CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...
```

交叉编译设置`GOOS`/`GOARCH`即可（linux/amd64、linux/arm64、windows/amd64、darwin/arm64）。`go version -m bin/ois`可核对依赖与`CGO_ENABLED=0`；版本号用`-ldflags "-X github.com/wyw-ai/loom-oi/internal/ois/server.Version=…"`（`oic`、`oi`分别为`internal/oic/relay.Version`、`internal/client.Version`）。

<a id="pki"></a>
## 2. 证书与指纹

| 用途 | 要求 |
| --- | --- |
| OIS服务器证书 | 两个监听器共用；SAN须覆盖OIC与消费者使用的主机名或IP；TLS最低1.3 |
| OIC客户端CA | OIS配置`tls.client_ca_file`；access监听器只接受链到该CA的客户端证书 |
| OIC客户端证书 | 每个Access一张（轮换期可两张）；OIS按证书的SPKI SHA-256指纹映射到Access，不看CN/SAN |

指纹可由`oi admin access create … --pin-cert lab-a.pem`直接计算登记，或用OpenSSL：

```sh
openssl x509 -in lab-a.pem -pubkey -noout | openssl pkey -pubin -outform der | sha256sum
```

私钥只留在对应主机；OIS不保存私钥或令牌明文。

## 3. 部署OIS

1. 选择存储（`store.driver`必须显式给出，失败时绝不自动换后端）：
   - SQLite：本地磁盘路径，不要放网络共享文件系统；OIS以WAL、`synchronous=FULL`、增量vacuum运行并在`<db>.lock`持有独占锁。
   - PostgreSQL 18：预先建库与账号；`synchronous_commit`不得为`off`；OIS以advisory锁保证同库只有一个实例（这不是选主或HA）。
2. 写配置（示例见[参考§1](reference.md#ois-config)），相对路径相对配置文件目录。
3. 初始化空库并取得首个管理令牌（只输出一次，妥善保存）：

   ```sh
   ois init --config ois.json --subject admin
   ```

   `init`只为空库建表；已有库若缺少有效管理令牌，`init`会补发一个（须在OIS停止时执行，存储锁保证这一点），否则拒绝。
4. 启动：`ois serve --config ois.json`。启动顺序为打开存储并取得单实例锁→核对模式版本→读回设置→epoch+1→加载快照→打开两个监听器；任何一步失败即退出并给出原因（例如`AnotherInstanceActive`、`SchemaTooNew`、`run ois migrate`、`auto_vacuum`）。
5. 升级二进制后若提示待迁移：先备份数据库，再执行`ois migrate --config ois.json --yes`。`serve`从不自动迁移。

日志为JSON行（stderr），以subject、channel与exposure标识请求，不记录令牌、票据、`Authorization`/`OI-Attach-Ticket`头或数据库口令（PostgreSQL只记录用户、主机、端口与库名）。`SIGINT`/`SIGTERM`触发受控停止：拒绝新请求，所有通道以`Shutdown`中止，最多等待`T_close`＋1 s后关闭存储。

<a id="admin"></a>
## 4. 管理初始化

`oi`从环境变量读取令牌与地址（避免令牌出现在进程列表）：

```sh
export OI_URL=https://ois.example:7443 OI_CA_FILE=ca.pem OI_TOKEN=<admin token>
oi admin access create lab-a --pin-cert lab-a.pem            # Access与证书指纹
oi admin policy create pa --subject alice --selector 'lab-a/*' --actions discover,open
oi admin token create --subject alice                         # 消费令牌，明文只返回这一次
oi admin config                                               # config_version、epoch、存储状态
```

配置写操作（Access、令牌、策略与维护模式）带`If-Match`（`oi`自动取当前`config_version`，或用`--if-match N`做乐观并发）；版本不符返回409并给出当前版本。存储整理与审计修剪不改变配置版本，不要求`If-Match`；修剪以导出的审计摘要校验删除范围。审计：`oi admin audit --after 0`。

## 5. 部署OIC

1. 写配置（[参考§2](reference.md#oic-config)）：`ois.url`指向access监听器（`wss://host:port`），每个exposure只在此处定义目标地址；主机名目标必须配置`allowed_cidrs`。
2. `oic check --config oic.json`列出每个exposure的revision（目标、网段、协议与传输的内容哈希；描述与准入模式不参与）。
3. `oic run --config oic.json`。OIC主动出站连接，断开后以1–30秒指数退避重连；每次重连得到新generation，旧代通道立即中止。
4. 修改exposures后发送`SIGHUP`，或等待至多2秒的自动检测：绑定到被删除、禁用或改目标的exposure的通道以`RevisionChanged`中止，新快照重新登记；只改描述或准入模式不影响已开通道。其他字段（Access、OIS地址、证书、limits等）的修改需要重启，诊断中给出`config_error`。
5. 可选诊断：`diag_listen`只接受回环地址，`GET /stats`返回通道数、中继字节、许可剩余时间、按原因统计的关闭数等。

## 6. 消费者

```sh
oi discover                                   # 本主体获准discover的exposure、revision与在线状态
oi connect lab-a/http --listen 127.0.0.1:8080 # 每个本地连接一个新通道
oi connect lab-a/http --stdio                 # 单通道接标准输入输出
oi channel <id>                               # 状态与关闭原因
oi close <id>                                 # ClosedByClient
```

`--listen`只接受回环地址；同一主机上的其他本地用户也能连接这个端口并使用该消费令牌的授权，多用户主机应改用`--stdio`或在部署层隔离（共同架构§5.3）。本地连接按B1规则：对端FIN映射为半关闭，ABORT映射为RST，截断不会表现为正常结束。

## 7. 运行中会发生什么

| 事件 | 行为 | 运维动作 |
| --- | --- | --- |
| 撤销策略、吊销令牌、禁用Access或删除指纹 | 管理调用返回前，受影响的Preparing通道已关闭、Open通道进入Closing并在`T_close`内断开；禁用/删指纹还会关闭该Access的控制连接 | 无；已转发的字节无法撤回 |
| 存储不可用（PG停止、锁连接丢失） | 新Open与管理写返回503 `StoreUnavailable`；已开通道继续；OIC登记先在内存接受；探测（`STORE_PROBE`）恢复后自动恢复并补写登记 | 修复存储；无需重启OIS |
| 存储写满或只读（SQLite磁盘满、PG只读或磁盘满） | 同上fail-closed；日志`store write failed`带`failure`（`disk_full`/`read_only`/`io_error`），`GET /v1/admin/store`的`write_failure`同值；只读探测仍成功时不恢复，只有一次极小写入提交成功才恢复 | 释放空间或恢复可写；无需重启 |
| 管理写并发超过`WRITE_QUEUE` | 超出部分立即503 `StoreBusy` | 稍后重试；吊销等关键写应重试到成功 |
| OIC控制连接中断 | 不再准备新通道；已开通道最多保留到许可期限（`L`），或OIS连续`OFFLINE_MISSES`次未收到心跳应答后判离线 | 检查网络与证书 |
| OIC进程长暂停 | 恢复后立即以`LicenseExpired`中止本代通道，到期后不再转发 | 无 |
| OIS重启 | 所有通道、票据与generation作废（从不持久化）；epoch+1；OIC以新代数重连并重新登记；目录先由存储恢复，`access_online`在重新登记前为false | 消费者重新Open；不要自动重放业务请求 |
| 数据库被旧副本替换 | OIC报告更大的`last_epoch_seen`时，epoch前推并写一次`epoch_rollback_detected`审计；备份点之后的撤销不会自动恢复 | 用§9的`ois restore`进入维护模式，核对并重新执行撤销后恢复 |
| 证书轮换 | 先为Access添加新指纹（此时两个）→OIC换证重启→删除旧指纹；仍用旧指纹的连接被关闭 | 按顺序执行 |

所有中止原因只说明传输阶段，从不表示业务未执行；需要结果时由业务协议判断。

## 8. 排错

| 现象 | 常见原因 |
| --- | --- |
| Open返回404 | 主体无discover授权或exposure不存在 |
| Open返回403 `Denied` | 有discover无open；或用管理令牌调用消费API |
| Open返回409 `RevisionMismatch` | 目标配置已变，先`oi discover`取新revision（`oi`会自动重试一次） |
| Open返回503 `AccessOffline` | OIC未连接或未完成登记 |
| Open返回503 `Maintenance` | 恢复后或管理员开启了维护模式；核对后`ois admin resume` |
| 管理写返回503 `StoreBusy` | 同时进行的管理写超过`WRITE_QUEUE`；重试 |
| Open返回412 `ReadinessRequired` | exposure或请求要求`require_ready`；P1没有就绪探针 |
| Open返回429 | `N_OIS`/`N_ACCESS`/`N_SUBJECT`额度；Closing通道在关闭完成前仍占用额度 |
| 附着返回401/409/410 | 票据错误或身份不符／票据已用／通道已终止 |
| 通道`TargetConnectFailed` | OIC无法连接目标；业务结果未知，不要自动重放 |
| 通道`TargetAddressNotAllowed` | 主机名解析结果不在`allowed_cidrs`内；未发起任何连接 |
| OIC握手失败 | 证书不链到`client_ca_file`、指纹未登记或Access已禁用 |

<a id="backup"></a>
## 9. 备份、恢复与迁移（B3§8）

**备份**。SQLite：`ois backup --config ois.json --out /backup/ois-20261104.db`，OIS可继续运行；用只读连接`VACUUM INTO`生成一致副本，`integrity_check`、模式版本与meta核对通过后才改为最终文件名，输出含SHA-256、`store_id`、`config_version`与`epoch`的JSON，应与副本一起保存。中断只会留下`.partial`文件，下次备份自动清理；不要直接复制运行中的`.db`而忽略WAL。PostgreSQL：使用部署方的`pg_dump -Fc`或`pg_basebackup`，OIS不代为执行。每次备份前记下`oi admin config`的`epoch`，恢复时要用。

**恢复**（OIS必须停止；命令会取得单实例锁）：

1. SQLite：`ois restore --config ois.json --from /backup/ois-20261104.db --yes`。`--from`须为独立完整备份；旁边有非空WAL时拒绝，请先用`ois backup`生成一致副本。副本先校验，再在私有临时文件中标记后原子替换；原库与未合并的WAL保留为`ois.db.replaced-<UTC>`，主库缺失时遗留WAL也会隔离保存，不能重放到新库。可读原库的检查点若被长读事务阻塞，恢复会拒绝，结束读事务后再重试；副本的`store_id`与原库不同也拒绝。原库已损坏时仍可恢复，此时用`--min-epoch`给出事故前看到的epoch。
2. PostgreSQL：先用`pg_restore`把备份恢复到配置的数据库，再`ois restore --config ois.json --external "pg_restore <备份名>" --min-epoch <事故前epoch> --yes`只做标记。
3. 两种方式都写审计`restored_from`、进入维护模式，epoch取备份、被替换库与`--min-epoch`中的最大者，随后启动再加一；旧通道、票据与代数从不持久化，OIC以新代数重连。
4. 启动`ois serve`：维护模式下发现、管理API与OIC登记照常，**新Open一律503 `Maintenance`**。备份点之后做过的吊销、删除与禁用已随恢复丢失：对照审计导出或变更记录重新执行。
5. `ois admin resume`（读取`OI_URL`、`OI_TOKEN`、`OI_CA_FILE`或对应参数）退出维护模式；也可`oi admin maintenance off`。`oi admin maintenance on`可在计划操作前手动进入。

**SQLite→PostgreSQL迁移**（反向用于回切）：停止OIS→`ois export --config sqlite.json --out ois.export.jsonl`（版本化JSON行：来源与meta、逐行数据、末行逐表行数与SHA-256）→在空PG库执行`ois import --config pg.json --in ois.export.jsonl --yes`（一个事务内建表、写入meta与全部行，并回读重算，与末行一致才提交；`store_id`、epoch、`config_version`与审计序号保持）→把OIS配置改为PG并启动→OIC自动重连。目标必须没有meta或任何业务行，不要先执行`ois init`；空模式可复用。目标非空、文件被改动或中途失败都不会提交任何导入数据，新建模式也会回滚，源库从不被修改；不支持双写或在线切换。导出文件含令牌摘要与全部配置，应按凭据保管。

<a id="maintenance"></a>
## 10. 存储维护与容量告警（§8.1.1）

`oi admin store`（`GET /v1/admin/store`）区分库文件、WAL、页数与空闲页（PostgreSQL只报数据库大小）及最近写入失败类别。审计默认不自动删除：先`oi admin audit export [--through-seq N] --out audit.json`保存记录与规范化摘要，再`oi admin audit prune --export audit.json`；服务端在同一事务内重算摘要，与导出不一致时不删除任何记录，修剪本身写审计`audit_pruned`。SQLite删除后的空间由低峰维护回收：`oi admin store maintain --vacuum-pages 10000 --checkpoint truncate`（每次至多10000页，经单写队列执行并写审计`store_maintained`）；有长读事务时检查点返回`busy`、WAL暂时增长，读结束后再执行即可归零。OIS停止时可用`ois maintain`执行同样操作；PostgreSQL只支持`--optimize`（ANALYZE），其余由数据库自身维护。

SQLite在OIS进程内使用一个读连接和独立写连接；并发读取等待读连接，仍受各自请求期限限制，WAL允许读写并行。该实现避免动态读池关闭后仍滞留底层描述符；长读取也可能使排队的Health探测超过原期限，触发StoreUnavailable；这表示探测未按时完成，不等于物理数据库损坏，存量通道沿既定规则继续。读取吞吐和1C1G资源适用性须另由G5验证，不能从当前回环恢复测试推出容量。

## 11. 限制

不提供主备、探针、H/D路径或Unix socket转发；不由OIS执行PostgreSQL物理备份与时间点恢复。备份、恢复与迁移耗时及文件大小只作记录，不作容量或RTO/RPO承诺。
