# Registry有界批量插入实施计划

本项承接固定2a81完整开发轮的G2-12 PostgreSQL失败：第5个实体的token_create仅3.6729ms即被拒绝；此前upsert_registry已消耗504.8752ms，原500ms上下文到期后进入fail-closed。详见[原完整记录](liveness-complete-linux.json)。现有日志没有逐SQL、队列、锁和I/O分段，不能把某个瓶颈认定为唯一原因。本计划只减少源码中已经确认的逐行SQL开销，不承诺因此解决该样本。

## 最小实现及不变语义

只调整`sqlCore.UpsertRegistry`的PostgreSQL曝光INSERT段：使用参数化多行VALUES，每块最多64行、512参数，不限制整个registry的原合法数量；超过64行按原输入顺序分块，仍在同一原事务中。每块SQL模板长度和参数槽数有界，字段值沿原输入约束，不新增总字节上限；值不拼入SQL，日期继续走原UTC/微秒变换。SQLite保留原逐行路径。每行同为八列，不引入新依赖、COPY、ON CONFLICT、自动重试或SQL多语句。

原调用顺序保持：单writer排队、lifecycle进入、专用session advisory锁存活检查、业务BEGIN、删除当前Access旧快照、插入全部新曝光、事务内读当前config_version、逐条按序插入audit、COMMIT和实际连接析构join。空输入仍清空该Access并执行原version/audit行为；其它Access、epoch、config_version及audit顺序不变。业务原500ms、专用锁检查独立背景上下文、持久化批次原seq/prune逻辑和fail-closed恢复条件不变。

namespace错误仍在同一事务内按原输入位置处理：若发现错行，先执行本块已经积累的合法前缀；前缀本身有SQL/取消错误时保留它，否则返回原ErrInvalid。无论在哪一块失败，DELETE、全部已插入块与audit均不部分提交。数据库约束仍为原权威，重复PK/FK/非法值不得用upsert绕过。事务失败及COMMIT结果未知仍按原分类返回，不新增成功推断；没有对外契约或验收阈值变化。

对N行、M条audit的正常逻辑命令，原N+M+5变为ceil(N/64)+M+5（N为0时两式同为M+5）。例如N3/M1为9→7；这不是实际网络RTT实测，也不是失败请求的N/M记录。pgx冷prepare、连接重取、ResetSession和调度仍可能有成本，需保留完整新样本。

## 有效验证与复证出口

沿原真实双后端store夹具验证多行替换与空清空；新回归跨64行边界核精确曝光字段、其它Access、版本/epoch和audit顺序。注入块内/跨块重复主键、后行namespace错误、原beforeAudit故障及非法audit数据，要求旧快照与其它Access保留、无部分audit。保留原取消/单实例锁/Close析构及persist顺序回归；已有未知COMMIT是relay fakeStore的“已提交后返回错误”语义对照，不冒称真实PG丢失COMMIT回包。新批量事务回滚用真实数据库，不以mock SQL返回值替代。

Windows先跑完整store开发单测及vet，PG可用性如实标注；在同一专用Linux/PG环境以新固定Git来源运行完整store unit/race与必要原回归，随后与其它修复一起进行完整根/依赖质量及原G2-12双后端32实体复证。命令、原期限、失败与字节全保留。单独bulk回归通过不构成500ms性能、G2或G4通过；旧run/ITER不改写，alpha/main不变。

本计划须先提交、推送并逐字回读，再修改产品与测试。它属于既有双后端原子持久化合同内的实现优化，无新增产品决策请求。

## 实现与 Windows 开发检查

先行596f335推送回读后已实现上述64行/512参数路径及真实数据库回归。[开发记录](registry-batch-local.json)绑定两份最终Go源码、命令和完整双后端31节点库存。Windows完整store实际105项PASS、20项SKIP、0项FAIL；其中19项PG条件跳过、1项Linux FD跳过，无DSN时未创建的PG合同子项另不计通过。新增16个实际SQLite节点全部通过，覆盖1141/1422仅属该Windows包。vet、fmt及独立代码复核通过。

真实PG批量命令、QueryTracer计数和JSONB非法第二audit回滚尚未执行；本机没有启动PG。当前结果不证明原500ms失败已解决。下一步按[聚焦Linux计划](g2-repair-linux.md)在同一新固定来源核完整store/wsio unit/race及原G2-12，不拼接Windows覆盖或旧通过叶。
