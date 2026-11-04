# OIS模块规格

G2 的版本化目录快照、最终检查协调与有界发布见[已采用契约](../../architecture/p2/g2-observation.md)；准确采用见[ADR-005](../../decisions/ADR-005-g2-adoption.md)，本工作区已纳入alpha的D20/CLI修正、D21、G3向量和全部历史复证；G2核心已实现，正在开发验证。当前生产代码变化使G1/G3/G4适用性均为Inconclusive，最终源码须复证；G2没有正式通过记录。

v1/P1详细契约已按[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用，后续研究范围另行确认；[ADR-002](../../decisions/ADR-002-state-and-binding.md)已确认业务状态外置、准确绑定与重建方向；OIS/OIC语言按[ADR-001](../../decisions/ADR-001-core-language.md)；P1实现见[产品文档](../../product/README.md)，历史G1证据见[ITER-004](../../evidence/p1-g1-evidence-005.md)。历史固定2ec3670的G1/G4正式运行均通过，D18 Linux资源回归已验证；四单发布回读已完成，当时G1/G4恢复Passed并集成alpha；当前G2源的复证进度见TASK-007检查点，历史反例保留，见[ITER-006](../../evidence/p1-g1-resource-evidence-007.md)和[ITER-007](../../evidence/p2-g4-resource-evidence-008.md)。概念见[OIS](../../concept/ois.md)，共同接口以[顶层架构§4–8](../../architecture/alpha-v0.7.md#registration)为唯一契约。

历史固定019139f的G3四项、34叶/62正式资源phase通过；三单追加与逐字回读完成，G3仅按该固定源码及环境保留历史Passed，见[ITER-013](../../evidence/p2-g3-evidence-013.md)。D21仅修辅助日志kind，首轮失败及产品职责/接口保持，不外推G2/G5/G7。

| 处理单元 | 输入 → 输出 | 共同约束/失败 | 验证归属 |
| --- | --- | --- | --- |
| Identity/Catalog | 已认证主体、描述及版本 → 过滤后的目录 | 声明不等于真实性；目录不是访问许可；有界分页/描述 | G1，#3/#4 |
| Policy/Open | 目标revision、主体、额度 → Channel准备/拒绝 | 本地允许项取交集；Ready提交前复核授权；不转换业务凭证 | G1/G2/G3 |
| Relay | 两个认证数据腿 → 有界原生字节流 | 控制/数据分开；不解析shell/MCP工具语义；不跨失败隐式重放 | G3/G5/G7 |
| Store | 管理变更/版本条件 → 事务结果与审计 | PG/SQLite同管理契约；无自动后端降级；临时Channel不持久恢复 | G4，#4 |
| Leadership/Recovery | 明确支持的部署模式 → 当前任期/重新接入 | SQLite首版单实例；PG主备只在G6验证范围启用；旧实例复活须fencing | G4/G6 |

应用职责：外部工具适配器通过现有客户端契约访问，OIS无需新增统一Invoke或命令账本；命令result_unknown不能由通道关闭原因替代。将来的Loom上层只成为消费者/业务平台，不引入Agent Session、记忆或事件持久化到OI核心。

正常：固定目标授权后两数据腿配对；反例：执行成功后响应丢失，OIS只知道传输中断，不能返回“命令未执行”或替用户重试。提供方操作级权限与结果查询需要其自己的协议。

H15/C2已确认首版单实例与显式迁移边界；#4的v1事务/恢复契约已由B3采用，恢复实现、bff7aa2的G4四项正式复证及四单回读已闭环。当前G2源码的G1/G3/G4需最终同源复证；PG关闭锁交接的独立反例与修复准备、G3旧黑洞向量的v2准入适配见[TASK-007检查点](../../../spec/TASK-007-p2-g2-readiness/README.md)，不能用历史Passed掩盖新缺口。新增工具兼容验证归#3/#11；不得因文档模块化放行主备或生产部署。

## 应用身份与失败责任

目录提供获准Exposure及版本；别名可由外部工具维护，不能绕过Open的subject/目标/策略检查。应用控制网关暴露一个端点时，OIS的open授权只覆盖该端点，网关仍须做设备/应用的业务鉴权；仅拆描述标签不能制造资源隔离。

Pod UID、操作ID、事件ID与任务job属于提供方/上层，不能用Channel ID替代其持久状态。目标配置变化令旧绑定失效；提供方实例不可观测时不捏造新revision或恢复保证。OI给可核实的传输状态，业务结果未知由外部协议表达，不能统一转换为可重试失败。

[未来harness链](../../designs/application-paths.md#harness)要求消费者、目标、通道和业务session可分别识别，不要求OIS新增模型、记忆、事件游标或业务任务表。#4双后端/恢复契约不变，本轮没有主备或持久结果实验。

## 目录通用性与资源边界

Exposure的传输profile与业务描述分开，OIS只按已支持传输、元数据外壳、权限和版本协调；不把描述中的工具名转为统一业务API。Access/Exposure不承担Pod调度或真实资源锁，多个暴露项可能共享业务资源。

[应用仲裁](../../architecture/alpha-v0.7.md#resource-arbitration)在外部执行入口；OIS的Channel关闭/租约到期不能证明点击停止，也不能释放业务锁。现有#4临时连接不持久恢复、PG/SQLite和许可/任期契约保持；本轮未改恢复承诺或增加数据库对象。

## 目标配对与传输分层

OIS必须保持获准主体→Exposure/revision→两数据腿的精确关系，不用“最后选定目标”或类型/名称自动改路。物理共享或未来连接池不合并授权、字节流与取消域；共同许可/任期失效仍按既有规则影响关联通道。当前v1独立WSS按B1实施，每个传输Open建立数据腿，非每Agent Session的资源分配；池化仅为成本触发备选。H8可信中继、双数据库和恢复范围不变，详见[架构§5.7](../../architecture/alpha-v0.7.md#target-binding)。

## 通道策略与部署接缝

OIS的模块责任由[loom-oi共同承诺](../../concept/loom-oi.md#promises)推导。当前R承担控制协调和中继；[通道主方案](../../designs/ois-control-relay.md)将新Open的D/H路径候选纳入负载与可用性研究，再由实际中继字节、控制预算和故障域推导A/B/C部署。研究不要求先批准H，也不把H视为已支持。

`policy/catalog/store`维持单一管理权威；`relay`只持有获准配对与临时流状态。候选H需为尝试身份、路径代数和唯一提交明确共同契约；这些是待设计字段，不能与OIC接入generation混用。共同拒绝不能用路径回退绕过，直接数据路径也不代表离线授权。B/C还需内部认证、准确中继实例绑定、撤销同步和有限许可；不另建策略权威、不恢复业务载荷。详见[§8.4](../../architecture/alpha-v0.7.md#control-relay)，#3/#4共同契约保持。

既有边界由[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)记录：C1默认/C2首版支持已定，C3只确认H研究边界。随后v1/P1协议、状态竞争和存储契约由ADR-004准确采用；[准备度评估](../../evidence/implementation-readiness-004.md)保留此前缺口和持续观察，不再作为重复批准依据。

v1/P1的详细契约已收束为[P1规格包](../../architecture/p1/README.md)（B1–B4，已按[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用）；本页职责边界不变，详细v1实施按该规格及适用门槛执行。

SQLite资源修正保持B3读连接≤4、独立写连接与WAL语义；不能通过重采故障后基线或放宽FD比较使D18通过。生产修正后的G1/G4已在固定2ec3670重新验证，四单发布回读已完成，当时G1/G4恢复Passed并集成alpha；当前G2源的复证进度见TASK-007检查点。

最新历史bff7aa2的G1/G4复证见[ITER-010/011](../../evidence/p2-d20-revalidation-011.md)，四单发布、六项CI和alpha15deeab集成已完成；当前G2源码复证尚未完成，不外推历史结果。
