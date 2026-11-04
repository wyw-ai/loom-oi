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

所有写操作带`If-Match`（`oi`自动取当前`config_version`，或用`--if-match N`做乐观并发）；版本不符返回409并给出当前版本。审计：`oi admin audit --after 0`。

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
| OIC控制连接中断 | 不再准备新通道；已开通道最多保留到许可期限（`L`），或OIS连续`OFFLINE_MISSES`次未收到心跳应答后判离线 | 检查网络与证书 |
| OIC进程长暂停 | 恢复后立即以`LicenseExpired`中止本代通道，到期后不再转发 | 无 |
| OIS重启 | 所有通道、票据与generation作废（从不持久化）；epoch+1；OIC以新代数重连并重新登记；目录先由存储恢复，`access_online`在重新登记前为false | 消费者重新Open；不要自动重放业务请求 |
| 数据库被旧副本替换 | OIC报告更大的`last_epoch_seen`时，epoch前推并写一次`epoch_rollback_detected`审计；备份点之后的撤销不会自动恢复 | 核对并重新执行撤销（P2维护模式另行实现） |
| 证书轮换 | 先为Access添加新指纹（此时两个）→OIC换证重启→删除旧指纹；仍用旧指纹的连接被关闭 | 按顺序执行 |

所有中止原因只说明传输阶段，从不表示业务未执行；需要结果时由业务协议判断。

## 8. 排错

| 现象 | 常见原因 |
| --- | --- |
| Open返回404 | 主体无discover授权或exposure不存在 |
| Open返回403 `Denied` | 有discover无open；或用管理令牌调用消费API |
| Open返回409 `RevisionMismatch` | 目标配置已变，先`oi discover`取新revision（`oi`会自动重试一次） |
| Open返回503 `AccessOffline` | OIC未连接或未完成登记 |
| Open返回412 `ReadinessRequired` | exposure或请求要求`require_ready`；P1没有就绪探针 |
| Open返回429 | `N_OIS`/`N_ACCESS`/`N_SUBJECT`额度；Closing通道在关闭完成前仍占用额度 |
| 附着返回401/409/410 | 票据错误或身份不符／票据已用／通道已终止 |
| 通道`TargetConnectFailed` | OIC无法连接目标；业务结果未知，不要自动重放 |
| 通道`TargetAddressNotAllowed` | 主机名解析结果不在`allowed_cidrs`内；未发起任何连接 |
| OIC握手失败 | 证书不链到`client_ca_file`、指纹未登记或Access已禁用 |

## 9. P1限制

P1不提供备份/恢复/迁移命令：不要直接复制运行中的SQLite文件而忽略WAL；PostgreSQL使用部署方的`pg_dump`/`pg_basebackup`（B3§8的维护模式与工具在P2/G4实现）。不提供主备、探针、H/D路径或Unix socket转发；资源数字只作记录，不作容量承诺。
