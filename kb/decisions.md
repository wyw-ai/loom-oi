# 决定与工作授权台账

本页维护当前有效范围；完整旧叙述见[整理前固定版本](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/kb/decisions.md)。原始评论/用户指令是授权依据，AI总结不是。采用、实施、实验和Issue关闭分别判断。

## 当前人类决定与要求

| ID | 准确范围 | 来源 | 当前影响/限制 |
| --- | --- | --- | --- |
| H1 | 暂不接Loom，未来由Loom侧适配 | [#9原回复](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6561327484) | Loom非当前依赖；不批准消费细节 |
| H2 | 结合功能比较Go/Rust | [#8](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6561546252) | 研究要求，语言决定后来见H12 |
| H3 | 调研成立条件并建产品/消费者前置分析 | [#3](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6562173600) | #10后按H7/W7转持续案例，不重复建单 |
| H4 | 细化探针/监控/不同节点场景 | [#2](https://github.com/wyw-ai/loom-oi/issues/2#issuecomment-6562416689) | 不批准全场景兼容 |
| H5 | 按项目功能/维护/可用性给语言推荐 | [#8](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6562293923) | 工程推导不是性能结果，H12随后确认语言 |
| H6 | 允许关闭#9，边界已收束 | [#9](https://github.com/wyw-ai/loom-oi/issues/9#issuecomment-6562311062) | 只覆盖#9，不批准实验 |
| H7 | 持续收案例，按现有材料设计，变化时重评 | [#10](https://github.com/wyw-ai/loom-oi/issues/10#issuecomment-6564017519) | 不要求全部消费者齐全才设计 |
| H8 | 采用R可信中继；默认PG、无PG可SQLite并优化规模 | [#4](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039) | 具体存储/迁移/HA保证仍评审 |
| H9 | OI不实例化业务功能，提供注册与通道 | [#3](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6564999241) | 具体字段/K2默认未批准 |
| H10 | 到实现节点按门槛实验，未过不越级 | [#11](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708) | 条件就绪不是实验已通过；无新门槛通过 |
| H11 | 重申解耦，并要求协议/状态/互斥具体解法 | [#13](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6586567010) | 不是接受R1/R2/R5新增细节 |
| H12 | OIS/OIC首版统一采用Go | [#8](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)，plumeink，2026-11-02T19:22:29Z | [ADR-001](../docs/decisions/ADR-001-core-language.md)；R3 已采用，CLI/驱动/性能/双原型/HA未一并批准 |
| H13 | R1/R2分别准备实验物料；认可业务状态外置、准确绑定和可重建运行状态 | [#13](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6596770259)，plumeink，2026-11-03T06:09:12Z | [ADR-002](../docs/decisions/ADR-002-state-and-binding.md)；R5已采用，完整支持范围/R6细节及实测不一并批准 |
| H14 | 当前分类/状态中文化，沿用八类，按需确保编写可调用规范 | [#14](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6596717601)，plumeink，2026-11-03T06:05:38Z | DISC-002-R2；不强制每类新skill，不改机器枚举/历史凭据，不等于全套SDD细则已采用 |

## 工作授权

| ID | 持续规则/授权范围 | 来源定位 |
| --- | --- | --- |
| W1–W3 | 融合脑暴与Issue、仓库skill/KB、已有alpha、回复驱动整体分析 | 本会话连续指令；[原台账](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/kb/decisions.md)保留细节 |
| W4–W5 | 隔离实验及每轮实验后自动综合分析/图文联动 | 本会话实验室与综合分析请求；不授权生产操作 |
| W6–W8 | 按已有物料收束、明确授权归档已收束单、建立#11条件汇总 | 原台账记录准确对象；不是此后任意关闭权限 |
| W9 | 优先适合任务的ImageGen等专业绘图渠道 | 请求a462428d-5c6b-42b1-bfca-f63763ce2030 |
| W10 | 主入口清晰，旧文件用Git维护 | 请求e04c6b71-69d7-4bfd-81a3-aa6f0b0fff8a |
| W11 | 每轮讨论融合提交确认Issue，仓库SDD/skill建设 | 请求7f2a134b-b517-47de-8cb7-e0206a03c2e3 |
| W12 | 重做PDF应用求解与技能，#12关闭失效 | 请求b4a5c411-8202-444d-ac0e-34f6e4926f15；只覆盖指定旧单 |
| W13 | 后续图统一中文，组件/协议等专名保留 | 请求962c8b24-44b0-47dd-ac63-b1441280c014 |
| W14 | 回溯10月31日指定段并与#13综合 | 请求9d8609ad-2d49-4b58-b60c-d4e9e8c71dc8；不批准新产品取舍 |
| W15 | 优化Issue/文档/索引与引用结构，保持SDD运行 | 请求74bf7bb3-76ba-4795-8024-3f9d9f084042；本轮治理编辑已授权 |

W16：本轮请求`a4ebd9a8-d59d-402d-b287-d5a016b81859`要求依据新回复继续迭代，落实H13/H14并维护现有确认Issue。

W17：用户2026-11-03请求`648c9395-1e01-477e-af83-0b79096d98db`明确要求每次处理Issue后，除更新正文，还要回应其Issue评论，供核对迭代改动。执行细则见[评论回复规范](../docs/issue-management.md#comment-replies)；回复不能代替用户批准或产品实验。

当前尚待决定的内容由[Issue导航](../docs/alpha-issues.md)定位，不在本页再复制一份待确认清单。需求确认与指纹见[DISC-001](../docs/changes/DISC-001.json)；结构维护见[DISC-002](../docs/changes/DISC-002.md)。

W18：请求`ab5b0ef4-176b-4794-b32c-70bb63e62268`授权清洗并融合2026-11-03新讨论；alpha记录为[DISC-003](../docs/changes/DISC-003.md)，没有新技术采用。

W19：请求`da911c62-2921-4c84-afc6-6b651bd89f8b`要求方向2纳入方向1的研究，建立loom-oi主体理念并迭代规范，局部结论回到整体。授权本轮工作方法、概念及相应文档/Issue改写；不自动接受H路径、存量迁移M、进程拆分、离线授权或性能承诺。整合提案与范围见[DISC-004](../docs/changes/DISC-004.md)。

W20：用户请求`667d7fc5-0554-4e86-8400-367ff940821c`要求Issue已发布内容只追加，优先回复/新Issue/子Issue，保留各阶段与经验血缘以避免无依据重复探索。立即替代旧的正文原位重写要求；仓库现行规格仍由Git维护。本轮执行记录[DISC-005](../docs/changes/DISC-005.md)，不扩大产品采用、分派、关闭或合并权限。

W21：用户请求`c982c208-4c39-4a20-971b-5fae9138a559`澄清“经验血缘”只是需求描述，要求准确表达。更正W20中AI的命名归因：用户要求的是阶段可追溯与避免无依据重复探索，未指定专业术语或图谱技术。现行表述见[问题与方案演进记录定义](../docs/issue-management.md#lineage)；直接落实文字澄清，不增加产品采用或新的审批。

W22：请求`d70fe7f3-6aba-49a7-889b-9a731fede41f`要求核对防重复探索的优化，并展开是否采用图谱技术。完成能力分析/方案比较已授权；不等于指定或排除图技术，也未批准数据库、自动拦截或原型实施。新增候选按[DISC-005-R2](../docs/changes/DISC-005.json)供#14评审；原W20只追加规则保持。

W23/W24历史补记：请求`3d2f8661-2bc8-425e-827a-e93a77c43750`要求调研可靠方案并实现需求，`b8baa6a7-7110-4151-8db7-a8d1bf110125`要求Git仓库级、不要过重。AI随后在feature/TASK-002-issue-evolution实现脚本并提交PR #16；这只记录当时的执行与解释，不证明用户指定了该工具方案。W25已澄清偏差，该方案撤回，不合入alpha。

W25：请求`59abbba0-ee87-43e3-baa8-cfc39f82d9bc`明确要求门控/结构/流程规范，识别疑似重蹈覆辙时用Issue额外确认；检查全部Issue，已无疑问与分歧的决定关闭，并新建汇总供确认。授权本轮规范纠偏、撤回PR #16、按准确范围关闭#8及收束#14、创建#17；不扩大产品采用、实验通过、成员分派或合并权限。原Go/H8/H13决定保持，未决准入/存储/通道范围继续评审。

## H15 / W26：C1/C2/C3采用与开发入口评估（追加）

H15：plumeink，[原回复](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)，2026-11-03T17:32:13Z：“C1,C2,C3均按建议走，但要加入观察清单，后续需要根据整体效果持续验证”。准确范围见[ADR-003](../docs/decisions/ADR-003-first-release-boundaries.md)，更新本页历史H8/H9描述中的待定范围：C1默认、C2首版支持/迁移、C3研究边界现已采用；全部R6、H生产实现、HA及实验通过未被一并批准。

W26：用户请求`36d90823-0b90-44ed-8fdd-80a472f20cd6`要求从高可用、高可维护、高正确性等维度判断开发条件。直接授权评估与现有流程回写；不把要求判断理解为启动产品编码或放行门槛。开发入口与O1–O4观察见[本轮评估](../docs/evidence/implementation-readiness-004.md)。

## W27：B1–B4规格包与Windows前置修复（追加）

W27：本会话`7f8a3c59-64b2-4ffb-93bc-d8e4548e20c2`（2026-11-04，+08:00）。用户先要求“分步骤启动……汇报后等待我确认，当前不修改文件、不编码”；汇报后选择“授权启动：会话分支重置到 origin/alpha，完成后直接推送 alpha，沿用以往做法”；随后要求“收束并补齐现有 B1–B4 规格，逐项关联已确认决策、待确认差异、测试向量及已有观察项；形成一次清晰的整体确认材料。Windows 兼容问题明确列为前置小修复。确认并核验 G0 后，再启动 P1 实现。”授权规格收束、F0修复、发布确认材料与直接推送alpha；不授权产品编码、记录G0通过或代开发者确认。执行见[DISC-007](../docs/changes/DISC-007.md)。

## W28：开发规则对齐、#18采用与连续开发启动文本

用户请求`9f0fe3b4-5d91-4f1e-b2ef-1eb3b48f9eb4`及`11dafb4a-fa26-420e-8931-43fb49723ac5`明确要求开发规则对齐、完成#18采用与G0核验，并面向Opus 5.5高自主开发提供完整loom-oi启动prompt。原话和准确差异见[ADR-004](../docs/decisions/ADR-004-p1-adoption.md)。B1–B4及R6承接范围已采用，B4恢复用户95%；本轮为治理工具/采用/G0与启动文本，不宣称产品已实现或取消后续门槛。

## H16：G2三组具体契约准确采用（追加）

本会话用户于2026-11-04分别选择“采用 Commit-Snapshot（推荐）”“采用 Probe-HTTP（推荐）”“采用 Export-Pull（推荐）”。真实来源为工具调用`call_536f5c10d81142ac9e5a95e96565e3aa`的item 0/1/2，完整问答见[唯一凭据](../spec/TASK-007-p2-g2-readiness/approval.json)，准确范围与后果见[ADR-005](../docs/decisions/ADR-005-g2-adoption.md)。采用固定提案deff207的DISC-009-R1/R2/R3及六个原规格片段；无可用请求UUID，不伪造来源。本工作区已集成alpha 570b573，G1/G4前置已恢复，G2产品实施尚未开始；不代表G2实测、门槛通过或生产SLO，也不重复确认C1/B1–B4。

## H17：数据腿活性修订整体采用

本会话用户2026-11-05对`call_c1f9a7b9fce746ab8b151b1adf9e6cc0` item0回答“采用完整方案（推荐）”。准确问答见[唯一凭据](../spec/TASK-007-p2-g2-readiness/liveness-approval.json)，固定207cf91的DISC-010-R1全scope及后果见[ADR-006](../docs/decisions/ADR-006-data-leg-liveness.md)。采用异步控制写与空闲窗口并承担固定依赖补丁维护/旧端限制，旧B1/B4仅该心跳部分由新契约承接；不证明任何产品门槛通过。
