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

## v0.6设计收束输入

| ID / 类型 | 来源与核对 | 范围 |
| --- | --- | --- |
| E33 / User requirement | [#10评论](https://github.com/wyw-ai/loom-oi/issues/10#issuecomment-6564017519)，2026-11-02回源 | H7持续案例和质变评审，不等于产品采用 |
| E34 / Source | [RFC6455](https://www.rfc-editor.org/rfc/rfc6455.html)，[PostgreSQL 18锁](https://www.postgresql.org/docs/18/explicit-locking.html)、[可用性](https://www.postgresql.org/docs/18/high-availability.html)，本轮核对 | 协议机制/数据库职责；不证明OI封装、主备算法、资源容量已正确 |
| E35 / Proposal | [v0.6架构](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/alpha-v0.6.md) | 按已有案例与C01–C06给出实现基线；本轮无新增实验读数 |

## E36 · W7关闭授权与专业绘图要求

User instruction，2026-11-02当前会话：关闭完成收束的Issue，仅保留关键决策；所有当前图用ImageGen等专业渠道重新生成并校对。实施映射见[当前Issue结构](../docs/alpha-issues.md)，绘图质量检查见[逐图记录](https://github.com/wyw-ai/loom-oi/blob/d4ea1fbd9f388e63476473bd3687b7a1344b2a39/docs/architecture/diagrams/imagegen-review.md)。不构成新增机制/实现/部署实验数据。

## E-v07：最新回复与一手存储依据

2026-11-02核对：[#3 H9](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6564999241)、[#4 H8](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)、[#11 H10](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)为人类来源；已确认范围见decisions，不把“需详细说明”当采用。

[SQLite WAL](https://www.sqlite.org/wal.html)：同主机共享内存、长读影响检查点；[PRAGMA](https://www.sqlite.org/pragma.html)：incremental回收模式与optimize；[查询规划](https://www.sqlite.org/queryplanner.html)：索引/扫描关系；[在线备份](https://www.sqlite.org/backup.html)；[3.51.3修复记录](https://www.sqlite.org/releaselog/3_51_3.html)；[PG18锁](https://www.postgresql.org/docs/18/explicit-locking.html)。均为Source级机制依据；由此推出的队列、保留策略、迁移和支持范围为OI设计提案，不是项目实测。版本固定后须在LAB-006复验。

## E42–E44：DISC-001新讨论与工作授权

| ID / 类型 | 原始来源与范围 | 结论限制 |
| --- | --- | --- |
| E42 / Discussion | [DISC-001 D01–D07](../docs/changes/DISC-001.md)，PDF哈希2c0304ef…43bc86、6页、90条消息头；两附件相同，全文/图像已读 | Mac/ECS经历、工具外置方向、设备/Pod愿景和Go倾向；没有日志/真实OI兼容/性能数据 |
| E43 / User instruction | 本会话7f2a134b-b517-47de-8cb7-e0206a03c2e3与前一请求：每轮讨论融合提交开发者确认Issue，建设仓库SDD/skill | 工作纪律已授权，不批准具体产品方案/实现/语言 |
| E44 / External workflow source | [外部实践固定版本与取舍](../.agents/skills/discussion-sdd/references/external-practices.md)：Spec Kit、Superpowers与Agent Skills格式 | 仅借鉴方法，不提高OI产品证据级别，不引入其自动分支/审批指令 |

## E45–E46：r2重做的反馈与机制核对

- E45 / User instruction，2026-11-03：本会话`b4a5c411-8202-444d-ac0e-34f6e4926f15`否定#12交付，要求修改技能、重做相同PDF并关闭失效单。授权范围为#12替代和方案/技能修正，不是关闭全部活动技术Issue或接受产品方案。
- E46 / Source：[DISC-001一手依据](../docs/changes/DISC-001.md#references)列OpenSSH命令传递、MCP明确2025-11-25参考绑定、ADB设备选择及Pod UID/网络机制。2026-11-03回源；实际客户端/平台版本未知，不能外推兼容或性能。E24已有另一MCP版本参考，两者不混为同一绑定保证，实际实验须固定选用版本。

E42同源本轮重新全文/六页图像阅读，补回D07的决策流程分歧。没有新增原始实验数据或提高C01–C11实测范围。

## E47：#13三项诉求与机制回源

2026-11-03读取[plumeink原回复](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6586567010)（创建/更新均为2026-11-02T19:34:04Z）及#3 H9原文。E47为User requirement，不是产品实验或方案批准；[DISC-001 r3](../docs/changes/DISC-001.md#feedback13)逐项回应。

同日回源Kubernetes Pods/Nodes及MCP 2025-11-25 transports，链接在回应末尾；只用于类比边界及stdio/HTTP/会话事实。真实提供方/客户端版本仍未知，不能由规范推出OI兼容或互斥保证。

## E48：10月31日原始区段找回及演进复核

2026-11-03根据W14找回E02同名Markdown，文本读取L1050–1769，指定区段L1204–1693（15:45:18–16:19:13）。48,216字节/2,411行与旧登记一致；源SHA沿E02历史记录，未重新下载字节哈希。核对v2/v3/v0.6固定提交、H8与#13当前回复，详见[回溯报告](../docs/evidence/alpha-origin-review-001.md)。它是Discussion/设计历史证据，不是产品采用或新实验；B1/B2含义须分情形，MCP/UDP聊天判断不作事实继承。

## E49：H12明确语言采用

2026-11-02读取[#8原回复](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)，plumeink，创建/更新2026-11-02T19:22:29Z。确认OIS/OIC首版统一Go；[ADR-001](../docs/decisions/ADR-001-core-language.md)记录准确范围。此前当前正文遗漏该回复，此轮修正；无性能/HA证据新增。

## E50–E52：新回复与实验准备

| ID / 类型 | 来源与核对 | 支持范围与限制 |
| --- | --- | --- |
| E50 / 人类决定及工作要求 | [H13原回复](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6596770259)，plumeink，创建/更新均2026-11-03T06:09:12Z | R1/R2准备物料；R5三项边界采用；不是完整支持矩阵、K2细节或产品实验通过 |
| E51 / 人类治理要求 | [H14原回复](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6596717601)，plumeink，创建/更新均2026-11-03T06:05:38Z | 中文分类/状态、八类沿用与规范调用目标；不强制新建多套技能 |
| E52 / 准备与环境记录 | [两套物料](../experiments/materials/README.md)、[只读盘点](../docs/evidence/materials-readiness-2026-11-03.json)及[ITER-003分析](../docs/evidence/application-materials-003.md) | 输入/用例/空模板及检查可复核；前置字段未齐，没有新产品run/样本或真实隔离证明 |

## E53–E54：新讨论与网络机制核对

| ID / 类型 | 来源与核对 | 支持范围与限制 |
| --- | --- | --- |
| E53 / 讨论原文 | [DISC-003来源/清洗表](../docs/changes/DISC-003.md#sources)，2026-11-03上传Markdown，5,322字节/269行/61条消息；SHA-256见原表 | OIS依赖、转发负载与分进程假设；四张图片只有链接。重复引用不加权，闲聊不作事实，收束语不作技术采用 |
| E54 / 官方机制来源 | 2026-11-03直接核对[RFC 8445 §5.1.1](https://www.rfc-editor.org/rfc/rfc8445.html#section-5.1.1)及[RFC 8656 §1](https://www.rfc-editor.org/rfc/rfc8656.html#section-1) | 区分地址/连接建立与载荷中继，纠正P2P必然中心化的笼统断言；不证明OI的WSS兼容ICE/TURN、吞吐或真实可达 |

本轮是来源核对与设计推演，未复算旧raw、运行新实验或取得产品负载数据。

## E55：项目整体思路修正

2026-11-03用户请求`da911c62-2921-4c84-afc6-6b651bd89f8b`：方向2应是方向1的调研方向，要求先建立loom-oi主体理念，局部讨论最终收束回项目且上下合理。它纠正问题分解和工作方法，未采用P2P、存量迁移或拆进程。结合E53的“非中继则中心负载不同”与H8的R基线，可推导路径研究应进入同一负载/可用性主问题；不能推导已测得收益。处理见[DISC-004](../docs/changes/DISC-004.md)。

## E56–E57：演进记录设计的原始参考

2026-11-03直接核对下列作者/标准资料；仅借鉴所列含义，相关设计推论见[能力方案](../docs/designs/issue-evolution.md#3-图谱要分清三个层次)。

| ID | 来源 | 支持范围与限制 |
| --- | --- | --- |
| E56 | [W3C PROV-DM，2013推荐版](https://www.w3.org/TR/2013/REC-prov-dm-20130430/) | 实体、活动、责任主体和派生关系的来源表达；不证明本项目防重复算法有效，不要求采用特定图数据库 |
| E57 | [Michael Nygard：Documenting Architecture Decisions，2011-11-15](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) | 决定的背景、状态、后果和替代记录；未覆盖本项目全部探索过程或自动条件匹配。旧Issue仍遵守本项目只追加规则 |

## DISC-006：采用与准备度依据（追加）

| ID | 实际来源 | 范围与限制 |
| --- | --- | --- |
| E58 | [H15原回复](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)及[实际来源快照](../docs/issue-history/2026-11-04/DISC-006-source.json) | C1/C2/C3按建议、加入持续观察；不批准整个R6/H实现或HA |
| E59 | alpha `19b094fcdd220d39778d2517da1d905c4b057146`当前文件、共同架构/模块、gates及已有报告；[准备度审查](../docs/evidence/implementation-readiness-004.md) | 无Go产品源/模块，接口仍是草图；本轮无新实验；例行检查按旧分析器核验/复算，不增加样本或外推结论，旧机制/物料不升级产品证据 |
| E60 | 2026-11-04核对[SQLite WAL](https://www.sqlite.org/wal.html)、[PG 18复制](https://www.postgresql.org/docs/18/warm-standby.html)、[Google SLO方法](https://sre.google/workbook/implementing-slos/) | 约束同主机WAL/检查点、复制数据损失窗口和用户体验指标；不证明OI恢复、HA或任何数值目标 |
