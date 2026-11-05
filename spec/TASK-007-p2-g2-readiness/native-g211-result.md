# 原生 G2-11 开发结果

固定源码 `9c229d2717c5b1694c4b73a1bb7d37712eff9199`（tree `afe8a5667a75f679985bda67c97259336c771564`）。本次仅原生主机一次固定构建、225辅助节点及G2-11六叶；**实际pipeline/host execute均exit1，完整strict仍FAIL，不是正式证据运行。来源与生命周期通过不能覆盖业务失败。**

完整同源 `expected_spec()` 未缩小或伪造父节点；6叶实际2PASS/4FAIL、3个父节点全FAIL。SQLite1、PostgreSQL0另调用原 `validate_competition` 得到本叶语义PASS，分别118/117cycles，共235；这些是局部诊断结果。未执行的其它G2组仍缺证，不能宣布完整G2通过。

| 叶/round | Go | 实际结果 | complete cycles | 原资源2s | 首错raw行 |
|---|---|---|---:|---|---:|
| sqlite/0 | FAIL | critical 后实收200，原判据429失败 | 0 | 未形成检查 | 93 |
| sqlite/1 | PASS | 单叶语义 PASS | 118 | PASS，126.816538ms | — |
| sqlite/2 | FAIL | critical 后实收200，原判据429失败 | 0 | 未形成检查 | 25820 |
| postgres/0 | PASS | 单叶语义 PASS | 117 | PASS，178.176595ms | — |
| postgres/1 | FAIL | final_selection 未形成联合输入 | 0 | 未形成检查 | 51378 |
| postgres/2 | FAIL | critical 后实收200，原判据429失败 | 0 | 未形成检查 | 52003 |

两个完成叶的14慢源窗口分别60.385171331s、60.072581437s；资源共2check/7sample，原3连续与2s均保留。四个失败叶没有形成资源检查，不能借外层已回收倒填资源通过，也不能单凭缺check称泄漏。

三个critical失败叶final200/critical300见证合格，新增前后同次角色Stats均观测exporter Active=0/Youngest=0，Full五分量仍真；native、GET、DELETE及provider/control Wait正常返回，context尚active。原后置抓取实际200，所需429不成立。timeout2/success97/rejected5是合计计数；429拒绝也走相同write/failure路径，**不能把timeout2对应初始两个200占槽writer或某个具体请求**。Active0与200也仅是其采样/处理时刻的事实，未证明释放时点或唯一原因。

PG1在final_selection失败：70个final样本中36次Full，36次原young均拒绝，首拒年龄204.738403–390.743408ms；实际Drop14/上限32，94条准备历史无overflow。Full与exporter Active2没有同次交集；最后Stats实际跨原socketD返回而被拒，不续期。记录不足以排除采样间存在短暂相位。

## 来源与退出

910源码逐Git blob/mode/SHA核对一致；225辅助节点全PASS且无skip。最终清单164文件254,191,950B，另加manifest自身，tar共165成员/254,371,840B，SHA256 `2e35341446ddbae57c0fe7c6e98516f31c3afaa5fdec4b55e35a28ddbe7472e9`。G211原GoJSON 68,381,781B，SHA256 `bea261c95e67c407901ffec8ed5e041a1a3e2c23de1fef2f7dbd516902c693eb`。准备76原件、私有env/config与源前后绑定一致；秘密DSN不公开。

7child退出为compile/build-ois/build-oic/build-oi/build-info/helpers=0，G211=1。实际pipeline309.101312179s，G211stage136.242295792s；前5命令共享预构建900s、helpers240s、G2111200s、integration5700s，全部截于准备起点parent9000s（host9030s）。所有wait/source后验/结果write边界均在原D内；无timeout/监督errors。runner3948804仅group=False身份Gone；7个child组分别在原stage及collect核Gone/empty，不能把runner身份检查夸大为单独runner组扫描。

私有PG3948584/start2006434303与boot/exe/data一致，原pg_ctl fast stop0、组空；停止与采集共享300s并截parentD。最终PGlog3148B/21行保留准备旧前缀，CST03:02:05.845真实shutdown。新增collector固定纳PG最终日志/stop原件，未改正在执行的driver。

## PG FATAL的解释边界

PGlog第10行CST03:00:04.010为 `terminating connection due to administrator command`。PG1首错raw51378是03:00:03.992719468+08，Go叶FAIL raw51902是03:00:04.127876829+08。固定源码env_test.go:262–265注册清理，263调用`DROP DATABASE IF EXISTS … WITH (FORCE)`。日志落在首错之后、叶退出之前，与该清理路径相容，且早于最终私有PGstop；但未记录该SQL执行时刻/目标backend PID，**不能确证由DROP触发，更不能反推PG导致先发生的final_selection失败**。ERROR/PANIC各0，FATAL实际1条保留。

## 本地分析纠错与结论限制

独立入口01错误要求child.boot_id而exit1；02按固定process_identity的6真实字段校验，boot仍由prepared/前后platform绑定，exit0。直接witness调用先遗漏preparation=True、再误把(conn,registered)当scalar，两次原错均保留；最终03正确调用exit0。root-summary02仅修错误行筛选。以上均未改raw、产品或同源分析器判线，不计作产品新失败或通过。

没有完整质量/95%覆盖、G1/G3/G4/G2正式复证或400clock结果；不改变任何门槛。与旧QEMU样本的环境和诊断源不同，本次两叶通过不能唯一归因TCG，也没有采用新输入方案。详尽哈希与各叶raw定位在同名JSON的sources；不复制大型原件。

## 项目回写与下一实现

本轮按[原生先行规格](native-g211.md)与[联合失败诊断规格](joint-failure-diagnostics.md)执行；产品源码仍等同3be，仅测试诊断变化。新增时序不支持本次省GET解决占槽失败，保留原GET→DELETE。下一最小候选是在已连接的两条慢socket释放原GET前，先完成末八Set及必要的有界Full/young准备；原Zero/released采样贴真实释放，后置final200/critical300继续独立实采。前置观察不能替代后置证据，也不能制造不存在的年轻批次。新等待的panic/Goexit清理边界需在编码前固定，仍用原D和单一socket owner。

本轮未改变共同准入/活性/存储契约、支持范围或图06状态，因此不重绘架构、不新增formal/ITER。G1 Failed、G2 Blocked、G3/G4 Inconclusive及19旧run/14ITER保持，alpha/main未推进。完整同源质量、正式G1→G3/G4/G2及既有授权主机SSH/Bash验证仍待继续；本次实际主机范围不外推为严格1C1G证明。结构化来源见[结果JSON](native-g211-result.json)，原件保留在ignored coverage路径而非冒充已入库的正式证据。
