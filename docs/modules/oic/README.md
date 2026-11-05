# OIC模块规格

数据腿活性条款现由已采用的[异步控制写与空闲窗口](../../architecture/p2/data-leg-liveness.md)限定替代原B1/B4心跳部分；准确采用见该规格入口。实现和同源验证待完成，正常排空、原IDLE/授权及资源期限保持；控制会话默认心跳未变。

G2 的本地 Probe-HTTP、独立观测版本与最终检查见[已采用契约](../../architecture/p2/g2-observation.md)；准确采用见[ADR-005](../../decisions/ADR-005-g2-adoption.md)，本工作区已纳入alpha的D20/CLI修正、D21、G3向量和全部历史复证；G2核心已实现，正在开发验证。当前d9本地G1已完整封口、精确复算失败，见[ITER-014](../../evidence/p2-local-g1-first-evidence-014.md)，G3/G4仍Inconclusive，最终源码须复证；G2没有正式通过记录。

v1/P1详细契约已按[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用，后续研究范围另行确认；[ADR-002](../../decisions/ADR-002-state-and-binding.md)已确认业务状态外置、准确绑定与重建方向；OIS/OIC语言按[ADR-001](../../decisions/ADR-001-core-language.md)；P1实现见[产品文档](../../product/README.md)，历史G1证据见[ITER-004](../../evidence/p1-g1-evidence-005.md)。历史固定2ec3670的G1/G4正式运行均通过，D18 Linux资源回归已验证；四单发布回读已完成，当时G1/G4恢复Passed并集成alpha；当前G2源的复证进度见TASK-007检查点，历史反例保留，见[ITER-006](../../evidence/p1-g1-resource-evidence-007.md)和[ITER-007](../../evidence/p2-g4-resource-evidence-008.md)。概念见[OIC](../../concept/oic.md)，共同对象/消息/准入以[顶层架构§3–7](../../architecture/alpha-v0.7.md#objects)为唯一契约。此页按模块索引处理责任，不复制一套协议。

历史固定019139f的G3四项、34叶/62正式资源phase通过；三单追加与逐字回读完成，G3仅按该固定源码及环境保留历史Passed，见[ITER-013](../../evidence/p2-g3-evidence-013.md)。D21仅修辅助日志kind，首轮失败及产品职责/接口保持，不外推G2/G5/G7。 当前G3旧黑洞向量的v2准入适配及最终复证由[TASK-007检查点](../../../spec/TASK-007-p2-g2-readiness/README.md)承接，不改变原流许可、资源时限或零重放契约。

| 处理单元 | 输入 → 输出 | 状态所有者与失败规则 | 验证归属 |
| --- | --- | --- | --- |
| 本地配置与登记 | 管理者允许清单、身份 → Access/Exposure登记 | 本地target_ref为目标权威；拒绝消费者任意地址；目标/权限变化提升revision | G1，#3/#4 |
| 控制连接 | 认证WSS、epoch/generation → 心跳/登记/PrepareChannel/Revoke | 旧代数命令拒绝；重新接入不复活旧Channel或Ready | G1/G3/G4 |
| 目标拨号与转发 | 当前许可、允许目标 → 原生流/明确传输错误 | ChannelReady前最终核验；预算/缓冲有界；关闭不回滚业务 | G1/G3 |
| 观测 | 允许probe及样本 → attempt/有效Observation | 版本/顺序/TTL约束；超时不伪造负证据；严格准入不使用陈旧缓存 | G2，#3 |
| 资源清理 | 取消、期限、断线、撤销 → 关闭绑定/释放资源 | 只清理对应代数；不向新目标隐式重放 | G3/G5 |

应用职责：应用窗口/桌面、手机设备和Pod只作为外部能力所在环境；UI串行、设备选择、应用登录和子进程由提供方持有。网关在线不能冒充设备/应用Ready。业务脚本/argv即使经过通道，也不是OIC控制协议字段；核心不增加exec/spawn API。

正常：已启动外部服务→本地登记→获准Open→原生协议使用。反例：两个Exposure映射到同一桌面仍互相干扰，提供方必须实施真实资源互斥；换一个OIC ID不能制造隔离。

实施入口：C1默认按H15采用，K1/R6的v1字段及跨模块契约已由[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用；[G0台账](../../../experiments/gates.json)设计核验已通过，可按B1–B4开始P1。后续阶段仍须真实实现及适用实验证据；不得用虚拟客户端证明真实设备兼容。新增设备案例归[DISC-001](../../changes/DISC-001.md)及#3/#11。

## 不同部署的目标绑定

| 部署 | OIC允许项/身份 | 生命周期反例 |
| --- | --- | --- |
| ECS宿主 | 固定低权限执行服务端点；程序/cwd权限仍归服务 | 服务重启不凭心跳伪造业务实例连续；旧操作由服务查结果 |
| PC/设备网关 | 明确MCP或外部bridge端点；多设备在业务端点内鉴权或分独立端点 | 不把两个Exposure贴在共享万能端点视为应用隔离；网关在线不保证设备/应用Ready |
| 一个选定Pod共置 | 部署方提供接入身份与允许端点；目标描述关联外部Pod UID | 同名替换需新实例绑定/revision，旧Channel不迁移；目录回收/凭证撤销由外部部署系统负责，不仅依赖正常退出 |

[完整布局比较](../../designs/application-paths.md#fleet)保留集中网关/每节点OIC替代，但不授予OIC自动遍历集群或任意kubectl能力。Pod共置的网络可达不自动共享文件系统，工作区权限由业务部署明确。Access generation、配置revision、外部实例身份仍是不同维度；不新增核心调度对象。

## 通用接入与运行状态

按[架构§2.2](../../architecture/alpha-v0.7.md#decoupling)，同一受支持传输新增业务只改外部服务、允许项、描述和revision；不增加OIC业务SDK。传输profile与业务协议描述分开，核心不因exec/MCP/UI名称加载执行器。观测仅按明确通用/提供方契约归约，缺证据Unknown。

[§3.1状态表](../../architecture/alpha-v0.7.md#oic-state)定义“业务无状态、连接运行有状态”：凭证/配置由部署方重建，Channel/代数/票据/缓冲/FIN/probe状态有界且重启失效。重认证/对账后新通道，不续旧Ready、不重放业务，不释放提供方设备锁。V09检查同内核换业务，V10检查旧状态拒绝与重建；均待真实实现验证，R5边界已采用；字段/参数详设归R6。

## 准确拨号与资源边界

每次Open核对并固定主体、目标/revision、当前代数、许可及Channel；A1→B1与C1→B2不因共享OIC而合并。旧代清理/重配只作用于匹配绑定，不回退到另一个同名目标。业务会话/句柄由服务管理，不新增Session→资源分配。单路失败不无差别关其他流；进程整体退出仍影响所持流，按§3.1重建，不承诺业务连续。三类资源及V04/V09/V10见[共同契约](../../architecture/alpha-v0.7.md#target-binding)。

## 路径变化如何回到项目承诺

按[loom-oi整体契约](../../concept/loom-oi.md#invariants)，本地允许项、最终准入、目标/版本与有限许可是所有路径的约束。[H候选](../../designs/ois-control-relay.md#normal-failure)需要区分接入generation和单次路径尝试，旧尝试不能提交或清理新尝试；共同拒绝终止Open，不换路绕过。路径准备可造成连接副作用，不能自动重放业务。此处是研究影响，不新增已实现P2P接口；当前R及业务无状态/运行有状态边界保持。

既有边界由[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)记录：C1默认/C2首版支持已定，C3只确认H研究边界。随后v1/P1协议、状态竞争和存储契约由ADR-004准确采用；[准备度评估](../../evidence/implementation-readiness-004.md)保留此前缺口和持续观察，不再作为重复批准依据。

v1/P1的详细契约已收束为[P1规格包](../../architecture/p1/README.md)（B1–B4，已按[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用）；本页职责边界不变，详细v1实施按该规格及适用门槛执行。

FIN修复及新G1运行见[ITER-006](../../evidence/p1-g1-resource-evidence-007.md)（C45/C47）；成功完成不覆盖许可/ABORT失败。D18仅涉及OIS存储资源，OIC契约保持。

最新历史bff7aa2的G1/G4复证见[ITER-010/011](../../evidence/p2-d20-revalidation-011.md)，四单发布、六项CI和alpha15deeab集成已完成；当前G2源码复证尚未完成，不外推历史结果。
