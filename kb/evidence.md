# 证据台账

最后核对：2026-11-02（Asia/Hong_Kong）。E01–E12 是此前 v2 核对记录，其中“本轮”指该次核对；v3 新输入列在下方。下面记录来源支持到哪一步，不把原文意见当作实测结论。

| ID / 类型 | 当前可用内容 | 来源及核对范围 | 局限 |
| --- | --- | --- | --- |
| E01 / User decision（工作方法） | 用户要求合并脑暴与 Issue 工作，主动提供充足解决方案，最终方向由人类决定，并在仓库建设 skill/KB、重做 Issue 规划 | 本轮用户指令；[方法落地](../docs/prompts/architecture-partner.md) | 用户授权这种工作方法，不表示逐字批准 v2 prompt 或下述架构选项 |
| E02 / Fact（材料） | 原始导出为 48,216 字节、2,411 行，哈希 `8a01a08ec9912141099103bb66d1d116861f893e88085ce282fbbd6b0e3ec145` | `[已脱敏]-73dc649d124d0d8914d3698a0254eadc(1).md`；本轮重新计算哈希并读取 L480–810、L980–1040、L1390–1585、L1700–1765 | 本轮是重点片段复核，不宣称重新通读整份；缺失媒体仍不可还原，原件不提交仓库 |
| E03 / Intent | 能力接入与 Agent 业务分开，不管理 Agent Session，不把 Magpie 耦合进核心 | E02 L635–673；[蒸馏 C1–C3](../docs/alpha-discussion.md#3-已识别的共同方向) | 不推出只能透传或必须实现统一调用；不是完整架构获批 |
| E04 / Intent | 服务端登记提供方能力，不拥有全部工具业务；统一调用和透明通道仍有分歧 | E02 L494–599、L986–1004、L1555 起 | 记录两方意图，不抹成一种共识 |
| E05 / Fact（讨论状态） | Node/Workload 定义未收敛；“一机一 Node”和“Node 为能力实例”都出现过 | E02 L1399–1553 与前文 | 不用其中一句覆盖后续纠正；S2 为新提案 |
| E06 / Intent | 1C1G 机器只需部署 OIC 的接入诉求明确 | E02 L1704–1760 | 无 OIC 资源实测，不承诺任何数值上限 |
| E07 / Fact（源码） | Loom daemon 的 reconcile_agents 管 Agent worker 启停并做 actor 清理 | [daemon.rs@58b7178](https://github.com/wyw-ai/loom/blob/58b71782fc51069a78a41964113959a0b5f81a87/crates/cli/src/cmd/daemon.rs#L1017)，本轮读取函数实现 | 仅该固定快照，不是最新主干审计；不能整体充当纯连接器 |
| E08 / Fact（源码） | Adapter 含 scope/model/cwd/env；AgentPaths 含 session 与作用域工作区 | [adapter.rs](https://github.com/wyw-ai/loom/blob/58b71782fc51069a78a41964113959a0b5f81a87/crates/agent-runtime/src/adapter.rs#L79)、[agent_serve.rs](https://github.com/wyw-ai/loom/blob/58b71782fc51069a78a41964113959a0b5f81a87/crates/cli/src/cmd/agent_serve.rs#L768) | 能说明业务耦合，不能证明未来集成点已经存在 |
| E09 / Fact（协议） | HTTP 非幂等请求的自动重试有严格限制；不能把响应丢失当作未执行 | [RFC 9110 §9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2)，本轮核对 | 对 OI 采用保守失败模型是 AI 推导；RFC 不替 OI 定义协议 |
| E10 / Fact（协议） | QUIC 同时有流级和连接级流控 | [RFC 9000 §4.1](https://www.rfc-editor.org/rfc/rfc9000.html#section-4.1)，本轮核对 | 不能推导任意桌面/业务资源隔离；本轮未选 QUIC |
| E11 / Fact（协议） | 查询时 MCP latest 解析到 2026-07-28；transport 与消息语义分层，仍有版本兼容、流与取消约束 | [固定版 transports](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)，本轮核对 | 协议未来可能变化；不声称所有现有 MCP 服务已经升级，不把示例 OI API 当 MCP 标准 |
| E12 / Fact（工程） | 重规划前 alpha=8abc35f、main=63c1db9；仓库尚无 OIS/OIC 实现；#1–#9 为此前建立的单、当时均无评论 | 创建前通过 GitHub 与本地树核对；[旧快照](https://github.com/wyw-ai/loom-oi/tree/8abc35f25866f4193543d6d0c2390df5ac3267a2) | 时间快照，后续动态事实需刷新 |

新旧替代：旧规划把所有设计选择单独留在 Candidate 区，v2 将 D1–D6 归入对应现有 Issue 的 Decision Needed；这是组织方式更正，不改变其 Pending 状态。外部事实若过期，先更新本表，再检查提案及 Issue 的受影响语句。

## v3 增量证据

| ID / 类型 | 来源、核对范围 | 支持的结论与局限 |
| --- | --- | --- |
| E13 / User decision | [#9 用户回复](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6561327484)，读取正文及评论 | H1：暂不接 Loom、OI 独立、未来 Loom 侧适配；不决定消费 API 或核心语言 |
| E14 / User decision（要求） | [#8 用户回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6561546252)，读取正文及评论 | H2：结合功能比较 Go/Rust，Rust 未定；不等于选 Go |
| E15 / Fact（当前快照） | v3 开始时读取所有 open/closed Issues 与 #1–#9 全部评论；alpha=d680386，main=63c1db9 | #1–#7 无回复，#8/#9 各一条；之后的动态状态要再读，不根据旧快照覆写新回复 |
| E16 / Fact（语言机制） | [Go/Rust 一手来源](../docs/proposals/oic-language-tradeoffs.md#5-一手来源2026-11-02-核对)：Go GC/runtime/context/cgo、Rust ownership/Send/Sync/platform、Tokio task/select | 已核对官方说明；只支持语言机制，不支持项目性能/效率胜负；没有 OIC 实测 |
| E17 / Fact（回源复核） | 同 E02 原件，本次读取 L986–1040、L1399–1585、L1704–1760 | 核对具体调用例、对象词义变动、独立能力诉求、通道原意、1C1G 仅 OIC；没重读全文，没恢复缺失媒体 |
| E18 / User decision（方法要求） | 当前会话用户指出现有未回复单片面、缺整体架构与依赖证据，需要优化整合 | 支持重做结构和推导方式，不表示批准 v3 的任何产品推荐 |

E07/E08 的 Loom 源码事实仍有效于旧快照，但 H1 使其不再是当前 OI 集成/语言选型的需求依据。v2 Proposal 未升级为事实；被 v3 替代的推荐见 [迁移表](../docs/alpha-issues.md#3-旧问题如何整合)。

## v4回复与运行证据

| ID / 类型 | 来源和范围 | 能支持什么 / 不能支持什么 |
| --- | --- | --- |
| E19 / User requirement | #3 [评论](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6562173600) | 要求成立条件取证与前置产品单；不表示接受A |
| E20 / User requirement | #2 [评论](https://github.com/wyw-ai/loom-oi/issues/2#issuecomment-6562416689) | 要求更深节点/探针/监控场景；不是具体字段批准 |
| E21 / User requirement | #8 [评论](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6562293923) | 要求项目选型与质量目标；不是Go采用 |
| E22 / User authorization | #9 [评论](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6562311062) | 明确允许关闭边界单；不证明测试完成 |
| E23 / Source | [Kubernetes probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/)，2026-11-02核对 | 探针职责和故障处理可作对照；OI不照搬重启权限/缺省语义 |
| E24 / Source | [MCP 2026-07-28 transports](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)，本轮回源 | 消费者/提供方须验证绑定和版本；不证明某个真实客户端支持 |
| E25 / Source | [OTel signals](https://opentelemetry.io/docs/concepts/signals/)、[Metrics SDK](https://opentelemetry.io/docs/specs/otel/metrics/sdk/#cardinality-limits)、[Prometheus](https://prometheus.io/docs/practices/naming/)，本轮回源 | 监测信号与标签/基数机制；未测本项目exporter成本 |
| E26 / Mechanism fixture | LAB-001新版5组：[raw与报告](../docs/evidence/alpha-lab-round1.md) | TCP可达时5次503/5次超时未知；局部串行等待差异；不证明TTL状态机/生产可用性 |
| E27 / Mechanism fixture | LAB-002新版两策略各5次：[raw与报告](../docs/evidence/alpha-lab-round1.md) | 计数效果后响应丢失，盲重放导致第二次效果；不证明持久去重或OI实现 |
| E28 / Environment fact | run中的environment.json与本轮工具链检查 | namespace不可用；当前是受信进程/回环隔离；无Go/Rust工具链、非1C1G。不能宣称强沙箱/语言性能测试 |
| E29 / Review evidence | 独立只读审阅，输入为源代码/manifest/environment/raw，未先读推荐 | 纠正超时被归未就绪、补试次覆盖；复核只支持指定范围，不替代真实部署测试 |

旧run与v1精确源码保留；错误的not_ready汇总名作为已纠正历史，不再引用为当前claim。跨单可信度不整体升级，实验等级见[实验规约](../.agents/skills/architecture-evidence-lab/references/protocol.md)。

## v5复核与综合分析

| ID / 类型 | 来源与核对 | 支持范围 |
| --- | --- | --- |
| E30 / Reanalysis | 四个既有run校验和/原分析器重算；本轮读取新版全部raw | 旧与新读数可复现；没有新样本，不将旧新run合并扩大n |
| E31 / Source | [RFC 9110 §9.2.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.2.2)、[RFC 9000 §13.3](https://www.rfc-editor.org/rfc/rfc9000.html#section-13.3)，2026-11-02回源 | 非幂等调用重试与传输信息重传的层级不同；不代表选择QUIC或本项目实现通过 |
| E32 / User requirement | 当前会话要求每轮实验后自动结合KB分析并尝试结论 | W5方法授权；不授权代替最终产品决定 |

[ITER-001](../docs/evidence/alpha-synthesis-001.md)把E26/E27与既有来源结合为C01–C06；推论与观测分列，旧摘要错误继续Superseded。
