# OIS模块规格

v1/P1详细契约已按[ADR-004](../../decisions/ADR-004-p1-adoption.md)采用，后续研究范围另行确认；[ADR-002](../../decisions/ADR-002-state-and-binding.md)已确认业务状态外置、准确绑定与重建方向；OIS/OIC语言按[ADR-001](../../decisions/ADR-001-core-language.md)；没有产品实现。概念见[OIS](../../concept/ois.md)，共同接口以[顶层架构§4–8](../../architecture/alpha-v0.7.md#registration)为唯一契约。

| 处理单元 | 输入 → 输出 | 共同约束/失败 | 验证归属 |
| --- | --- | --- | --- |
| Identity/Catalog | 已认证主体、描述及版本 → 过滤后的目录 | 声明不等于真实性；目录不是访问许可；有界分页/描述 | G1，#3/#4 |
| Policy/Open | 目标revision、主体、额度 → Channel准备/拒绝 | 本地允许项取交集；Ready提交前复核授权；不转换业务凭证 | G1/G2/G3 |
| Relay | 两个认证数据腿 → 有界原生字节流 | 控制/数据分开；不解析shell/MCP工具语义；不跨失败隐式重放 | G3/G5/G7 |
| Store | 管理变更/版本条件 → 事务结果与审计 | PG/SQLite同管理契约；无自动后端降级；临时Channel不持久恢复 | G4，#4 |
| Leadership/Recovery | 明确支持的部署模式 → 当前任期/重新接入 | SQLite首版单实例；PG主备只在G6验证范围启用；旧实例复活须fencing | G4/G6 |

应用职责：外部工具适配器通过现有客户端契约访问，OIS无需新增统一Invoke或命令账本；命令result_unknown不能由通道关闭原因替代。将来的Loom上层只成为消费者/业务平台，不引入Agent Session、记忆或事件持久化到OI核心。

正常：固定目标授权后两数据腿配对；反例：执行成功后响应丢失，OIS只知道传输中断，不能返回“命令未执行”或替用户重试。提供方操作级权限与结果查询需要其自己的协议。

H15/C2已确认首版单实例与显式迁移边界；#4的v1事务/恢复契约已由B3采用，真实实现与G4证据仍待完成。新增工具兼容验证归#3/#11；不得因文档模块化放行主备或生产部署。

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
