# P1实施规格包：R单实例纵向任务（B1–B4）

数据腿活性条款现由已采用的[异步控制写与空闲窗口](../p2/data-leg-liveness.md)限定替代原B1/B4心跳部分；准确采用见该规格入口。实现和同源验证待完成，正常排空、原IDLE/授权及资源期限保持；控制会话默认心跳未变。

本目录是首个R单实例纵向任务的已采用B1–B4规格。[ADR-004](../../decisions/ADR-004-p1-adoption.md)记录本会话原始确认、固定原稿及95%覆盖率更正；[DISC-007](../../changes/DISC-007.json)记录准确采用片段。G0是否放行以[门槛台账](../../../experiments/gates.json)为准，产品实验尚未运行。共同边界仍以[共同架构](../alpha-v0.7.md)为准。

## 一览

| 项 | 规格 | 收束的实施歧义 | 已采用依据 | 本轮采用差异 | 测试向量 | 观察项 |
| --- | --- | --- | --- | --- | --- | --- |
| B1 协议与状态机（#3） | [b1-protocol.md](b1-protocol.md) | 三平面线格式与版本；唯一提交点与READY；半关闭/ABORT；错误码；幂等 | H9、H13、C1、C3、H12 | D1-a～D1-g | TV-B1-01～19 | O1、O3、O4 |
| B2 授权与目标安全（#3） | [b2-security.md](b2-security.md) | 证书到Access的映射；授权链；一次性票据；命名空间与revision；DNS重绑定；许可与失联 | H9、H13、C3 | D2-a～D2-f | TV-B2-01～14 | O1、O4 |
| B3 存储与重启（#4） | [b3-storage.md](b3-storage.md) | 共同契约与错误；最小模式；两后端设置与单实例；epoch；存储不可用；迁移命令；备份恢复契约 | H8、C2、H13、H10 | D3-a～D3-f | TV-B3-01～11 | O2、O4 |
| B4 任务与验收（#11、#15） | [b4-baseline.md](b4-baseline.md) | 版本锁定；初始测试配置；拓扑与范围；P1完成判定与G1映射；CI；观察记录 | H12、C2、C3、H10 | D4-a～D4-g | TV-B4-01～06 | O1～O4 |

已采用依据的原文：H8[#4](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)、H9[#3](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6564999241)、H10[#11](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6565021708)、H12[ADR-001](../../decisions/ADR-001-core-language.md)、H13[ADR-002](../../decisions/ADR-002-state-and-binding.md)、H15/C1–C3[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)。确认B1–B3即同时收束[DISC-001](../../changes/DISC-001.md#requirements)-R6剩余的详细契约（字段、参数、线格式与并发提交），不重开R5或C1。

一个完整场景串起四项：管理员为`lab-a`登记证书指纹与策略（B2、B3）→OIC-A出站连接并登记`lab-a/http`（B1§4、B2§4）→消费者发现并Open，得到票据（B1§3）→两腿附着，OIS在协调者内复核后发READY（B1§6）→原生HTTP字节双向转发，背压有界（B1§5、B4§3）→策略被撤销时，管理调用返回前通道已进入关闭，目标收到RST（B1§6、TV-B1-12）→OIS重启后旧票据和旧通道都不能复活，OIC以新代数重连（B3§6、TV-B3-04）。

<a id="f0"></a>
## 前置小修复F0（已完成，不需要产品确认）

| 问题 | 修复 | 验证 |
| --- | --- | --- |
| 中文Windows（cp936）下检查器用区域编码读取UTF-8文件而报错；`str(relative_to())`产生反斜杠，导致目录覆盖与指纹键不匹配；git的`autocrlf`把文件转成CRLF，使SHA-256指纹与run摘要全部失配 | 检查器显式UTF-8读写，生成目录固定LF；路径比较改用`as_posix()`；新增`.gitattributes`保持LF；`tools/test_portability.py`在任意系统守住这两条约束；同步刷新DISC-001-R4与DISC-002-R1的待确认指纹 | 修复前：工具测试1个错误、实验测试10个错误、5个检查器中4个失败；修复后Windows原生45项测试与全部检查器通过 |
| 实验覆盖测试只为ITER-001复制分析文件，ITER-002/003加入后在任何系统都报错（既有缺陷，验证F0时发现） | 夹具为全部迭代记录复制分析与产物（单独提交） | 28项实验测试通过 |

受哈希约束的实验源（`run.py`、`analyze.py`、`cases/`）没有修改。`run.py`在Windows新建run时仍会写出反斜杠路径键；它影响的是将来在Windows上新产生的实验记录，应在下次修订实验工具时按`experiments/sources/vN`快照规则一并修正。

<a id="confirm"></a>
## 当前采用

本会话用户已明确要求完成#18采用和G0核验；原话与准确差异见[ADR-004](../../decisions/ADR-004-p1-adoption.md)。B1–B4及R6承接范围已采用，B4恢复完整产品交付单元95%单测，不再次请求GitHub同意。#18追加阶段回应；原正文保留初始提案。H/部署B/C、HA、应用支持范围与产品实测不因此通过。

<a id="g0"></a>
## 确认后如何核验G0

| [G0判定项](../../../experiments/gates.json) | 需要的已采用依据 |
| --- | --- |
| 职责与通道边界 | H9、H8、ADR-002、ADR-003/C3，以及B1 |
| 准入契约 | ADR-003/C1，以及B1§6–7、B2 |
| 双后端支持边界 | H8、ADR-003/C2，以及B3 |
| 语言采用与测试判定 | ADR-001，以及B4 |

核对原始会话/评论与准确范围→保存唯一确认记录与B1–B4采用片段→运行`python tools/check_sdd.py docs/changes/DISC-007.json --phase implement --require DISC-007-R4`（连带R1–R3，不包含F0记录R5）→发布并回读采用与设计核验产物→在gates.json写G0设计记录（decision_sources保留原始来源，artifacts绑定相同规格片段及ADR/确认凭据）→`python experiments/check_gates.py --stage P1`返回0后进入B4§5的M1。已采用范围内连续推进，不把后续G1–G7提前作为编码条件。具体开发、进度恢复与完整交付见[开发流程](../../sdd/development-workflow.md)。
