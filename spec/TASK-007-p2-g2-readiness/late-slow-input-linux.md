# 末八慢源后置的单次 Linux 结果

固定源码`cc653e0554c57fd13857e74e431e011c4e219dfd`、tree`c4da7aeb970daeea58fe43b9468231b0255796a6`，按[先行计划](late-slow-input.md)完成唯一两步开发运行。**整体失败：184个helpers节点通过，G2-11双后端六叶及三个父节点全部失败。** 记录见[固定来源、原件与独立分析](late-slow-input-linux.json)。这不是正式run/ITER，也不代表新源码完整质量。

## 实测范围与原件

源archive892个Git文件逐字、mode与blob精确核验，305080320B，SHA256 `aac0eabb189241251cd817da6c877d196cf53b0ee3ff901056016805104b53bd`。实现提交20个文件1168399B已远端逐字回读。沿用同一专用Linux/真实PG、原240/1200s两步预算及integration共享5700s、guest9000s/host9030s；未改clock、PG、VM或测试输入参数。

|实际步骤|退出|started至completed|结果范围|
|---|---:|---:|---|
|完整相关helpers|0|63.2273583s|184唯一节点全PASS，旧162＋新22，无FAIL/SKIP|
|G2-11双后端六叶|1|48.007814s|六叶、三个父及包均FAIL；完整同源expected_spec未裁剪|
|parent整体|1|126.481612s|两步complete、source前后exact、errors为空，所有原D与写返回边界未超时|

宿主deploy/execute/collect退出分别0/1/0。119项manifest原件102438820B，加21247B manifest共120文件102460067B，逐字回收；tar102563840B，SHA256 `cfc3d000521fa44ccb790e7c35e4e39db46a4c712849f1cc5654ff9449830d3f`。两个child实际确认Gone且组为空；collector仅核runner身份Gone，未声称扫描其组。

部署前预检01重新核62旧身份及组，五个无完整控制记录的旧目录另由当前UID1000 cwd/exe观察核验。预检02在部署前误用要求已部署目录的`--prepared`，真实FileNotFoundError保留，没有开始测试；部署后预检03通过且新任务not_started，随后只执行一次。首机制诊断因失败对象含附加字段而未满足成功schema退出；第二版显式投影原字段后分析，无原件修改。完整严格G2解析首次即成功产出失败结论。

## 六叶失败的区分性事实

六叶均完成原真实P2/Q4预热、唯一arm32、末八次Set，以及完整exporter资格；均在`final_selection / slow_full`终止，没有进入critical、业务重叠或完整60s。资源模块缺少正式settled完成检查，不能据此写资源通过或产品泄漏。

|后端/轮|final样本|full真／原D内|young首拒年龄(ms)|export准备期间末八健康源数|实际Drop|
|---|---:|---:|---|---:|---:|
|SQLite/0|24|1／0|426.4312|2/8|19/32|
|SQLite/1|26|1／0|412.5325|1/8|16/32|
|SQLite/2|25|0／0|未调用（原短路）|1/8|16/32|
|PG/0|24|2／2|307.3319、338.0179|3/8|16/32|
|PG/1|26|0／0|未调用（原短路）|1/8|16/32|
|PG/2|24|0／0|未调用（原短路）|2/8|16/32|

149个final样本均pending8/P2，Q4成立148次，PG/0另一次Q3。四次full中SQLite两次callback已跨原有效D 26.3315/5.7205ms；PG两次在D内仍不满足young200。原history无overflow/incomplete/omitted，没有记录到合格callback被忽略。SQLite/0同次投影前三条query仅约24ms，第四条已426ms，证明存在新query不等于八条都年轻。

两Stats耗时17.6439–29.3475ms，末八Set耗时0.0837–0.1825ms。四次旧query在本次HTTP采样开始前已超过200ms，优化采样本身不能救回这四个反例。原3s在冻结时仍余1783–1884ms，先约束的仍是首socket+1s；32Drop未耗尽。

末八源在exporter baseline开始至末Set开始间，共10/48个source-leaf取得Version2真实健康HTTP请求、发头及完成，并出现19条同binding Changed；每叶仅1–3源，没有八源全部取得机会。实际支持“部分健康机会存在”，不支持“该排序足以建立原联合输入”。Changed可合并，不能逐一冒称Begin/Finish归约；旧Version历史保留，未完成的60s计数为null，不借预热补数。

## 机制复审与后续边界

本轮QueryMS和RefreshMS都为500ms。query分配时同时设置两期限；正常到期时周期刷新也已到期，随后原sweep/pump轮转，不是必然再等一个额外刷新周期。没有证据把本轮失败定性为产品公平性或500ms控制处理违规。

另一个实现差异需要先行说明：原查询资格包含`dirty || refreshAt到期`，统计backlog却只计`dirty || pending`，周期已到期、受满槽阻挡的工作可能尚无显式dirty标记。直接扩大Stats读取并集、超时加速重试、在任意pump前补dirty是不同方案；最后一种还存在janitor锁前旧now与控制处理锁内新now交错的反例，不能宣称调度等价。

下一候选仅研究在唯一周期入口落实既有周期待办，并保留通知合并计数原义。它必须先固定规格、证明同序消息／binding／ID消耗／原截止／发送资格等价，再编码和做新来源开发检查；任何反例都阻止采用该实现，不把统计变化伪装成旧证据通过。原200/300ms、3s/1s、32Drop、完整60s和全部业务判线保持。现有G1 Failed、G2 Blocked、G3/G4 Inconclusive、alpha/main及19run/14ITER均不变。
