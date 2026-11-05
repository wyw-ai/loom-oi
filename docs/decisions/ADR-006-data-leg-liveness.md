# ADR-006：数据腿异步控制写与空闲窗口的准确采用

2026-11-05 · 设计已采用，尚未实现或通过产品验证。唯一详细契约见[数据腿活性规格](../architecture/p2/data-leg-liveness.md)，本页解释本次选择及其准确边界。

## 原始确认

本会话用户在工具调用`call_c1f9a7b9fce746ab8b151b1adf9e6cc0`的item 0回答“采用完整方案（推荐）”。完整原问题、答案、questionItemId及固定指纹保存在[唯一凭据](../../spec/TASK-007-p2-g2-readiness/liveness-approval.json)。原问题明确指向[#3 I3-P14](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6653590966)的固定`207cf918bbafdcd29d49a3d4b32f31d6bf690c3f`提案，整个data-leg-liveness scope原字节与指纹保持；没有请求UUID或新的GitHub人类评论可据，不伪造来源。

## 选择与后果

整体采用DISC-010-R1：连接级有界异步PING/PONG，OIS固定PING节拍、两个连续空闲观察窗口判定；合法DATA进展或真实有界DATA待办免积累空闲miss。观察nonce到期与真实wire写生命周期分开，第二空窗即使没有第二条PING实际发送也可终止，初始最早逻辑T3。有效PONG不计DATA活动；原IDLE、许可、撤销、正常DATA/FIN排空和关闭总期限保持。

维护仓库内固定coder/websocket v1.8.15副本及最小本地replace补丁，新有限PING/PONG入口复用writeFrame而绕过旧writeControl的5s包装，其他调用默认不变；每数据连接增加一个控制worker、至多258B控制payload（现有库编码/TLS缓冲另计）。保留许可证、上游hash、补丁与离线/漏洞关联维护。OI v1字节互通；混合旧端不承诺同等背压存活，不自动协商新wire版本。

本次准确替代B1 §5/B4 §3数据腿心跳定义的适用部分，旧Accepted文件与确认对象保留原字节并由本ADR限定替代范围。控制会话心跳、READY准入、每方向一F、身份/目标授权、共享IDLE既有更新点及未知业务不重放继续有效。没有采用新业务执行器、更多DATA缓冲、独立控制传输或永久方向存活保证。

## 实施与验证

按[TASK-007实施计划](../../spec/TASK-007-p2-g2-readiness/liveness-implementation.md)完成依赖、有限状态与三端接线。正常Completed仍走原drain/close-after-queue/forceAt，在原transport关闭点才取消/join新worker；确定性反例必须覆盖正常尾部与挂住控制owner并存，不能只测试异常关闭。

G1失败及G2/G3/G4实际状态保持。采用后仍需完整unit/race/95%有效单测、静态与漏洞、两后端负载/背压/半关闭/撤销/恢复/新旧端矩阵及原资源期限复证；历史失败和归档分析器不变。没有以采用宣布产品PASS、alpha合入或G5/G7通过。Issue采用回应将在固定采用记录提交回读后追加，AI回应仅转述本原始问答。
