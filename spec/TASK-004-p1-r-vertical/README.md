# TASK-004：P1 R单实例纵向闭环（B1–B4，M1–M5，G1出口）

2026-11-04 · 基线`2a00afafaaee5ec3b7e43af1588ae940e2d9d590`（alpha）· 工作分支`feature/TASK-004-p1-r-vertical` · 集成目标`alpha` · 关联[#18](https://github.com/wyw-ai/loom-oi/issues/18)（采用）、[#3](https://github.com/wyw-ai/loom-oi/issues/3)/[#4](https://github.com/wyw-ai/loom-oi/issues/4)/[#11](https://github.com/wyw-ai/loom-oi/issues/11)/[#15](https://github.com/wyw-ai/loom-oi/issues/15)。

## 基线与授权

- 本会话用户指令（2026-11-04 05:08，+08:00，原话）：“基于 alpha 的最新文档、既有决议与仓库规范，启动 Autopilot，完整开发并交付 loom-oi 已确认首版。自主规划、实现、验证和迭代，持续保存进度；完成必要验证与评审后可合入 alpha 并继续。已确认事项无需重问，仅遇新的关键取舍或无法自行解除的阻塞时请我介入。”
- 采用来源：[ADR-004](../../docs/decisions/ADR-004-p1-adoption.md)（B1–B4与DISC-001-R6，含95%覆盖率更正）；G0已在[门槛台账](../../experiments/gates.json)记录通过。本任务只实现已采用范围，不新增产品选择，因此不建立新的`approval.json`。
- 集成授权：上述指令允许在任务评审、质量检查与适用门槛完成后合入alpha。alpha当前没有分支保护（本库套餐的保护与规则集接口返回403，2026-11-04核实），集成方式为快进推送；不合并main，不部署生产。

## 目标与边界

完成[B4§4](../../docs/architecture/p1/b4-baseline.md#acceptance)定义的P1：一个OIS（部署A）、两个OIC、三个HTTP夹具与一个消费者，按[B1](../../docs/architecture/p1/b1-protocol.md)、[B2](../../docs/architecture/p1/b2-security.md)、[B3](../../docs/architecture/p1/b3-storage.md)实现R通道、授权与目标安全、PG/SQLite共同存储，并以真实实现证据申请G1。包含与排除项以B4§4为准；探针、备份恢复工具、H/HA、MCP兼容与1C1G结论不在本任务。

### 已采用范围内的实现细化

以下是实现所需的细节选择，不改变B1–B4的行为语义；产品文档在M4同步。

| 项 | 实现细化 | 对应规格 |
| --- | --- | --- |
| 错误响应体 | `{"code":"<B1§7错误码>","message":"…"}`，HTTP状态按B1§7 | B1§7 |
| 票据生成 | `tk_`＋base64url(HMAC-SHA256(进程内32字节随机密钥, 角色‖channel_id))；每个通道只保存票据SHA-256，幂等重放时重算同一消费者票据 | B1§7幂等、B2§3只存摘要 |
| 消费者附着URL | 按本次API请求的Host生成`wss://<host>/v1/channels/{id}/attach` | B1§3 |
| 管理API | `/v1/admin/{config,accesses,tokens,policies,audit,stats}`；写操作必须带`If-Match`；令牌明文只在创建响应中返回一次 | B2§2、B3§3 |
| 配置文件 | OIS与OIC均为严格解码的JSON；B4§3各符号可在`limits`中覆盖 | B4§2/§3 |
| OIC配置重载 | Unix收到SIGHUP、或每2秒发现配置文件内容摘要变化时重新加载；新配置无效则保留旧配置并记录错误 | B2§4（配置变化后重发register） |
| OIS离线判定 | 控制连接断开时不立即判离线，按连续缺失心跳计数 | B2§5、TV-B2-10 |
| 诊断计数 | OIS `GET /v1/admin/stats`（管理令牌）；OIC可选回环`diag_listen`的`GET /stats`，默认关闭 | B4§4资源回归、§6 O3 |
| `oi connect` | `--listen`只接受回环地址，每个本地连接一次新Open；`--stdio`为单通道 | B4§4、D4-f |

## 实施与验收

1. **文档先行**：本文件先提交到`feature/TASK-004-p1-r-vertical`并回读，再开始编码。
2. **M1**：`internal/protocol`编解码与模糊测试；`internal/ois/store`共同契约、SQLite与PostgreSQL实现及契约测试。
3. **M2**：OIS身份、目录与策略、通道协调者、中继、消费者与管理API、单实例启动顺序。
4. **M3**：OIC配置与revision、控制连接、准备与拨号、中继、许可与重载。
5. **M4**：`oi` CLI与Go库流接口；`tests/integration`真实二进制夹具（三个HTTP夹具、测试CA、可切断控制连接或丢弃新连接的TCP代理、PG连接切断代理）；产品文档。
6. **M5**：B1–B4全部P1向量在Linux CI对SQLite与PG 18.6通过；CI；G1证据与综合分析；评审后合入alpha。

**单元测试覆盖率（完成门槛≥95%）**：受管交付单元为`cmd/`与`internal/`下全部非测试Go源码（OIS、OIC、oi及其共用包），包含没有测试的包；排除项仅`tests/`（集成/E2E驱动与夹具），没有生成代码。统计命令固定为`go test -count=1 -covermode=atomic -coverprofile=coverage/unit.out ./cmd/... ./internal/...`后`go tool cover -func=coverage/unit.out`的total；每个包只计其自身测试（不用`-coverpkg`），`tests/integration`不计入。存储适配包的测试连接SQLite临时文件与PG 18.6（`OI_TEST_PG_DSN`），所以门槛以带PG服务的Linux CI结果为准。

**其他质量信号**：`go test -race`；`internal/protocol`的帧与控制消息模糊目标各运行`FUZZ_TIME`；`go vet`、staticcheck v0.8.2、govulncheck v1.9.0；四平台`CGO_ENABLED=0`构建；Windows构建、单测与仓库Python检查。

**G1证据**：预注册清单`experiments/manifests/G1-P1.json`在运行前提交；产品运行器与分析器放在`experiments/product/`，运行`tests/integration`并按既有证据目录协议保存原始事件、环境、执行记录、摘要与校验和；`experiments/verify.py`按运行记录登记的分析器路径查找重算器，旧LAB runner、分析器与run不改。证据在Linux CI生成后下载入库，完成ITER综合分析后再把G1记为通过。

**文档代码一致性**：正式评审前逐项记录“规格章节→实现文件/符号→验证→结论”，覆盖API/CLI、默认值、权限、状态与失败、存储与恢复、部署示例。

## 进度检查点

| 项 | 状态 | 证据/备注 |
| --- | --- | --- |
| 文档先行 | 完成 | `f801c8b`推送并回读 |
| M1 协议与存储 | 完成 | `d6adb1d`：protocol 98.9%、store 95.1%（本机SQLite＋PG 18.6各3次稳定） |
| M2 OIS | 完成 | relay 98.9%、server 98.0%；`ois serve/migrate/init/version`；修复：serve不再创建SQLite库（否则init无法设置auto_vacuum） |
| M3 OIC | 完成 | config 99.1%、registration/target 100%、relay 96.2%、tunnel 99.0%；`oic run/check/version`；OIC依赖不含OIS/存储（`go list -deps`核对） |
| M4 CLI、集成夹具、产品文档 | 完成 | `oi`客户端与CLI（95.6%）；`tests/integration`真实二进制夹具与B1–B4全部P1向量；产品文档`docs/product/`（`d3c5995`） |
| M5 向量、CI、G1、评审与合入 | 进行中 | CI（`f0ef2de`起）：Linux构建/静态检查、单测＋覆盖率门槛＋race（PG 18.6）、模糊、两后端集成、Python、Windows。Linux CI暴露并已修复：OIS关闭握手不受T_close约束（coder/websocket等5 s且CloseNow不能抢占）、OIC被拒附着连接滞留连接池（TV-B1-11 READY逃逸）、存储连接池突发后FD不回基线、SQLite目录路径、若干测试竞态（`84b79c1`、`895ebf9`）。`895ebf9`全部CI通过，Linux覆盖率97.3%。G1预注册与产品证据运行器`5de16de`。首次G1运行（`9a42f4e`，CI `41746765582`）verdict=fail：TV-B2-11许可被迟到heartbeat复活、TV-B4-06完成后截断，修正于`11e9f21`、`c6c639f`；代码评审5项（`c6c639f`）、[一致性表](consistency.md)D1–D13（`f984329`、`1a88db6`）、Windows检查器代码页（`f42117a`）、epoch上界差一（`f844b29`）均已处理。`f844b29`：CI `41756241232`六个作业全部通过；G1主run（CI `41756259486`）observed/pass，覆盖率97.4%，零容忍0；[ITER-004](../../docs/evidence/p1-g1-evidence-005.md)综合分析完成。待：Issue阶段评论回读、G1记录、合入 |

开发环境：Windows本机Go 1.27.3（toolchain自动下载）、本机PG 18.6仅用于开发调试；产品证据以Linux CI为准。

## 交付

完成时追加：最终范围、源码与报告入口、实际检查与限制、G1记录及Issue阶段回执。
