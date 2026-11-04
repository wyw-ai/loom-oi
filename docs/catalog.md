# 文档目录

<!-- Generated from catalog.json by tools/check_structure.py; do not edit. -->

按用途查文件；阅读路线见[文档导航](README.md)，维护规则见[结构规范](structure.md)。角色不是采用或实验状态。

| 文件 | 角色 | 唯一用途 |
| --- | --- | --- |
| [docs/README.md](README.md) | 索引 | 文档导航 |
| [docs/alpha-discussion.md](alpha-discussion.md) | 记录 | 原始讨论蒸馏与历史演进，不维护当前问题状态 |
| [docs/alpha-issues.md](alpha-issues.md) | 索引 | alpha Issue 导航 |
| [docs/architecture-artifacts.md](architecture-artifacts.md) | 规范 | 跨产物维护与绘图规则 |
| [docs/architecture/alpha-v0.7.md](architecture/alpha-v0.7.md) | 规格 | loom-oi 架构与实现方案 · alpha v0.7 |
| [docs/architecture/diagrams/README.md](architecture/diagrams/README.md) | 索引 | 架构图册 |
| [docs/architecture/diagrams/v0.7/README.md](architecture/diagrams/v0.7/README.md) | 索引 | alpha v0.7 精确架构图册 |
| [docs/architecture/p1/README.md](architecture/p1/README.md) | 索引 | P1实施规格包导航：B1–B4一览、F0前置修复、整体确认与G0核验步骤 |
| [docs/architecture/p1/b1-protocol.md](architecture/p1/b1-protocol.md) | 规格 | v1三平面线格式、READY提交点、Channel状态机、错误码与幂等 |
| [docs/architecture/p1/b2-security.md](architecture/p1/b2-security.md) | 规格 | v1身份与证书映射、授权链、一次性票据、目标安全与许可 |
| [docs/architecture/p1/b3-storage.md](architecture/p1/b3-storage.md) | 规格 | PG/SQLite共同存储契约、最小模式、单实例、epoch与存储不可用行为 |
| [docs/architecture/p1/b4-baseline.md](architecture/p1/b4-baseline.md) | 规格 | P1任务基线：版本锁定、初始测试配置、拓扑与范围、完成判定、CI与观察记录 |
| [docs/changes/DISC-001.md](changes/DISC-001.md) | 记录 | DISC-001：讨论来源、应用范围与确认记录 |
| [docs/changes/DISC-002.md](changes/DISC-002.md) | 记录 | DISC-002：Issue与文档结构维护 |
| [docs/changes/DISC-003.md](changes/DISC-003.md) | 记录 | 原讨论清洗与已被DISC-004替代的进程专项收束记录 |
| [docs/changes/DISC-004.md](changes/DISC-004.md) | 记录 | 项目主体理念与通道整体收束的来源、范围、影响和验证 |
| [docs/changes/DISC-005.md](changes/DISC-005.md) | 记录 | Issue只追加、演进方案纠偏、轻量门控与已收束问题清理的阶段记录 |
| [docs/changes/DISC-006.md](changes/DISC-006.md) | 记录 | C1/C2/C3落实与开发条件审查来源/增量/追踪 |
| [docs/changes/DISC-007.md](changes/DISC-007.md) | 记录 | B1–B4实施规格包与Windows前置修复的来源/增量/追踪 |
| [docs/changes/DISC-008.md](changes/DISC-008.md) | 记录 | 开发规则对齐与会话采用的来源、范围、验证及发布 |
| [docs/changes/README.md](changes/README.md) | 索引 | 讨论变更索引 |
| [docs/concept/loom-oi.md](concept/loom-oi.md) | 概念 | loom-oi主体理念、整体价值、共同承诺与上下推导的唯一概念入口 |
| [docs/concept/oic.md](concept/oic.md) | 概念 | OIC：受控环境的接入端 |
| [docs/concept/ois.md](concept/ois.md) | 概念 | OIS：能力目录、策略与可信中继 |
| [docs/decisions/ADR-001-core-language.md](decisions/ADR-001-core-language.md) | 记录 | ADR-001：OIS/OIC首版统一使用Go |
| [docs/decisions/ADR-002-state-and-binding.md](decisions/ADR-002-state-and-binding.md) | 记录 | ADR-002：业务状态外置、准确绑定与运行状态重建 |
| [docs/decisions/ADR-003-first-release-boundaries.md](decisions/ADR-003-first-release-boundaries.md) | 记录 | H15的C1/C2/C3准确采用及持续观察范围 |
| [docs/decisions/ADR-004-p1-adoption.md](decisions/ADR-004-p1-adoption.md) | 记录 | B1–B4准确采用、95%更正与G0设计入口边界 |
| [docs/decisions/README.md](decisions/README.md) | 索引 | 决议索引 |
| [docs/designs/application-paths.md](designs/application-paths.md) | 设计 | 通过 loom-oi 使用远端能力 |
| [docs/designs/issue-evolution.md](designs/issue-evolution.md) | 设计 | 规范层的问题与方案演进、重复探索确认及工具化方案撤回边界 |
| [docs/designs/ois-control-relay.md](designs/ois-control-relay.md) | 设计 | 端到端通道：路径策略、实际中继负载与部署的完整候选 |
| [docs/distributed-architecture-conclusions.md](distributed-architecture-conclusions.md) | 兼容入口 | 历史架构入口（兼容导航） |
| [docs/evidence/README.md](evidence/README.md) | 索引 | 证据报告索引 |
| [docs/evidence/alpha-feedback-002.md](evidence/alpha-feedback-002.md) | 记录 | ITER-002：最新回复、整体架构与实验门槛 |
| [docs/evidence/alpha-lab-round1.md](evidence/alpha-lab-round1.md) | 记录 | alpha 首轮隔离实验：观测、反证与边界 |
| [docs/evidence/alpha-origin-review-001.md](evidence/alpha-origin-review-001.md) | 记录 | 原始讨论回溯：从端到端能力通道重新审视 #13 |
| [docs/evidence/alpha-synthesis-001.md](evidence/alpha-synthesis-001.md) | 记录 | alpha 实验后综合结论 · ITER-001 |
| [docs/evidence/application-materials-003.md](evidence/application-materials-003.md) | 记录 | ITER-003：应用实验准备与回复分析 |
| [docs/evidence/implementation-readiness-004.md](evidence/implementation-readiness-004.md) | 分析 | H15采用后的开发入口审查与整体效果观察清单；无新实验 |
| [docs/evidence/p1-g1-evidence-005.md](evidence/p1-g1-evidence-005.md) | 分析 | ITER-004：P1纵向闭环与G1产品证据分析（两个预注册run、修正与O1–O4） |
| [docs/evidence/p1-g1-resource-evidence-007.md](evidence/p1-g1-resource-evidence-007.md) | 分析 | ITER-006：首轮测量/FIN失败、5668限定通过与2ec最终G1主证据；历史反例、资源完整性及当前发布闭环范围 |
| [docs/evidence/p2-g4-evidence-006.md](evidence/p2-g4-evidence-006.md) | 分析 | ITER-005：P2双后端恢复G4四项直接证据与C43当前G1资源证据失效限定；一个预注册run，前置补证及Issue回读待完成 |
| [docs/evidence/p2-g4-resource-evidence-008.md](evidence/p2-g4-resource-evidence-008.md) | 分析 | ITER-007：5668的SQLite描述符反例、D18修正及2ec最终G4主证据，Linux FD正负对照与范围限制 |
| [docs/issue-history/README.md](issue-history/README.md) | 索引 | 实际Issue正文/评论快照的来源、保真边界与历史缺口 |
| [docs/issue-management.md](issue-management.md) | 规范 | Issue初始六节、只追加阶段、反馈回应与问题与方案演进记录规范 |
| [docs/modules/client/README.md](modules/client/README.md) | 规格 | 消费接入与外部工具适配规格 |
| [docs/modules/oic/README.md](modules/oic/README.md) | 规格 | OIC模块规格 |
| [docs/modules/ois/README.md](modules/ois/README.md) | 规格 | OIS模块规格 |
| [docs/product/README.md](product/README.md) | 索引 | 产品文档入口：P1与P2恢复的交付组件、五步开始、文档导航与当前不含范围 |
| [docs/product/operations.md](product/operations.md) | 规格 | 运维指南：构建、证书与指纹、OIS/OIC部署、管理初始化、运行中行为、排错、备份恢复与迁移、存储维护与告警、限制 |
| [docs/product/reference.md](product/reference.md) | 规格 | 配置与接口参考：OIS/OIC配置、limits映射、revision算法、HTTP API（含维护与存储端点）、错误体、CLI与诊断计数 |
| [docs/prompts/architecture-partner.md](prompts/architecture-partner.md) | 规范 | Architecture Partner | 架构共创、问题求解与持续演进 |
| [docs/prompts/autopilot.md](prompts/autopilot.md) | 规范 | 面向完整首版交付的Autopilot启动文本 |
| [docs/proposals/README.md](proposals/README.md) | 索引 | 专题分析与历史推导 |
| [docs/proposals/alpha-solution-design.md](proposals/alpha-solution-design.md) | 记录 | alpha 整体架构推导与方案 |
| [docs/proposals/consumer-scenarios.md](proposals/consumer-scenarios.md) | 分析 | 产品承诺、能力节点与监测场景 |
| [docs/proposals/oic-language-tradeoffs.md](proposals/oic-language-tradeoffs.md) | 分析 | OIC/OIS：从功能需求比较 Go 与 Rust |
| [docs/sdd/README.md](sdd/README.md) | 规范 | loom-oi 规格驱动协作规范 |
| [docs/sdd/development-workflow.md](sdd/development-workflow.md) | 规范 | 连续开发、单任务spec、准确确认、95%单测与完整交付执行规则 |
| [docs/structure.md](structure.md) | 规范 | 文档结构与引用规范 |
| [kb/README.md](../kb/README.md) | 索引 | 知识库入口 |
| [kb/conclusions.md](../kb/conclusions.md) | 记录 | 架构结论索引 |
| [kb/decisions.md](../kb/decisions.md) | 记录 | 决定与工作授权台账 |
| [kb/evidence.md](../kb/evidence.md) | 记录 | 证据台账 |
| [kb/lessons.md](../kb/lessons.md) | 记录 | 工作经验与流程验证 |
| [kb/lineage.md](../kb/lineage.md) | 记录 | 重要问题阶段的来源、替代/反证关系与停止重启条件 |
