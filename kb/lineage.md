# 问题阶段与经验血缘

本页索引“从什么前提得到什么方案、为何改变、什么条件下值得重试”，不再复制实时设计。节点和关系只追加；误记通过后续更正节点说明。当前阅读入口可以更新，但不能消除旧节点。规则见[阶段追加与血缘](../docs/issue-management.md#lineage)，原文保真见[快照目录](../docs/issue-history/README.md)。

## 当前阅读入口

| 问题 | 当前可核实阶段与规格 | 确认边界 |
| --- | --- | --- |
| #15 通道主问题 | [I15-P03追溯说明](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)沿用[I15-P02原回应](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6605914076)及[固定通道方案](https://github.com/wyw-ai/loom-oi/blob/885cb7a50d3b9a8a1f20a445654e3c9039a1eedc/docs/designs/ois-control-relay.md)；现行入口仍在#15 | R来自H8；H/M/部署细节未采用，没有新实验 |
| #14 协作规范 | [I14-P02阶段回应](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6607872608)及[固定追加规范](https://github.com/wyw-ai/loom-oi/blob/ecb4c707105d8af33caafcb3493527a0bc77d6d4/docs/issue-management.md#append-only) | W20已要求只追加；旧正文中的“原位更新”作为历史保留，不批准产品方案 |

## 已记录节点

| 节点 | 来源/阶段 | 当时判断与证据级别 | 范围/局限 |
| --- | --- | --- | --- |
| SRC-CH-01 | [原讨论定位](https://github.com/wyw-ai/loom-oi/blob/3d21e2f375c6ee87d3abe69402984755a9786952/docs/changes/DISC-003.md#sources) | 非中继会改变中心负载、通道可能拆进程；讨论意图/假设 | 无产品测量，缺图不补猜 |
| I15-P01 | [实际留存正文/评论](../docs/issue-history/2026-11-03/I15-P01-observed.json)，[原交付评论](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6603765526)，[固定r1方案](https://github.com/wyw-ai/loom-oi/blob/3d21e2f375c6ee87d3abe69402984755a9786952/docs/designs/ois-control-relay.md) | 先问A/B进程、D/H后置；AI提案与文档检查 | 不是已采用，也不是进程方案被实验反驳；原始创建快照未知 |
| SRC-W19 | 用户请求`da911c62-2921-4c84-afc6-6b651bd89f8b`，见[固定DISC-004](https://github.com/wyw-ai/loom-oi/blob/885cb7a50d3b9a8a1f20a445654e3c9039a1eedc/docs/changes/DISC-004.md#sources) | 指出方向2属于方向1研究，要求回到loom-oi主体 | 方法修正授权，不等于采用H/M |
| I15-P02 | [实际正文/评论快照](../docs/issue-history/2026-11-03/I15-P02-observed.json)，[原回应](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6605914076) | 完整通道问题：R基线＋H研究，路径→B_R/失败→部署；AI整合提案 | 替代旧确认次序；R原采用及新技术待确认范围分开 |
| I14-P01 | [治理正文/评论快照](../docs/issue-history/2026-11-03/I14-P01-observed.json)，[原回应](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6605914809) | 已增加整体收束规则，但旧Issue维护仍要求原位重写 | 本轮捕获基线，不称为#14最早阶段；feature工作流独立 |
| SRC-W20 | 用户请求`667d7fc5-0554-4e86-8400-367ff940821c` | 明确已发布Issue只追加，偏好评论/新单/子单，保留每阶段与经验血缘 | 工作要求即刻执行；细化机制由DISC-005承接，无产品采用变化 |

## 阶段关系

| 关系ID | 起点 → 终点 | 类型、准确范围与理由 |
| --- | --- | --- |
| EDGE-001 | SRC-CH-01 → I15-P01 | 来源：原讨论触发承载研究；整理却提前限定进程 |
| EDGE-002 | I15-P01 → I15-P02 | 修正/替代：替代A/B先行确认，保留有效部署反例；依据SRC-W19，不能解释为A/B不可行或H已采用 |
| EDGE-003 | SRC-W19 → I15-P02 | 来源：按整体目标重组同一主问题，未产生新P2P探索分支 |
| EDGE-004 | I15-P02 → I14-P01 | 经验提炼：整体理念/双向推导进入协作规则；规范采用不等于产品验证 |
| EDGE-005 | I14-P01 → SRC-W20 | 被纠正的规则：旧“当前正文原位更新”不满足阶段追踪；因果来自明确用户反馈，不仅是时间先后 |

## 再次探索前需要知道什么

| 经验ID / 范围 | 停止或修正的原因 | 何时有理由再试 / 当前未知 |
| --- | --- | --- |
| EXP-CH-01：先冻结进程再研究路径 | 路径会改变实际中继字节、端侧成本及可用性前提；旧确认次序遗漏这些变量，是分析缺陷而非性能实测失败 | 已固定具体拓扑/路径/负载与硬故障要求时，可以独立比较A/B/C；保留原场景和判定预算。未知的是实际产品收益，不永久否决拆进程 |
| EXP-GOV-01：每轮重写Issue保留最新答案 | 旧决策依据和探索阶段在平台阅读路径上被覆盖；Git并不天然留存其全文 | 当前采用阶段追加＋仓库现行规格；仅当用户以后明确改变该维护规则时再评估覆写，不能以“更整洁”自作例外 |

本轮记录从真实可取得阶段开始；尚未形成完整历史图谱，也未补造所有中间版本。暂缓/被否决/证据不足与证伪严格分开。不存在新条件但想换名重提时先复用这些结论；有合理复验目的可以重新设计实验，不能被经验条目永久封禁。

## DISC-005新增阶段与关系（追加）

| 节点 | 实际来源 | 本阶段结果与边界 |
| --- | --- | --- |
| I14-P02 | [发布评论](https://github.com/wyw-ai/loom-oi/issues/14#issuecomment-6607872608)、[回读原文/哈希](../docs/issue-history/2026-11-03/DISC-005-published.json)、[固定规范](https://github.com/wyw-ai/loom-oi/blob/ecb4c707105d8af33caafcb3493527a0bc77d6d4/docs/issue-management.md#append-only) | 执行W20：旧正文/评论保留，新阶段追加；拆单/经验细则供核对，没有新产品采用 |
| I15-P03 | [发布评论](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)、同一回执 | 追加两阶段追溯、准确停止/重试条件；技术方案仍沿用I15-P02，没有新实验或采用 |

| 关系ID | 起点 → 终点 | 类型、范围与理由 |
| --- | --- | --- |
| EDGE-006 | SRC-W20 → I14-P02 | 来源：用户明确的只追加要求已经执行，不重新索取该权限 |
| EDGE-007 | I14-P01 → I14-P02 | 修正/替代：只替代旧Issue原位重写规则，保留中文化、SDD及未合并feature原范围 |
| EDGE-008 | I14-P02 → I15-P03 | 实际应用：同一问题通过追加保留经历，而非再改正文或拆成重复P2P单 |
| EDGE-009 | I15-P02 → I15-P03 | 沿用：技术候选/采用/验证边界不变，只补追溯和重试依据 |

发布后核对：原节点快照未变，15个旧Issue标题/正文/状态/成员字段均与写前一致，#14/#15六条旧评论未改。固定阶段提交为`ecb4c707105d8af33caafcb3493527a0bc77d6d4`，新回执记录的是当次真实读取，不保证永久平台状态。
