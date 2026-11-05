# 原生 beforeWrite 开发结果

固定被测 `7b5f7503dc67e084a941b15de7f40c2cf2fbde58`，tree `8eba2b6df2cda5980ea9c8374ad6400c3a78cb9d`；复用先行规格 `814c1d4754a844797322c3722ad2052a01233eb0`。独立 `git diff 3be014afecd6b0ca78cc22b1501cc1488d213fd1 7b5f7503dc67e084a941b15de7f40c2cf2fbde58 -- cmd internal third_party go.mod go.sum` 为空，产品源码保持3be。**实际pipeline/host execute均exit1；G2-11六叶0PASS/6FAIL，全部失败于cycle0，三父也FAIL。不是正式门槛实验。**

7阶段顺序为integration预编译、ois/oic/oi构建、build-info、helpers、G211；退出0/0/0/0/0/0/1。257 helpers（37根/220子）实际唯一PASS，无fail/skip；它们不能覆盖六业务叶失败。pipeline189.075211071s。

| backend/round | Go | 失败cycle | 原始首错事实 | raw行 |
|---|---|---:|---|---:|
| sqlite/0 | FAIL | 0 | critical后实收200，所需429不成立 | 99 |
| sqlite/1 | FAIL | 0 | critical后实收200，所需429不成立 | 514 |
| sqlite/2 | FAIL | 0 | 等待首个真实200响应头时EOF | 864 |
| postgres/0 | FAIL | 0 | 等待首个真实200响应头时EOF | 1317 |
| postgres/1 | FAIL | 0 | 原输入预算已耗尽 | 1775 |
| postgres/2 | FAIL | 0 | beforeWrite Full采样遇原context deadline | 2358 |

没有完成cycle或60s业务窗口；失败叶没有完成的原B4资源检查。外层进程回收通过不能倒填资源通过，也不能仅因未形成check断言泄漏。固定runtime最终独立复算已完成：完整expected_spec保持、六strict组仍FAIL、六份provenance PASS；各叶资源因GoFAIL与check0<1仍FAIL。独立consistency最终交接与runtime数值一致；不重新定义判线。

独立复算确认6份preload、5份accepted beforeWrite见证局部有效，PG2未接受保持null；SQLite0/1的final与critical query也有效，但原Stats两角色Active均0、post-critical实收200。selected429→final为295.071/296.237ms，critical仅18.916/19.298ms，不能判成控制500ms超限。

SQLite2/PG0在connect后585.860/600.913ms才释放GET，首次响应头均EOF；固定OIS的读头/读写预算为200ms，存在具体相容机制，但没有服务端逐socket关闭原因，不能确证唯一根因。OIC读头为5s，不能外推该OIS解释至PG1。PG1的16次final Full全部young拒绝，首拒年龄206.777–344.544ms；PG2的36次beforeWrite Full同样拒绝，首拒200.052–383.132ms，最后实际dialCtx截止。

原始分析01误拒18条既有diagnostic_only FD附属行、health_index一次误传B4/FD集合均已原样保留；02按真实接口校正后通过来源/health检查，Go与strict失败保持。这些是独立分析调用错误，不是产品来源失败。runtime最终summary/report分别SHA `3ef15f94ea37fe4b56cf949f2ec88cc1e24fabfb302819a97c5750fea6afc309` / `21ec6fcfa19e143d4752e874ff710dfed4c1d170015e194a8295ae4e71608f40`。

927源逐Git blob/SHA清单核对；准备70文件及最终164文件另加manifest回读精确。最终原件183657076B，tar183838720B，SHA256 `f073efddf5716cb198054b2adb179e7f1f364439abe1df7521a24a0756df791c`。G211 raw3523385B，SHA256 `2d84c02cba67c0e6d9a6766ebe2c6a5b793d0746ad5863a59e6309b9b1a7d450`。只引用摘要/哈希，不复制大原件。

同boot，旧9身份已fresh核Gone；新依赖树为独立复制，私有PG新data/credential。runner3955057仅原group=False身份Gone，7child组退出及collect均Gone/empty。PG3954868/start2006871245精确fast stop、组空，0.152157158s；host stop开始至collect写返回16.6105373s。原parent9000/host9030、首传输1800、装配600、预构建共享900、helpers240/G2111200、integration共享5700、stop+collect共享300秒均未续期，所有wait/source后验/结果写边界不超原D，监督errors[]。来源与生命周期通过不是产品通过。

独立分析入口01/02最初误把本地plan与bootstrap新增字段/排序后的plan直接等同，03误假设全部binary均CGO1；已按固定驱动实形纠正（integration1、三cmd0），保留原失败脚本与记录，无原件或判线改动。

下一最小候选把原末八Set/Full准备移到尚未dial之前，受原prepare3s约束，并把前置全8query年龄收紧为80ms，再执行原两socket与后置final200/critical300。必须明确这撤掉了旧前置等待的dial1s交集；原helper1s仍从首次实际dial入口建立并夹prepareD，不能续期或重试socket。实际SQLite0/1从release到selected429约100ms，原前置157.754/159.856ms批次到该时刻已258.939/261.228ms；80ms仅为更严格准备输入留余量，不能保证后续资格、Full或export重叠。新3s内也可能始终无Full80，尤其PG2现有数据不能证明它会成功。该新策略尚未固定或实现，需下一先行规格、反例和同源完整六叶验证。

本轮保留原GET→DELETE、16Set/32Drop、P2Q4、首头/Active/连续429、独立final200/critical300、真实末Set起完整60s及资源2s。不将timeout合计对应某个请求；429路径也计同类写失败。缺乏服务端逐socket关闭事实时，不把EOF解释说成唯一根因。

G1 Failed、G2 Blocked、G3/G4 Inconclusive保持；19旧run/14ITER、alpha/main不动。没有完整质量/95%覆盖、正式复证、clock、G5/G7或环境唯一归因。产品及共同契约/支持范围未变，图06和KB稳定结论无需改动；开发反例与下一计划由本规格回收，不新建formal/ITER。完整来源见[结果JSON](native-before-write-result.json)，大原件留在其明确标注的本地ignored路径，不冒称已入库正式证据。已授权首版开发继续，阶段发布后按固定新规格推进。
