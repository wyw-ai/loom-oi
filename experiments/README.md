# Architecture Evidence Lab

本实验室为 alpha 架构提供可复现证据，入口是 [仓库 skill](../.agents/skills/architecture-evidence-lab/SKILL.md)。2026-11-02（+08:00）首轮运行，run 名使用 UTC。实验代码与证据一同版本化；它们不是 OIS/OIC 产品实现。

## 已执行与待执行

| 实验 | 问题与输入 | 当前结果/级别 | 影响 |
| --- | --- | --- | --- |
| [LAB-001](runs/LAB-001-20261101T191556Z-7863f9/report.md) | TCP 可达是否代表就绪；慢探针是否拖累同轮健康目标 | Mechanism fixture；每类5次，实际 HTTP/超时观测；另有明确排除的模型行 | #2/#7：接入、目标、就绪与探针调度分开 |
| [LAB-002](runs/LAB-002-20261101T191558Z-270709/report.md) | 副作用后响应丢失，盲重试会怎样 | Mechanism fixture；两策略各5次，真实本地 HTTP 故障 | #4/#6：连接中断不等于业务未执行 |
| LAB-003 | 同契约 Go/Rust 的连接、缓冲、探针/统计、故障与维护对比 | Blocked：本环境无 go/rustc/cargo；也非1C1G目标环境。设计见 [语言对比](../docs/proposals/oic-language-tradeoffs.md) | #8；没有测出语言赢家 |
| LAB-004 | 真实 MCP/stdio/HTTP 消费者与提供方兼容 | Awaiting input：缺真实首批消费者/版本/部署样本；不能用自制 HTTP 服务冒充 MCP 兼容测试 | #10→#3 |
| LAB-005 | 统计导出拥塞、标签基数/探针规模与控制流公平性 | Planned：需真实仪表/导出实现；当前只有指标契约 | #2/#4/#8 |
| LAB-006 | OIS故障、重登记、旧连接代数与长流失效 | Planned：需 OI 协议/实现及目标拓扑 | #4/#5/#8；未验证 HA |

详细结果和可外推范围见 [首轮证据报告](../docs/evidence/alpha-lab-round1.md)。不是所有未知都适合立即用模拟器作答；消费者意图要有真实来源，语言比较要有对应实现。

## 复现与核验

在仓库根目录：

    python experiments/run.py experiments/manifests/LAB-001.json
    python experiments/run.py experiments/manifests/LAB-002.json
    python experiments/verify.py experiments/runs/LAB-001-20261101T191556Z-7863f9 --current-source
    python experiments/verify.py experiments/runs/LAB-002-20261101T191558Z-270709 --current-source
    python experiments/test_evidence.py

每次生成新目录，不覆盖原始数据。runner 只支持固定受信 fixture，标准库运行，无需 pip 下载。verify 检查 hash、执行结果和 raw 重算；test_evidence 验证原始数据损坏、伪造摘要、重复试次、attempts不一致和失败运行冒充成功会被拒绝。

## 实际隔离与局限

清洁环境、独立进程组、临时工作目录；固定 127.0.0.1；CPU 10秒、地址空间512MiB、128 FD、单文件8MiB、wall30秒。结束清理进程组和临时目录。namespace 实测失败，原因记录在 environment.json；bwrap 的可用性检查也失败。本 runner 不提供 OS 网络/文件系统沙箱，不能执行不可信代码或真实系统故障注入。

执行环境 cgroup 为8GiB、8 CPU额度，不能称为1C1G资源测试。局部地址空间限制也不等于完整机器规格。没有继承用户密钥，没有连接生产服务；数据为合成fixture。证据文件包括预注册问题、源代码哈希、环境、原始观测、stderr、退出结果、重算摘要和校验和。源码当前版本与运行前哈希一致；后续修改要保留旧run对应版本。

## 更新纪律

按照 [产物联动表](../docs/architecture-artifacts.md) 更新 Issue、KB、正文、图与验收；只对有直接证据的窄 claim 增强可信度。产品决定仍由人类确认。
