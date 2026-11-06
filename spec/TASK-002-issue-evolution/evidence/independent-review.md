# 独立只读行为验证报告

本报告保留首次运行所见结果，不随后续修复倒改。任务范围：修订后的Discussion SDD第9步独立任务；只读仓库与GitHub，不查看主流程测试、验证报告或已有草稿，不发布评论、不修改仓库。仅在本目录写临时输入、运行输出与报告。不是OI产品实验或人类批准。

## 结果

流程可以帮助执行者从“独立转发工作器”找回旧尝试，并在核对原文后作出有依据的继续研究建议，没有把B写成已被实验否定。当前任务条件足以重新讨论针对进程崩溃的B隔离方案；100条合成长连接不足以证明性能、生产代表性或心跳预算已完整。独立草稿见`issue-stage-draft.md`，含具体部署、正常/失败链、A/B/C取舍、最小证据计划及准确沿用/替代范围。

同时发现一个可复现的数据语义问题：种子记录把未知条件编码为非空字符串，工具误把它们报告为已知且相同。工具的警语、原文复核和人类判断使本次没有错误冻结探索，但这处默认输入会增加误用风险，不能称为已经完全消除重复/误拦。

## 实际读取与运行

读取了AGENTS、Discussion SDD、Architecture Partner及其完整prompt、KB入口、loom-oi理念、SDD项目收束、Issue规范、结构规范、阶段模板、内容评审、演进工具说明、kb/lineage、演进README/初始记录；为证据计划读取Evidence Lab和实验契约。原阶段读取I15-P01/I15-P02实际快照，以及当前通道设计和架构/模块相关契约。没有读取`tools/test_issue_evolution.py`、`tools/test_tool_entrypoints.py`或主流程报告/草稿。

工作树HEAD：`27523aa82b205b2ba29d5ba6e4b8d501eaac9047`；指定基线：`0efb165e988f138936b322affb36164c63223bbe`；分支：`feature/TASK-002-issue-evolution`。本轮评审对象包含工作树未提交修改，首次数据输入指纹为`b227f08aebab1ce789f1c4e4d371694f00e0da0cdebab5df7cf408a3841ee141`。工具/记录/技能内容哈希另见`source-fingerprints.json`。

实际运行命令（cwd为仓库根目录；JSON参数文件均位于本报告目录）：

```bash
git status --short
git rev-parse HEAD
git branch --show-current
python tools/issue_evolution.py validate
python tools/issue_evolution.py validate --base 0efb165
python tools/issue_evolution.py search '独立转发工作器' --issue 15
python tools/issue_evolution.py trace A-CHANNEL-INTEGRATED
python tools/issue_evolution.py trace A-PROCESS-FIRST
python tools/issue_evolution.py trace L-PATH-BEFORE-PROCESS
python tools/issue_evolution.py impact C-PATH-LOAD
python tools/issue_evolution.py graph --issue 15
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/stated-conditions.json
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/remaining-unknowns.json
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/copied-unknowns.json
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/explicit-unknowns.json
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/same-conditions-replication.json
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS /workspace/scratch/d07f11535a0e/append/validation-agent/worker-mechanism.json
python tools/issue_evolution.py compare A-PROCESS-FIRST /workspace/scratch/d07f11535a0e/append/validation-agent/stated-conditions.json
```

命令退出码均为0；详见`commands-results.json`保存的输入命令、stdout/stderr和退出码。独立读取在线#15正文和所有3条评论、#4的H8原回复，使用GitHub只读fetch接口，实际返回分别保存在`issue15-live.json`、`issue15-comments-live.json`、`h8-live.json`。当前可见最新阶段是I15-P03。未执行任何平台写入。

## 旧结论的适用范围

| 来源 | 核对结果 | 对当前假设的约束 |
| --- | --- | --- |
| I15-P01正文和交付评论 | 已提出B独立中继；明确没有产品负载实测，也明确硬崩溃隔离要求可使B成为替代 | 工作器不是新机制；未测不等于证伪，保持正常/失败及不重放约束 |
| I15-P02正文和原回复 | 把路径纳入共同问题，取代A/B先行确认；H/M/部署仍未采用 | 路径可变时先查中继负载；本验证固定R可继续B研究，不等待穷尽H |
| I15-P03 | 允许在路径/负载/硬故障明确时再比A/B/C，不永久排除进程隔离 | 本轮满足重新讨论条件，但合成并发数不补齐载荷速率/预算或真实拓扑 |
| H8原回复 | plumeink于2026-11-01明确K3按推荐方案，K4支持PG/SQLite | 沿用R方向，不能借此批准B或新预算 |

建议在同一#15追加有条件阶段，而不是修改旧评论、重开旧单或制造独立P2P单。草稿以P03为前序，沿用P02研究依赖，复用P01的B，新增条件及证据缺口；它不supersede整套旧路径判断，也不记录为真实人类需求。

## 反例与工具/规则问题

1. **未知条件被表示成已知的相同值（应修复）。** 初始记录的`L-PATH-BEFORE-PROCESS.conditions`为`path=未确定`、`load=未测量`、`failure_goal=未固定`。我没有读取主测试，只照当前记录手工构造`copied-unknowns.json`；evaluate返回`reuse_prior_reasoning`、三项same、`unknown=[]`。将语义相同的未知改为null，返回`insufficient_context`且三项unknown。这与README“重要条件未知用null”相冲突。正确含义只能是复用“这些条件还不清楚”的提醒，不能说相同部署前提已核实。建议修正未发布种子资料；已发布记录以后用追加更正，不覆写历史。旧未知→当前明确应单独表达为新明确条件，不能伪造已知旧值。
2. **合理复验没有被自动禁止。** 同一未知字符串输入只把purpose改为replicate，得到`replicate_with_plan`。这证明命令保留显式复验路径；但未知仍须补齐，工具返回并不能证明复验目的或预算合格。正常evaluate返回reuse也明示不阻止探索。作为行为验证，本执行者没有把两个输出当否决。
3. **别名可命中旧问题，不代表机制同一。** `独立转发工作器`命中`A-PROCESS-FIRST`，机制为`process-boundary-first`。这实际上代表旧错误分析次序，不是B的进程隔离机制。核对原文后我仅用它检查重启条件，没有把新讨论写成又要进程先行；手工声明真实部署机制`separate-relay-worker`返回`different_mechanism`，也不证明历史不存在同一B。建议README显式说明此例，未来按需要补正确机制节点；无需增加模型或数据库。
4. **单一suggestion需结合细项和准确替代范围理解。** 三项声明条件对lesson返回reevaluate；加入未知拓扑/速率/心跳预算后变为insufficient_context，但changed仍保留。对旧attempt返回review_evidence，因为E-OLD-NEW替代关系存在。这些不是冻结讨论：读原边可确认仅替代分析次序，不否定B。规则文字已足够支持这个判断；执行者必须真的读原文。
5. **Git保真边界如实暴露。** `validate --base 0efb165`返回preserved_files=0，因为基线没有演进记录。这不证明当前新文件未来不可修改，更不证明GitHub原文永久保真。原快照的字节校验成功与完整历史是不同保证。

## 完成边界

已完成真实工具调用、来源读取、独立判断、可评审的Issue阶段草稿，以及自行设计的反例。没有运行OI性能/故障实验，没有产品新样本或实施，没有更改repo/GitHub，没有生成真实DISC/阶段节点，没有用退出码或本报告代替人类批准。未运行主流程测试。阶段草稿不适用新单六节格式，亦未伪造DISC记录去跑SDD；其内容按已有阶段模板和三项独立内容阅读检查组织。

主流程收到初始缺陷通知后表示拟修复；本报告仅记录修复前所见。修复后的复核应另存文件和指纹，不覆盖这里的失败证据。


# 修复后独立只读复核

原失败证据保持在`report.md`及`commands-results.json`，未改写。收到主流程修复通知后，重新只读运行validate、指定基线validate、原别名search及7条原compare调用，所有命令退出0；逐项输出保存在`recheck-commands-results.json`。没有查看或运行主流程单元测试，不以主流程报告的测试数量作本复核依据。

复核数据指纹：`2677d281ed5bd43fcdb8caad64e2c0bef8e2171077b930da04921de84460612d`。工具、记录和README哈希见`recheck-source-fingerprints.json`；HEAD仍为`27523aa82b205b2ba29d5ba6e4b8d501eaac9047`，评审包括当前未提交工作树。

| 复核输入 | 实际输出 | 独立判断 |
| --- | --- | --- |
| 由search读取当前初始记录 | path/load/failure_goal均为null | 默认记录不再教使用者把未知写成已知字符串 |
| 明确R、合成100并发长连接、转发崩溃不影响控制心跳 | reevaluate；3项changed的kind均为newly_specified | 可以重新评估；没有伪称已知道旧条件与当前不同 |
| 同上，但拓扑、速率、心跳期限保留null | insufficient_context；新明确的3项和仍未知的3项同时保留 | 不吞掉有效新增条件；不能下通过判定，但可以继续有条件设计 |
| path/load/failure_goal均为null | insufficient_context；unknown=3；same为空 | 初始所见“未知相同→reuse”在正确输入格式下已消除 |
| 旧attempt A-PROCESS-FIRST与明确条件 | review_evidence；E-OLD-NEW仍列为替代 | 仍须回读替代范围，不自动否定独立工作器 |
| 声明separate-relay-worker机制 | different_mechanism | 不自动套用研究次序lesson；也不证明历史没有B部署机制 |
| 保留原错误字符串输入 | reevaluate；字符串被列为newly_specified | 工具仍按声明值工作，不具备自然语言未知值识别；该输入违反README的null约定，不作为真实条件已齐备的证据 |
| 原字符串输入purpose=replicate | replicate_with_plan | 显式复验仍允许；它不能认证实际复验目的或预算完整 |

README现已明确“独立转发工作器”是回找旧专项的候选别名，process-boundary-first是旧分析次序，不能视为全部隔离方案的机制ID。这个说明直接覆盖了本次易混点，仍保持仓库脚本与JSON的轻量范围。

结论：针对独立行为任务发现的默认未知值问题，修复已在本次实际调用中得到核实。流程能够支持本场景合理重评，并将条件缺口留给具体证据计划；它不是自动语义裁判。原始错误字符串仍可误导比较的边界需要如实保留，后续执行者须按null约定录入并核对原文。当前6节点/4关系的覆盖不足以证明全项目漏检率或误拦率，首次导入基线的preserved_files仍为0，不能外推历史完整或不可变保证。

`issue-stage-draft.md`保留了首次独立求解形成的完整草稿。其部署建议与证据缺口经本次修复不变；若后续引用工具输出，应把“条件改变”准确读为“先前未知、这次明确”，不能用工具提示制造旧时已知事实。本次仍未发布Issue、修改仓库、运行产品实验或替代人类批准。


## 独立任务产出的完整草稿

### I15-P04（独立验证草稿，不发布、不占用编号）｜在固定R路径与崩溃隔离目标下重评独立转发工作器

阶段类型：有条件方案修订。成熟度：方案推演；采用：本阶段未采用；产品实验：未运行。

**触发与边界**：本草稿来自只读行为验证的假设：“重新提出独立转发工作器，因为以前没有真正测量；路径固定为中继，负载为合成的100并发长连接，转发崩溃不得中断控制心跳。”它不是新收到的用户需求、真实Issue评论或实验结果，不生成产品DISC、需求编号或批准记录。在线回读时#15已有I15-P01—P03；如未来转为真实讨论，发布前须重新核对编号和来源。

**前序与准确关系**：承接[I15-P03](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6607873200)声明的重试条件，沿用[I15-P02](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6605914076)“路径→实际中继负担及故障→部署”的判断；复用[I15-P01](https://github.com/wyw-ai/loom-oi/issues/15#issuecomment-6603765526)对应旧正文中B同机拆进程的正常/失败机制。原阶段替代的是先冻结A/B、后研究路径的确认次序，没有实测反驳B。本轮不把旧B换名计作新发明，也不推翻仍有效的方法判断。

**场景、变化与推荐**：在验证场景中，Mac侧合成客户端经R中继访问ECS侧OIC连接的受控服务，建立100条长连接；OIC的控制WSS直接接入OIS控制进程。正常时仍必须接对获准目标；转发进程崩溃时允许受其影响的数据流中断，但要求控制连接及心跳维持。路径已经固定为R，因此没有必要先完成H/P2P研究才能讨论这个明确的进程故障目标。100并发只是输入规模，尚未规定每流速率、双向比例、持续时间、TLS与慢消费行为，不能声称已代表生产负载或已有性能收益。

推荐把**B：同机独立中继工作器**作为该条件下的候选，针对崩溃隔离先设计验证；A作为行为及成本对照。该推荐首先来自故障边界，不以吞吐提升为前提。具体机制如下：

1. OIS控制进程持有主体认证、目录/策略、接入epoch/generation、控制WSS与心跳；中继工作器只持有配对数据socket、有限许可与缓冲。心跳不能绕经工作器，也不能依赖工作器IPC排队或持有的全局锁。
2. Open保持既有检查顺序：控制核对主体、Exposure/revision、接入代数和策略，OIC核对本地允许项并拨号；控制为两条数据腿固定同一中继实例/实例代数，签发角色及Channel绑定的一次性附着凭据，工作器通过认证内部链取得许可。两腿齐备并通过最终准入后才能转发；工作器不得改目标、自行续权或成为第二策略权威。
3. 工作器独立启动/重启，服务管理与进程组不把工作器退出传播为控制进程退出；工作器不可用时，控制有界标记承载不可用、拒绝新的数据Open，但继续处理心跳及管理请求。控制不得同步等待清理100条流而阻塞心跳。两进程分别限制资源，同时按相同总资源做A/B比较。
4. 工作器崩溃导致其数据流失败，控制记录传输中断；旧请求可能已经执行，不能重放。新工作器使用新的实例代数，拒绝旧票据；恢复访问须新Channel、重新授权与准入。心跳继续不表示目标Ready或业务成功。若控制—工作器失联，停止新Open/续权，旧流按有限许可结束。

| 选项 | 在本假设下的判断 | 保留的代价/边界 |
| --- | --- | --- |
| A：同进程分模块/预算 | 可作为最低复杂度及测量对照；若注入的是进程致命退出，共进程无法满足控制继续存活的目标 | 仅把可恢复错误捕获成功，不能冒充致命崩溃隔离；若未来撤回硬故障要求，A仍可能合适 |
| B：同机独立工作器 | 推荐进入有条件的设计/对照准备；只针对工作器进程退出提供可检验的隔离边界 | 新增内部认证、实例绑定、撤销/期限传播、独立服务管理和监控；共同主机、网卡、系统级OOM仍可击穿两者 |
| C：跨机中继池 | 本轮不需要默认引入；只有主机故障域或单机容量要求出现时再比较 | 增加入口、证书、路由、分区与运维；不自动解决控制或数据库可用性 |

**证据与结果**：已读取旧阶段实际快照、当前完整Issue正文及3条评论，并读取[固定885cb7a通道设计](https://github.com/wyw-ai/loom-oi/blob/885cb7a50d3b9a8a1f20a445654e3c9039a1eedc/docs/designs/ois-control-relay.md)。P01与P02均明确没有真实OI部署/负载实测，P03没有新增实验；所以“过去没真正测量”与这些范围内的来源一致，但不能外推成项目历史从无任何实验。保留R的[H8原确认](https://github.com/wyw-ai/loom-oi/issues/4#issuecomment-6564934039)，该确认不包含本阶段B部署或心跳数值预算。现有来源支持继续讨论，不支持宣称B已达标。

仓库检索能用“独立转发工作器”找回旧讨论；其登记机制`process-boundary-first`指旧分析次序，不能直接作为本轮隔离机制身份。本次用它对照重启条件：三个已声明条件返回`reevaluate`；补上尚未知的拓扑、载荷速率及心跳期限后返回`insufficient_context`，同时仍列出已知变化。对旧尝试节点返回`review_evidence`，因为有替代关系；核对关系原文后，替代并未否定工作器。以上都是查询提示，不是批准或拒绝。

**还需要的最小证据**：未来若真实需求成立，应先固定实现提交、构建/运行版本、实际拓扑、TLS与缓冲配置及总CPU/RSS/FD预算；把100条合成长连接的时长、每流速率/方向/慢消费、负载生成器和种子写进预注册。控制心跳需明确周期、超时阈值、允许的最大应答间隔，以及“不得中断”是否要求控制WSS不重连、接入generation不变。未定义这些数值时可以继续设计，但不能给通过判定。

最小对照包含A/B无故障负载，以及100条数据流已建立后的工作器进程致命退出（如明确目标PID的SIGKILL）。独立采集器记录控制PID、控制连接/代数、心跳发送与接收时间、最长间隔/漏答、数据流失败、CPU/RSS/FD及吞吐；不能只看工作器自报“隔离成功”。A的故障必须等价覆盖承载进程，不能只抛一个被捕获的协程错误。预注册重复次数、交替顺序和判定后再运行；保存失败、原始时间戳和环境，分别报告故障隔离与性能结果。

随后检查最小恢复/授权反例：旧实例票据不能附着新工作器；中继重启不恢复业务载荷；权限撤销/控制分区仍受有限许可约束。共享主机停机或网卡故障是B仍失败的整体反例，不能将工作器崩溃测试外推为HA。本轮没有执行上述实验，也没有产品样本、ITER或门槛晋级。

**停止与重启**：若B在限定故障注入中仍中断控制心跳，保留失败结果，先定位服务管理、共享锁/IPC或资源争用；不以“已经拆进程”宣布完成。若满足崩溃隔离却没有吞吐改善，可以只保留隔离收益。资源/运维成本超过认可预算时重新比较或缩小支持范围；路径、真实负载、主机故障要求、实现版本或有明确目的的复验改变时再开新阶段，不因换名重试，也不因旧阶段被替代永久冻结B。

**项目回写与确认范围**：保持获准目标、业务状态外置、有限许可、准确绑定和不重放；本假设仅收窄R路径下的进程故障目标，H/M研究和既有人类采用不被覆盖。真实后续若形成采用，再更新通道设计、架构§8.4和OIS模块；#3/#4提供共同授权/恢复约束，#8/#11承接资源与验证缺口，不另建平行P2P问题。当前仅输出只读验证草稿，未改仓库或Issue，无产品决策请求；未来真实评审须确认故障范围、心跳/成本预算和B的投入边界，不能由本草稿或工具退出0代替人类采用。


原运行记录、临时条件输入和来源指纹集中保存在[independent-runs.json](independent-runs.json)，按原文件名读取。原命令中的临时绝对路径是当次实际执行位置，不是当前仓库依赖。
