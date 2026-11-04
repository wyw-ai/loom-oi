# ITER-007：G4资源反例、读池修正与固定源码复证

2026-11-04 · 实现级产品实验。本未发布迭代承接两个G4 run：`5668b5335f364df7ee182128d0154f9cb64d5643`首轮 **observed/fail**，修正后的`2ec36709471fe75c0e194c0ddf8f7699b7939fe2`复证 **observed/pass**。原失败、原summary与ITER-005历史输入均保留；新run作为本迭代主证据，不把旧run重绑定到新代码。

新G4的四项直接判定、Linux读池正负对照及严格资源回归已经通过；**当前G4为Inconclusive：直接证据通过，待Issue/ITER发布闭环，不提前Passed。** 同源G1结果由[ITER-006](p1-g1-resource-evidence-007.md)独立承接，不重复计入本轮。旧版本FD反例仍成立，但不能继续把修正后的当前G4直接结果写成Failed。

## 两次输入、固定来源与独立核验

| 输入 | 版本与结果 | 本轮用途 |
| --- | --- | --- |
| [G4-P2-20261104T072937Z-bebf95](../../experiments/runs/G4-P2-20261104T072937Z-bebf95/report.md) | `5668b53`；[Actions 41792534101](https://github.com/wyw-ai/loom-oi/actions/runs/41792534101)；observed/fail | superseded：保留11h/sqlite严格资源反例及定位过程；仅当前通过证据用途被新版本取代，不改原失败 |
| [G4-P2-20261104T080816Z-097edc](../../experiments/runs/G4-P2-20261104T080816Z-097edc/report.md) | `2ec3670`；[Actions 41797067163 / job 126463543110](https://github.com/wyw-ai/loom-oi/actions/runs/41797067163/job/126463543110)；observed/pass | primary：支持该固定源码的原四项G4判定及D18 Linux回归 |
| [G4-P2预注册](../../experiments/manifests/G4-P2.json) | `67a164ab0f4f393ae90426d1e87aab0823ea523e`，2026-11-04 05:29:34 UTC | SHA-256 `74aaedae340299213b23739e074a0d054f6076d6d6894660f07f74a1866ea9db`；两次均保留原criteria、向量、步骤和load=false |
| [原G4分析](p2-g4-evidence-006.md)、[ITER-005](../../experiments/iterations/ITER-005.json) | `6c7d5ad`的历史observed/pass | 保留原时点与范围，不重复计为本迭代样本 |

两次原summary均经`verify.py`精确复算。首轮114项source-manifest已与Git `5668b53`逐字核对；新run执行`verify.py --current-source`得到integrity=verified、recomputation=observed，另独立核对116项源文件SHA-256均等于Git `2ec3670`对象。下载的10个文件与入库副本逐字相同。去掉运行附加的subject_version/preregistration后，新manifest与已提交预注册对象完全相等；未事后提高或放宽原判线。

| 原始记录 | 首轮失败 | 修正复证 |
| --- | --- | --- |
| raw.jsonl行数／字节 | 5,419／2,366,351 | 5,715／2,589,185 |
| SHA256SUMS自身SHA-256 | `842a4d92521c52c8b7e51fec38eb6b4a48a712de045bdf62c495b5af9b4e94b9` | `f9f2eea639960217d50f495e5db9c1642e3392549b225fdd46ef7d6c5d053b03` |
| 被测提交时间UTC | 07:27:23 | 08:03:35 |
| run标识／首个Go事件UTC | 07:29:37／07:29:38.707371841 | 08:08:16／08:08:16.706916246 |
| 无换行Output分片数 | 631 | 668 |

独立按(step, Package, Test)重组Output，两次均无尾残，OBS分类计数与各自summary相等；D16分片缺陷未造成这两次摘要漏记。预注册先于两次执行。校验与复算证明原件完整、分析可重复，不能单独证明产品正确或历史故障根因。

新run环境为GitHub托管ubuntu-24.04镜像20261026.354.1、Linux amd64/kernel7.0.0-1017-azure、2个可见CPU（Xeon Platinum 8573C）、MemTotal=8,121,972 kB，Go1.27.3、Python3.12.15、SQLite3.54.1及PostgreSQL18.6（Debian18.6-1.pgdg13+2）。environment记录small_fs=true、PG工具来自服务容器；11e两后端及11a/b/c实际执行通过。32MiB为预注册tmpfs设置；单机回环不是G5真实1C1G、WAN、HA或真实应用兼容环境。

## 首轮反例：功能完成后，固定FD基线未归位

首轮唯一失败leaf为`TestTVB3_11h_AuditRetention/sqlite`，父测试随之失败，不是两个独立故障。该测试先完成审计导出、篡改/错误摘要拒绝、合法修剪及SQLite增量vacuum，再记录O2并检查资源。

| 旧raw行 | 原始事实 |
| --- | --- |
| 4869 | profile-161，OIS PID8519，无OIC；基线goroutine14、FD16，channels/tickets/accesses均0 |
| 4870 | 审计304行；库1,413,120→73,728字节、345→18页；此前功能断言已完成 |
| 4871 | check-162，started_ns=125310032766，deadline_ns=127310032766，固定2秒预算 |
| 4872–4910 | 39条样本均为同PID、goroutine14、FD17；无FD回到16的样本 |
| 4911–4913 | 未归位、leaf FAIL；最后sample距begin约1.994s，没有成功final |
| 4916、4922、4924 | PG对照基线goroutine13/FD12，三个样本相同，106ms完成，leaf PASS |

首轮全部资源为101个profile、103次begin、619次sample、102次成功final；仅上述check未完成，其余完成106–1130ms。TV-B3-11a–i有18次check、17次成功。原13个G4向量ID为12 PASS/1 FAIL，75个必需父子检查为73 PASS/2 FAIL；三项criteria通过，SQLite规模维护项失败。unit/race各309个测试PASS、覆盖95.9%，不能抵消资源反例。

**C46仍为受限的历史实测结论**：在`5668b53`正式run中，11h/sqlite功能断言完成后，固定2秒窗口的39个样本FD17对基线16未归位；该run失败。E68/EDGE-039保留。原raw没有FD链接/池状态，不能识别第17个FD，更不能据常量+1外推无限增长、永不释放或泄漏速度。

## D18修正与Linux可区分对照

源码审查发现固定modernc SQLite Unix VFS在仍有文件锁时，可将已关闭连接的主库FD放入待关闭链。因此`database/sql`只有一个idle reader不保证数据库文件描述符已回到原数。D18把实际读池固定为一个保留reader、另保留独立writer，仍满足已采用B3“读连接≤4”。代价是并发读在同一连接上等待；原context取消和WAL读写分离保持，不增加吞吐或容量承诺。

新run中`TestSQLiteReaderDescriptorsReturnToBaseline`在Linux unit/race均实际运行生产默认与旧池负对照。测试先固定同一健康基线，再持有真实reader并发起第二次快照读取；默认池等待并复用连接，旧策略仅在独立测试store中设max4/idle1，确实打开第二reader再释放。以下是完整重组后的T.Log及断言结果，不是仅依据测试名称推断机制。

| 新raw位置 | 默认池 | 旧池负对照 |
| --- | --- | --- |
| unit 1186–1189、1192–1195 | MaxOpen=1/Open=1/InUse=0/Idle=1；数据库相关FD为6→6，按每个文件目标计数均等于基线 | MaxOpen=4/Open=1/InUse=0/Idle=1、MaxIdleClosed=1；FD为6→7，多出的正是一个主库文件FD |
| race 2521–2524、2527–2530 | 同样6→6、单一idle reader；PASS | 同样6→7、只多主库FD；PASS意味着反例按预期出现，不是旧池满足基线 |

两种配置随后都执行store.Close并断言该数据库的所有FD消失；这也通过。负对照直接证明该受控序列里旧池存在“逻辑连接已关闭而主库FD仍保留”的机制，支持D18窄修正；它没有为旧run缺失的第17个FD补造身份记录。

`TestSQLiteReaderWaitCancellationAndWALWrite`在unit/race分别见raw1181/2516 PASS：占用reader的活读事务期间，独立WAL写入成功；等待reader的请求遵守context取消；释放后新快照读到新配置版本，reader回池。源码与旧采用边界一致；未依靠提高FD容差、故障后重采、延长期限或跳过Linux检查来通过。

observer侧fd_diagnostic只在发布baseline或清理失败时记录FD链接，不参与资源判定、不延长清理期限。新run共276条，全部stage=baseline，没有cleanup_failure；11h初始OIS链接见raw5144–5145/5157。它们用于定位，不能把链接数与在另一时刻采集的全进程open_fds混作同一快照。

其中两条诊断明确报ENOENT：raw4985/5014，对应双向迁移已停止的原OIS PID8306/8431。`prepareOISBaseline`先预采目标后端的参考快照并停止参考环境；`useOISBaseline`复用该快照，随后`startOIS`才替换原进程。发布派生profile时，诊断仍读取旧`e.ois`的PID，因此原进程的/proc已不存在。这是诊断覆盖的生命周期缺口，不是资源stats采样失败被置零：profile-143/148逐字复用参考PID8354/8459及原sampled_ns，实际check读取新PID8413/8509，各三个完整有效样本、109ms成功。保留两条error，不宣称诊断全无错误；参考快照与实际check允许PID不同，单次check内PID保持稳定。

## 新11h精确回归与全部资源序列

| 新raw行 | SQLite | PostgreSQL |
| --- | --- | --- |
| 固定基线 | 5143，profile-161、PID8833，goroutine14/FD16，无OIC | 5156，profile-163、PID8862，goroutine13/FD12，无OIC |
| 功能观察 | 5146，审计304行，库1,413,120→73,728字节，345→18页 | 5158，审计303行，库8,459,967→8,380,943字节；无SQLite式页回收结论 |
| begin | 5147，126565795604→128565795604ns | 5159，127738909130→129738909130ns |
| 三个连续样本 | 5148–5150，全部同PID、goroutine14/FD16、通道/票据0 | 5160–5162，全部同PID、goroutine13/FD12、通道/票据0 |
| 完成／leaf | 5151：105ms；5152：PASS | 5163：107ms；5164：PASS |

新raw的资源序列有101个baseline profile、103次begin、566次sample、103次成功final，覆盖96个带资源记录的测试、1623个进程样本快照。独立按现有严格规则逐条重算，检查profile/backend/角色、冻结快照、PID、先后与2,000,000,000ns绝对窗口，均无问题；另逐次核末尾三个样本的FD精确相等与goroutine双向界限。

103次完成105–1137ms，最小deadline余量862,067,018ns；最终三个样本的goroutine相对基线为0..1、FD差全0，OIS Preparing/Open/Closing/tickets与OIC channels归零，在线拓扑匹配基线，OIC Connected/Registered及epoch/generation有效。20次检查先有额外轮询才完成，不把全部566个样本都宣称为已归位。TV-B3-11a–i全部18次check成功。

上述全记录资源重算是独立诊断，原G4 manifest仍没有G1完整minimum_checks清单；没有事后修改manifest/summary或把G4升级成完整G1/O3证据。G1完整范围另由ITER-006的同源新run判定。

## 四项G4判定、完整质量与运行观察

原13个唯一向量ID和75项必需父子测试检查全部PASS；必需后端、tmpfs与PG工具分支没有跳过。原四项criteria全部pass，overall为observed/pass，problems、failed_tests、zero_tolerance_lines均空。

| 判定项 | 首轮→新run | 新run具体支持 |
| --- | --- | --- |
| 并发版本冲突 | pass→pass | 两后端配置冲突与11g；queue8、burst40，各1成功/7冲突/32 StoreBusy |
| 备份与重启恢复 | pass→pass | 04、11a/b/d/i与store回归；维护期间新Open为0，恢复后的丢失吊销需管理员重施并resume；显式双向迁移 |
| 旧任期拒绝 | pass→pass | 10、11c；已见epoch2后恢复为3，旧Channel结束、旧票据拒绝 |
| SQLite规模维护拥塞及磁盘不足 | fail→pass | 11e/f/g保持通过；11h两后端功能与严格资源条件均通过 |

| 步骤 | 新run结果与耗时 |
| --- | --- |
| unit | 313个PASS测试事件；19.997s；完整cmd/internal覆盖95.9% |
| race | 313个PASS测试事件；31.643s；含上述Linux正负对照与等待取消/WAL写回归 |
| vectors | 264 PASS、5 SKIP、0 FAIL；196.787s；事件含父子项，不能当独立实验样本 |
| fuzz-frame / fuzz-control | 命令各60s；目标Elapsed60.06/60.07s，均有2 workers实际fuzz记录；步骤60.385/60.397s |
| vet / staticcheck v0.8.2 / govulncheck v1.9.0 | 全部退出0；1.443/15.702/6.445s；raw5714明确No vulnerabilities found |

八步均完成且无timeout，总耗时392,799ms。五个SKIP仍为预注册支持边界：B1-17/postgres、11f/postgres、B3-05/sqlite、B3-08/postgres及load=false的B4-06_Load；没有跳过必需G4检查。四条O3观察只涵盖两后端大流慢读与1000次无串流检查，不是G1完整1/8/32通道×1/64MiB负载矩阵。

O2/O4保留为小库回环环境观察，无新增性能判线：

| 链路 | SQLite | PostgreSQL |
| --- | --- | --- |
| 11a在线备份 | 10ms、73,728字节；23次并发写、0错误 | 156ms、12,248字节；90次并发写、0错误 |
| 11b恢复／resume至Open | 9ms／4ms；维护期间Open=0 | 160ms／4ms；维护期间Open=0 |
| 11d导出／导入 | SQLite→PG为6／34ms | PG→SQLite为24／8ms；两方向各16审计行 |
| 11e故障检测／写恢复 | 满盘1／333ms，不重启 | 只读8／319ms，不重启 |
| 11g最慢快速拒绝／撤销 | 1／1066ms | 2／1020ms |

11f/sqlite持有外部长读期间300次写，最慢2ms，撤销2ms，WAL 61,832→9,059,912字节；释放读事务后检查点与资源断言通过。11i两后端各中止两次导入并验证原子边界。以上不构成恢复SLO、容量、实际磁盘故障覆盖或RTO/RPO保证。

## C48与项目闭环

**C48 / 实现级限定支持：在`2ec3670`的正式G4 run中，原四项criteria、13向量/75必需检查及全质量通过；Linux正负对照区分旧动态读池的额外主库FD与固定单reader的精确归位，11h两后端及全部已发资源记录满足原固定基线条件。** E70/EDGE-041承接本新增来源。它支持D18在该版本、引擎及回环环境内的修正，不识别旧raw缺失的FD身份，不证明任意负载/平台或长时稳定性。单reader也有可用性代价：长LoadSnapshot/ListAudit占用读连接超过STORE_PROBE_MS时，Health可能因等待连接而context超时，依B3§6触发StoreUnavailable；这不等于数据库物理损坏，也不能写成修正完全不影响可用性。实际负载下的等待、探测与业务余量交G5另测。

本报告与[ITER-007](../../experiments/iterations/ITER-007.json)纳入两次新G4输入，completion仍为pending-publication。C46/E68/EDGE-039原失败与C48/E70/EDGE-041新主证据同时保留；ITER-005及所有原run不改。KB、门槛、任务一致性、图06及产品入口由本轮主线联动；Issue #3/#4/#11/#18仍须追加阶段并保真回读，不能预造评论ID或完成事实。

当前G4应为Inconclusive，准确原因是直接证据已通过而发布闭环尚未完成；G1同源复证与闭环见ITER-006。只有分析/独立评审、KB/图文/Issue回读、门槛一致性及最终CI完成，才按实际状态恢复并合入alpha。任何再次影响适用性的产品改动或正式失败都须重新评估并保留新证据；不凭本次通过抹掉历史反例。

B1/B2/B3/B4采用契约、原门槛和预注册判线保持。G2/G3可继续其独立准备，正式入口随实际门槛恢复；G5真实1C1G与G7真实应用兼容各有自己的资源和验收条件，G6仅属HA，本轮不代替这些验证。
