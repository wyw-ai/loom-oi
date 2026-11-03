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
