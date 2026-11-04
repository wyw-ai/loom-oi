# loom-oi 产品文档

本页说明已实现的产品交付了什么、怎样开始使用：P1（R单实例纵向闭环）与P2的双后端单实例恢复（[B3§8](../architecture/p1/b3-storage.md#backup)）。行为规格以[B1–B4](../architecture/p1/README.md)为准，本组文档只描述已实现产品的安装、配置与接口，不另定义协议。各门槛是否通过只看[门槛台账](../../experiments/gates.json)。

## 交付内容

| 组件 | 二进制 | 作用 | 源码 |
| --- | --- | --- | --- |
| OIS | `ois` | 部署A：api监听器（服务端TLS，消费者/管理API与消费者腿）＋access监听器（mTLS，OIC控制连接与提供方腿）；SQLite或PostgreSQL存储；`backup`/`restore`/`export`/`import`/`maintain`与维护模式（B3§8） | `internal/ois/...`、`cmd/ois` |
| OIC | `oic` | 一个Access的出站接入：本地目标配置与revision、登记、准备与拨号、提供方腿中继、心跳许可、配置重载 | `internal/oic/...`、`cmd/oic` |
| 客户端 | `oi` | 发现、Open、附着；`connect --listen`回环转发与`--stdio`；`oi admin`管理Access、指纹、令牌、策略、维护模式、存储维护与审计导出/修剪 | `internal/client`、`cmd/oi` |
| Go库 | `internal/client` | 同一流接口供Go测试与集成使用（`Client.Dial`返回字节流） | — |

三者均以Go 1.27.3、`CGO_ENABLED=0`构建，支持linux/amd64、linux/arm64、windows/amd64、darwin/arm64（[B4§2/§5](../architecture/p1/b4-baseline.md#ci)）。

## 五步开始

1. 准备证书：一个OIS服务器证书，一个签发OIC客户端证书的CA（[运维指南§2](operations.md#pki)）。
2. 写OIS配置并初始化：`ois init --config ois.json`输出一次性管理令牌；`ois serve --config ois.json`启动。
3. 用管理令牌登记Access与证书指纹、授权策略并发放消费令牌：`oi admin access create lab-a --pin-cert lab-a.pem`等（[运维指南§4](operations.md#admin)）。
4. 在目标所在主机写OIC配置并运行：`oic check --config oic.json`核对revision，`oic run --config oic.json`接入。
5. 消费者：`OI_TOKEN=… oi --ois https://ois:7443 --ca ca.pem discover`，再`oi connect lab-a/http --listen 127.0.0.1:8080`。

## 文档

| 文档 | 内容 |
| --- | --- |
| [运维指南](operations.md) | 构建、证书、OIS/OIC部署、管理初始化、日常操作、故障与恢复行为、备份/恢复/迁移、存储维护与告警、限制 |
| [配置与接口参考](reference.md) | OIS/OIC配置字段与`limits`、revision算法、HTTP API、错误码、CLI、诊断计数 |

## 当前不包含

探针与NotReady（readiness恒为`unknown`，`require_ready`一律返回412，G2）；真实路径故障验证（G3）；由OIS执行的PostgreSQL物理备份（使用部署方`pg_dump`/`pg_basebackup`）；主备或HA（G6）；H/D路径；Unix socket/命名管道转发；1C1G资源结论（G5）；MCP等真实兼容（G7）。见[B4§4](../architecture/p1/b4-baseline.md#acceptance)。
