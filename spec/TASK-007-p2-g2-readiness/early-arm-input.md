# G2-11 在原准备起点提前安装唯一 Drop 批次

本页固定夹具输入排序修正，依据[固定dd62原生结果](native-pending-identity-result.md)及pending身份独立诊断；不是新产品采用，不保证六叶通过。先行规格提交、推送、逐字回读后按既有Autopilot授权实施；源码和监督审查完成后由主代理执行一次新native完整六叶。所有旧原件、原同源结果和父/完整G2失败保持。

## 1. 目的与不变项

本轮SQLite三叶在P2/Q4接受、arm之前已实际经过18/20/25个query，分别集中于约61–63ms窗口；PG三叶各4个。原基线及preload首尾实际已经空，单纯再等空基线缺少区分力。本候选只把原32Drop提前到原prepareStart之后、首8Set之前，尝试保留这波目前已被正常归约的查询，不通过暂停进程、hold控制流或注入消息制造同步。

原prepare3s、实际helper入口起1s与原socket/cycle截止、16源各一次Set、两批8Set、单批32Drop、8 query/原500ms、前置80/final200/critical300、16/P2/Q4、exporter两socket/2048GET/200ms、原40ms与连续429资格、native→GET→DELETE→Wait/释放顺序、500ms控制判线、最后Set后完整60s、原cycles及2s资源回收全部保持。没有新增HTTP请求、轮询、sleep、重试、并行采样或产品变化。只新增下述一次本地既有Snapshot观察，真实成本计入原3s。

pending身份日志仍是显式开关下的诊断，不成为新通过条件；新源码默认无诊断元数据。真实负remaining、缺失/迟到和部分事实不得补造或参与豁免。

## 2. 精确准备顺序与唯一所有权

1. 沿原newEnv、initial采样、一次victim准备；原`prepareStart`及`D=prepareStart+3s`不重起。初始化唯一preparation失败所有者，先具备freeze/取消/清理责任再操作门闩。
2. 在尚未首8Set、尚未preload采样时，对原lab-a proxy调用**一次**现有`Snapshot()`；检查其原Overflow/Failure和连续ID，立即记录真实返回`resourceNS()`。从该已返回对象取最后event ID，空Events取0。不得稍后重扫最终wire回填，不调用`g2Mark`再额外Snapshot；可复用其取末ID逻辑。
3. 紧接该返回后记录原`drop_arm_started_ns`，沿原函数逐一安装**恰32个**已有精确match：lab-a/control/oic_to_ois/observation_snapshot，仍执行原Drop，关键消息不命中。原每次安装前/最后返回均检查同一D。只安装一次，不按观察状态补装或取消后重装。
4. 全批正常完成后才执行原首8Set：两健康＋固定六慢，仍每源一次。随后沿原10ms节拍及同一3s执行`preload_probe`，只要求原P2/Q4、16 jobs/capacity及真实PID/epoch/gen/Register/Ack inventory。Full分量继续仅诊断；不恢复旧preload Full/young门槛。
5. 接受preload后沿当前顺序取原exporter baseline，在尚未dial时唯一末8Set；原pre_dial_full80循环、actual helper origin/D、Zero/release、导出完整资格、final200与critical300全部保留原独立采样。60s仍从最后真实Set返回起算。

arm错误、原D越界、panic、Goexit只保留实际部分：安装数0..32、开始字段有则保留、未完成字段null；尚未Set时Version为空/未执行事实按原形保持，不能造preload成功。已创建门闩即时归原单一清理所有权；先冻结原首错误及所有已取得字段，再执行原Cancel/Close/join，一次输出失败。异常观察者不二次通知、不吞原panic/Goexit；不能因新增arm位置漏掉部分32项回收。

## 3. 新声明与最小原始字段

新strategy固定为`six-source-early-arm-pre-dial-fresh-v1`；其余competition_preparation声明保持当前target_window、query/preload witness和80数值。主expected_spec需整体精确匹配：缺失、旧strategy、null、未知类型/字段不得降级。130普通叶/108组/106resource及原clock范围不变，不因局部六叶裁完整声明。

保留现有`drop_arm_started_ns/drop_arm_completed_ns/drop_arm_count`。新增两个共同标量，成功full/complete逐值相等；失败记录实际已发生值，未执行null：

- `drop_arm_before_event_id`：唯一arm前Snapshot的末ID，空前缀为0，非bool非负整数；原事件ID从1连续，因此同时确定精确前缀长度。
- `drop_arm_before_snapshot_returned_ns`：该原Snapshot调用返回后紧贴的真实时间，不从arm开始、最终wire或墙钟推算。

成功必须满足：

```text
prepare_started ≤ arm_before_snapshot_returned ≤ drop_arm_started
≤ drop_arm_completed ≤ preload_set_started ≤ preload_set_completed
≤ preload_accepted ≤ exporter baseline start ≤ baseline end
≤ final_set_started ≤ slow_input_applied ≤ preDial sample…accepted
≤ helper_started ≤ 原后置final…prepared ≤ prepare_deadline
```

上式展开各阶段时保持当前所有更细deadline、callback、实际写完成及critical约束，不以共同顺序替代这些条件。原`drop_arm_count`成功恰32、全部真实Dropped总数仍≤32；未消耗的matcher可按原生命周期取消，不冒充实际发生32次。

### 前缀与Drop合法性

Go保存arm前原Snapshot的Overflow/Failure检查；解析器用同叶完整原wire核前缀ID恰`1..drop_arm_before_event_id`（0为空），其原生Received不得在所报Snapshot返回之后，且此前缀不得有Dropped。该原始cursor须不晚于preload witness的cursor，不能用较早/较晚注册替换同一个控制连接。

preload中允许本次arm后实际Dropped的Snapshot，但Register/RegisterAck仍必须真实成功转发，原最新Register/Ack选择、完整16绑定、PID/epoch/gen、控制ConnID因果均不变。所有Dropped必须是lab-a、control、oic_to_ois、observation_snapshot、同一preload控制连接、非注入、未写/未写失败；每条真实query/binding仍由原完整History校验；ID必须严格大于arm前cursor。总数≤32，禁止任何关键消息/其他角色/其他连接/错误binding Drop。

`ReceivedNS`在proxy实际取得互斥锁、发布event之前采集。因此允许`ReceivedNS<arm_started`但该event真实发布于原cursor之后的既有竞态；不能新增`ReceivedNS≥arm_started`伪边界。也不能凭最终Written或未来事件回填原cursor。时间合法性沿原wire和query原D核验；此cursor证明原前缀和发布顺序，不虚构每个matcher安装的网络收包时刻。

## 4. 最小实现范围与有效反例

Go主要接缝：vectors_g2_competition_test.go 的arm调用移位；g2_probe_first_helpers_test.go 的一次arm前置条件与registration前缀合法Drop；g2_six_source_helpers_test.go 的首8Set必须在arm完成后及共享标量；g2_failure_diagnostics_test.go 的有限可空字段。必要小helper/原相关测试随新策略调整，不删除未受影响回归、不改产品或通用env。原请求数量只因既有有界准备loop的实际状态变化，不新加HTTP类型或节拍。

必要Go反例：

1. 准备D建立→唯一cursor→32arm完成→首8Set→P2/Q4→末8Set→原三个资格的真实顺序；第二次arm/Set先于arm/未满32/越D拒绝。preload即使Full=false或young=false，只要原P2/Q4和身份成立仍能接受，防止恢复旧joint。
2. 空前缀0、非空连续前缀、overflow/Failure、未来/回填cursor；允许合法snapshot Drop出现在preload prefix，仍拒关键消息/注入/错误连接或binding/超过32及arm前已Dropped。
3. 安装0/中间/第32次error、panic、Goexit及完成越D，真实部分计数/null/首错freeze/所有门闩取消保留；不创建socket、不重启准备。
4. 原Received早于arm但发布在cursor之后可接受；ID在cursor内的旧Drop拒绝；Register/Ack原并发写返回次序不能被错误加强。真实proxy定向边界优先复用原fixture，不加产品hook。
5. 原80/200/300恰等、超1ns、未来Received/未完成Write/挑年轻子集、partial/null、full与critical独立、同源16binding、原12异常socket清理等仍按实际库存运行；年龄或Drop守卫不能因排序修改失效。

Python需原始完整正例与逐字段反例，尤其新旧strategy不得互借、两个新增标量缺失/错类型/不一致、cursor前旧Drop、关键消息Drop、超过32、旧probe-first顺序、Received-before-arm合法发布形状、旧/新Register串配，以及preload不要求Full但final/critical仍必须满足。不能只测试拼装字段自身。

修改主分析器前，逐字归档当前旧策略主模块及recorded loader所需原八模块（analyze、g2_analyze、g2_clock_analyze、clock_guard、local_execution、local_run_seal、dependency_quality、vulndb_snapshot）与source-map，实际验证闭包而非靠名单假定。旧19run/14ITER按原recorded source精确复算、原manifest/raw/summary与归档字节保持；新声明不得在当前模块按缺字段回退。新pending诊断phase修复单独保留其旧拒绝与修正结果，不混成业务通过。

## 5. 验证与可推翻条件

固定Go1.27.3、Windows完整旧298 helpers＋实际新增节点、原Go3m/外层240s、GOMAXPROCS2、-p1/-count1/OI_IT1、独立RUNDIR、integration vet与局部fmt；保存每次原失败、结构化argv、stdout/exit/source前后哈希及完整实际库存。Python定向反例、相关完整套件和19/14旧档复算独立记录。辅助通过不代表真实输入可建立。

最终源码固定与监督审查后，在独立新native根执行预定完整六叶（两后端各三轮）及真实helpers，不挑叶、不拼前轮；主分析器使用完整expected_spec，父/未执行范围照实。仍可显式启用原pending诊断作因果解释，但其配对complete不作为业务PASS。

本候选被以下任何事实推翻：提前arm未保住首波；P2/Q4或Full16未在原3s内成立；Full到来时原八条仍超80；32已耗尽；后置final/critical/exporter/控制/60s/资源任一失败。失败后保留事实重新评估，不放宽阈值或重放同一业务。原500ms query仅规定单项期限，不保证八项同批，也不提供32Drop的持续时间下界。

## 6. 下一原生环境的窄派生

下一次使用独立新UUID，原七步、期限和9c工具/cache seed保持。已有三轮27正常身份加本dd62的runner、七child、专用PG九身份，共36个精确旧身份；原failed-preparation四身份、start/exit未知的candidate和Linux机制runner/六child线索另列，不能据此补造旧退出。当前主机fresh检查必须重新执行，不用本次Gone代替下一时刻。

短child的capture/run/cleanup等机制继承原fa529已验证来源；新增历史清单/计数和SPEC绑定会改变文件字节，因此仅在逐项差异及其余函数AST精确核验后继承机制验证，不能声称新整个文件已做过旧实测。旧失败目录、所有机制原件及本轮结果保持。最终新Go辅助库存、完整integration源码闭包、产品源码、归档和执行UUID必须据新实际输出重绑，不沿用298/67/958数值充作新事实。

此页先提交并逐字回读，再实施新策略及执行器窄差异。独立契约追溯及阶段窗口原件保存在本地 `coverage/native-short-child-execution-01/early-arm-plan-01/`；本页没有通过结论，不更改用户已采用产品范围。
