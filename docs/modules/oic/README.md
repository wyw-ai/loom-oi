# OIC模块规格

当前模块细节评审中；[ADR-002](../../decisions/ADR-002-state-and-binding.md)已确认业务状态外置、准确绑定与重建方向；OIS/OIC语言按[ADR-001](../../decisions/ADR-001-core-language.md)；没有产品实现。概念见[OIC](../../concept/oic.md)，共同对象/消息/准入以[顶层架构§3–7](../../architecture/alpha-v0.7.md#objects)为唯一契约。此页按模块索引处理责任，不复制一套协议。

| 处理单元 | 输入 → 输出 | 状态所有者与失败规则 | 验证归属 |
| --- | --- | --- | --- |
| 本地配置与登记 | 管理者允许清单、身份 → Access/Exposure登记 | 本地target_ref为目标权威；拒绝消费者任意地址；目标/权限变化提升revision | G1，#3/#4 |
| 控制连接 | 认证WSS、epoch/generation → 心跳/登记/PrepareChannel/Revoke | 旧代数命令拒绝；重新接入不复活旧Channel或Ready | G1/G3/G4 |
| 目标拨号与转发 | 当前许可、允许目标 → 原生流/明确传输错误 | ChannelReady前最终核验；预算/缓冲有界；关闭不回滚业务 | G1/G3 |
| 观测 | 允许probe及样本 → attempt/有效Observation | 版本/顺序/TTL约束；超时不伪造负证据；严格准入不使用陈旧缓存 | G2，#3 |
| 资源清理 | 取消、期限、断线、撤销 → 关闭绑定/释放资源 | 只清理对应代数；不向新目标隐式重放 | G3/G5 |

应用职责：应用窗口/桌面、手机设备和Pod只作为外部能力所在环境；UI串行、设备选择、应用登录和子进程由提供方持有。网关在线不能冒充设备/应用Ready。业务脚本/argv即使经过通道，也不是OIC控制协议字段；核心不增加exec/spawn API。

正常：已启动外部服务→本地登记→获准Open→原生协议使用。反例：两个Exposure映射到同一桌面仍互相干扰，提供方必须实施真实资源互斥；换一个OIC ID不能制造隔离。

实施依赖：C1默认已按H15采用；剩余K1/R6字段及跨模块契约评审完成并满足G0；本页的纸面设计不放行实施。当前可以准备目标替换、旧generation、Ready过期、预算耗尽的验收向量；不得用虚拟客户端证明真实设备兼容。新增设备案例归[DISC-001](../../changes/DISC-001.md)及#3/#11。

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

本轮采用范围以[ADR-003](../../decisions/ADR-003-first-release-boundaries.md)为准：C1默认/C2首版支持已定，C3只确认H研究边界；具体协议/状态竞争/存储实现未随之全部批准。开发缺口与联合观察见[准备度评估](../../evidence/implementation-readiness-004.md)，不能以方向采用替代G0。
