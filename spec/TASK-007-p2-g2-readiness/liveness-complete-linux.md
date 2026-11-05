# 同源完整活性开发检查计划

状态：先行计划在283a769固定，随后明确指定2a81a22424ff3e717d923c1532062b5826c6aa9d，已经单次执行、完整回收和独立核验；整体开发结果FAIL，详见文末与[liveness-complete-linux.json](liveness-complete-linux.json)。以下计划保留当时的范围和原预算。此为development，不创建正式manifest/run/ITER，不含真实clock、fuzz-state或fixtures，不宣称完整G2验收。

## 固定范围和执行顺序

所有步骤使用同一最终Git提交、同一专用VM/boot、既有PG18.6与32MiB小盘，顺序执行。根完整质量不再用原三个包467项代替；普通非零保留后，只有确认当前child退出、组空、来源/原期限完整时才能继续下一独立步骤。超时、来源漂移或未知进程身份立即停止，不重试。

|顺序|范围|原工具预算秒|
|---|---|---:|
|1–2|完整`./cmd/... ./internal/...` unit atomic覆盖、race，原`-p=1`|1800 / 2400|
|3–5|根`go vet ./...`、固定staticcheck、固定govulncheck显式同库|900 / 1200 / 1200|
|6–7|原frame/control各真实`-fuzztime=60s`|各900|
|8–12|完整嵌套unit/race/vet/staticcheck/query，由最终固定`dependency_quality.commands`给出原argv/env|300 / 600 / 120 / 300 / 120|
|13|新旧G2 helpers精确111节点|240|
|14|双后端Load完整12组合、164flow、请求5330MiB|5700|
|15|G2-11完整双后端六叶|1200|
|16|G2-12两个根双后端四叶，包括原PG实体失败|600|

父guest总绝对D固定9000s，host多30s只供等待退出及保全，不给guest补时间。后四integration沿已有5700s共享vectors D，各child再与本表自身预算和父D取最小。source前后哈希、输入检查、建进程及写结果都消耗原D；各step不能保证同时用满，不能把预算相加重开窗口。保持独立Go child，不为共构建丢失各child截止。TestMain现总会buildAll，OI_IT_BIN只指定输出目录；保留真实构建成本及每child独立bin/it。

根unit/race沿当前runner命令，不新增Go watchdog；integration沿原`-timeout 90m`。嵌套必须明确使用已采用正式adapter的最终命令（Go默认watchdog），不得把它伪称为旧foundation的Go120s开发命令重跑或把旧120s失败改为通过。最终helper尚未固定，prepare必须核命令与resolver规格，不凭现在猜测封包。

## 同源、依赖和环境

准备器只从root显式指定的40hex Git提交建立全仓archive，每blob按Git对象与SHA256核验；guest全部Git文件集合和字节前后核对。73项补丁来源封套（72个独立模块文件与根go.mod）另由同提交`source_fact`取得前后exact。所有工具模块从固定Git字节加载；宿主driver与helper另保存快照/哈希。

dependency声明用最终固定`expected_spec()`合成仅内部开发case并保存，不构造正式manifest或伪造local attestation。两依赖测试仅子进程使用已登记`CGO_ENABLED=1`、`GODEBUG=netdns=cgo+1`、`RES_OPTIONS=attempts:1 timeout:1`；根、其他工具及主机不继承。完整`./...`、原上游及自有测试不可过滤，实际cgo选择日志与允许skip仍由最终validator核验。原解析器候选完整TestBadDials的窄通过不能替代本轮完整嵌套质量。

漏洞输入沿既有官方快照：`coverage/g2-local-vulndb-01/snapshot-spec.json`及`coverage/formal-environment-preparation-01/handoff.json`，ZIP SHA256 `7e893ac89f6b5d7e8d3431ee7cddabc358cea99c8822d3ac545882c38af26c3e`。不下载新库。根scan与query同一file URI，每次原call之前/之后由固定模块核全目录与snapshot/provenance，保存真实before/start/end/after；中途失败不补造after。原ZIP/provenance及两个profile保全，query JSON不因exit0直接通过。环境文件路径与哈希复用现有输入记录，日志只保存PG“已配置”布尔，不输出DSN/token。

## 原件和独立复算

复用foundation及post-seal驱动的来源校验、真实Popen/PID/startticks/PGID/SID、host HANDLE、单次执行、信号/清理和不可覆盖原件机制；无VM.stop、PG重启、改钟、业务重试。完整原stdout/stderr、command/env/绝对D、group gone、childlogs及source before/after必须回收。归档上限沿既有有界规则，达到上限必须保留失败，不截断冒充完整。

根/依赖profile按真实语句计数，合并分母包含全部根产品与自有补丁12句，JS1保守未覆盖；不以局部或四舍五入文本宣称95%。完整root unit/race按最终库存，dependency按最终helper核全部包/本地34节点/允许skip；Load按12组合164worker及实际字节/终态/资源，G2-11按最终export-first严格parser的六叶全历史，G2-12按四叶及窗口前后完整观察。全部新结果独立于旧失败，缺项/失败/超时不得拼旧结果。

## 准备阶段验证和待固定项

目前只允许Python纯mock与AST/字节检查，不运行Go/SSH/deploy。测试须覆盖父D不重起、vectors共享D、验证和结果写回耗时、非零正常退出仍保留、超时只清理已核身份、源码/DB漂移、漏原件、结束后源回读、query截断/OSV以及子环境不外溢。最终固定SHA、全根测试库存、helper111清单与实际VM活态由后续root授权时确认；旧记录只是输入引用，不冒充当前在线核验。

## 单次执行后的完整开发结果

固定2a81树6412bbe的16步全部结束，guest耗时2051.1008799秒，实际exit1；unit、G2-11、G2-12退出1，其余退出0。全部步骤无监督错误或超时，原9000s/5700s及各child截止保持；857份Git源码前后精确，322份带哈希回收原件共240431997字节逐项一致，runner及16个child身份/进程组均已Gone。Host三次SSH分别0/1/0，使用同一VM身份，没有VM/PG重启或改钟。运行前没有另做独立空闲检查；执行中的状态读取不冒称执行前空闲凭据。

根unit有1915个具名PASS、TLS关闭子项及父项FAIL；race完整1917项PASS，无测试skip。根vet/staticcheck/govulncheck及两个原60秒fuzz通过，frame/control分别217503/100679次；control末段原0/sec记录保留，不能称持续有进展。完整依赖unit/race各281项PASS、2个原条件SKIP，包级TestMain通过，实际cgo日志及完整10包集已核；依赖vet/staticcheck、同一官方库的版本query通过。原183b失败仍保留，不以新环境结果重写旧样本。根7707/8119，加自有依赖11/12，完整7718/8131=94.92067396%，低于95%。

Load严格独立复算通过：双后端12格164条实际连接全部完成5330MiB，目标tuple、两端通道、字节和FIN/Completed终态相符，12次回收各三个样本均在原2秒内，FD/goroutine差为零。164个worker记录的CloseError均为已关闭连接错误，原夹具将其与DATA/FIN FirstError分开保存；没有删除这些原错误或称所有操作均nil。只支持这一次开发Load，不等于全产品质量或门槛通过。

G2-11六叶均在准备阶段失败，没有完整竞争cycle或60秒观察。两叶耗尽原3秒preload；四叶在exporter输入后未能于原连接+1秒与3秒较早截止内同时满足full和young200。后四叶失败时3秒仍有约765–1370ms，不能统一写成3秒耗尽。丢弃额度、探针P2/Q4及dirty/query轮换的真实相位继续诊断，不增加32Drop、不延长期限、不用末尾wire回填旧见证。

G2-12 SlowReaders两后端共80轮严格业务及资源通过，均在主动close前取得两次失败累计差。Entity SQLite本叶32实体、业务和资源通过，但必需父项因PG失败，严格组结果仍FAIL。PG第5实体token_create实际3.6729ms即遭503，先前upsert_registry已在原500ms预算下耗504.8752ms并触发fail-closed；没有队列/SQL/锁/I/O分段，不把token SQL、CPU或某条SQL断言为唯一原因。七个失败叶缺完整资源检查，不等同于已证明泄漏。

执行期间Windows两次有界wsio检查分别与Load/G2-12重叠，GOMAXPROCS2/-p1；没有宿主CPU/调度曲线，不能量化干扰或据此归因。第三次关闭测试在Linux结束后完成，[独立新测试记录](liveness-close-tests-local.json)绑定后续38f22cf，不拼入2a81的完整覆盖率。下一步先解决准备输入与registry写路径的具体缺陷/成本，再以同一新固定来源完整复证。正式19个run与14个ITER、G1 Failed/G2 Blocked/G3及G4 Inconclusive、alpha/main保持；本记录不新增正式样本。
