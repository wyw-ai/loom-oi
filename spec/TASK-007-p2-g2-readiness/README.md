# TASK-007：P2/G2 观测、准入与导出

**入口现状（2026-11-05）**：当前 feature 已纳入 alpha `d8bb599e4ec9a0502c143e1b020fe4cafae20209` 的完整 G3 与历史证据。用户已要求转为本地继续，Actions 计费阻塞不再作为等待条件；专用隔离 Linux VM、真实 PostgreSQL 和离线漏洞库已准备。本地封口实现与第二次DEV严格复算已完成，四份正式预注册已随 `d9b8849` 固定。该源码首个G1已完整封口并精确复算为observed/fail：unit/race、两后端控制切断及负载和附带G2输入失败，G1保持Failed，依赖门槛保持关闭；原件及后续测试输入细化见本页末节和[控制许可见证规格](control-license-witness.md)。G2-11/12附带失败的有界取证方案见[失败即时诊断](input-failure-diagnostics.md)。G2正式运行及G1/G3/G4最终复证闭环仍待完成，历史通过原件保持，未推进alpha。

状态：用户已准确采用 Commit-Snapshot、Probe-HTTP、Export-Pull，DISC-009-R1/R2/R3 均 Accepted；[唯一原始确认](approval.json)绑定已发布固定规格，解释见[ADR-005](../../docs/decisions/ADR-005-g2-adoption.md)。进入实施时 G1/G4 前置已恢复；现需对最终 G2 源码完整复证，现已固定G2本地正式预注册，尚未运行。

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
| M3 独立发布与导出 | 有界调度/控制公平、目录挑战查询、Export-Pull、有限指标 | 核心已实现；exporter当前活动快照与writer终态修正已完成聚焦检查，G2-11完整六窗口仍须同源重验，不拼接旧开发叶 |
| M4 产品证据 | G2-01–14 与 TV-B1-19；两后端、G1适用回归、>=95%有效单测、race/静态/安全；manifest先于run | 四份本地正式预注册已固定；第二次DEV严格核验完成，d9首个G1已完整封口且原摘要精确复算失败；15原件与232源码核验完成，ITER-014反例分析及Issue闭环正在保存 |
| M5 闭环 | raw复算、独立评审、文档代码一致性、综合分析、图/产品文档/Issue/ITER/gate 联动 | 首次本地失败run与ITER-014闭环进行中；记录完整不等于产品通过 |

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

### alpha 15deeab 合并与第三次真实 clock 开发结果

已合并alpha15deeab中的D20有界缓冲、CLI取消修正与全部G1/G4历史run、ITER-008至011和Issue回执；冲突保留G2控制v2/观测/准入路径，旧验证对象和Accepted片段保持原样。独立15deeab首次G3运行41845796987为observed/fail：4条blackhole_phase预演记录误标正式行为，TASK-006另行保存原件及修正闭环。当前G2源码仍须最终同源G1/G4/G3复证。

第三次clock开发早检41845999913绑定2311c25，制品13332705573原ZIP SHA-256为5ef0ad3271da2c43a5dc9f40cc01783b30b5bd1583668f92551233f645909edf，下载后305个文件逐字核对。实际297次跳钟、298次恢复（含最终恢复），completed=297、exit=1、restored=true；NTP原状态恢复。8个sample叶未通过，12个preflight/final/total-prepare叶完成240轮；这不是400轮完整通过，也不是正式G2证据。原始文件保存在ignored coverage/g2-real-clock-03/original，不拼接此前早检为正式结果。

已定位sample验证的两项仪器缺口：proxy完成Write不能证明OIS已归约同序样本的保守期限，旧firstview可能随后被合法收紧；remaining_ms取整且零即Unknown，其权威终点区间须按整数表示推导。原失败没有完整firstview轨迹，不能将8叶逐一归因为同一机制。后续窄修复使用原query期限内的真实归约见证与完整失败数据，不放宽产品TTL、500ms或2s边界。

### Query owner 与 clock 归约见证修正

OIC控制writer补齐两个确定反例：对端已读完整snapshot但本地Write未返回时，新唯一query有界等待该owner交接；已selected但尚未写出的过期/取消owner立即释放。等待上限取新query原D与已有Write原D的较小者，不扩8槽、不续100ms。旧writer逐字overlay下两个新回归均失败，新源OIC全包与三deadline分支通过；两次独立锁序/期限复核未发现高置信阻断。该修复不声称唯一解释旧Linux离线，因为旧CI缺完整query历史。

clock仪器在held response释放前取得唯一probe的实际OIS success RTT count基线，原encoded query期限内仅精确+1才取firstview；完整exposition、wire和请求区间由独立分析器交叉核对，其他source/第二response不得借全局counter冒充。next snapshot仅提前arm，不等待其实际命中；整数权威区间改为[viewStart+(r−1)ms, viewEnd+r ms)，失败即时输出完整轨迹。32项clock/collector Python回归及17类严格反例、实际Go3根9分支通过，真实clock本机明确skip。没有修改产品TTL或guard/workflow，没有用合成400轮当真实通过。

alpha15deeab合并后的完整PG单测为95.1%，tools39及experiments219项、vet通过；历史16run/11ITER精确核验通过。merge提交5b51dfb已保存，原Accepted对象与冻结证据保持。接下来固定此修复源码在专用VM执行新的400轮clock早检；G2-11完整双后端竞争和最终同源正式复证仍未闭环。


### 竞争输入完整窗口与 clock04 原始复算

G2-11 causal09 的六叶 Go 与旧行为复算通过，但独立审阅发现60s包含healthy准备，且条件回调可能在原3s后才返回true。保留09原件及旧分析器，不称其完整饱和输入通过。新仪器记录最后一次慢源Set完成的slow_input_applied_ns，结束不早于该时点+60s；准备仍共用原3s且实际返回再次核期限。独立新causal10一次完整双后端×三轮全部Go/原始行为通过：716 cycles，慢输入60.019376–60.4938919s，准备740.5638–807.8091ms，最慢control Received→Closed+1为4.4916ms，双取消均在原1s内。44项ordinary分析回归通过；Windows FD未测，不替代正式Linux G2。

第四次真实clock开发运行41855654890固定fbda7ca：原artifact13339066371共5,997,009字节，ZIP SHA-256为73c19bf798b5cb070b01b6d2a136da869e602216039b7a09c93db37e9939dfdc；305文件与603源哈希核验，59,312行raw无Go失败，20叶400轮，400shift/401restore及9,451见证，finished为400/exit0/restored=true。原固定分析器独立复算失败，原结果保持：60条FD诊断被误路由为行为；87条held health响应在本轮terminal及restore之后由下一轮Set释放，不应算本轮续期；另外3轮source最早到期界未扣整数零权威边界，差262,503–485,870ns。

主collector仅排除kind=fd_diagnostic、diagnostic_only严格true且无schema字段的诊断，仍保留原observations与严格资源数据；任何声明schema或异常flag继续拒绝。clock按实际terminal限定新响应，验证stopped version确有当轮请求、全health不在原response至terminal内产生新header、全部时间不超过完整history观测且完成不早于响应。源remaining_ms与失权都向下取整，最早Unknown保守下界是query.WriteStarted+(remaining−1)ms；不增加TTL或时限容差。修正候选对同一原raw独立复算20叶400轮及400次严格B4均通过，旧失败和每版分析保留。这是诊断复算，不能把不同分析器的结果改写成原固定源码正式pass；最终G2仍须新固定源码的ordinary与clock同一正式run。

正式候选已逐项枚举ordinary108组/130叶/106资源环境及clock20×20，加入实际输入/代理/归约仪器根。G1/G4复验证据显式预注册vectors_timeout_minutes=90，runner只允许整数90；旧未声明G1/G4仍40m、G2仍90m，普通workflow整体150m不变。它是完整测试执行预算，产品500ms/2s、TTL、容量及全部判定保持；候选尚未作为正式manifest提交运行。


独立复审补齐竞争cycle的输入先后：每个backlog_before.started_ns必须不早于实际prepared_ns。真实causal10六叶原数据仍全部满足；将真实PG round_1的slow/full/prepared顺移至首个关键cycle之后的反例原先可误过，现在被明确拒绝。ordinary分析回归46项通过，新候选对同一716cycles诊断复算六叶pass；没有新Go样本，也未修改原causal10源快照/原件/旧摘要。


本次全部修正后的实验工具233项通过（g2-d20-experiments-03.log），clock15项及ordinary46项均包含在内；旧02的单个早到期反例仍按旧整数界构造而失败，已改为真实零权威下界前1ns，原日志保留。go fmt及vet通过。产品cmd/internal自fbda未再改，完整PG覆盖沿用本轮6907/7261=95.12464%记录；Linux固定fbda的unit/race/build-lint/fuzz已通过，integration旧夹具G2-11五叶失败证据保留，当前窄修须新Linux运行验证，不能称六项CI通过。


### G3旧黑洞向量的v2准入适配

旧5b51 CI双后端黑洞新Open被正确拒为503 ReadinessCheckFailed，但旧Go/分析器仅容许201或AccessOffline而失败。按已采用G2 §6.1.2，v2 best_effort仍需可靠本地决定（本G3环境默认CHECK_MS=1000；G2专项500ms为测试override），不能将控制无响应冒充Unknown；正在补准确原始HTTP/原因/零资源与模式绑定的新分支，同时保持原流许可2–4s、清理2s及零重放。此为已有契约交互的测试/分析适配，不改产品或冻结TASK006证据；最终当前G3须新正式run。


### PostgreSQL关闭与锁交接的确定性修复准备

固定9f3c825的Linux CI41863406865 unit在TestRestoreExternal/postgres失败：已调用旧Store.Close后，立即Restore仍得到AnotherInstanceActive。此提交cmd/internal相对fbda无差异；覆盖门禁与race由unit失败而跳过，不能称它们失败或通过。原日志/摘要保留。当前pgLock.close仅将专用sql.Conn归池，再db.Close；pgx终止连接不等待服务端释放session锁，需核实同步解锁与关闭并发边界。

后续先用真实PG的测试连接门闩保留服务端旧session，构造Close返回后Restore仍被旧锁阻挡的确定反例；不以sleep或盲重试吞掉AnotherInstanceActive。修复须先拒绝新的写与Health重取锁，等待已进入写入/维护/导入/健康操作退出，再关闭业务连接并在专用锁连接用原有5s预算确认unlock，最后销毁该连接。必须保持单实例不重叠、关闭幂等、错误返回与失败时丢弃连接；确切覆盖范围和实现由原始反例及独立复核确定，不预宣称已修。产品改动统一纳入最终同源G1/G3/G4/G2复证及完整≥95%单测。

### alpha d8bb599 的 G3 历史闭环合并

固定d8bb599的CI41864620584六项全部成功后，alpha已按预期旧值快进并回读；main仍为63c1db9。本次纳入D21及两份G3原始run、ITER-012/013、综合分析、三单回执与全部历史验证对象。当前源码的G1/G3/G4仍为Inconclusive，019139f的G3 Passed只保留在原固定源码/环境范围内。图06按当前状态重新渲染并视觉核验，其余十图保持。

合并检查已验证18个历史run、13个ITER且无pending；9份SDD review、B1–B4 implement、结构/物料通过；当前工作区（含待另提交的v2分析器适配）的39项tools与242项experiments测试通过。当前P2按G1 Inconclusive返回2，准确阻挡后续依赖阶段。G3 v2分析/仪器适配与PG关闭修复独立保存进度，不混入这次历史证据合并，也不凭本轮工具检查称产品门槛通过。

### G3 v2 开发适配与严格复核

历史合并5add63b的55个远端文件共30,501,285字节已逐字回读。新G3黑洞向量已改为核对实际v2预检拒绝、错误reason、无Channel/票据/Prepare/新target与READY，并记录原始请求时间、三张同进程OIS快照和两次日志fence。新的显式预注册字段只影响新G3候选，旧manifest与旧分析器保持；G3原34叶/62正式phase、原流2–4s及两段2s回收均不变。

Windows开发G3-05双后端10叶及两helper根22子项Go通过，raw为f1f521fc4ef57d192dfb5e0fc5665963b667ff0bb826b3c5642bdef6dca17066。该原件含D21合并前两条准备误标，完整路由仍失败；明确隔离后的10叶严格行为诊断通过，不替代Linux资源或正式G3。独立审阅又以实际反例补齐故障窗口上界及快照epoch与资源phase交叉绑定；旧候选6个子反例失败原件保留，修后25项G3行为测试通过。最终统一源码仍需新正式G1/G3/G4/G2复验。

最终适配的完整experiments 244项通过（g3-v2-final-experiments-01.log），结构与G2三项implement核验通过；18历史run/13ITER、13Accepted与六个G2片段保持。G2/G1R5/G4R4候选只新增两helper根22子项的必需Go库存，不加入G3行为判定；新G3候选显式声明v2分支。全部仍为ignored准备，待PG修复及同源质量完成后统一发布正式预注册。

### Actions外部阻塞与关闭修复复核

G3适配已提交7ce8c02并完成6文件逐字远端回读。其CI41870724904六项均在启动前失败，六份原始annotation完全相同：GitHub提示近期账号付款失败或支出限额需要提高；未执行测试，不能算代码失败或通过。日志接口404与原始checks/annotations保存在ignored coverage/g2-ci-7ce8；已向用户请求恢复Actions，继续本地可完成工作，不改账户支付设置、不替换正式Linux证据。只取消已被取代的8705/5add两个CI，旧9f运行保留以回收竞争输入结果；原失败不删。

PG关闭第一候选已用真实锁连接Terminate门闩复现旧Close返回后Restore仍AnotherInstanceActive，修后对应ACK与完整store测试通过。但独立复核又实证取消事务的后台析构缺口：真实业务socket.Close仍被门闩阻挡时，前台write/export已返回、Close及竞争Open均nil。old-cancel-drain-01.log保留；第一候选不能称最终修复。正在只对PG事务入口及迁移失败路径持有显式sql.Conn，等待其本地事务/连接析构结束后再释放生命周期租约和实例锁；这不是远端未知提交的业务确认，也不为整个关闭增加新的5s SLO。原5s仅用于锁连接检查/最终unlock查询。

另一个真实负例确认公开Restore吞掉清理错误：恢复事务已提交epoch77、maintenance=true及一条restored_from审计，unlock请求黑洞在原5s后失败并关闭socket，但defer忽略Close错误而整体返回nil。后续将Store的Export/Import/外部Restore及直接持有Store的migrate/init/maintain命令准确传递清理失败；保留已经完成的数据结果和已输出token，错误明确区分主操作与关闭失败，不声称回滚、不重试、不改变单事务或维护模式契约。此前第一候选的测试与源码快照保留，新增回归须验证该已提交状态与错误同时可见。


### PG 关闭修复完成与本地执行方向

真实 PG 负例及独立复核后，Store 关闭先阻止新操作并等待既有操作退出；取消事务通过专用 sql.Conn 等待本地后台析构，之后使用原锁连接的 5s 查询预算确认 advisory unlock，错误时丢弃连接且不声称已确认释放。关闭幂等，SQLite 保留原事务路径；5s 不是整体关闭期限，也不把连接析构解释为远端未知业务提交已确认。

公开 Export/Import/Restore、失败 Open 清理，以及 migrate/init/maintain CLI 现在保留主错误与关闭错误。主操作确实完成时保留结构化结果和已输出 token，并以非零退出报告清理失败；Export 只有实际 Sync/Close 也成功才保留完整文件与摘要。真实 PG 测试核实恢复 epoch、维护态和审计恰一次，未自动重放。新增 Store 11 根/17 子项和 CLI 3 根/20 子项，旧源码确定失败原件及修后日志均保存。

最终产品文件的本地 Windows 完整 cmd/internal 单测通过，7030/7387=95.1671855963%（Go 显示 95.2%），1594 项通过、0 失败、1 项 Linux 专属 SQLite FD 检查跳过。原始日志为 g2-pg-close-full-unit-01.jsonl，覆盖 profile SHA-256 为 8db5a6f43a3ca34b93d80f38c2f74252428b0df68b88df3740fc6553643b8f7e。产品 vet、staticcheck v0.8.2、govulncheck v1.9.0 通过；Go 1.27.3 对 Linux amd64/arm64、Windows amd64、Darwin arm64 各三二进制构建两次，各平台字节一致。以上不是 Linux race/FD 或最终正式门槛证据。

用户对 Actions 计费阻塞明确回复“转为本地，暂时无法处理，继续本地工作”（chat-request:call_e891f41bfe1b479182346b3170a0dfd3，2026-11-05，本会话用户）。据此继续本地实现与验证，不再等待账号恢复；保留原 Actions 六项未启动事实，不伪造 CI 成功。准备专用本地 Ubuntu 虚拟机承接 Linux、真实 PG、race 和 clock 检查，单独记录执行环境/镜像/来源/隔离身份；旧 Linux CI 证据和冻结批准不改，原 95%、500ms、2s、TTL、容量及失败判线保持。只在任务虚拟机内施加 clock，不能改 Windows 或用户提供远程主机的时钟。正式复证前先固定本地执行规格和源代码，当前仍不标记任何新门槛 Passed，不合入 alpha。

9f3c825 旧 Linux integration 完整日志回收后，G2-11 六叶中五叶通过，SQLite round_2 在关键处理后 exporter 再查询为 200 而失败。首个 429 后仅约 59ms 即解除拥塞，现有记录不能证明两连接持续占用；该输入失败保留。新输入准备将在原 connect+1s 内、同两个连接和原请求数上观察连续至少五个 429，首个完成至最后开始至少 40ms，中间 200 重置候选，不重启输入或延长产品期限。严格分析器必须核对全部原始查询，不能用固定 sleep 或单个 429 代替稳定输入；完整新开发运行仍需保存全部结果再诊断。


完整新 G2-11 开发运行六叶已结束，五叶 Go PASS、SQLite round_2 FAIL。失败发生在 cycle 2 构造 victim：真实 preflight Ready 只余 5ms，约 3.6ms 后 final 返回 Unknown/timeout，并出现同 sample 的 expired 更新；前两 cycle 的稳定 429 和关键操作已成功。该原始失败与全部 wire 保留，未重试，当前证据符合固定 sampleExpiry 的保守取消，不据此改产品。

后续仅调整 victim 的输入相位：newToken 后仍使用原 3s 绝对 context，在发 Open 前 Lookup 已存在的 Ready 及完整 readiness_view 绑定，剩余期扣除完整 Lookup 往返后至少原 CHECK_MS=500ms。保存选中 view、Lookup 起止、选择时间和原截止，再用同 context 只 Dial 一次；选择不到即输入失败，已发 Open 失败不重试。该相位不保证 Open 必成，最终 preflight/final 仍为权威；不刷新 probe、不延长样本/TTL/准备期限，不改变 native 路径或同两个慢读连接/原请求数、1s 输入与 500ms 关键操作预算。严格分析器将交叉核对选择与 victim 的实际先后及绑定；此说明先提交回读再实现。


### 本地隔离 Linux 执行规格（用户转本地后的实现准备）

依据上一节用户准确指令，将验证执行地点转为本地专用 Ubuntu 虚拟机。原 hosted 分支及旧 run/分析器完整保留；新本地执行使用显式 `local-isolated-vm-v1`，不能设置伪 GITHUB_ACTIONS、RUNNER_ENVIRONMENT 或 workflow_ref。正式 manifest 必须显式声明此 profile，并固定基镜像、QEMU 与启动记录的摘要及预期 VM 身份。真实 VM 先准备依赖，恢复仅回环 SSH 转发、无主机共享的隔离网络后才运行产品；OS、内核、Go、PG、编译器、CPU/内存和虚拟化方式按实记录，不能把 TCG 或应用配额写成目标整机能力。

clock 本地入口仅接受 Linux 专用 QEMU DMI UUID、`loom-oi-clock-<uuid>` serial 与 root 管理的不可组/他人写入 attestation。该文件绑定本轮 run、固定 source SHA、boot ID、普通 runner UID/GID、VM UUID/serial、host 随机启动身份及镜像/QEMU/启动记录摘要；拒绝符号链接、身份不一致、未知 profile、普通宿主或未隔离声明。镜像摘要是宿主启动者对实际下载字节的声明，不冒充 guest 独立读取底层镜像。普通用户准备二进制和执行产品，只有 guard 提权改 guest CLOCK_REALTIME；每次原始见证及 syscall 审计须保持同一 profile、attestation 摘要、boot/run/source/nonce/进程身份。hosted 原身份键集不扩成历史必填，本地使用独立版本，Go 仪器、wrapper、collector、离线核验都明确辨别并交叉绑定。

宿主侧独立监督进程与 guest guard/wrapper 分离，记录固定 VM 进程与启动身份、原始命令、开始/结束和完整退出状态；无论 guest wrapper 退出、超时或被杀，最后独立执行已有恢复/审计收集路径。只有原始 guard 完整恢复、NTP 状态回到原值、无孤儿产品/测试进程并保存审计才可通过；恢复不可确认时停止该任务 VM 并保留失败，不能以销毁 guest 代替恢复成功。独立监督回执必须纳入新正式运行的来源核验，缺失时不能晋级。Windows 及用户远程 SSH/Bash 主机的时钟不在施加范围。

所有产品判线保持：G2 ordinary 原 130 叶及 clock 20 叶各 20 轮、±30s 的原100ms仪器容差、500ms CHECK、2s准备/资源窗口、完整资源基线/FD/goroutine及各预算；PG/SQLite、完整cmd/internal ≥95%、race、三项60s主动fuzz和固定静态工具不缩减。先实现并验证 local profile 的拒绝/恢复/源绑定边界，再固定同源预注册运行 G1/G4/G3/G2；本地开发测试和仪器自检均不冒充门槛通过。必要回归包含错误/缺失 profile、DMI/boot/source/run/nonce/UID不符、可写或链接attestation、缺外层监督或恢复失败、混用hosted/local身份及guard回包换身份；不会为测试执行真实宿主跳钟。

victim 输入剩余期明确按整数零失权语义保守计算：`max(0, (remaining_ms-1)*1ms - (selected_ns-lookup_started_ns)) >= 500ms`；其余一次 Dial、原3s绝对截止和真实绑定保持。


### 稳定输入与 victim 相位的开发回合结果

稳定429分析现保留Windows同一HTTP开始/结束可能相等的真实采样分辨率，但不允许请求重叠、倒序、缺失或超出原1s输入期。01新raw SHA-256为de50ce636cec2a69051b97af4d08bdbbe97639c741fcde46abdcef6f00b6a500：五成功叶598cycle严格行为复算通过，SQLite round_2的临界样本失败保留，未称整轮通过。

victim相位已按前述原3s绝对期限与保守剩余期实现，完整view/Lookup起止/选择/截止均作为原始输入记录；strict analyzer交叉核对cycle、重复轮、绑定和实际Open前后。开发02完整结束349.305s，三叶Go通过、三叶输入失败。新victim原raw漏round字段使严格全组拒绝，该原件不补造；源码已补显式round并通过helper/schema检查，未自动第三次运行。最终纯Go三根26子项通过，ordinary Python 59项通过，未改变产品源码/判线。

02另外三个失败周期的两个147456B写入均完整，但在原1s内观察到88/86/88次HTTP200、零429；PG两例export timeout各增加2、rejected增加60/81。原wire/counter支持第三observer可能先占空槽，使另一个慢pipeline读到拒绝，而不是已证明两个成功scrape同时被慢读阻塞；不能归为控制500ms失败，也不以VM/Windows标签替代根因证据。

下一步只补慢读输入的因果前置：在同两个连接、同2048请求及原最早connect+1s绝对截止内，通过不消费socket字节的MSG_PEEK确认各自首个真实HTTP响应头是200，再允许第三observer进入现有连续429资格检查。必须保存两次peek的原始有限头字节/解析状态与起止；非200、头不完整或超时即输入失败，不重启连接、不重试已发业务、不延长预算、不改变窗口或添加固定sleep。peek本身不证明持续阻塞，原五次429/40ms、关键操作后429、关闭前两次真实失败和全部资源判线继续必需。此说明先提交回读再编码，原01/02与所有旧分析结果保持。

产品9b553f7另完成三项本地主动fuzz各60s：FuzzDecodeFrame、FuzzDecodeControl、FuzzObservationState均通过，原日志及SHA保存在g2-pg-close-fuzz-01；仍非Linux race或正式G2证据。本地clock规格bcc4e13已完成逐字远端回读，专用VM依赖准备与profile边界实现并行，尚未真实跳钟或正式复证。


### 本地执行方向发布与下一次慢读输入诊断

I11-P12（6638111809）及I18-P08（6638117304）已追加发布并逐字回读，21条旧评论及两单元数据保持、6个链接解析；真实回执见docs/issue-history/2026-11-05/TASK-007-local-execution-published.json。仅登记用户转本地方向与开发检查点，没有新增正式run/ITER或门槛晋级。alpha仍d8bb599，main仍63c1db9。

MSG_PEEK纯parser/真实TCP不消费等两根16子项通过，但唯一真实slow-input检查仍失败：两首完整200头、随后87个observer均200，没有满足稳定429。这一反例不删除。该版本失败日志未预先留完整源码hash，不能把之后补写join/日志的当前源码指纹冒充其固定来源；原日志及该限制保留。

独立源码与原件复核发现429分支不占slot，却使用同一Write/Flush和timeout计数。故两个首200加timeout增2不能证明两个持槽200同时阻塞；产品显式Flush已存在。02的first observer距双写完成仅1.55–1.68ms，不支持该组post-write准备已耗尽200ms的说法。下一步仅进行一次透明HTTP诊断：真实metrics.NewHandler、同两连接/2048请求/RCVBUF1024、原1s与10ms observer/五次429跨40ms不变；通过ConnContext关联真实TCP tuple和request ordinal，在ResponseWriter透传Unwrap、原Write/FlushError调用前后保存status、单调起止、返回字节/有限错误类别及handler进入/返回。预分配最多65536事件，热路径不IO、不加阻塞门闩、不增加HTTP；结束join后保存原件，overflow/缺结束/连接错配均失败。

该诊断区分两持槽200实际重叠、200与无槽429重叠、首200已完成或写超时发生于准备阶段；保留输入前/关闭前/关闭join后的Registry原快照和全部observer/源hash。未复现只记录轨迹，不重跑凑通过。诊断本身不提供正式G2，通过后也须基于所见机制另定最小输入修法，所有产品与输入预算保持。


### 本地 clock 执行适配与真实 Linux 边界自检

已实现独立本地profile身份、root-owned attestation与DMI/boot/source/run绑定、普通runner与guard回包的逐次身份检查；hosted v1原解释保持。普通本地执行若没有适用的整run回执仍unsealed/fail，不能借不含clock绕过收尾。G2 clock必须在原3600s步预算内、最多120s收到宿主独立恢复回执，绑定原四clock文件、wrapper退出与同nonce恢复，缺失/混源/异常均拒绝；宿主回执不依赖最终summary，避免循环自证。

独立复核实证旧SourceFileLoader可接受同mtime/size的陈旧pyc，现guard、wrapper及product runner的相邻执行入口统一按读取的.py原字节compile，旧缓存负例保留。nonce仅接受普通私有文件的规范65B内容，拒绝FIFO/尾垃圾/链接；特权raw只在已打开且归普通runner拥有的安全父目录内独占新建，不跟随已有目标。guard与recover在同nonce的root锁上互斥，busy不改钟/NTP，进程终止由内核释放；旧recover绕过忙锁后仍改FakeClock的反例已保存。

固定六文件字节快照已传入隔离Ubuntu，并逐SHA回读，前后boot一致：真实Linux下local_execution 10项、clock_guard 26项、clock_run 14项共50项全部通过，无skip。包括root文件/FIFO/symlink边界、真实跨进程flock及SIGKILL释放；所有时钟行为仍用FakeClock或mock，未执行真实跳钟，不作为400轮证据。原件在g2-local-profile-linux-01并绑定本轮实际源hash。Windows完整实验工具315项通过，4项Linux root专属项如实skip；上述Linux自检补足这些边界，不替代全部Go/Linux门槛。

宿主监督器21项初版mock未覆盖错误的旧机制runner入口，独立后续检查发现并保存新测试对旧源码FAIL；当前已固定experiments/product/run.py，并用真实产品argparse入口契约验证，22项通过。产品runner主分析器的同mtime/size缓存反例也已修，新增9项runner定向回归通过。来源/命令等价性由真实接缝验证，不能仅凭mock数量宣称可执行。

专用VM最终实际隔离启动已核验，Go1.27.3、PG18.6 Ubuntu包、gcc及固定静态工具/官方离线漏洞库已准备。当前开始固定8d677614源码归档的Linux完整产品单测/race开发检查，产品字节与9b553f7一致；没有.git的归档来源准确标注，不能冒充正式run。原G2慢读透明诊断也只执行预定的一次，结论另按实际原件登记。

### 本地执行适配检查点（2026-11-05）

本次完整实验工具回归已为317项通过，4项Windows上的Linux root专属边界如实skip；此前固定六文件的真实Linux50项全部通过且无skip，覆盖这些文件/锁边界。go vet ./...、结构、九份DISC review、适用implement、13份ITER及材料检查通过；历史18份run、13项Accepted与264份冻结原件逐字核对未改。P2仍按当前源返回G1 Inconclusive，不能把预期阻塞写成门槛通过。

固定8d677614归档的Linux产品单测第二次完整装配通过：1598个测试、22个包，7061/7414语句即95.2387375236%，耗时130.3488896s；源清单208文件前后不变，原raw SHA-256为19d881328b397152b0edf7c48048ae674aff6e27f7bb9d59824731d48e3f0e1a。第一次归档漏tests/pki造成五包setup失败，原错误和部分覆盖率保留，未计为完整覆盖率。对应race仍在运行，已出现writer交接write_expiry断言失败，先保留整轮并定位，不重跑或放宽时限。

仅执行一次的透明慢读诊断得到209条事件、21次observer（20次429、1次200），没有溢出或清理错误。这次输入合格，未复现前次失败：两个慢连接的前三个200很快结束，第四个200 Write才在约+5.2371ms起持续至+203.9006/+204.4753ms超时，两持槽200实际重叠约198.6635ms。该实测说明首个可见200可能已是历史响应；不把本次成功推广成输入可靠或旧失败唯一根因，完整209文件源码快照与轨迹保留。仍需非占export槽的当前活动快照来精确准备输入，另行固定实现细化后再编码。

本检查点未新增正式run/ITER，没有执行真实clock，也未推进alpha或任何门槛。

### G2-11 Active 输入细化（编码前固定）

属于已采用G2§8有限诊断的实现细化；不改Accepted scope、权限、配置、13个Prometheus family或产品预算，不重问R3，不宣称旧失败唯一根因。

**字段与一致性。** ObservationRuntimeStats新增`ExporterActive`/`exporter_active`（整数0..2）与`ExporterYoungestActiveAgeNS`/`exporter_youngest_active_age_ns`（非负int64）。同一Handler固定两条monotonic start；无活跃为0/0，否则age=min(now−各start)，不钳位200ms。私有短锁统一原nonblocking取槽+登记、清除+归还及快照；429不登记，所有返回/错误/panic经defer释放。锁不跨Render、Write、Flush、Registry或Hub/Agent锁；槽数、200ms原起点/截止均不变。

**接线。** OIS在监听前保留唯一Handler，原TLS/admin `/v1/admin/stats`解Hub锁后附快照，不新增无认证路由。OIC保留同一诊断实例，Agent.mu下只取指针、解锁后读取；仍原loopback `/stats`。禁用为0/0。不得聚合多个Handler成单实例0..2，不新增身份/label；统计只代表采样时刻，不保证后续占槽或剩余写预算。

**原期限时序。** D=较早slow.connected_ns+1s。测试helper仅增加双TCP建立后、任一GET发送前的回调：在不晚于原写D的ctx内，实际stats一次200且Active=0/age=0，否则本次输入失败；不重连、不重试业务。随后原每socket2048 GET、RCVBUF1024、双写join和两首200 MSG_PEEK保持。只用非metrics stats串行按原10ms节拍等待首个Active2且youngest≥40ms快照，全部在D内；再执行原5次连续429/40ms。首header仍是有效首响应证据，不能替代占槽。无固定sleep、并行负载或新期限。

**原始字段。** cycle/failed_cycle新增`export_slot_input`，精确键：`schema="g2-export-slot-input-v1"`、`origin_ns`、`deadline_ns`、`zero`、`writers_released_ns`、`samples`。zero及每sample含`started_ns/completed_ns/http_status/process`；process沿既有`{pid,sampled_ns,stats}`，保留真实完整Stats，无token。zero一次；samples为1..128份，末份是首合格值。失败保留已发生部分，leaf失败，不补造。

**独立重算。** 严拒null/bool/错schema/缺字段/错角色PID；origin精确绑定slow连接，D差1e9。双connect完成≤zero开始≤结束≤writer释放≤双写开始；zero必须0/0。samples开始不早于双写及peek完成，全部非重叠、原D内、身份不变；末项2/≥40ms，之前不能已合格。末项完成≤首metrics观察开始。不能混两个样本、复用跨cycle区间或重起D；同次start=end允许真实时钟粒度。原200重置429尾段、后置429、关闭前失败计数≥2、500ms关键控制、2s资源、3s准备、整60s慢probe及满队列全部不变。age本身不证明网络Write阻塞。

**回归与最小diff。** metrics/handler及测试核0/1/2一致性、slot复用、429不计、各退出释放、慢Write期间Stats不阻塞；protocol/api仅两字段；OIS server/adminStats、OIC agent/reload及权限/生命周期测试；export helper的nil回调保留G2-12旧路径；competition输入/raw与g2_analyze对应精确边界/伪造反例。新测试根/子项实际落盘后登记unit/race候选，不改分母/criteria。旧原件与分析版本保持，正式通过仍须新固定源码Linux证据。

### 本地运行封口与离线漏洞库（执行适配）

沿已采用的 local-isolated-vm-v1 执行适配，普通 G1/G4/G3 与 G2 统一使用独立 `execution-receipt.json`。collector 仍一次生成原 raw、execution、summary、report、coverage；仅本地最终 SHA256SUMS 延至宿主独立核对退出与保存来源后生成。不得追加 raw 末行协议、重写 summary 或延长任何产品、步骤、资源与 clock 预算。无封口的 raw PASS 不能交付或晋级。

回执 schema 固定 `loom-oi-local-execution-receipt-v1`，字段限于：execution_profile、run_id、evidence_run_id、source_sha、boot_id、vm_uuid、vm_serial、attestation_sha256、host_nonce_sha256、base_image_sha256、qemu_binary_sha256、launch_sha256、host_sources、host_vm、runner、process_checks、clock、files、host_started_monotonic_ns、host_completed_monotonic_ns、errors；未知字段拒绝。身份全部串配 manifest/env/raw/原 attestation。host_sources 只准固定 supervisor/seal/local_execution 三个相对路径，摘要必须等于该 run 的 source-manifest，不能借当前工具重算旧源。host_vm 绑定实际 HANDLE、creation FILETIME、固定启动名与命令摘要；普通 clock 严格 null，G2 保留真实 clock 子回执及独立最终恢复原件。

runner 与每个 step 使用真实 Popen 后、wait 前的 PID/start_ticks/PGID/SID/UID/GID及退出记录；宿主确认 collector 退出，逐一核已由 start_new_session 建立的 step group 为空。不得把 SSH 继承组当私有组杀掉。PID复用、残留或身份不明保留失败；必要回收不改成正常退出，恢复不可确认时只停已核身份的任务 VM。process_checks 与固定 case.steps 一一对应且至多11项；残留PID列表至多128项，溢出记失败；errors至多16个固定代码（runner_nonzero、runner_exit_unconfirmed、process_identity_unconfirmed、residual_process_group、process_inventory_overflow、clock_recovery_failed、artifact_collection_failed、artifact_changed、source_mismatch、vm_identity_mismatch、transport_failed、deadline_exceeded），不接受自由命令/路径；回执上限64KiB，原 attestation/clock 限制不扩。

files 必须精确覆盖 run 全部已生成白名单普通文件，仅排除本回执与 SHA256SUMS，拒路径穿越、链接、漏项及事后新增文件。宿主先后复核原件摘要，exclusive原子写回执，再完成SHA封口；不含自身摘要，无循环。缺失/畸形/串源回执直接拒绝；结构与原件有效的失败回执仍能保存反证，verify返回 execution_supervision=failed，check_iterations 的 primary 明确拒绝。完整回执不覆盖原产品失败，也不允许 summary PASS 掩盖执行失败。

新本地 manifest 显式固定官方 Go 漏洞库 ZIP/provenance 摘要、字节数、获取UTC、db.modified、三个index摘要与完整member/ID数量；材料原件随run封口，源码清单绑定校验代码与预注册。runner保持 `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0`，仅增加实际 `-db file://`；不用GOVULNDB假接，不换工具版本或1200s。扫描前后核全部提取member与索引闭包，env/raw记录唯一同一快照和实际argv，缺库/变化/扫描错误均失败，不联网回退。旧hosted、旧run、归档分析器与原判线保持；本轮ZIP为7e893ac89f6b5d7e8d3431ee7cddabc358cea99c8822d3ac545882c38af26c3e，完整来源数值见准备原件及 `coverage/local-run-sealing-plan.md`，不得把该快照称作永久最新。

必要回归覆盖缺/错附件、source/boot/nonce/PID串配、残留、失败回执不可primary、普通零clock调用、封口前后原件不变、DB缺ID/错hash/错-db/中途变化/真实非零；全部历史run仍精确复算。正式执行前固定同源预注册，原95%完整分母、race/fuzz、FD/goroutine及所有预算不变。

本轮离线漏洞库声明采用go-vulndb-snapshot-v1，固定url、zip_sha256/zip_bytes、provenance_sha256、download_completed_utc、modified、index_sha256（db/modules/vulns三个完整路径）、members、vulnerability_ids、modules；禁止未声明环境回退。实际路径仅由OI_VULNDB_ZIP/OI_VULNDB_PROVENANCE/OI_VULNDB_DIR配置并记录file URI。保留一次env/vulndb_snapshot事实及govulncheck前后两次目录核验事实，三者快照/URI一致，真实start命令必须使用同一-db。ZIP/provenance为run平面归档文件，不作为Git源码。分析器验证原始事实与命令，verify再独立打开归档材料核全部member及ID闭包。

### 控制 writer 终态先于交接的修复细化（编码前固定）

Linux race原反例中write_expiry返回nil并调用了新请求builder。可控conn门闩在真实socket写完成后等待release/closed；原写deadline通过websocket关闭conn后也可使Write返回原n,nil。因此原Write返回nil不单独证明操作在有效期内结束。此外run先release owner、后cancel session，真实非nil写错误也存在先唤醒接替者的交接窗口。两条机制分别验证，不能只修发布顺序就推定前一反例消失。

实际Write返回后、主动cancel派生ctx之前，检查该ctx.Err和原绝对deadline；若实际已取消或到期，即使底层返回nil也转为该终态错误。正常期限内nil保持成功。任何终态写/构造错误须先取消session，再通知本次done和释放query owner；接替者不能看到已释放slot而仍接受新请求。query计数、线序列、关键lane、公平性、原100ms测试写预算/500ms请求期限及生产既有期限全部保持。

用可控门闩分别覆盖nil-after-expiry和error-before-handoff；旧源须出现预期失败，新源须拒绝新builder/请求且结束writer，同时验证合法到期前交接和仅新请求取消不终止健康session。保留原完整Linux race失败及固定源；修复后先聚焦检查，再对最终产品源跑完整unit/race。

### 后续完整本地检查与握手门闩归属细化（编码前固定）

更新后的Windows完整产品单测1619项通过、1项Linux专属FD跳过，22包通过，7080/7440=95.1612903226%；vet通过。Linux同产品215份源文件与Windows对应文件逐字一致，但完整开发unit仍失败：write_expiry子项在约0.02s得到nil并触发builder；覆盖率7107/7467=95.1787866613%不覆盖该失败。后续race还观测到TLSIdentity/valid在约207ms保守返回timeout（测试配置200ms），这是另一条反例，尚未定位，不合并原因也不放宽截止。

两份独立源码复核确认测试夹具可误绑定旧HTTP握手写入：Go HTTP transport可以在收到101后使Dial返回，而发送握手的Write尚未返回；handoffConn在底层Write之后才读取armed，因此新arm可被旧调用消费。waiting.entered还可能由普通WithDeadline传播触发，不能单独证明已进入满槽交接。原失败日志未记录Write身份，不倒写唯一根因；此前真实错误与nil-after-expiry的独立终态反例和修复仍保留。

下一步仅固定门闩输入归属：在handoffConn.Write入口原子取得本次是否已arm，随后才实际写socket；只有该次已取得资格且完成真实全部写入才发布written/等待release。先开始的未arm旧调用不得在返回时抢未来arm。等待用例另核原query owner仍同一对象且writing、新query尚未入队，再接受handoff信号。产品writer、本次100ms写预算、500ms请求期限、全部正式判线不变。

增加确定性反例：真实net.Pipe/TCP旧写已完整被peer读取但返回受控，之后arm并放行旧返回；旧夹具错误消费应被新测试检出，新夹具旧调用返回且arm留给下一次真实写。下一写仍须被peer完整读取并确实阻塞返回，释放后join。测试不得用固定sleep或扩大超时制造成功。等正在运行的完整检查自然结束并保全后才改冻结Go源；修复后先聚焦原边界，最终完整检查重新绑定新源，不拼接旧失败叶。

离线扫描接缝已真实验证：首次GOPROXY=off阻挡Go对工具模块的显式元数据查询，scanner尚未执行，错误原件保留；改为已缓存module download目录的file://代理后，仍原go run golang.org/x/vuln/cmd/govulncheck@v1.9.0及同一-db固定快照，原1200s内exit0并输出No vulnerabilities found。固定8d产品归档与官方DB前后摘要相同，属于执行工具开发检查；不替代最终产品源扫描，不联网回退。

### TLS 身份单测的输入有效期与独立到期边界（编码前固定）

dev03完整race仅TLSIdentity/valid及父项失败，实际200ms请求在207.1228ms后返回Unknown/timeout；wrong_name/wrong_ca通过。该结果符合超时优先的产品规则，不能推断CA或hostname校验有错。证书与Plan构造已在计时外，现有raw没有TLS内部阶段，未证实首次加密初始化或调度是唯一原因。

此单测的目标为真实TLS CA/hostname身份分支，其通用testOptions曾顺带使用200ms。仅该身份场景显式设置已采用默认DefaultTimeout=1000ms，仍真实建立新的TLS连接并分别要求正确身份成功、错误名称/CA拒绝；不改变通用testOptions、产品实现、正式G2环境200ms或任何SLO/验收预算。原207ms失败原件保持，不重标为通过。

为直接约束原截止，另补一根at_deadline/after_deadline两子项：使用已有私有execute的now注入与真实net.Pipe HTTP200首头，确认确实完成真实头且请求ctx未超时，再让最终判定now落在原200ms绝对D或D+1ns；必须Unknown/timeout，不能接收晚到Ready。只测试本地函数时间接缝，不改OS钟、不sleep。原already_cancelled不拨号、真实slow_header超时、失败不替换样本及正式G2-02的200ms全部保留。该调整区分身份输入覆盖与到期拒绝边界，不降低任一已采用产品判线；先聚焦新旧边界，再重新绑定最终完整检查。


### 本地执行实现完成与下一次固定源验证（2026-11-05）

本次实现包含b3aee53先行规格的活动槽快照、writer终态及本地整run封口/离线库，0999be8的Write入口门闩归属，以及644db4的TLS身份输入和原截止单测。独立复核与root检查完成，原失败不改；TLS仅两个测试文件变更，Windows unit、Linux unit/race各25项通过，真实200头在原D/D+1ns必须Unknown/timeout。Windows缺gcc故未执行race，不冒称通过。最终完整Linux单测/race将随固定源码的正式质量步骤统一运行，不把先前dev03失败重标或拼接成成功。

当前稳定工具全量356项通过、Windows10项平台skip，39项tools通过；vet、结构、九份DISC review、两个适用implement、材料与18run/13ITER复算通过。历史235+27份冻结文件及另两份confirmation原件、13项Accepted及15个冻结范围逐字保持；仅刷新DISC-007-R5两项Pending指纹。P2检查仍按实际G1 Inconclusive返回阻塞（exit2），不是RECORDED PASS。五份本地正式候选已完成静态检查，仍需固定预注册并运行；尚无正式新run/ITER或门槛晋级。

恢复会话时发现原QEMU进程已退出、SSH拒绝连接，原因未确证；无未完成正式run或真实clock。原启动、状态和连接失败原件保留。以同一restricted网络/磁盘重新启动专用VM后，实测boot为f302db19-7689-450b-85c4-a2bb99cf7bc8、launch摘要2198dabe45aed6357a108febd8e74e470a9d95509f77eed5aa134529cf865cdb；HANDLE/FILETIME、实际命令、DMI和guest事实均已核。原任务PG按原目录恢复并核实际18.6与新进程身份；新run必须使用新boot/launch，不复用旧证明，也未操作Windows或远程用户主机时钟。

固定[DEV-LOCAL-SEAL](../../experiments/manifests/DEV-LOCAL-SEAL.json)仅供执行适配开发smoke：真实Windows监督器启动专用guest原collector，只执行原900s的go vet。未执行的unit、engine与fuzz输入必须依原规则缺失，预期产品observed/fail和runner exit1；宿主独立见证/退出/空组/原件均有效时来源可以complete。不得作为产品gate/primary或以零工作制造PASS；本开发原件留在ignored coverage，后续正式run独立取得完整产品证据。实际执行需本次代码、manifest先提交回读，root-owned新run attestation和清洁Git checkout准确绑定同一source；若smoke失败，保留并修接线，不自动重试或放宽判定。

### 首次真实封口开发运行与分析输入顺序修正（编码前固定）

ba3287c实现38文件已推送逐Gitblob回读一致。固定源码的真实DEV-LOCAL-SEAL-20261105T030158Z-8a79a9完成：vet正常exit0，collector/host均exit1，宿主独立活体、wait后空进程组及原件封口complete/errors=[]，未调用clock。其最终verify仍失败，不能宣布整个smoke通过：原summary.problems的缺fuzz条目按frame→control排列，而已保存manifest的JSON键排序使复算按control→frame排列。两者仍为observed/fail，唯一差异已保存到recomputation-diff-01.json；原manifest/raw/summary/receipt一律不改。

根因是collector分析保存前的case对象，而dump(sort_keys=True)改变了磁盘manifest字典插入顺序。下一步只把stored manifest在首次观测之前规范化为与实际保存字节一致的JSON对象，并令运行/分析共用其case；保持所有字段值、数组顺序、实际argv/期限及既有分析器判定。禁止事后重排失败列表或重写原summary来掩盖该反例。新增回归需从非字母顺序fuzz输入，经实际collector写manifest/summary接缝，再从存盘manifest严格复算相等，且原缺失输入仍fail、不能primary。旧18run/13ITER须精确复算不变。

修复先完成聚焦工具回归和独立复核、提交回读；随后以新的source/run/attestation身份执行一次同DEV manifest，独立保全新旧开发原件，不使用旧run ID或重试未知业务结果。四份正式候选已通过独立静态评审，case与原criteria/预算/计数保持；待此实际封口接缝闭环后再固定预注册并运行，不放宽产品门槛。

该窄修已完成：collector在实际保存manifest后、首次观测前读回stored/case；实际文件接缝的两项新回归对旧源均FAIL，修后均PASS。完整experiments 358项通过（Windows10项平台skip），18run/13ITER复算精确保持；独立只读复核未见高置信阻断。首DEV原件未改，修后新身份实际smoke尚待执行。I11-P13/I18-P09已追加逐字回读，23旧评论/两单元数据保持、6固定链接核验；[回执](../../docs/issue-history/2026-11-05/TASK-007-local-implementation-checkpoint-published.json)记录实际失败与待办。ba3287c四平台三二进制各两次构建逐字一致、go mod verify通过；它们准确绑定该提交，不替代最终正式源码的验收。


### 正式本地预注册与执行入口（2026-11-05，运行前）

f4f8a259的第二次唯一DEV入口使用新run/attestation身份，运行DEV-LOCAL-SEAL-20261105T032149Z-9f0437：vet25886ms/exit0、host总34.4244025s；产品仍为预期observed/fail且guest/host exit1。此次严格verify(run,True)完成原件校验、summary精确复算和来源complete，primary按非passing verdict明确拒绝。232源文件逐Gitblob一致、8份guest原件与host副本一致、10份封口文件hash一致，实际collector/step均wait后gone且组空；clock=null、boot仍f302db19-7689-450b-85c4-a2bb99cf7bc8。首轮失败与所有准备命令错误原件保留。本结果只完成执行适配开发验证，不是产品验收通过。

选用以下四份预注册。旧候选、旧manifest/run/ITER及采用片段保持；只选G4资源版，不同时运行同ID的无资源比较候选。独立评审核对固定criteria、原vector前缀、83项共享回归及新增测试源码；空输入、缺ordinary companion、clock轮数/资源数改19和普通case私添clock均被拒绝。人类说明已按本地环境收束，G4使用同版本原生pg_dump/pg_restore；执行case除纯说明scope外与已审候选完全一致。

| 预注册 | 直接范围与固定输入 | 宿主共享总上限 |
| --- | --- | --- |
| [G1-P1-R5](../../experiments/manifests/G1-P1-R5.json) | 原五项P1 criteria与O3 load=true，双后端、619项必需step/package结果、原资源判线 | 150min/9000s |
| [G4-P2-R4](../../experiments/manifests/G4-P2-R4.json) | 原四项恢复criteria，load=false，670项必需结果；显式79叶/85最少资源检查、32MiB tmpfs与真实PG18.6工具 | 150min/9000s |
| [G3-P2-R2](../../experiments/manifests/G3-P2-R2.json) | 原四项路径故障criteria，34业务叶/62正式phase，757项必需结果；明确已采用v2预检的黑洞失败语义 | 150min/9000s |
| [G2-P2](../../experiments/manifests/G2-P2.json) | 普通108组/130叶/106资源env与同一run的20叶各20轮真实clock；1049项必需结果，205资源test/591最少cleanup，load=false | 210min/12600s |

最终subject_version必须为包含这四份文件和已审实现的同一完整清洁提交，随后逐字推送回读；运行时manifest/env/raw/source/root attestation/宿主回执全部绑定该SHA。各run独立run ID、root-owned attestation和原件目录，不能复用DEV身份或外部clock样本。普通向量OI_IT_CLOCK=0，只有G2的同run clock step可用专用VM guard；不得改Windows或用户远端主机时钟。原unit1800s/race2400s、Go vectors90m/collector5700s、clock3600s与质量工具期限保持；共享总预算不能保证各步骤同时用满，不增加产品宽限。

先完成活态PG、原生工具、32MiB tmpfs、固定离线库与缓存闭包，再依次运行并保全失败、核验/分析/发布/登记。当前G1/G3/G4仍Inconclusive，G2仍Blocked，预注册不晋级任何门槛。Windows默认中文编码（utf8_mode=0、cp936/gbk）下39项tools和358项experiments通过，10项平台skip；历史18run/13ITER精确复算，P2仍因G1未复证返回exit2。架构图/产品契约与已采用片段无本次语义变化，保持既有图；正式证据闭环后再同步状态。

### 首次本地 G1 反例与下一轮质量输入细化（编码前固定）

固定d9b884929aec6c8a14460876f13c14958239f652的G1-P1-R5-20261105T033158Z-5e02d0已实际进入正式运行。unit耗时174203ms、race耗时311873ms，均exit1且未超时；其余步骤仍在执行，最终原件与判定尚未封口。unit的metrics包以及race的metrics、TestLicenseExpiry、TestFinalCannotExtendPrepareDeadline、TestServeCommand失败均保留；未见DATA RACE警告不等于race通过，不提前生成完整覆盖率或成功结论。

metrics的原始栈精确落在Handler完成Render后检查原entry+200ms的主动ErrAbortHandler；活动归属测试直接在裸goroutine调用Handler，导致该合法中止击穿测试包。只能确定检查时已经到期，不能区分Render、Snapshot、调度或GC的耗时贡献。下一步仅修同名TestExporterActivityExcludesRejectedWriter：前两条POST/NoBody通过真实acquire、503 request_rejected和Flush门闩持槽；第三条GET仍真实429。断言503两次、request_rejected=2、rejected=1、success=0、render未调用及Active从1到2、第三条退出仍2、逐一释放到1和0；空态age及slots均零。失败也必须释放已启动worker的门闩并join，不能recover后跳过断言或判pass。原Handler/Activity、2槽、64KiB、200ms以及真实HTTP与G2竞争向量保持。既有render_exhausts_budget同时核到期后无响应/Flush且活动归零，不更改它的真实截止。

其他三项先按输入阶段区分，不能把共同出现推定为同一根因。LicenseExpiry在700ms心跳、100ms margin之后才执行完整v2打开；原失败只证明READY写返回后未观测到running，不证明未发送READY，也不能判断曾短暂运行后到期或从未运行。下一步以既有5000ms普通许可完成Running准备，再发送原700ms心跳并核AckSeq等于该心跳序号，Running必须先于发送；保存同代数的新实际本地deadline，核其位于发送/收到Ack分别加600ms的夹定窗口，不能误取初始5000ms许可。此后至终态不续期或替换该许可，仍验证到该原D后LicenseExpired、目标RST、后续Prepare拒绝和remaining=0。准备与被测到期窗口分开，不改变产品许可、margin、原700ms被测输入或5s等待上限。必要阶段日志只记录实际值和先后，不补造原run缺少的时刻。

FinalCannotExtendPrepareDeadline当前在PrepareTTLMS=200的TCP/mTLS/WS准备中未得到attach，尚未触及Final不能延长原截止的目标断言。下一步沿prepareMsg原3000ms准备预算完成attach/bind，保存原channel deadline且不得重写，再在该原D前最后200ms内发送原1000ms Final。发送前必须仍在D前，且本次Final独立截止会晚于原D，否则明确输入失败；attach/bind后在Agent.mu下保存原channel指针、generation及非零D。必须核对同一唯一CheckID的正向AdmissionResult及非空DecisionID，随后同锁核保留对象的finalCheckID/decisionID与该结果相等、generation及原deadline未变、decisionDeadline等于原D；不得因map中对象已被删除而跳过断言。若发送或处理已越D，明确输入失败且不重试，随后仍保留实际timeout/目标RST。本测试的fake OIS发送大于剩余D的1000ms字段，仅验证OIC的防御性min；真实OIS仍须按冻结规格发送受原D限制的剩余预算。不能从发送Final重新起Prepare计时，不能把准备已过期当成目标断言通过。该单测输入调整须先独立审查；正式G2向量、产品期限和总步骤预算保持。

ServeCommand的原固定sleep300ms不能证明服务已经启动。下一步仅用线程安全的日志writer解析完整真实msg=ois started记录并触发一次门闩，日志快照持同一锁；为真实启动输入设置5s观察上限，确认启动后再cancel，并保留原5s停止观察。原先的准备是固定300ms，不将新准备上限称为原有整体5s或总测试时长不变；unit/race步骤1800/2400s及产品关闭期限保持。提前Main返回或等待超时仍失败，继续要求正常退出码和真实ois stopped记录。不得并发读取strings.Builder、增加固定sleep、注入假启动或修改产品取消语义。失败路径取消并join本次worker，不误杀其他进程。

下一轮本地质量步骤显式使用quality_package_parallelism=1，仅使unit/race的实际argv增加唯一-p=1。全部cmd/internal包、测试、覆盖分母、race、包内并发、GOMAXPROCS、原1800/2400s以及9000/12600s共享上限保持；vectors、clock、fuzz及扫描命令不变。该控制减少同时运行的包级测试程序，不声称已证明CPU竞争是本轮唯一原因，也不能用串行通过改写并发失败。GOFLAGS保持空，不以全局环境暗改其他步骤。

该声明仅允许明确local-isolated-vm-v1 case中的整数1；新runner按声明构造命令，原raw start记录完整argv，分析器严格核唯一unit/race start及原全部参数、无过滤或第二个并行标志，并核实际go env GOFLAGS的唯一空值事实。缺失、错型、额外过滤或声明与命令不符均失败；无声明的旧case原行为和复算保持。先归档本次实际分析器及其相邻依赖原字节，再修改工具。四份已固定manifest保持，以新版本G1-P1-R6、G4-P2-R5、G3-P2-R3、G2-P2-R2承接新输入；原criteria、必需结果、资源、clock轮次与预算不减少。当前run完成封口并保存反例后，才修改运行中的工具源或开始新guest检查；修复先做聚焦验证、固定新源码，再新增正式run，不能拼接本轮通过叶。


### 控制许可输入见证与负载反例（编码前固定）

本轮两后端 TV-B2-10 实测终态分别为切断后1.9532166s/1.9129249s，违反原2–4s下界；失败保持。另发现G3许可剩余输入使用HTTP完成后的SampledNS，未扣整个采样请求耗时，不能作为保守下界。对应[唯一修正规格](control-license-witness.md)固定真实请求区间、同一业务流/进程/控制连接绑定、实际切断区间与独立读取终态、精确离线拒绝及原资源回收；不修改产品许可、heartbeat、已采用判线或共享SampledNS。新四manifest显式声明request-window-v1，六个控制叶独立复算；未声明的旧case、原run和summary保持。规格先提交回读，再实现和聚焦检查。

跨平台可复现构建已在原run通过（851.74s），随后TV-B4-06_Load的SQLite和PostgreSQL分别失败（90.83s/94.41s），多条流返回ChannelInterrupted。原Go JSON与三个服务日志已按后端独立保全；原因尚未确证，不沿用旧IdleTimeout解释。其余步骤仍继续，未把局部构建通过或覆盖率不完整的unit产物当作整体质量通过。


### 部分响应中断的下一项区分性诊断（编码前固定）

本轮两后端32×64MiB负载有六条部分响应后ChannelInterrupted，已完成原始Go输出重组、worker/Channel/目标tuple关联及固定d9源码审阅，仍不能确认首个失败操作。先按[首终态诊断规格](terminal-diagnostics.md)增加OIC winning终态、OIS有限操作错误与最终leg记录；不放宽原PING/IDLE、许可、负载或清理界线，不把资源回收成立写成业务通过。规格包含bind拒绝时实际Bridge归属、Completed后的许可优先级、Ping返回/cancel顺序、固定库阶段分类及worker退出日志顺序；独立复核已完成，新代码/测试尚待实施。只有新固定源码的聚焦观测才能区分候选，不能倒写本轮唯一原因。

### 首次本地 G1 完整封口与失败闭环（2026-11-05）

固定d9的唯一G1-P1-R5-20261105T033158Z-5e02d0已完整结束：unit/race/vectors退出1，其余五步退出0，全部未超时；宿主入口耗93m36.611s，来源监督complete/errors=[]。15原件逐字入库，232源码与固定Git对象相等，实际runner及8步骤进程组均gone/组空；[独立核验](local-g1-first-validation.json)与原摘要精确复算一致。来源完整不能替代产品通过，原95.2%缺失整个metrics包，不是完整≥95%证据。四项直接criteria为pass、超时/资源为fail，整体observed/fail，G1保持Failed。

[ITER-014综合分析](../../docs/evidence/p2-local-g1-first-evidence-014.md)按C56/E78/EDGE-049保留单测输入、B2许可相位、Load六条部分响应和附带G2-11/12准备/慢读/存储拒写反例。原18run/13ITER及历史validation保持，新增run与ITER同次保存；I3-P12/I4-P08/I11-P14/I18-P10已实际追加并逐字回读，ITER-014闭环完成，未推进alpha。图06已有准确的G1 Failed状态和关闭依赖，本次无图语义变化、不重渲染。

原运行封口后才接入新local质量调度和request-window工具，归档d9分析器及三个相邻依赖原字节。主树39项tools通过；experiments运行375项，其中10项Windows平台skip、其余通过，新工具下本run仍通过原件核验并精确复算为失败。单测输入、控制见证及首终态Go代码已静态编写/独立复核，尚未编译运行；失败即时诊断按a73a290先行规格实施中。先聚焦回归和区分性观测，再固定最终共同源码与G1-P1-R6/G4-P2-R5/G3-P2-R3/G2-P2-R2；不拼接通过叶，不直接盲重跑四套正式运行。


### 诊断实现与 Windows 开发回归（2026-11-05）

首次本地失败证据已由`37c898f11c12412546d53f42ff96727384f3cd98`完成四单发布回读和ITER-014闭环；16文件979060B远端逐字节一致。以下为新源码开发进度，不改该正式失败样本或其分析。四处单测输入修正、request-window控制见证、首终态/Ping有限诊断、六类存储写入返回诊断和G2失败即时记录均已实现，遵循先行13fdd25/c68c380/840b72e/a73a290规格，产品期限、权限、请求数和原判线保持。

[开发验证记录](local-diagnostics-development.json)绑定31份Go文件的实际字节：全仓编译、fmt及vet通过；完整Windows cmd/internal带真实PG单测1693项通过，1项Linux专属FD检查跳过，覆盖7222/7582=95.2519124242%。新增产品70个具名结果及修改既有6项逐项唯一PASS；控制/G2诊断和既有Active辅助合计112项PASS、无skip。静态工具发现并修复两处新helper风格/unused问题后，受影响44项再次PASS，其余源码字节保持。产品cmd/internal的staticcheck通过；完整Windows staticcheck仍因原非Linux clock guard必然Fatal而报告SA4006，保留非零原件，不记全量通过，待Linux完整检查。

独立复审发现的“统计失败在cancel后才冻结”与“缺失counter产生NaN导致失败事件丢失”已窄修，真实HTTP/context接缝与缺family/label/非有限值反例通过；原14处成功OBS形状、截止与请求次数不变。首轮无PG的93.4%/30skip、首次vet及staticcheck失败全部保留。OIS逃逸分析确认健康读写的局部诊断op不进入堆，只有首次失败发布副本分配；不据此推断吞吐或TCG性能。

后续先固定本批新源码，在专用Linux VM验证完整unit/race、实际六控制叶发射及原严格parser，再做双后端Load/G2-11/G2-12的一次区分性观测。正式G1-P1-R6/G4-P2-R5/G3-P2-R3/G2-P2-R2尚未创建/运行；失败原因未确定前不盲重跑正式套件。当前G1 Failed、G2 Blocked、G3/G4 Inconclusive保持，未推进alpha，也没有G5/G7部署证据。


### 固定7da的 Linux 开发验证与下一步（2026-11-05）

[Linux开发记录](local-linux-diagnostics-development.json)保存一次串行执行的实际命令、原期限、原件摘要和独立核对。完整产品unit/race各1697项唯一PASS，无测试skip/fail或DATA RACE；unit覆盖7250/7609=95.2819030096%，包含完整22包和metrics，race为19内部包PASS及3个无测试cmd包级skip。Linux全仓staticcheck通过。两质量步骤仅-p=1，四integration共用一次5700s截止；guest总1352.8493801s，未超时。323份回读原件23433311B逐SHA一致，708源码与固定7da一致，runner和7个测试进程组均空；12个产品二进制只有guest摘要，未冒称已下载。

六真实控制故障叶及严格同源解析通过：B2两后端保守区间分别[2.4892384,2.4898728]s和[2.4823818,2.4830486]s，G3 cut/blackhole四值2.4892045–2.4988029s，均在原2–4s内。相关14次资源检查42样本符合原条件。该窄检查不替代完整G3，不能抵消本轮其他失败。

Load仍FAIL：SQLite前五格50流成功，末格28成功/4中断；PostgreSQL82流全部成功。四失败经channel/目标tuple联接，OIC首胜者均leg_read/deadline且outer ctx active；不能唯一归为主动Ping写、CPU、TCG或数据库。十二次资源检查均通过，但业务中断保持失败。

G2-11六叶仍FAIL：两准备失败、三Active资格失败、一setup StoreUnavailable，没有完成竞争cycle或60s窗口。两准备失败都实际耗尽32个Drop；末样本backlog/pending不满不能反推早期所有采样。PG setup定位upsert_registry实际调用耗502.9008ms，调用前余499.5721ms，返回ctx deadline/余−3.3287ms；共享500ms不变，尚未区分底层原因。G2-12实体两后端各32实体和66次实际exposition复算通过；慢读两叶仍缺关闭前两次失败的完整200证据。native分别126.6287/136.4098ms，返回后仍余857.0482/816.8498ms，排除本轮native耗光1s；关闭后计数不能补算通过。

下一步按[Read阶段诊断先行规格](read-phase-diagnostics.md)增加有限分类，先以真实库原5s的PONG等锁/写帧反例核验接线。G2慢读另准备透传HTTP/net.Conn阶段观测，区别持续阻塞与短请求替换，尚未执行或改输入。冻结心跳、许可、导出200ms和所有原判线保持；本轮为普通开发调试，没有新增正式run/ITER或门槛通过。G1 Failed、G2 Blocked、G3/G4 Inconclusive、alpha/main与G5/G7未部署状态保持。


[HTTP机制诊断先行规格](metrics-http-diagnostics.md)固定一次Linux双profile观察：保留OIS/OIC不同Server配置、G2-12普通dial后RCVBUF1024和两个原1s起点，透明记录HTTP与TCP deadline/写入阶段。纯HTTP不伪造native OI交换，不作为G2通过；独立仪器先审查、冻结字节和窄测，再在新目录执行一次。


[G2-11准备历史先行规格](preparation-history-diagnostics.md)仅保存原来已经执行的采样与短路判定：512项窄记录、公共32项Drop表和增量wire游标；保留健康阶段成功、Set边界及失败部分样本。准备young为原200ms，关键操作前检查仍300ms。没有额外HTTP/快照、重试或新判线；只对采样到的状态归因。


### ReadPhase 与准备历史的有限实现检查点（2026-11-05）

按先行5dd605b/7ff35e9规格，已完成错误包装ReadPhase及G2-11有界准备历史，11份Go文件字节绑定于[开发记录](narrow-diagnostics-development.json)。ReadPhase仅输出六类固定值，OIC只记录winning leg_read，OIS只记录首读错误；真实库的PONG等锁与写入反例保留原5s期限，等锁返回后双向DATA仍可用。新测试操作和清理共用原10s绝对截止；旧Ping清理窗口不变。初版三包414项PASS/96.0%后，只有两份测试文件窄修；最终受影响wsio131项PASS，不把初版完整结果重标为最终复跑或全仓覆盖率。

G2准备历史只投影原采样，保存512项、32个公共Drop及实际短路/正常返回；统计错误仍在cancel前冻结。Windows原诊断及新增5根20子合计69项PASS，无FAIL/SKIP。两个实现都经独立只读复核，无剩余高置信阻断；全仓编译和vet、产品staticcheck通过，结构/采用/物料/迭代检查通过。P2门槛检查仍按实际阻挡退出2，G1 Failed/G2 Blocked/G3及G4 Inconclusive不变。

下一次开发Linux执行只选择三个受影响产品包unit/race、全仓staticcheck、全部69辅助项、原双后端Load和G2-11六叶；共用原总预算，新增诊断用于区分失败，不拼接旧通过叶。HTTP机制另用固定7da完整metrics包及go.mod/sum的逐Git blob子集，仪器采用执行前字节快照，两种来源明确区分；待该执行结束后再单独运行。没有新增正式run、manifest、ITER、门槛晋级或alpha合入。


### 第二轮本地诊断：已区分的失败与下一实现范围（2026-11-05）

固定eb64801的六阶段开发检查已完整结束并回读：unit/race三个受影响包各418项PASS，helpers69项PASS，全仓staticcheck通过；三包覆盖2112/2197，仅属局部检查。Load和G2-11退出1，全部无timeout或采集错误；715源文件无漂移、runner及六进程组均gone，166份回收原件逐字一致。详见[实际检查点](local-linux-diagnostics-second.json)，不能据来源完整改判业务通过。

Load的164条业务中161成功、3失败；三条均为OIC winning leg_read/deadline/active/pong_write_lock，定位到固定库自动PONG等帧写锁，尚未直接记录持锁者及持续进展。12格资源检查全部成立但不抵消中断。B1/B4每PING/两miss与每方向一F背压同时受约束；既有activity/forwarding实现解释不等于人类已采用新心跳语义，尚不跳PING、吞Read错误、改库期限或提交含糊选择。

G2-11六叶均失败：三叶的健康准备确实合格，后续19次五项full真时原query年龄都超过200ms；另外三叶full合格后未建立Active2/40ms输入。所有采样/短路与原wire逐项核对，没有漏接受完整合格callback，未完成cycle或60s。本轮没有StoreUnavailable。[固定六源预热先行规格](six-source-preparation.md)收束准备排序、三处query见证及slow起点后的严格目标计数；原32Drop/3s/young/query/60s等条件保持，独立审阅已补足final/critical见证，尚未编码或验证成功。

随后只执行一次固定7da的Linux HTTP机制，两profile均保存。40原件、30文件快照/12产品blob、进程和同一120s核验通过，真实exit1；slow请求不断更换ordinal，每次真实设置约200ms并在handler后清零，未捕获长期blockedWrite。关闭前失败counter均0，关闭后write_error为2/3；OIC另有observer迟到5.0212ms，原diagnostic_valid=false保持。这不证明G2全链根因，也不把标准库后台Read的timeout混为导出Write超时。

按[等字节Render优化规格](render-prefix-optimization.md)先减少固定schema重复前缀格式化；它不改变输出、两槽或期限，也不预告能使慢输入成立。新正式共同源、四门槛和alpha尚未产生；旧正式失败、Accepted对象及ITER-014保持，图06状态无需变化。

## H17 数据腿活性修复采用

用户已整体采用固定207cf91的DISC-010-R1，[原始问答](liveness-approval.json)与[实施计划](liveness-implementation.md)已保存；该修复承接三次pong_write_lock负载失败，尚未编码或复证。原完整窗口/IDLE/许可/尾部排空/清理、依赖维护与旧v1限制按[ADR-006](../../docs/decisions/ADR-006-data-leg-liveness.md)执行。G1失败、G2阻挡及G3/G4待复证保持，不再等待本选择；原开发失败和原始发布回执不改。

### H17采用回应与接线检查点（2026-11-05）

采用来源cf18ec及固定G2六源夹具6ac7249均完成远端回读；[三单采用回应回执](../../docs/issue-history/2026-11-05/DISC-010-adoption-published.json)已记录，原问答和scope字节保持。当前开始实施固定依赖写入口、wsio控制owner及三端接线，尚未形成最终实现提交或同源完整产品通过；上文“尚未编码”是采用时点的记录。OIS定向测试已覆盖双向DATA owner释放、正常Completed与阻塞控制写共存时原FIN排空、后到真实控制错误不替换首终因，继续独立复核。

六源夹具的固定6ac Linux开发运行已经完整执行并采集，正在严格复算原件：helpers、G2-11、G2-12分别实际exit0/1/1；G2-11六叶仍失败，G2-12慢读两后端GoPASS、实体数量PostgreSQL叶遇StoreUnavailable。只记录新失败事实，不将开发局部结果称为G2通过；正式旧run/ITER、G1 Failed/G2 Blocked/G3及G4 Inconclusive保持。

### H17三端接线与验证适配（2026-11-05）

底层固定183b已推送回读，[端点接线开发记录](liveness-endpoints-local.json)保存438项Windows单测通过、vet通过及独立审阅；真实TLS关闭阻塞、控制写错误、取消回调join和首次关闭D均已有回归。覆盖仅四个受影响包，不作完整95%或Linux/race结论。[183b独立Linux基础检查](liveness-foundation-linux.json)已结束并完整核验：三个root包unit/race各467项通过；完整依赖unit因原DNS后台协程未退在TestMain失败，race在原120s开发watchdog结束时仍执行原mask循环。全部原件及原失败保留，不能把具名项PASS当包级通过。

[嵌套模块正式验证计划](liveness-evidence-adapter.md)已先行固定974aad0并回读，正在补源码清单、独立测试、补丁完整分母及漏洞版本映射；旧分析器已逐字归档，19旧正式run仍可原样复算。G2六源运行完整事实已在[six-source-linux.json](six-source-linux.json)保存；根据其中两叶final合格后被exporter准备耗老的事实，按[首轮导出输入前置计划](six-source-export-first.md)修复准备顺序，不增加请求、重启D或降低判线。另四叶preload及PG首次500ms超时仍未解，保持失败。最终同源正式运行和alpha集成尚未开始。

[导出输入前置开发记录](export-first-local.json)保存111个Go辅助节点、115项相关Python检查及独立审阅；首轮把原exporter输入建立移到final健康见证之前，critical见证仍独立采集，3s/200ms/300ms/60s判线与六叶库存保持。这只证明夹具与解析接线，尚未执行新六叶业务。当前正式适配同时封闭遗漏整组依赖检查、精确覆盖分母、零退出非空OSV以及13/16步本地回执；中途失败材料与质量通过分别核验，完整新源码验证待继续。

### 完整依赖环境与阶段发布（2026-11-05）

3fc5db8实现与适配已固定回读32文件；[三单实施回应](../../docs/issue-history/2026-11-05/TASK-007-liveness-implementation-published.json)已发布并逐字核验，50条旧评论与正文/元数据保持。随后[完整BadDials根窄检查](resolver-local.json)在原183b源码取得9节点和包终结PASS，实际记录cgo选择及原环境；821源码、18原件、两个新进程组及原D核验通过。完整依赖unit/race仍未取得新通过，旧失败保持。

[解析器环境增量](resolver-environment.md)及[同源Linux开发计划](liveness-complete-linux.md)先行固定283a769并回读；仅嵌套unit/race接入子进程环境，根与其他工具不继承，原完整库存/预算/产品判线保持。新最终源码的完整根/依赖质量、Load、G2-11/12准备执行；正式G1→G3/G4/G2及既有主机SSH/Bash验证继续按原前置推进，无新增资源确认请求。

### 完整活性开发轮的实际结果（2026-11-05）

固定2a81的[16步开发轮](liveness-complete-linux.md#单次执行后的完整开发结果)已完整收回并独立复算，[结构化记录](liveness-complete-linux.json)绑定857源码、322原件及17个已退出进程身份。完整root race、两fuzz、根静态/漏洞、完整依赖unit/race及其静态/漏洞均通过；Load的164条流完成5330MiB，12次资源回收成立，原164条CloseError仍保留。root unit因TLS测试仅接受nil而失败，完整覆盖7718/8131低于95%；G2-11六叶准备失败、G2-12 PG实体因先前registry原500ms超时而拒写。仅两个G2-12慢读叶严格通过，不能称G2或全部质量通过。

后续38f22cf仅修正关闭测试判据并增加原D/回调join验证，[Windows完整wsio](liveness-close-tests-local.json)211项通过、403/423覆盖，独立复核及全仓vet通过。Python425项中415通过、10个原OS跳过，tools39项及记录检查通过；P2检查仍因真实G1 Failed退出2。不同来源与Windows覆盖不合成Linux通过。G2准备相位与PG写路径继续按原契约修复，没有新产品决策、正式run/ITER、alpha合入或目标主机部署。

[四单完整开发轮回执](../../docs/issue-history/2026-11-05/TASK-007-liveness-complete-published.json)保存实际发布和逐字核验。固定596f335的[probe-first输入](six-source-probe-first.md)、[registry有界插入](registry-batch-insert.md)与[下一Linux窄检查](g2-repair-linux.md)已在编码前推送回读；当前按该范围实施，不把候选效果或局部检查预记为成功。

### Probe-first 与批量注册开发检查（2026-11-05）

[Probe-first记录](probe-first-local.json)固定首8Set→真实P2/Q4预热→一次32Drop arm→末8Set的新夹具及同库存final/critical解析。Windows162个Go辅助节点通过；完整Python436项中426通过、10项原OS跳过，tools39项通过，19旧run/14ITER仍精确复算。独立审阅发现的前缀/注册串配/协议类型漏检已修正，旧模块571569B逐字归档；原3s/1s/200ms/300ms/60s判线保持。

[Registry记录](registry-batch-local.json)保存PG每块64行参数化INSERT及完整31节点双后端新库存；Windows实际完整store105PASS/20SKIP，新增16个SQLite节点通过。PG真实执行尚待同源Linux，不把Windows编译和部分覆盖称为PG性能或G2通过。两项普通契约内修复已完成开发复核，接续原六步Linux反馈；G1 Failed、G2 Blocked、G3/G4 Inconclusive保持，无新正式run/ITER或alpha合入。

### 固定61396fe的Linux共同反馈（2026-11-05）

[六步实际结果](g2-repair-linux.md#单次固定61396fe的实际结果)与[完整记录](g2-repair-linux.json)已核：两产品包unit/race各374PASS、全仓vet与162辅助节点通过，registry新增31节点（含真实PG15、SQLite14及2公共根）及原PG32实体场景通过；G2-12四叶业务/资源严格PASS。G2-11六叶均通过新预热与arm32，仍在final_selection失败，尚无控制竞争或60s成功证据。两叶未采到backlog16，另四叶共8次full真时实际query投影均超过200ms；不是32Drop耗尽，原3s仍有1.4–1.875s余额。

878来源、211原件加manifest、宿主0/1/0退出及原D完整，六child空组、runner身份Gone；监督完整不改整轮exit1。后续完整质量暂不执行，先按真实dirty/query/Changed时序和原验收来源继续定位；旧正式门槛、失败原件和alpha/main保持。

[四单结果回执](../../docs/issue-history/2026-11-05/TASK-007-g2-repair-published.json)已逐字回读并保留68条旧评论。下一项按[末八慢源后移先行规格](late-slow-input.md)只调整输入顺序：保留额外八源在原exporter准备期间的真实健康执行机会，完整资格后才设置为慢；保持200/300ms、3s/1s、32Drop及完整60s。不预告效果，先固定规格与反例，再做新来源完整六叶的区分验证。

末八慢源后置已完成原契约内接缝实现：Windows184个旧新helper节点全PASS、integration vet通过；Python33准备用例及完整440根（430PASS/10原skip）通过，19旧run/14ITER复算一致。实施先行来源为6e00b1b，原613失败及八模块分析闭包保留；[实现与本地检查点](late-slow-input.md#实现接缝与本地检查点)和[原始检查摘要](late-slow-input-local.json)承接。真实双后端六叶待固定新提交后运行，无正式门槛晋级或alpha/main改动。

固定cc653e末八慢源后置已完成唯一Linux两步反馈：184helpers全PASS，G2-11六叶均在final_selection失败，未进入critical或60s；892源文件及120收集文件精确，监督完整不改产品失败。[实测与反例](late-slow-input-linux.md)、[原件和独立分析](late-slow-input-linux.json)保留全部结果。周期刷新资格与显式dirty记账的差异仍需先行论证，未改原判线或产品调度。

本轮四单阶段回执已完成：[I3-P19/I4-P11/I11-P20/I18-P16发布回执](../../docs/issue-history/2026-11-06/TASK-007-late-slow-input-published.json)保留72旧评论/四正文及7固定链接逐项核验；没有借候选方案变更旧采用或门槛。

[周期待办实施规格](periodic-work.md)先行固定：仅 janitor 在原 now 下显式记账已有到期工作，以 private provenance 位保持 Changed 合并计数。先以固定旧路径差分验证 query/ID/期限不变，再做同源 Linux 五步反馈；该计划不保证 G2-11 成功，不扩统计公式或重判旧失败。

周期待办实现已完成Windows开发检查：[实际记录](periodic-work-local.json)保存完整relay276PASS、4根65新增节点、vet及7个有效错误变体。原查询/ID/期限与计数由固定旧路径差分核验；局部1873/1963覆盖不替代完整95%。下一步固定源码后执行同源Linux五步，尚无新G2业务或正式通过。

固定3be014a的[Linux五步结果](periodic-work-linux.md)与[原件/独立分析](periodic-work-linux.json)已完整收回：unit/race各276PASS、vet及184helpers通过，六G2-11仍失败。902源和142回读文件exact；四final失败叶Full69/100但young仍超200ms，两叶越过final后分别失去导出重叠、ABORT超原cycle D。无完整60s/资源check，不判产品500ms/SQL或资源泄漏；继续原契约内定位，不晋级正式门槛。

[本轮四单发布回执](../../docs/issue-history/2026-11-06/TASK-007-periodic-work-published.json)已完成逐字回读，保留76旧评论及四单元数据。失败事实与timeout请求归因限制均已回应；下一步仍是原契约内输入时序诊断，不凭局部PASS推进正式门槛。

[联合输入失败诊断规格](joint-failure-diagnostics.md)先行固定：只保存原Stats里的exporter活动标量、原角色请求边界及critical中实际发生的native/GET/DELETE/Wait事实，不改请求顺序或判线。3be原两条final合格样本的exporter Active实际为0；前置429不足以证明后续持续重叠。后续用独立原生Linux环境区分条件影响，省GET候选本轮推迟，旧六叶失败与门槛保持。

联合失败诊断已完成[Windows局部验证](joint-failure-local.json)：225唯一节点PASS（旧184完整保留，新增6根35子41）、integration vet通过，原成功cycle事件字节保持。独立审阅发现OIS错误observer异常会二次通知，先保存两个真实失败，再以单次通知位修复；最终helpers03绑定四文件，旧01/02保留。产品、协议、Python分析器及依赖未改；现固定源码后执行原生隔离构建/辅助回归/六叶，不预记业务或正式门槛通过。


### 原生联合诊断开发轮（2026-11-06）

固定9c229d2在已授权非生产主机完成七步：预构建五步及225 helpers通过，G2-11六叶2 PASS/4 FAIL，完整严格结果仍FAIL。[逐叶结果](native-g211-result.md)与[原件摘要](native-g211-result.json)记录SQLite round1的118 cycles/60.385171331s及PG round0的117 cycles/60.072581437s，各原2s资源通过；其余四叶和根/父失败保持。三个critical失败的前后Stats已显示exporter Active0，真实GET/DELETE仅数毫秒且正常返回；PG1未采到Full与Active2的合格交集。

910源码、164清单原件加manifest逐字核验，七child已退出且组空，runner精确身份Gone；私有PG已按身份停止，最终日志完整。309.101312179s与所有原共享D核验通过不替代业务通过。下一步先固定beforeWrite联合输入与异常清理规格，原final200/critical300、429、3s/1s/32Drop/60s/2s均保持；省GET暂缓。没有新正式run/ITER、完整产品质量或门槛晋级，alpha/main保持，继续已授权首版交付。


[beforeWrite联合输入先行规格](before-write-input.md)将首轮末八Set和必要的Full/young准备放在原两socket连接后、GET释放前；原Zero/released贴真实放行，后置final200与critical300独立实采、原429/60s及所有预算保持。新增前置Stats使用同一有限历史与更早原dialCtx D；panic/Goexit必须在外层owner尚未接管时冻结并回收。旧9c分析闭包先归档，新声明不兼容接受旧排序。该候选尚未实现或运行，按既有授权继续，不新增产品确认。

下一次同源原生六叶按[依赖复用先行规格](native-g211-reuse.md)准备：已验私有Go/PGprefix在同机独立复制，新cache/PGdata/凭据/UUID与新parent期限隔离，保留旧原件；原完整七步和预算不变，未开始下一次现场运行。上一轮四单回应已发布并回读，见[原生结果回执](../../docs/issue-history/2026-11-06/TASK-007-native-joint-published.json)。


### beforeWrite夹具与严格分析实现检查点（2026-11-06）

按814c1d4先行规格完成8个Go测试文件与Python适配，[本地实施记录](before-write-local.json)保存257个helper节点全PASS、原225逐名保留、新增32及vet通过。真实环回验证panic/Goexit及observer再次异常不会提前发GET或遗留socket/done，原首异常和清理前context保留。前置见证深拷贝，后置final200/critical300独立采集，原dialD与60s保持。

Python139个G2相关测试通过；其后仅新增合法query批正例分支并重新通过41个准备测试，分析器字节未变。四个故意错误变体被18个断言失败抓住；9c八模块580244B逐字归档，19旧run/14ITER复算精确。三次Go初始失败均保留。两代理交叉审阅及root复核无剩余已知阻断，产品源码仍等同3be；新原生六叶和完整质量尚待执行，没有门槛晋级或alpha推进。
