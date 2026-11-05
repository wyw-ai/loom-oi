# 同源完整活性开发检查计划

状态：仅host准备；最终源码尚待root指定，未打包、未部署、未执行。不得使用当前dirty字节、自动选HEAD或沿用旧183b/49b9来源。此为development，不创建正式manifest/run/ITER，不含真实clock、fuzz-state或fixtures，不宣称完整G2验收。

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
