---
name: architecture-evidence-lab
description: 为 loom-oi 的架构争议、性能/可靠性判断、协议兼容、探针与监测设计提供可复现证据。用户要求隔离实验室、实验数据、证明或反证架构、比较方案、复核实验结果时使用；设计对照实验、隔离运行、保存原始数据和来源，严格限制结论范围；每轮实验、重跑或证据复核后自动完成综合分析、结论收敛与 Issue、KB、架构图和验收回写。
---

# Architecture Evidence Lab

与 Architecture Partner 共享问题与原始来源，分离“提出方案”和“检验证据”的判断过程；不是另一个只接收摘要的 Issue AI。先读 [实验室规约](references/protocol.md)、[实验索引](../../../experiments/README.md) 和对应 Issue/评论。只在用户已指定主题/分支工作。

## 执行流程

1. 先读取[阶段阻挡墙](../../../experiments/GATES.md)和[gates.json](../../../experiments/gates.json)，在实际实现到节点时核对目标阶段依赖；运行`python experiments/check_gates.py --stage <阶段>`。未通过、未运行、失败、不确定均不得放行依赖阶段；独立设计/样本准备可继续。再定义会改变架构选择的具体判断。区分用户取舍、技术事实、可测假设；无法用实验决定的产品承诺进入产品 Issue，不制造数据代替人类决定。
2. 运行前写 manifest：原始问题/来源、假设、竞争解释、对照、输入与版本、故障点、反证/判定、重复/顺序、资源预算、隔离等级、数据/发布边界。先确定判定再看结果；事后修改产生新版本并保留原结果。
3. 检查实际隔离能力。当前 runner 仅运行审阅过的本仓库固定 loopback fixture，清空继承环境、独立临时目录/进程组、设资源与时间限制。它不是不可信代码沙箱；无网络/文件系统命名空间不能声称容器级隔离。外部服务、真实桌面、带密钥或不可信插件实验需要适当隔离与明确授权，先继续可安全执行的本地实验。
4. 运行 [runner](../../../experiments/run.py)，保存预注册 manifest、代码哈希、环境、stdout 原始数据、stderr、退出码、模型/实测区分、重算摘要和 SHA256SUMS。失败、超时和矛盾结果全部保留，标 Inconclusive/Blocked，不能删样本或补造数字。
5. 用 [verify](../../../experiments/verify.py) 和原始数据复核。机制实验不替代实现测试；单机回环不证明 NAT、1C1G、生产 HA、MCP 兼容或 Go/Rust 性能。少量样本报告逐项结果和范围，不伪造总体 p99/显著性。
6. 对会改变关键推荐的结果，按 skill-creator 的验证方式使用独立只读审阅：提供 manifest、原始数据、代码和环境，不先提供推荐或汇总结论；审阅者检查竞争解释、有效性和可外推范围。审阅无需额外审批时直接进行；不让实验审阅者写生产系统或代替用户作选择。
7. **自动进入实验后综合分析，不停在运行报告。** 按[收尾流程](references/post-round.md)读取KB/最新Issue，比较新旧证据，给出支持/反驳/不确定的claim，以及保留/改变/撤回的具体建议、代价和推翻条件。失败/Blocked也分析；不再等用户发起下一轮。
8. 每轮检查 [产物联动表](../../../docs/architecture-artifacts.md)：架构正文、关系/时序/部署/状态图、接口、README、Issue、验收及语言推荐。同步更改或明确记录为何无影响；渲染更改图并核对语义，不只检查语法。
9. 依据原始证据和评审更新对应gate；失败立即降级，Passed晋级必须是综合分析/联动/迭代与远端回读完成后的最后一步，明确范围/版本/环境/判定、证据指纹与阻塞。Ready不是Passed；不能用机制夹具解除实现门槛，不自行豁免。上游实现/契约/环境改变时使受影响下游失效。检查器只核对记录与依赖，不替代语义审阅，也不是后台监听器。
10. 在docs/evidence记录综合结论、kb/conclusions记录claim状态、experiments/iterations记录输入指纹与联动。执行`python experiments/check_iterations.py`，消除未分析run；保存并回读仓库/Issue后才交付。该检查不能证明语义正确或外部写入成功，须实际核对。

## 准备真实实验物料

当用户要求准备实验时，使用[物料入口](../../../experiments/materials/README.md)：分别建立输入、对照、正常/故障用例、判别条件、记录模板和运行前缺口，运行`python experiments/check_materials.py`。物料自检与前置记录完整不等于产品实验可运行；实际runner能力、隔离、版本与阶段另核实。纯准备也完成综合分析并更新条件Issue；不填造raw、结果或通过门槛。

## 已有执行入口

在仓库根目录运行：

    python experiments/run.py experiments/manifests/LAB-001.json
    python experiments/run.py experiments/manifests/LAB-002.json
    python experiments/verify.py experiments/runs/<run-id> --current-source

执行命令不是性能或兼容性承诺。新增实验复用证据目录协议，实验代码放 experiments/cases；不要把模型输出当作仪器读数。完整产品实现、真实系统故障注入、付费资源和扩大范围不由本技能自动授权。

## 停止与交付

证据足以区分当前选项即停止扩测；仍不确定则说明哪条未知有区分度。缺工具链/真实客户端/目标设备时记录 Blocked，不以玩具替代物宣称已满足该验证。完成实验后的综合分析闭环，再返回结论、复现入口、原始证据、架构影响与仍待人类取舍的内容；不靠实验数量评价完成度。自动指执行实验的AI在同一任务内连续收尾，不宣称Python命令可唤醒后台AI；已有输入/结论无变化时不制造重复记录。
