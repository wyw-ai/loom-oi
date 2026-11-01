# alpha 问题索引与候选决策

整理日期：2026-11-02（Asia/Hong_Kong）。输入基线：`alpha@1b0de95`。本轮只把已有讨论与架构缺口组织为问题，不改变产品方向，不执行原型、不分配负责人、不关闭 Issue、不合并 main。

**入口：[GitHub 跟踪父项 #1](https://github.com/wyw-ai/loom-oi/issues/1)。** 已实际创建 8 个研究/风险 Issue 和 1 个治理跟踪 Issue。D1–D6 仍为文档中的候选，尚未创建为 GitHub Issue；确认“可以建单”也不代表确认某个方案。

## 1. 已实例化的问题

以下分值是 AI 于整理日的初评快照，状态初始均为 Triage；后续状态和正文以 GitHub Issue 为准。评价尺度及处理权限见[管理规范](issue-management.md)。I/U/N/E/C 分别是影响/紧迫/不确定/成本/可信度，S=2I+U+N。表按依赖主题排列，不是自动排期。

| 整理编号 / GitHub | 问题 | Type | Priority | I/U/N/E/C | S | 支持的候选 |
| --- | --- | --- | --- | --- | --- | --- |
| ALPHA-01 / [#2](https://github.com/wyw-ai/loom-oi/issues/2) | 三场景验证 Node/Workload 与能力粒度 | Architecture | P1 | 5/5/4/2/2 | 19 | D2 |
| ALPHA-02 / [#3](https://github.com/wyw-ai/loom-oi/issues/3) | 比较透明通道与适配层的消费成本 | Product | P1 | 5/5/5/3/2 | 20 | D1、D5 |
| ALPHA-03 / [#4](https://github.com/wyw-ai/loom-oi/issues/4) | 中转/直连网络比较基线 | Technical | P1 | 4/4/5/3/2 | 17 | D3 |
| ALPHA-04 / [#5](https://github.com/wyw-ai/loom-oi/issues/5) | 授权、目录过期与撤销执行边界 | Risk | P1 | 5/4/4/3/2 | 18 | 约束 D1、D2、D3 |
| ALPHA-05 / [#6](https://github.com/wyw-ai/loom-oi/issues/6) | 断线、取消、回退与结果未知 | Risk | P1 | 5/4/4/3/2 | 18 | 约束 D1、D3 |
| ALPHA-06 / [#7](https://github.com/wyw-ai/loom-oi/issues/7) | 进程故障与共享资源并发边界 | Technical | P1 | 5/4/4/3/2 | 18 | D4、D6 |
| ALPHA-07 / [#8](https://github.com/wyw-ai/loom-oi/issues/8) | 1C1G OIC 成本测量规范 | Technical | P2 | 4/3/4/2/2 | 15 | C5、D4 |
| ALPHA-08 / [#9](https://github.com/wyw-ai/loom-oi/issues/9) | Loom 集成契约与 daemon 复用边界 | Architecture | P2 | 4/3/3/3/2 | 14 | 受 D1、D2、D5 约束 |

每个 Issue 都有独立 Context、Problem、Goal、Scope、当前认识、问题、方案、验收、分值理由和关系。本文件不复制其完整正文。GitHub 父项与各 Issue 使用 Markdown 双向关联，未建立原生 Sub-issues 或 Projects。

## 2. 依赖与建议讨论顺序

1. 先用 #3/#2 为 D1/D2 准备消费和对象证据；它们可以比较全部候选，无需等用户先选好方案。
2. #4 比较网络路径，#5/#6 同时列出授权及失败语义约束。这些研究互相提供反例，不形成相互阻塞的闭环。
3. #7/#8 为托管与隔离范围补齐故障和资源成本；#9 在选定消费边界后收敛集成草图，前期仍可做源码映射。
4. **生产契约/实现** 才依赖相应 D 项确认。涉及核心抽象至少需要 D1/D2；传输实现需要 D3；进程监督/资源辅助分别需要 D4/D6；官方入口需要 D5。

这是 AI 建议的证据准备顺序，不是人力分工、排期或最终选型。#8 当前没有 OIC 二进制，可先交测量计划；实际达标必须另有实测，不据文档打勾。

## 3. 待确认候选卡片

以下六项沿用讨论中的 D 编号，不增加探索主题。共同元数据：`Type=Decision`、`Topic=alpha`、`Status=Candidate`、`创建授权=待用户确认`、`最终选择=Pending`。来源均为 [alpha@1b0de95 讨论 §6](https://github.com/wyw-ai/loom-oi/blob/1b0de953e847fec040664ef4e11de32836e6ddbf/docs/alpha-discussion.md#6-待决策清单) 及对应架构章节；不是重新核验过原始聊天。当前认识、候选方案来自原探索稿，以下问题边界、反证与验收为 AI 整理。

评分为 2026-11-02 AI 初评；E 只估计证据齐备后的比较整理与决策记录，不重复计入上表研究成本。C=2 只代表文档证据可追溯。所有卡片均可独立交给 AI 继续准备材料，但创建 GitHub Issue 或接受方案仍需用户确认。

### D1：OI 的消费契约是否包含可选统一调用适配

- **Context / 当前认识：** 架构 §4、§6.2；双方认可能力连接与业务分开，但“统一调用入口”与“透明通道”未达成一致。
- **Problem：** 若不明确调用抽象，控制方接入负担和 OI 团队承担的协议维护责任无法界定。
- **Goal / 价值：** 确定对调用者承诺什么，避免由实现偶然决定产品边界。
- **Scope：** 透明连接与可选调用适配的边界；不选择中转/直连，也不接管业务状态。
- **Open Questions：** 哪些消费场景值得统一？错误、Schema、流式与协议版本谁维护？原生通道能否继续使用？
- **Proposed Solutions：** A 仅透明通道；B 通道之外提供可选适配。A 简化核心但增加消费端工作；B 改善接入但扩大协议维护面。原稿 AI 倾向 B，未获用户接受。
- **验证/反证：** #3 比较相同能力的消费成本；若适配丢失关键协议语义或维护成本超过接入收益，应反对相应适配范围。
- **Acceptance Criteria：** A/B 的责任、成本及至少一个反例可评审；用户明确选择或继续待定；记录对 §2/§4/§6/§7、图1/图3及 D5 的影响。
- **Priority / 指标：** P1；I/U/N/E/C=5/5/5/2/2；S=20。它直接决定首版边界和后续契约。
- **Relations：** [#3](https://github.com/wyw-ai/loom-oi/issues/3) 提供证据；[#5](https://github.com/wyw-ai/loom-oi/issues/5)、[#6](https://github.com/wyw-ai/loom-oi/issues/6) 提供约束；影响 D5 和 #9。

### D2：Node / Workload 与能力操作的粒度怎样定义

- **Context / 当前认识：** 架构 §3、§6.1；聊天里 Node/Workload 多次换义，没有稳定对象词典。
- **Problem：** 宿主、接入身份、提供实例、能力声明与连接若混用，会影响生命周期、目录和权限。
- **Goal / 价值：** 选择能覆盖实际接入场景的最小对象模型，形成可维护的术语与契约。
- **Scope：** 对象身份、基数、能力组/操作粒度；不默认引入 K8s 调度或 OI 业务 Session。
- **Open Questions：** 重启和重连是否改变 ID？外部服务如何登记？同机多提供方如何分权？操作独立注册还是归能力组？
- **Proposed Solutions：** A Node 为接入环境、Workload 为可选部署实例；B Node 为提供实例、宿主仅作位置。A 有接入环境的统一管理边界；B 以能力实例为中心，但同宿主身份与配置可能需额外关联。两案都需独立决定操作粒度。
- **验证/反证：** #2 的电脑、外部服务、虚拟手机映射与多实例反例；若需要虚构对象或一 ID 多义，应修改候选。
- **Acceptance Criteria：** 实例、声明、位置、连接能明确区分；三场景和生命周期反例完整；用户确认术语/粒度后统一 §3/§6 和图2，未确认不冻结字段。
- **Priority / 指标：** P1；5/5/4/2/2；S=19。所有登记与路由契约依赖对象含义。
- **Relations：** [#2](https://github.com/wyw-ai/loom-oi/issues/2) 提供证据；关联 [#5](https://github.com/wyw-ai/loom-oi/issues/5)、[#9](https://github.com/wyw-ai/loom-oi/issues/9)；影响 D4/D6 的责任归属。

### D3：首版采用什么网络路径策略

- **Context / 当前认识：** 架构 §4.1、§6.4、§10；中转与直连双方观点都被保留，尚无测量。
- **Problem：** 首版是否投入双路径取决于真实网络收益、可达性及运维成本，凭偏好会扩大或遗漏工作。
- **Goal / 价值：** 为首版传输范围和失败回退形成有证据的选择。
- **Scope：** 中转/直连/回退及支持网络条件；不同时决定工具抽象和协议库。
- **Open Questions：** 直连在目标网络中能否建立？收益和排错成本如何？撤销、认证、路径切换的语义能否一致？
- **Proposed Solutions：** A 先中转，直连后续验证；B 直连优先并回退；C 先小规模测量再决定。A 降低实现面但付出带宽/延迟；B 可能降低流量却增加协商和故障面；C 延后选型以换证据。原稿 AI 建议先测中转基线，未确定首版路径。
- **验证/反证：** #4 在局域网、跨 NAT、受限出口对照；缺少关键场景不形成确定结论。
- **Acceptance Criteria：** 场景数据与样本局限可检查；用户确认首版/后续支持边界及失败策略；更新 §4/§6/§7/§10，不写未经确认的可用性数字。
- **Priority / 指标：** P1；4/4/5/2/2；S=17。影响传输实现及基础设施投入，当前缺数据。
- **Relations：** [#4](https://github.com/wyw-ai/loom-oi/issues/4) 提供证据；[#5](https://github.com/wyw-ai/loom-oi/issues/5)、[#6](https://github.com/wyw-ai/loom-oi/issues/6) 提供授权和回退约束；与 D1 独立判断。

### D4：OIC 是否托管能力进程

- **Context / 当前认识：** 架构 §2.3、§5.3、§8.1、§9；进程可独立运行不等于必须由 OIC 管启动/停止。
- **Problem：** 把能力登记和部署托管混为一谈会引入额外生命周期、恢复和升级责任，整体迁入 daemon 又会带入业务。
- **Goal / 价值：** 明确首版生命周期所有者及接入成本，避免轻量诉求与托管范围相互冲突。
- **Scope：** 已有外部服务、可选子进程监督；进程内插件作为故障代价对照。不决定仓库布局、不迁移 daemon、不做通用部署平台。
- **Open Questions：** 谁启动/重启/升级能力？能力崩溃如何上报？外部服务能否始终接入？托管给 OIC 增加多少资源和配置负担？
- **Proposed Solutions：** A 仅连接已有服务；B 加可选监督。A 保持提供方自治但增加自部署步骤；B 提供一体化安装但承担恢复与版本管理。原稿 AI 倾向先外部服务，仍待确认。
- **验证/反证：** #7 比较故障边界，#8 定义成本测量；若监督收益不足或侵入 Agent 生命周期，应收窄范围。
- **Acceptance Criteria：** 生命周期责任表、故障策略和成本证据齐全；用户明确首版范围；对应 §2/§5/§8/§9 及契约是否包含 start/stop/deploy 同步调整。
- **Priority / 指标：** P1；4/4/4/2/2；S=16。影响模块职责与首版故障承诺。
- **Relations：** [#7](https://github.com/wyw-ai/loom-oi/issues/7)、[#8](https://github.com/wyw-ai/loom-oi/issues/8) 提供证据；[#9](https://github.com/wyw-ai/loom-oi/issues/9) 提供复用边界；不能替代 D6 的资源隔离选择。

### D5：首版提供哪些官方消费入口

- **Context / 当前认识：** 架构 §2.2、§4、§6.2；讨论 SDK/CLI 与 MCP facade，但未形成规格。
- **Problem：** 同时承诺所有语言/协议入口会增加维护；仅凭调用 token 印象也不能证明某入口更合适。
- **Goal / 价值：** 以真实调用者的接入成本确定首版入口组合及维护边界。
- **Scope：** SDK/CLI、可选 MCP facade；不运营通用 MCP 平台，不由入口反推必须采用 D1-B。
- **Open Questions：** 真实消费者需要什么？MCP facade 包装连接还是统一操作？错误、流、取消与版本如何维护？
- **Proposed Solutions：** A SDK/CLI；B 在必要入口外增加可选 MCP。A 维护面较小但消费方可能自行包装；B 可适应已有工具生态但要维护映射与版本。入口可以包装透明连接，因此不能与 D1 合并为同一选择。
- **验证/反证：** #3 对同一能力做接入步骤和协议损失比较；若没有明确消费端收益，不扩入口范围。
- **Acceptance Criteria：** 给出真实消费例子、范围/维护成本和不支持项；用户确认首版入口及版本承诺；同步 README、§2/§4/§6/§9，不能把候选 MCP 画成必需组件。
- **Priority / 指标：** P2；4/3/4/2/2；S=15。重要但需要先明确核心消费契约。
- **Relations：** [#3](https://github.com/wyw-ai/loom-oi/issues/3) 提供证据；关联 D1、[#9](https://github.com/wyw-ai/loom-oi/issues/9)。

### D6：OI 是否提供通用资源独占或租约辅助

- **Context / 当前认识：** 架构 §5.3–5.4；不同调用者可干扰同一资源，授权和资源占用不能混用。
- **Problem：** 全交给提供方可能增加重复接入工作；统一强制 Lease 又可能要求核心理解任意业务资源，仍不能凭声明保证实际独占。
- **Goal / 价值：** 明确资源并发的执行责任与 OI 可提供的有限辅助，避免虚假的隔离保证。
- **Scope：** 真实共享资源的互斥、租期、旧持有者处理及可选契约/辅助库；不建立统一业务 Session，不默认交付分布式锁平台。
- **Open Questions：** 多进程/多连接访问同一资源如何识别？谁能真正阻止旧持有者？辅助库是否有可重复的共性需求？
- **Proposed Solutions：** A 提供方负责全部资源规则；B 提供方执行规则、OI 可选提供契约/辅助库。A 核心简单但接入方重复实现；B 减少重复却增加语义和兼容负担。原稿 AI 倾向辅助而非强制 Lease，未获用户批准。
- **验证/反证：** #7 模拟双调用/旧持有者反例；如果 OI 无法在真实资源边界执行约束，就不能承诺 OI 已保证独占。
- **Acceptance Criteria：** 授权与资源占用分开；能力声明和执行保证分开；用户确认范围并记录失败/恢复边界；更新 §3/§5/§6/§10，不把独立流当资源隔离。
- **Priority / 指标：** P1；5/4/4/2/2；S=18。直接影响共享资源操作的正确性。
- **Relations：** [#7](https://github.com/wyw-ai/loom-oi/issues/7) 提供证据；[#5](https://github.com/wyw-ai/loom-oi/issues/5)、[#6](https://github.com/wyw-ai/loom-oi/issues/6) 提供约束；D4 的进程隔离不能代替本决定。

## 4. 本轮没有实例化的内容

| 内容 | 处理及理由 |
| --- | --- |
| D1–D6 的最终方案选择 | 保留上面的候选卡片，等待用户确认建单/讨论；未冒充已确定任务 |
| 非 AI 调用闭环的生产实现、具体 OIS/OIC API | 先在 #3 准备消费草图和验证材料；D1/D2/D3 未确认前不建“实现某套 API”的任务 |
| MCP 新版本是否无状态 | 作为 #3 的协议版本校核要求，不单独建事实查询 Issue；不从协议无状态推断业务无状态 |
| daemon 整体迁入 OIC、单仓/多仓 | #9 只提供职责/源码证据，不建立迁移工程任务 |
| Magpie、open-connector 的具体集成 | 当前未审计且非选定依赖，保留来源线索，不自动排期 |
| 多 Agent、全局调度、联邦、通用 Workspace/Artifact、微信/QQ机器人业务 | 超出当前主题核心或属于控制/提供方；不扩展探索主题 |
| 缺失截图与无法还原的指代 | 维持未知，不能制造需求或 Issue |

未强行为八种 Type 各建一个 Issue。Exploration/Decision/Implementation 分类和模板已定义，但没有对应的成熟新构想、获准方向单或已批准生产任务时不为凑分类建单。

## 5. 维护与回归主线

讨论某个编号时先读 GitHub 当前正文、评论和关联证据，重要进展回写 Issue。D 项确认后更新[讨论清单](alpha-discussion.md#6-待决策清单)、本文件对应卡片和受影响的架构章节/图；未确认继续保留候选。文档只保留映射和决策记录，日常状态以 Issue 为准。

本轮新增管理规范、问题索引、两份模板，并给 README、讨论及架构加回链；没有冻结新接口。模板仅在 alpha，不会自动出现在默认分支的 New Issue 页面。标签管理因入口能力/登录限制尚未完成，分类已在正文落实。最终是否采用规范、启用模板和合并 alpha，仍由用户决定。
