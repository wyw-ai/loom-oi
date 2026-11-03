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
| H10 | 到实现节点按门槛实验，未过不越级 | [#11](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708) | Ready不是Passed；无新门槛通过 |
| H11 | 重申解耦，并要求协议/状态/互斥具体解法 | [#13](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6586567010) | 不是接受R1/R2/R5新增细节 |
| H12 | OIS/OIC首版统一采用Go | [#8](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098)，plumeink，2026-11-02T19:22:29Z | [ADR-001](../docs/decisions/ADR-001-core-language.md)；R3 Accepted，CLI/驱动/性能/双原型/HA未一并批准 |

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
| W16 | 本次任务文档在spec；方案完全确认后单任务feature并先推文档；单测覆盖率≥95%；MR前核对文档代码一致 | 用户请求6eb9ade9-3e40-48d7-b9b1-c45f5de40245，2026-11-03；[开发流程](../docs/sdd/development-workflow.md)落实规则，本次仅规范/技能文档建设，不批准产品实现或合并 |

当前尚待决定的内容由[Issue导航](../docs/alpha-issues.md)定位，不在本页再复制一份Pending清单。需求确认与指纹见[DISC-001](../docs/changes/DISC-001.json)；结构维护见[DISC-002](../docs/changes/DISC-002.md)。
