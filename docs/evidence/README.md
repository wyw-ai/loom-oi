# 证据报告索引

以下是对应轮次的材料/分析记录，保留当时版本和局限；不作为当前规格或采用状态的第二权威。当前结论见[KB](../../kb/conclusions.md)，当前设计见[架构](../architecture/alpha-v0.7.md)。

| 报告 | 证明/解释范围 |
| --- | --- |
| [首轮机制实验](alpha-lab-round1.md) | 探针与响应丢失机制、纠错和原始记录；非产品验证 |
| [ITER-001综合分析](alpha-synthesis-001.md) | 窄实验结论如何影响架构建议 |
| [ITER-002回复分析](alpha-feedback-002.md) | H8/H9/H10与当时方案，不覆盖后续采用 |
| [实验条件盘点](lab-readiness-2026-11-02.json) | 当时环境检查；工具/条件存在不等于实验通过 |
| [ITER-003应用实验准备](application-materials-003.md) | R1/R2物料、缺口及H13采用分析；无新产品run/样本 |
| [本轮前置盘点](materials-readiness-2026-11-03.json) | 本地准备环境与未核验条件，不证明隔离或产品可运行 |
| [原始讨论回溯](alpha-origin-review-001.md) | 10月31日指定段落、期间演进与DISC-001 r4推导 |
| [开发准备度与观察清单](implementation-readiness-004.md) | H15采用后设计/实现/交付分层判断，B1–B4与O1–O4；无新产品实验 |
| [ITER-004 P1纵向闭环与G1证据](p1-g1-evidence-005.md) | 两个预注册产品run（失败run保留为反例）、修正与O1–O4观察；实现级G1证据，不外推G2–G7 |
| [ITER-005 P2双后端单实例恢复与G4证据](p2-g4-evidence-006.md) | 一个预注册主run为observed/pass，G4四项判定与C39–C42的实现级证据；C43撤回该时点G1资源完整性结论，当时G1为Inconclusive，G4仍Blocked待前置补证、Issue回读与完整联动，不外推观测准入、真实路径故障、容量、HA或真实兼容 |

真实运行按[实验室](../../experiments/README.md)登记原始数据和ITER；准备分析明确新样本为0，不生成虚构run。返回[文档导航](../README.md)。
| [ITER-006 G1补证、FIN与最终源码验证](p1-g1-resource-evidence-007.md) | b7失败、5668限定通过和2ec3670最终主run全部保留；C44/C45/C47/C49区分测量、产品反例与修正证据，四单发布回读已完成，G1恢复Passed；CI与alpha集成进度见TASK-005检查点 |
| [ITER-007 G4资源反例与修正验证](p2-g4-resource-evidence-008.md) | 5668的11h/sqlite FD反例保留；2ec3670新G4四项及D18 Linux正负对照通过（C48），四单发布回读已完成，G4恢复Passed；CI与alpha集成进度见TASK-005检查点，不外推容量或HA |

| [ITER-008/009 G1负载反例与G4限定通过](p2-g1g4-revalidation-009.md) | 73bb667的G1 observed/fail、G4 observed/pass原件及129源文件核验；C50/C51区分失败事实和未定根因，D19诊断不改判线，当前G1 Failed/G4 Inconclusive |
