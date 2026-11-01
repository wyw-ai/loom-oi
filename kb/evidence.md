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
