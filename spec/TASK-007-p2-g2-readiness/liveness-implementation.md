# TASK-007 数据腿活性修复实施计划

依据[ADR-006](../../docs/decisions/ADR-006-data-leg-liveness.md)与[原始采用](liveness-approval.json)，固定data-leg-liveness scope不变。本项修复阻挡当前负载/后续G2的共同数据腿行为，沿用当前任务feature；不是另一套业务或新应用功能。完成文档提交回读后编码；现尚未实现。

## 接线与所有权

1. 固定上游依赖副本与许可证/来源/补丁清单，go.mod本地replace指向仓库受管路径；只加有界PING/PONG帧写入口，内部沿原frame锁、mask、错误与关闭，跳过旧5s wrapper。旧默认Ping/自动Pong/控制会话保持。上游现有测试与新增125B/opcode、真实帧、ctx/Close/错误测试先通过；新增本地补丁代码纳入有效测试统计，不能借第三方排除掩盖自有逻辑。
2. 在内部wsio层实现唯一Reader回调到固定pending PONG、单控制worker与一个PING owner；固定125B pending/125B in-flight/8B nonce上界，copy与claim/retire序列明确，有限公平选择。真实写错误只通知原终态协调者，不在worker同步join自己；库不能通过取消某次观察误杀仍活连接。
3. OIS沿每腿单调t0及固定tick进行窗口归约，busy为具体DATA owner的来源转发/目的pending，覆盖enqueue→claim→finish并且只释放一次；sticky life记录两tick间活动。合法DATA和有效PONG分开，原channel.lastData不改。重排/晚PONG、退役write和长调度暂停不得制造历史发送或续D。
4. OIC与client的Accept/Dial回调显式启用同一新模式，控制会话保持原默认；三端transport真实关闭后解除写并join。正常Completed先按既有DATA/FIN排空、forceAt与原首终因，不在保存业务首终态时抢先CloseNow。现有读相位诊断保留真实故障分类，并增加必要有限新状态，不能用诊断构造新的成功路径。

## 验证顺序和边界

依赖API→控制owner/窗口纯状态与真实WS→三端有界接线→完整产品unit/race/静态/漏洞→固定新来源G1/G3/G4/G2开发及正式复证。确定性窗口用可控逻辑输入检验精确边界，真实socket/ctx/关闭另测；不改宿主或用户主机时钟。保持所有原半关闭/未知结果/许可/撤销/资源回收断言，按新准确合同更新原心跳时间预期并保留替代原因，不删失败路径来凑PASS。

旧版本矩阵区分正常wire兼容与完整背压能力，仅新端组合承接新策略；不静默回退或重试未知业务。控制worker/固定payload成本纳入原资源窗口及完整交付成本。未来正式清单加入新增根/叶与依赖源清单、当前准确语义，旧manifest/run/summary/Accepted源不重写。

当前固定Render已完成Windows与Linux metrics定向unit/race；六源G2夹具与分析器还在独立复核，将先提交固定源码并运行原预算开发输入。两项检查不能替代新活性实现的全产品复证。alpha/main、用户主机部署和最终release门槛均按原授权/前置继续，不由本计划直接晋级。
