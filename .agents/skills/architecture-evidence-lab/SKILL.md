---
name: architecture-evidence-lab
description: 为 loom-oi 的架构争议、性能/可靠性判断、协议兼容、探针与监测设计提供可复现证据。用户要求隔离实验室、实验数据、证明或反证架构、比较方案、复核实验结果时使用；设计对照实验、隔离运行、保存原始数据和来源，严格限制结论范围，并联动 Issue、KB、架构图和验收。
---

# Architecture Evidence Lab

与 Architecture Partner 共享问题与原始来源，分离“提出方案”和“检验证据”的判断过程；不是另一个只接收摘要的 Issue AI。先读 [实验室规约](references/protocol.md)、[实验索引](../../../experiments/README.md) 和对应 Issue/评论。只在用户已指定主题/分支工作。

## 执行流程

1. 定义会改变架构选择的具体判断。区分用户取舍、技术事实、可测假设；无法用实验决定的产品承诺进入产品 Issue，不制造数据代替人类决定。
2. 运行前写 manifest：原始问题/来源、假设、竞争解释、对照、输入与版本、故障点、反证/判定、重复/顺序、资源预算、隔离等级、数据/发布边界。先确定判定再看结果；事后修改产生新版本并保留原结果。
3. 检查实际隔离能力。当前 runner 仅运行审阅过的本仓库固定 loopback fixture，清空继承环境、独立临时目录/进程组、设资源与时间限制。它不是不可信代码沙箱；无网络/文件系统命名空间不能声称容器级隔离。外部服务、真实桌面、带密钥或不可信插件实验需要适当隔离与明确授权，先继续可安全执行的本地实验。
4. 运行 [runner](../../../experiments/run.py)，保存预注册 manifest、代码哈希、环境、stdout 原始数据、stderr、退出码、模型/实测区分、重算摘要和 SHA256SUMS。失败、超时和矛盾结果全部保留，标 Inconclusive/Blocked，不能删样本或补造数字。
5. 用 [verify](../../../experiments/verify.py) 和原始数据复核。机制实验不替代实现测试；单机回环不证明 NAT、1C1G、生产 HA、MCP 兼容或 Go/Rust 性能。少量样本报告逐项结果和范围，不伪造总体 p99/显著性。
6. 对会改变关键推荐的结果，按 skill-creator 的验证方式使用独立只读审阅：提供 manifest、原始数据、代码和环境，不先提供推荐或汇总结论；审阅者检查竞争解释、有效性和可外推范围。审阅无需额外审批时直接进行；不让实验审阅者写生产系统或代替用户作选择。
7. 回写“支持/反驳/不确定的具体 claim + run 路径 + 局限”，沿依赖链更新 Issue 和 KB。新增数据只提高相应窄 claim 的可信度，不能把整套架构标成已验证或 Accepted。
8. 每轮检查 [产物联动表](../../../docs/architecture-artifacts.md)：架构正文、关系/时序/部署/状态图、接口、README、Issue、验收及语言推荐。同步更改或明确记录为何无影响；渲染更改图并核对语义，不只检查语法。

## 已有执行入口

在仓库根目录运行：

    python experiments/run.py experiments/manifests/LAB-001.json
    python experiments/run.py experiments/manifests/LAB-002.json
    python experiments/verify.py experiments/runs/<run-id> --current-source

执行命令不是性能或兼容性承诺。新增实验复用证据目录协议，实验代码放 experiments/cases；不要把模型输出当作仪器读数。完整产品实现、真实系统故障注入、付费资源和扩大范围不由本技能自动授权。

## 停止与交付

证据足以区分当前选项即停止扩测；仍不确定则说明哪条未知有区分度。缺工具链/真实客户端/目标设备时记录 Blocked，不以玩具替代物宣称已满足该验证。返回结果、复现入口、原始证据、架构影响与仍待人类取舍的内容；不靠实验数量评价完成度。
