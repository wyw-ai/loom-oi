# 原生 fresh-query 开发结果

固定被测 `33f436a3eb6e3365067f6ba1c9a34dc8092b3de0`，先行规格 `514a9170e59f9106dbb8a6812790e0cb7463e0fa`；tree `99180d1febc574e1dec92bf76aa79e2066287540`。独立 Git 对照 `3be014af… → 33f436a3… -- cmd internal third_party go.mod go.sum` 为空，产品源码不变。**实际 host/pipeline exit1：六叶2 PASS、4 FAIL，三个父与包FAIL；不是正式门槛通过。**

七阶段依次为 integration 预编译、ois/oic/oi 构建、build-info、helpers、G211，退出 `0/0/0/0/0/0/1`。pipeline 318.944095239s，G211 阶段143.816510748s。277 helpers（41根/236子，16文件完整源闭包）唯一run/唯一PASS，零FAIL/SKIP。

| backend/round | Go | 独立逐叶行为 / B4 | 完成cycles | 实际慢输入窗口 | 原始结果 |
|---|---|---|---:|---|---|
| sqlite/0 | PASS | PASS / PASS | 117 | 60.235744439s | 完整逐叶行为及原B4核验通过 |
| sqlite/1 | FAIL | FAIL / 无完成检查（复算fail） | 0 | — | pre_dial_full 原3s到期；未创建 exporter 慢socket |
| sqlite/2 | FAIL | FAIL / 无完成检查（复算fail） | 0 | — | pre_dial_full 原3s到期；未创建 exporter 慢socket |
| postgres/0 | PASS | PASS / PASS | 116 | 60.061888856s | 完整逐叶行为及原B4核验通过 |
| postgres/1 | FAIL | FAIL / 无完成检查（复算fail） | 0 | — | pre_dial_full 原3s到期；未创建 exporter 慢socket |
| postgres/2 | FAIL | FAIL / 无完成检查（复算fail） | 0 | — | pre_dial_full 原3s到期；未创建 exporter 慢socket |

两个 round0 合计233 cycles；实际末Set起完整慢输入窗口分别60.235744439s、60.061888856s。其完整8query前置最大年龄6.693224/59.390687ms，后置final为107.766994/163.485973ms；critical为110.634729/168.921414ms。三个阶段都以各自实际采样独立验证，原200/300ms约束未替换。B4各1次check、3次连续样本，清理完成124.591747/127.029944ms，均在原2s内。这些是逐叶的实际结果，不能覆盖父FAIL。

四失败均有原始 `competition_failed_preparation / pre_dial_full`，accepted为空、后置full/cycle/complete未发生；固定源码在其后才进入socket helper，因此不是已连接后EOF。其Full成立但young80未通过的采样次数分别136、129、129、133，不能将短路/部分投影充作完整8条最大年龄。缺失的B4完成check不记通过，也不据此单独断言泄漏。

当前同源 runtime 复算保持完整 expected_spec：**六个strict组均FAIL**；两成功叶仍被真实父FAIL阻止组通过，四失败叶本身FAIL。其余102个已登记组未执行，完整注册套件仍FAIL，未裁范围形成整个G2 PASS。`runtime-analysis-01` 已冻结，其handoff及全部引用文件逐hash核对；独立因果复审也已冻结并核对；本轮不预定原样重跑。

独立诊断对527次拒绝核得：首个遇到的年龄均超过80ms；已检查前缀的其他谓词合法，不伪造短路后的其余query。再用完整wire构造有意更宽的必要区间 `[WriteStartedNS, ReceivedNS+80ms]`，忽略写完成、可见性、pending及最新八条限制；在四叶首次至末次真实Full**采样时刻**之间，fresh distinct来源上界分别只有4/5/4/7。因此这段内仅加快Stats不能创造八个新鲜来源；不声称该连续时间段内Full始终成立。

原3s的初始与晚段仍有年轻历史query候选，实际邻近Stats均不Full，且初始候选中已有正常返回的Snapshot，晚段位于32Drop耗尽之后。年轻历史query不等于当前pending；相邻采样间的短暂联合机会仍未知，不能断言全3s数学上没有机会。固定OIS按空槽和各自refresh/期限补query，OIC查询读取既有Snapshot；真实P2/Q4本身不承诺八条query同刻产生。这与相位分离相容，不证明产品违反500ms、公平性或其他契约。

两个成功叶的连接到两首真实200完成分别3.271928/3.032010ms；全部233cycle均核原helper起点/D，466个首头真实200。cycle0的实际OIS Active2及post429仍成立，后来cycle不把未执行的post零值写成429，也不把首次Full外推为整个60秒满载。新时序没有在这两叶复现旧连接后等待Full形态，不能由一次新旧对照宣称唯一因果。

下一步保留四反例，以同一真实pending query/snapshot批次与Full交集为输入边界审查具体准备机制；先检查原3s、32Drop、16Set、P2Q4、80/200/300/500ms下的可行时序，再固定任何新规格与代码。单纯重跑、加快轮询、给旧query续Received/期限或拼接异时样本都不能作为修复。目前没有已验证能保证六叶成功的新调度，本轮不以放宽条件制造通过。

准备及生命周期独立核验2128＋296项通过。942源码逐Git blob/SHA集合exact；最终164文件另加manifest，原件256035598B，tar256215040B，SHA256 `05e65e9ed95d231e6eff8965d20130188d3f88a0c24b572821c381a8ff22528d`。G211原GoJSON为70117596B，SHA256 `2bf6a3b11a913d098ea64b38818d38b6f134805190ec07d9d74dcd9f7c7dc44f`。只保留摘要和hash，不嵌大事件。

同boot的两旧根18实际身份已核Gone；新runner按原identity-only检查Gone，7child原退出与最终collect均组空。私有PG同一prepared身份fast stop、组空，0.148064887s，最终日志保全。原parent9000/host9030、传输1800、装配600、预构建900、helpers240/G2111200、共享integration5700及stop/collect300s没有续期；wait/source/结果写边界均在原D内，监督errors为空。来源与回收通过不替代业务通过。

本轮另补跑完整Python检查，actual exit0：experiments451＝441PASS＋10skip，宿主76.4180688s；tools39PASS。原件与显式三文件前后hash/固定Git一致；非verbose原日志不独立列举每个skip名称；另只读发现清单可定位10个Windows缺失Linux/root、/proc或POSIX条件的用例，不冒称它们执行过。它们不是本轮完整Go质量或95%分母通过。首次本地binder误把10个改动文件当完整16文件闭包，在远端前拒绝，原件保留，不算业务失败。

G1 Failed、G2 Blocked、G3/G4 Inconclusive保持；19旧run/14ITER、alpha/main不动。没有新formal/ITER、门槛晋级、clock/G5/G7结论或产品变更。主要原件hash及范围见[结果JSON](native-fresh-query-result.json)。大原件保存在明确的本地ignored路径，不冒称正式证据已入库；本次结果仍待Issue只追加发布回读。已授权的首版开发继续。
