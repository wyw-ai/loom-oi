# DISC-009：G2 观测、最终准入与导出契约收束

2026-11-04 · 修订 1 · 基线 `b50c08e228aa1c01686b35a59421ad2670154219` · `feature/TASK-007-p2-g2-readiness`。工作状态：正式待确认文档；三组需求均 Pending，未实施/预注册/运行。任务见 [TASK-007](../../spec/TASK-007-p2-g2-readiness/README.md)，唯一详细契约见 [G2 观测规格](../architecture/p2/g2-observation.md)。

## 问题、变化与不变范围

G1 已有 Unknown 分支，但当前控制协议没有观测发布、就绪检查往返或最终 ReadinessRequired 的隧道错误传播。共同架构要求 OIC 当前观测，B1 又让 OIS 独占 Channel/READY 提交；若不明确两组件之间的时点，单纯新增推送/查询会把过时快照误称为严格准入。

本轮推荐 Commit-Snapshot、Probe-HTTP、Export-Pull 三组：OIC 最终检查固定本次观测决定，OIS随后重查自己的授权/绑定/期限；本地明确允许的单源健康检查，独立 observation_revision；最新状态合并、挑战查询扣完整RTT、回环有限指标导出。健康检查后至 READY 的变化窗口是必须准确采用的新语义；它不保证业务成功。更强 Commit-Strict 仍需设计，屏障/预留名称不能当可行性证明。

独立复核指出，把 probe 配置塞进 Exposure revision 会扩大 B2 的旧流中断范围。当前方案据此改用独立观测版本，probe 变更失效样本/check/Preparing，不杀已 Open。OIC 已取消预留时不得转发；由于通知传播，消费者可能短暂看到 READY 再 ABORT，契约与竞态向量都明确这一点。OIS 不以 t_check 后的新健康快照重算本次已固定的决定。

C1 默认 best_effort、有效 NotReady 两模式拒绝、strict需有效Ready，及B1/B2身份/目标/许可、透明传输、不重放均保持，不重问原决定。G3可独立先行，设计不等待G4；未合入TASK-005不是本分支的实现基线。本轮处于待确认文档阶段，尚未实施或发布阶段评论。

## 来源、需求与真实依赖

| 来源 | 事实及准确支持范围 |
| --- | --- |
| H15：[原回复](https://github.com/wyw-ai/loom-oi/issues/17#issuecomment-6610997252)，plumeink，2026-11-03T17:32:13Z | “C1,C2,C3均按建议走，但要加入观察清单，后续需要根据整体效果持续验证”；只采用 ADR-003 列明范围 |
| W28：[既有采用](../decisions/ADR-004-p1-adoption.md)、[原始确认记录](../../spec/TASK-003-autopilot-entry/approval.json) | B1–B4和其实际承接的R6；不把没有具体接口的G2新增契约算作已批准 |
| E-G2：[#3](https://github.com/wyw-ai/loom-oi/issues/3)、[#11](https://github.com/wyw-ai/loom-oi/issues/11)、[#17](https://github.com/wyw-ai/loom-oi/issues/17)、[#18 I18-P02](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6620751696) | 已读正文和阶段记录：G2须具体收束，LAB-005是真实probe→归并→发布→导出；局部LAB-001不能替代 |
| B-G2：基线共同架构§6、B1 TV-B1-19、B2§4、B4§4、源码 | 既有约束与实现缺口；精确文件/符号见规格§2，不以设计文字证明实现 |
| W-G2：本会话用户，2026-11-04粘贴的交接文本 | 原文要求收束G2具体规格，新增契约在相关Issue追加并请用户确认，G3可并行；授权包含后续提交/Issue流程，不是本轮新产品选择已采用。准确原话和附件指纹如下；未提供请求UUID |
| R-G2：本轮独立运行态审阅 | RTT扣减/固定检查期限无高置信矛盾；指出probe改target revision扩权、A2屏障不能证明消除窗口。设计审阅不是产品实测或人类采用 |

W-G2原始附件：`C:\Users\c6321\.codex\attachments\47f4f367-9308-4c50-a124-2b1069e5b022\已粘贴的文本.txt`；原始字节SHA-256：`7d376952aea804816d2540c33acef3a8ff438523bd68e036c256ad931f03f4fd`。使用UTF-8解码，未提供请求UUID，不伪造会话请求标识。相关原文：

> - **G2**：观测/探针、就绪发布与导出、TTL 与乱序代数、控制公平性。
>   - 判定项：准入矩阵、TTL及乱序代数、独立发布、饱和控制公平性。
>   - 先把共同架构 §6 和 TV-B1-19 收束成具体规格包。
>   - 凡是新增对外契约或产品取舍，先在相关 Issue 追加确认点并用 ask_user 请用户确认，只暂停依赖它的部分；G3 可以并行推进。

| 需求 | 唯一确认目标/规格 | 验证/Issue | 依赖及理由 |
| --- | --- | --- | --- |
| DISC-009-R1 / Pending | Commit-Snapshot时点、v2消息/兼容、失败传播、消费者展示；[消息](../architecture/p2/g2-observation.md#wire)、[提交](../architecture/p2/g2-observation.md#commit) | V01/V02/V03；#3/#18，实验承接#11 | 依赖R2：实际本地决定需要明确观测来源/TTL/独立版本；既有B1/B2采用另核对，不能用本需求重复批准 |
| DISC-009-R2 / Pending | Probe-HTTP本地权限、独立observation revision、attempt/sample代数；[来源](../architecture/p2/g2-observation.md#probe-http) | V01/V02/V03；#3/#18 | 可独立采用来源/代数；不依赖R1获准才做设计；接入真实Open时需要R1 |
| DISC-009-R3 / Pending | Export-Pull、独立发布/公平性、候选默认与实验预算；[导出](../architecture/p2/g2-observation.md#export-pull)、[验收](../architecture/p2/g2-observation.md#acceptance) | V01/V02/V03；#3/#11/#18 | 完整发布/验收依赖R1，传递依赖R2；局部导出器设计不等于整项G2出口 |

替代选择、代价与可拆答确认表只在规格和任务入口维护。未建立新的Issue、评论或原生父子关系；后续优先由#3承载产品决定，#18承接实施范围、#11承接条件/验证，避免#17重复决定C1。

## 项目收束与产物联动

| 共同承诺 | 本轮保持/改变 | 影响及证据限制 |
| --- | --- | --- |
| 接对目标、权限连续 | 保持目标revision/授权/许可；新增观测版本不替代它们 | 仅新增待确认导航；不改已采用B1/B2正文或指纹 |
| 可解释的连接与失败 | 提出具体t_check与READY窗口、取消/到期/错误链 | 源码尚未实现；正常/反例轨迹及G2-01–14是计划 |
| 业务状态外置 | 保持不托管、不执行命令、不保证业务成功或历史可靠投递 | Probe-Report/主动导出是实质替代，尚无自动支持承诺 |
| 总资源与维护成本 | 提出有界探针/查询/发布/导出及有限指标 | 候选数字不是容量/SLO；首轮固定条件预注册后才实测 |
| 当前权威与历史 | G2细节唯一维护于p2/g2-observation.md；DISC只追踪；spec只任务 | 当前共同架构/模块仅加待确认导航；图1–11不改，避免将未采用时序画成现行实现；采用后按产物联动表更新 |

旧尝试核对：LAB-001只支持可达不等于就绪、局部独立调度；没有反驳或验证真实G2。此轮承接其剩余实现缺口，不把重新命名当新增证据。没有实验run、ITER或gate变动，旧Accepted/旧run保留。

<a id="validation"></a>
## 本轮验证与交付

V01为内容范围复核（已完成作者审阅及独立运行态/观测源专项审阅，纸面范围）；V02为结构/SDD/指纹/编码检查；V03为未来真实产品证据。当前未发布Issue、未取得新增采用、未运行产品；设计审阅及工具退出0均不改变这三项状态。自动检查记录：结构/链接/生成目录通过；DISC-001–009 review通过；选择DISC-009-R3实施检查因R1/R2/R3均Pending按预期拒绝；git diff --check通过。新建/修改文档经UTF-8无BOM、LF检查；全部既有Accepted需求对象与基线逐项比较不变。所有产品验证保持Blocked。

本轮新增规格、任务与DISC，更新catalog/索引/待确认导航。旧Pending整文件指纹仅因导航变化刷新，保持Pending；没有改写Accepted需求或其确认凭据。后续在相关Issue发布可独立评审的三组确认点，并按用户明确答复记录准确采用范围。

专项复核修正：独立观测版本保持B2；配置A→B→A使用单调observation_revision与不可复用work/query/check标识，digest只标识内容；目录低update_seq不得回退展示；原始用户附件替换错误的委派来源；preflight期限/幂等/HTTP取消、确定候选错误码与旧端ProtocolError限制已具体化；本地READY/取消两种顺序覆盖G2-08b/08c。以上为规格审阅结果，无产品run。

最终高风险复核：补齐所有观测消息的完整binding和必填/枚举组合校验、v1只走原Unknown提交路径、不同credential的同subject幂等票据仍绑原凭证、final固定后不因admission_mode调整反向改判，并明确正文不参与HTTP健康判断。35项tools单测通过；历史完整性检查核验6个run/4个ITER且无pending run，gate检查只确认继承台账有效，不放行阶段。当前G1资源采样/断言缺口正在补证，不能由旧台账Passed推断实施许可；已在任务与契约入口注明。无产品实验、旧run或Accepted规格变更。
