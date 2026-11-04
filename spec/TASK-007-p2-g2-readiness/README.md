# TASK-007：P2/G2 观测、准入与导出

**入口现状**：本工作区已通过 `ae2c375023466c80f6ba98f75b579612428e5aa4` 合入并远端回读 alpha `570b573f8151466fd0ad42b4cdf94ac3c81d323f`。G2 产品核心已实现，真实双后端向量正在开发验证，尚无正式 G2 manifest/run。因本任务生产代码和模块图变化，当前 G1/G4 适用性为 Inconclusive，历史通过原件保留；实现中间提交不等于交付。

状态：用户已准确采用 Commit-Snapshot、Probe-HTTP、Export-Pull，DISC-009-R1/R2/R3 均 Accepted；[唯一原始确认](approval.json)绑定已发布固定规格，解释见[ADR-005](../../docs/decisions/ADR-005-g2-adoption.md)。进入实施时 G1/G4 前置已恢复；现需对最终 G2 源码完整复证，尚未预注册或正式运行 G2。

## 基线、授权与唯一规格

- 分支：`feature/TASK-007-p2-g2-readiness`；原规格基线为 `b50c08e228aa1c01686b35a59421ad2670154219`。本次从已发布 G2 提交 `9aa01b7c67d93eb0a76f66f7f788e9a7f689fc78` 合入实际 `origin/alpha` 的 `570b573f8151466fd0ad42b4cdf94ac3c81d323f`，已包含 TASK-005 最终实现；合并已由 ae2c375 提交、推送并回读，不改原规格来源版本。
- 原始任务来自本会话用户2026-11-04的交接文本，[原话与SHA-256](../../docs/changes/DISC-009.md)可追溯；要求G2先收束具体规格并提交新增取舍供确认。本任务先完成待确认规格包及来源/范围/索引。G3 按已采用规格先行，纯设计不等待 G4；产品实施前需核对实际采用、G1 及影响到的回归条件。
- **唯一详细契约**：[G2 观测规格](../../docs/architecture/p2/g2-observation.md)。本任务不复制字段、算法和预算；准确来源及差异在 [DISC-009](../../docs/changes/DISC-009.md)，机器范围见 [JSON](../../docs/changes/DISC-009.json)。
- 已采用的 C1、B1/B2/B4、95%有效单测和无业务重放保持；[ADR-003](../../docs/decisions/ADR-003-first-release-boundaries.md)、[ADR-004](../../docs/decisions/ADR-004-p1-adoption.md) 不改。候选配置/预算不是现有生产 SLO。

## 已采用的三组新增决定

| 需求 | 推荐及需要接受的代价 | 实质替代 | 准确规格 |
| --- | --- | --- | --- |
| DISC-009-R1 | **Commit-Snapshot**：以 OIC 最终检查固定本次观测决定，OIS 保留授权与 Channel 提交权。接受检查后至 READY 的新健康变化窗口；观测配置本地取消若先发生，保证 OIC 零载荷，但消费者可能先 READY 后 ABORT。新增控制 v2、错误传播及兼容范围 | **Commit-Strict**：若不接受窗口，继续设计更强语义；加查询/屏障不等于已有可行协议，此时只暂缓依赖该选择的实施 | [消息](../../docs/architecture/p2/g2-observation.md#wire)、[最终复核](../../docs/architecture/p2/g2-observation.md#commit) |
| DISC-009-R2 | **Probe-HTTP**：单一、本地允许的 HTTP(S) 健康状态检查；独立 observation_revision；probe 改动失效样本/check/Preparing，已 Open 长流保持。首版不覆盖所有非 HTTP/复杂鉴权健康接口 | **Probe-Report**：提供方主动报告，另定义认证写入口、来源/重放/冲突/失效；尚不能直接实现为替代 | [来源与代数](../../docs/architecture/p2/g2-observation.md#probe-http) |
| DISC-009-R3 | **Export-Pull**：独立有界的最新状态发布，默认关闭的回环指标拉取，有限标签；可合并中间事件、不保证完整历史。冻结候选负载/预算后预注册验证 | 主动导出需目的地/凭据/队列/重试契约；可靠历史需扩大记录责任；只做本地诊断则不能宣称满足包含 exporter 的 LAB-005 | [导出](../../docs/architecture/p2/g2-observation.md#export-pull)、[预算与验收](../../docs/architecture/p2/g2-observation.md#acceptance) |

用户已分别选择表中三个推荐，固定规格的字段/参数和代价一并采用；原问题和答案见approval.json，不再重问C1或同一三组选择。R1依赖R2，R3依赖R1/R2；采用记录仅解锁准确设计范围；采用时G1/G4历史前置已恢复；当前实现和依赖已变化，适用性为Inconclusive，仍须完成新源码复证。

## 后续实施与验证计划

| 阶段 | 输入/交付 | 当前状态 |
| --- | --- | --- |
| D0 规格准备 | 独立来源、三组选择、sdd-scope 指纹、既有正文仅加待确认导航 | 内容审核、提案发布回读和三组采用完成；采用回应I18-P04/I3-P06/I11-P06已发布并逐字节回读 |
| D1 准确采用 | 原始人类回复/明确指令，选项与范围；必要时修订候选后绑定指纹 | H16原始答复绑定六个原指纹，采用记录/兼容修复10文件远端回读完成；本工作区已集成alpha 570b573历史基线；后续实现变化后的G1/G4当前适用性为Inconclusive |
| M1 观测源与代数 | Probe-HTTP、独立观测版本、TTL/乱序/失效、受控配置 | 核心/配置/实际调度已实现，双后端代数与TTL开发向量通过；最终统一验证待完成 |
| M2 控制与最终复核 | v2、preflight/final、错误/幂等/回收、旧版矩阵、消费者展示 | 控制v2、最终固定点、安全复核、错误与幂等已实现；开发竞态通过，真实旧二进制兼容开发通过，最终统一Linux验证待完成 |
| M3 独立发布与导出 | 有界调度/控制公平、目录挑战查询、Export-Pull、有限指标 | 已实现；独立发布、16源调度和有限标签/慢scrape开发通过，控制竞争开发通过，正式统一Linux验证待完成 |
| M4 产品证据 | G2-01–14 与 TV-B1-19；两后端、G1适用回归、>=95%有效单测、race/静态/安全；manifest先于run | 未预注册、未运行 |
| M5 闭环 | raw复算、独立评审、文档代码一致性、综合分析、图/产品文档/Issue/ITER/gate 联动 | 未开始；不得用记录校验替代产品通过 |

本任务已进入产品实现和开发验证。没有新增 G2 正式 run 或 ITER；开发失败日志保留，不拼接多个窄跑为正式样本，不修改历史 G1/G4 验证对象或旧证据。

## 本轮检查点

- [x] 初建工作树时核对原规格基线，当时未引入 TASK-005；本次实际集成见下方检查点。
- [x] 独立观测版本保持 B2 target revision 及已 Open 长流边界。
- [x] A1 观测固定点、异步健康更新、配置取消与晚到 READY 的竞态写入具体契约。
- [x] 完成内容审核与相关Issue阶段发布：固定规格提交`deff207de157b1bf40130494d47a85c4e8bfacce`的16文件逐字节回读；[I18-P03](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624265781)、[I3-P05](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624268297)、[I11-P05](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624270544)发布后逐字节回读，旧正文/元数据/评论保持，详见[回执](../../docs/issue-history/2026-11-04/TASK-007-g2-proposal-published.json)。
- [x] 用户逐项准确采用三组新契约，完整问答、实际tool call/item来源及原规格指纹见[approval.json](approval.json)；不再重复确认。
- [x] 采用记录与D2兼容计划已由`bf51b38146e8dee48f47e742c4e5116e773ff0bf`提交、推送并回读22个变更文件，字节一致。
- [x] 固定提交`4f12fea156bfdb7c97ba3d3d9f4a91a2a8970a2a`的10个变更文件远端逐字节回读完成；[CI 41789106132](https://github.com/wyw-ai/loom-oi/actions/runs/41789106132)在本次回读时仍为in_progress，尚无成功结论。
- [x] 采用回应[I18-P04](https://github.com/wyw-ai/loom-oi/issues/18#issuecomment-6624784353)、[I3-P06](https://github.com/wyw-ai/loom-oi/issues/3#issuecomment-6624787385)、[I11-P06](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6624790959)已发布并逐字节回读；回执正文12个唯一链接核验通过，3单原正文/元数据及20条旧评论保持，详见[采用回应回执](../../docs/issue-history/2026-11-04/TASK-007-g2-adoption-published.json)。
- [x] TASK-005最终alpha已纳入本工作区；G1/G4通过仍绑定2ec3670及其正式run，P2入口恢复。G2实施已开始；最终产品链、正式预注册及运行尚未完成。

G5/G7真实资源由[#11 I11-P07](https://github.com/wyw-ai/loom-oi/issues/11#issuecomment-6626516421)继续承接。用户已提供SSH可达候选主机、允许随时使用，并明确经OIC暴露现有SSH/Bash能力；无需重复询问时段或首组任务。资源预算、原生身份与真实样本边界仍需独立预注册和验证：当前候选主机不等于整机1C1G，不能据此宣告G5/G7通过或扩大支持矩阵。仓库不记录主机地址、账号或凭据。

本轮自动记录检查与限制见 [DISC-009](../../docs/changes/DISC-009.md#validation)。

## D2：准确问答来源的记录兼容修复（文档先行）

发现时`tools/check_sdd.py`只接受GitHub原评论或带`chat-request` UUID的会话指令。本次真实采用来自`request_user_input_async`的用户逐项答复，只有实际tool call id和questionItemId，没有请求UUID。不得伪造UUID，也不得借AI发布评论替代用户来源；先提交、推送并回读本补充规格，再修改记录器。

修正保持schema 2、唯一`confirmation_ref`、需求指纹及旧确认路径：新增明确的`source.kind=user_response`，locator为`tool-call:<实际call id>`；approval的`items`保存原questionItemId、完整question、原answer及准确需求scope。检查器要求真实结构与非空问题/答案、非负整数item序号、item内call id与locator一致、item唯一、scope合法且当前需求由明确item覆盖；缺失items、仅有AI quote、重复item、错call或未覆盖需求都拒绝。工具只检查记录关联，不能自行鉴别人类身份、推断自由文本是否同意或取代原话核对。

确定性回归覆盖合法三项采用、错call/item结构、空问答、重复item、scope遗漏或重复及指纹变化；原`user_instruction`/`github_comment`行为与旧Accepted对象保持。验收包括tools完整自有源码的有效单测覆盖率≥95%（统计包含未执行源码）、全tools回归、所有适用文档/SDD/编码检查和CI。六个G2规格片段字节不变，Pending→Accepted只来自已收到的准确用户答复；记录器修复不是新产品选择，不创建run/ITER或放行G1/G2。文档先行提交`bf51b38`时仅完成本段计划及采用文档，tools代码尚未修改。

随后完成`user_response_errors`及确定性反例，并经独立只读复核。39项tools单测通过；以coverage.py 7.17.1执行`coverage run --source=tools --omit='tools/test_*.py' -m unittest discover -s tools -p 'test_*.py'`，完整两个非测试源码共384条语句、5条未覆盖，覆盖率98.70%（`check_sdd.py`98.18%、`check_structure.py`99.39%），没有排除未执行自有源码。实际DISC-009 review/implement（R3连同R1/R2依赖）及旧DISC-007-R4检查通过；这仅证明来源结构、范围与指纹关联。工具不能自行证明外部答复身份或解释答案是否同意，仍以完整原问答核对为依据。修复已随`4f12fea156bfdb7c97ba3d3d9f4a91a2a8970a2a`提交、推送并完成10文件远端回读；三条Issue采用回应也已回读。CI 41789106132在本次记录时仍运行，不能把本地检查或发布完成写成CI/产品通过。

## alpha 570b573 集成检查点

2026-11-04，按本轮明确授权在本feature执行fetch及不提交的merge：G2原HEAD为`9aa01b7c67d93eb0a76f66f7f788e9a7f689fc78`，实际`origin/alpha`为`570b573f8151466fd0ad42b4cdf94ac3c81d323f`。TASK-005的[CI 41802573081](https://github.com/wyw-ai/loom-oi/actions/runs/41802573081)六项成功及alpha快进已由主流程核验；main保持不变。本工作区解决10个文档冲突，保留两侧目录、谱系、发布回执及工具改动；目录登记为70项。合并已由`ae2c375023466c80f6ba98f75b579612428e5aa4`提交并推送；相对570b573的32个新增/改变文件已逐字节远端回读一致。

G1/G4完整validation、historical_validation和publication仍采用alpha原对象，主证据仍绑定`2ec36709471fe75c0e194c0ddf8f7699b7939fe2`；未把合并版本写入旧run。六个G2冻结片段、全部Accepted需求、approval和旧发布回执保持。冻结g2-commit中的原基线说明不修改；后续产品实施沿用TASK-005已有维护/存储拒绝新Open边界，接线与错误次序在实施一致性表核对，若需实质改变再确认准确差异。

P2入口检查已通过（退出0），P3仍仅因G2/G3未完成而阻挡（退出2）；T02/T03由资源前置Blocked改为Planned，V03产品验证保持Blocked。M1–M5仍按上表实际进度，不以本次文档/tools集成替代产品实现、预注册或运行。

实际集成检查：结构/链接/生成目录、DISC-001–009 review、DISC-009-R3及其依赖和DISC-007-R4 implement、39项tools、84项experiments回归、12个run/7个ITER完整性及物料检查均通过。两个最新G1/G4主run均以`verify.py --current-source`精确复算通过。111个产品文件、178个证据/图/归档文件与最终alpha字节一致；G1/G4两个完整gate对象、各116项source-manifest和9个validation绑定产物均一致。13个既有Accepted需求对象、六个G2片段、approval、两份G2回执和两份G2工具原文件保持；没有刷新Accepted。只重算实际变化的Pending指纹，所有受改文档为UTF-8无BOM、LF，`git diff --check`通过。这些检查不产生新产品样本，也不替代后续实现后的质量与门槛验证。

## 当前产品实施检查点

观测core、标准库metrics、OIC probe/config/reload、有界控制writer、OIS挑战查询/幂等/最终准入、v2协议、消费者错误与回环诊断已完成接线。独立审查修正绝对期限起点、晚到回包/旧binding、原首终因保留、过期timer使用实际当前时刻等边界；协议消息、默认控制预算和错误值只取六个已采用scope，不修改旧tunnel帧。开发检查不能代替最终Linux race或通过记录。

独立metrics解析器采用验证依赖[Prometheus common expfmt v0.73.1](https://pkg.go.dev/github.com/prometheus/common/expfmt)，生产导出器仍使用标准库。Go模块解析使x/text从0.41.0升至0.43.0；其影响进入最终源码的完整质量/双后端复证，不以测试依赖名义忽略实际模块图变化。当前G1/G4因此及产品语义改动退回Inconclusive，2ec3670历史validation、旧run/ITER和回执完整保留。当前工作分支未交付G2，未将G2未完成当通过。

用户对`call_f8848b5f1fc9465083fd35dba2f2bf2a`第0项的原答复为“没有，一样的1c1g的就用现在这个验证就行”。后续G5按明确指令使用现有SSH主机，不再等另一台整机1C1G；仍记录实测2逻辑CPU/约1.58GiB，应用配额与整机规格区分。其他性能检查不自动豁免，G5/G7没有通过事实。


### 真实向量与有效覆盖检查点（尚未正式预注册）

- 完整 `cmd/... internal/...` 含显式本机PG的最近开发单测覆盖6693/7043=95.0305%，19个PG leaf实际PASS，唯一平台专属FD项Windows跳过。后续握手诊断、控制统计等修改仍需最终重新统计完整分母；包级高覆盖不能代替总体95%。早期fake接口、非法fixture参数及端口耗尽反例均保留。
- G2-01准入矩阵、02失败/Running不替代样本、03乱序与binding、04保守TTL、05最终重查、06固定决定窗口/原样本到期、07安全复核、08四组取消/重复/幂等、09独立发布、10饱和公平、12导出器、14诊断分层均有真实SQLite/PG开发通过。确定性竞态使用每场景20轮；11控制竞争与13真实旧版组合也已开发复验；03控制竞争的输入装配失败被保留，04修复后窄跑通过。不同文件/轮次开发通过不拼成一次正式run。
- G2-09最终使用已采用500/200/50/1500 ms probe配置，以实际reload后的合法URL路径绑定同轮请求，20轮/后端均证明健康结果在另一目标响应完成前可见。`visible-fastHeaderStarted`包含HTTP首响应到归约的前段，是归约→目录可见的保守上界，不能冒称采到内部归约纳秒点。早期加大slow timeout及query-string URL的fixture失败/非适用通过不作最终证明。
- G2-10每后端3×60s，16源/P2/Q4、14持续200ms超时；六窗口与96目标窗口开发通过。调度等待从原定due时刻到实际开始计，timer晚回调不重置等待；观察max真实等待1747.853 ms、max pending age1695.145 ms，每目标窗口至少42次执行，仍需正式同源Linux验证2500ms界限。
- G2-12真实32目标地址/revision、32subject/令牌与channel每后端串行变化，固定Exposure以隔离无限tombstone范围；独立expfmt解析13families（7counter/6hist）和295 series，最大21245B，实体变化前后标签集合不增。每端20轮两socket不读scrape实际429，最终窄复验每轮主动Close前timeout确增2，不能用主动关闭造成write_error伪造导出超时。原始连接/写入/指标/业务时序完整保留。
- G2-13以固定旧alpha570b573真实git archive构建ois/oic/oi，记录PAX commit、逐源/binary哈希与go version -m；不以mock冒充旧产品。已发现newOIC遇旧OIS的400 VersionUnsupported时只显示通用WebSocket错误，窄修仅解析完整有界响应的公开错误码并输出固定提示，不打印peer任意消息；五组合/旧CLI与0205/0107、非法v2消息开发检查已通过。
- G2-04的ObservedAt修改证明元数据不能修改本地单调期限；没有执行操作系统墙钟跳变，报告必须区分这两层。代理所有Drop/Injected均在原始记录标明，透明转发保留真实有序wire；1.4MB级日志由既有Package/Test完整行重组解析，不截断后据父PASS补叶。

测试env新增默认nil的配置/二进制选择接缝，保留旧B1–B4默认装配。CI integration使用完整Git历史取得固定旧二进制源码，整套runner上限90m适应新增60s×3与20轮向量；这仅是测试进程总上限，产品500/2500ms、IDLE、资源和各场景超时不变。正式manifest/analyzer、完整一致性表、产品手册、统一Linux证据与最终六项CI仍待完成；TASK-006尚未集成alpha，后续必须纳入其最终CLI取消与诊断修复再固定最终G2源码。

### 完整质量与资源补正检查点

- 最终查询回收/握手/统计修改后的完整Windows开发单测为`coverage/g2-full-unit-03.jsonl/.out`：6744/7098条语句=95.01268%，1445个根/子测试通过、0失败；仅Linux专属SQLite描述符用例跳过，显式本机PG参与。`go vet ./...`通过。Windows本机`-race`因缺cgo编译器没有运行，不作通过证明；Linux CI的完整单测/竞态/安全与四平台构建仍须完成。
- G2 writer现在按queued query身份主动回收期限到达/取消的槽位；旧回调不能删除新owner，已选中/在写的项由writer释放。builder返回后再次检查原context，未开始socket写的取消不消耗seq、不关闭健康session。保留旧源overlay反例与新确定性门闩验证。
- 全部22根/130个Go叶子的资源归属已核对：128叶映射106个原始环境/profile；旧OIS拒绝新OIC的2叶只证明兼容拒绝、零通道与明确错误，不宣称在线双Access资源回收。G2首次基线要求Ready、正常暖机、探针active/queued为0且FD稳定；结束时从实际原始关闭/取消时点计同一个2s窗口，恢复受测健康端点与关闭测试HTTP空闲连接也消耗该窗口，保持产品probe任务/配置/进程在线，先保存资源再输出大段wire历史。Windows开发25个资源检查至少3样本，最大951ms；Linux真实FD尚待统一验证。
- G2-11最后一轮完整开发中的PG两个窗口没有构成同时2个真实阻塞scrape，判FAIL并保存。夹具改为等待两socket真实写完成、10ms轮询（仍同一原1s输入界限），两失败叶复验通过。不能将跨版本的4个旧成功叶+2个新成功叶拼为一次正式run；最终固定源码必须整跑。
- 产品README、配置/API参考、运维、模块与当前架构状态已更新，所有已采用规格片段保持原字节。正式manifest/原始行为分析器与一致性评审仍在准备；没有新增run/ITER或通过记录。

静态检查纠正未使用变量/旧兼容包装及测试HTTP状态常量后，发现旧expiry测试用Table.Snapshot主动推进归约却丢弃Effect，抢在真实timer前消费过期事件；完整单测04因此失败。测试改为先被动等待实际Changed通知再query核验，10轮窄复验及完整05均通过（6746/7097=95.05425%）。失败原件保留，没有放宽期限或改产品expiry。staticcheck v0.8.2、govulncheck v1.9.0和84项现有实验Python回归均通过；正式新G2分析器仍在开发，未混入此次结果。

### D3：真实墙钟跳变的隔离验证（文档先行）

现有G2-04只改observed_at展示元数据，未覆盖已采用§9.2的真实墙钟跳变。追加专用GitHub hosted `ubuntu-24.04` job：使用该job独享的一次性VM，先准备工具链、PostgreSQL18.6、产品/测试二进制及覆盖±30s的测试证书，然后在该VM关闭实际时间同步并执行真实CLOCK_REALTIME跳变。依据[GitHub官方隔离说明](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners)，普通hosted runner每job新VM，区别于single-CPU容器或self-hosted；不在本机或用户SSH主机改时钟，不引入QEMU性能层。

测试仪器为受限特权clock_guard，业务/测试进程仍用runner UID/GID。误操作门闩要求OI_IT_CLOCK=1、GITHUB_ACTIONS=true、RUNNER_ENVIRONMENT=github-hosted与本job随机nonce；这些环境值不代替固定workflow来源的隔离依据。仅通过本地Unix socket接受顺序化round_id的±30s Shift及Restore，不能传任意绝对时间；实际调用clock_settime_ns。guard保存原REALTIME/MONOTONIC锚与实际NTP单元状态，各轮按原REALTIME+当前MONOTONIC经过量恢复，最后恢复原时间同步状态。finally、信号与父进程失联watchdog及workflow always恢复/上传共同兜底，不能声称SIGKILL后finally仍执行；VM销毁是最后隔离边界。

最小场景为Ready、NotReady、preflight在途、final在途、原总准备D五组，每组±30s×SQLite/PG共20叶、每叶20轮。样本组形成真实样本后阻止新明确样本，在query往返期间跳钟，核原剩余期内状态保持、按原单调TTL转Unknown且真实OIC样本也到期。检查组hold真实回应跨跳钟，仍按500ms检查预算准确timeout，迟到positive不能产生READY/载荷；总D组使final开始于原2s预算尾段，D先于新CHECK到期时仍PrepareTimeout，不能重给2s。原许可/授权、完整绑定与回收规则保持，测试token/证书预先覆盖偏移，避免把本有墙钟语义的凭证期限混为观测TTL。

原始证据同时保存guard与独立测试进程的REALTIME/MONOTONIC前后值、实际delta=(ΔRT−ΔMONO)、每段offset见证、round身份、guard/产品PID、完整wire/query/check与样本身份、终态/零载荷、恢复与NTP状态。时钟施加/恢复精度预设±100ms，仅是仪器容差，不改变500ms/2s/TTL产品判线。未实际跳变、漂移、恢复失败或身份变化均fail；资源窗口从原终态开始，恢复和清理也计入原2s。现有130叶仍单独验证；新增专门clock叶及源码/helper/workflow指纹纳入正式manifest/分析器，不能以元数据测试或普通Go PASS代替。

这属于已采用验收的缺口补齐，没有新产品决定。此说明先提交、推送和回读，再开始clock仪器/向量编码；当前尚无真实跳钟运行。


### 独立审查与最新完整质量（固定804f703之后的开发检查）

固定804f703的[Linux CI 41833218575](https://github.com/wyw-ai/loom-oi/actions/runs/41833218575)完整unit/race已通过，下载的unit.out逐块统计为6776/7124=95.11510%；旧frame/control两项主动fuzz通过。build-lint的四平台构建完成，但gofmt步骤失败；三个文件格式已修，仍需新源码CI重新确认。Python/Windows门槛失败明确来自G1当前Inconclusive，不抹除或跳过门槛。integration仍在运行，此时不宣称整套CI通过。

独立产品审查复现CHECK响应已成功但createPrepared执行时检查期限已过的边界：旧源overlay在恰好期限及期限后两个反例失败。修复在保留原授权/总D优先级后、分配任何Channel/票据/配额前再次检查同一个CHECK期限；v1的nil检查保持。新两根六子场景20轮、双后端relay/server受影响回归与vet通过；包级96.0%不代替修改后的完整分母重测。

新FuzzObservationState以公共归约操作序列覆盖Begin/Finish、明确样本与超时、重配置/reset/remove、过期/旧Work及容量不变量。种子检查与60s主动开发fuzz通过，1,003,376次执行；CI增加同目标60s步骤。Windows本机race因无cgo编译器仍未执行，不能用Linux旧提交的race结果覆盖这些新修改。

原始分析器核查补足08d异凭证实际attach HTTP401/TicketInvalid记录（有限凭证别名，不含token），20轮两后端开发通过。07将凭证DELETE与策略PATCH的实际管理响应、起止时间区分记录；14保留实际请求/部分响应bytes与本次目标连接记录。它们不改变产品或验收边界，新的定向复验与严格raw对账进行中；旧日志不足不靠Go PASS补造字段。


该检查点随后完成的完整开发单测06为6750/7099=95.08381%，1458个根/子场景PASS、0FAIL、1个Linux专属FD项Windows跳过；显式PG参与。定向raw-completion-01的05六叶、07十叶、14两叶均通过，08d最新两叶也经独立原始行为复算通过。tools 39项和experiments 129项、全部9份SDD review、B1–B4 implement、结构/历史迭代/物料检查通过；P2仍按当前G1 Inconclusive阻挡，未改记录或绕过检查。

D3仪器已实现13组不调用真实系统钟的模拟验证及Windows/Linux交叉编译；独立审查补正进程组清理失败仍恢复、同boot/job/source恢复锚、审计/独立单调见证和样本过早到期反例。当前没有真实跳钟通过事实。固定开发源码后通过clock分支在专用一次性VM早检；最终正式G2将以同一固定源码、同一专用job运行原普通向量及后置clock步骤，完整原始test2json/guard审计进入同一run复算，不用外部开发run拼接通过。原500ms CHECK、2s总准备/回收、TTL及各性能判线保持。

此检查点最后一次vet、staticcheck v0.8.2及govulncheck v1.9.0均通过；新增文件均经UTF-8/LF/no-BOM及diff检查。上述是开发检查点，不替代待运行的真实clock或最终整轮Linux证据。

D3最终审查还验证clock恢复失败仍尝试恢复NTP、审计写失败不截断恢复、真实product PID/epoch/generation跨轮不变；最后experiments全130项通过。正式clock行为分析组件仍未完成，不用已有scope核验代替期限语义复算。


D3的CHECK/总D判定沿冻结§6的“截止后不得提交”语义：在真实check携带的剩余期及观察到该消息的单调接收时间导出的保守过期上界后，保持墙钟偏移并释放真实positive，核验timeout/零READY与零载荷。毫秒向下取整的上界补1ms仅恢复编码精度，不是产品时间宽限。HTTP/ABORT到达观察器涉及写入及调度，既有helper总上限只是仪器退出界限，不能当成500ms/2s已经放宽，也不额外发明远端通知SLO。实际release/wire/原截止字段进入严格分析；13组恢复/来源仪器检查不代替该语义核验。


D3首次真实workflow [41840152659](https://github.com/wyw-ai/loom-oi/actions/runs/41840152659)固定8c2b297在启动测试进程前失败：runtime mask实际chrony/chronyd服务后，inventory只接受loaded而拒绝masked。原始审计没有child_started或shift，实际0轮；finally恢复与always恢复完成，NTP前后状态精确相同，锚残差约-0.017ms。保留原artifact、GitHub ZIP摘要及首错，不计为产品时钟反例或通过。窄修仅允许known unit的masked/inactive且UnitFileState一致；拒绝active、过渡、矛盾属性和命令错误。两个别名按原名称逐项mask/恢复，避免遗漏。新增模拟正反例与全部15组guard测试通过，旧8c源在同新增测试下预期失败保留；下一固定源码再运行开发矩阵，原±30s/400轮/全部期限保持。


D3第二次开发早检 [41841842228](https://github.com/wyw-ai/loom-oi/actions/runs/41841842228) 固定05caa54仍在准备阶段失败：chrony.service先被mask后，别名chronyd.service的LoadState变为error，重复mask/全名inventory无法成立。原artifact ZIP摘要与四份原件已逐字核验；0个child_started、0个shift、0轮产品测试。finally/always恢复完成，原两个别名active/enabled精确相同，锚残差−11812ns；保留第一次和第二次失败，不计作通过或产品期限反例。

第二次窄修先读取固定允许NTP单元的systemctl Id，保留原名称→canonical及状态；canonical必须有已采自指快照，缺失/未知/循环/活动矛盾在任何mask前拒绝。只对唯一canonical施加runtime mask，停止态也只核该集合masked/inactive，仍拒绝LoadState=error。恢复后重新遍历全部原名称，严格比对canonical/active/enabled。17项guard、10项wrapper模拟通过；旧05c源用同alias反例准确复现失败。它们证明恢复仪器的分支，尚不证明400轮真实跳钟或产品语义；下一次固定源码再运行专用hosted VM矩阵，所有原期限和预算不变。


### Linux失败回收与正式采集工具接线检查点

804f703的完整CI随后结束：integration失败包含TV-B1-17/sqlite与G2-11五叶。前者的旧fake OIS仅协商v1，被真实新OIC按VersionUnsupported正确拒绝，尚未进入原乱序prepare断言。夹具现按真实v2提供Welcome观测预算及完整绑定，明确读取注册后的初始ObservationChanged；错误代数输入仍保持结构合法，原“register_ack之前/错误generation均ProtocolError且零dial”不变。共享fake OIC仍用v1，B1-03两后端与B1-17适用sqlite定向通过；后端无关的PG重复叶保持原显式skip。G2-11并发输入与Linux失败仍在独立排查，尚无整轮通过。

05caa54的CI41841782565完整unit先通过，随后race在TestMetricsLoopbackServer/enabled看到success计数0。HTTP客户端可以在handler的Flush返回前读完Content-Length，立即观察终态计数缺少完成屏障。测试现利用原有graceful Shutdown等待handler完成并核ctx未超时，再核success恰为1；产品200ms写预算和计数逻辑不变。本机100次定向通过，最终Linux race仍待新源码确认；保留旧失败。

正式G2工具现在接入独立clock步骤及三类原始封套（preparation/guard/result），普通向量显式关闭OI_IT_CLOCK。G2专用hosted workflow在同一固定source顺序运行原向量与clock，always独立恢复且不覆盖wrapper原件。主分析器按固定step/package重组原始完整行；普通130叶与clock20叶/400轮均依赖独立B4资源复算，不用Go PASS代替行为。副分析器与clock_guard按source-manifest核对同目录归档原字节，旧主分析器原样保留于product-pre-g2，12个既有run仍可精确复算。19项collector新测试与112项product相关测试通过，包含完整400合成时序/资源及损坏反例；这些是工具验证，不是实际跳钟通过或新门槛证据。正式manifest仍待最终合并源码和实际仪器早检完成。


此接线检查点的完整tools 39项、experiments 182项均通过，全部9份SDD review、B1–B4 implement、结构/历史迭代/物料核对通过；P2仍如实由本G2源码的G1 Inconclusive阻挡。另补仅G2的fixtures步骤，真正执行tests/fixture中的三项健康仪器验收，保持产品unit/race覆盖分母不变。主工具与夹具分步保存，未宣称六项CI全绿或进入G5/G7正式运行。
