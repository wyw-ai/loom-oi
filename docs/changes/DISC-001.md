# DISC-001：讨论来源、应用范围与确认记录

修订5 · alpha · 当前应用设计见[应用路径](../designs/application-paths.md)，共同契约见[架构](../architecture/alpha-v0.7.md)。本页维护变更与追踪，不复制方案全文。基线`36612d7f777c177d6bb09a7ae4b83d722b4bfae2`；确认入口[#13](https://github.com/wyw-ai/loom-oi/issues/13)，公共契约#3、传输/存储#4、语言#8、条件#11。

## 当前范围与变化

当前方案将Mac/ECS工具、应用控制、宿主/Pod与未来流程放在同一OI边界内：提供方拥有业务实例/会话/资源，OI负责准确目标、授权和原生传输。资源先分独立端点、内部句柄、共享冲突三类，仅第三类应用外部仲裁。逻辑Channel与Agent Session、底层连接分开。完整解法、失败链和替代由应用设计/架构维护。

本修订整理正文和引用，沿用r4产品机制；补记#8已明确的Go采用。R3只覆盖OIS/OIC首版统一语言，不包括CLI语言、驱动、资源性能、双原型投入或HA。其余R1/R2/R4/R5仍Pending。G0–G7不变，没有新产品实验或raw复算。

<a id="sources"></a>
## 来源定位



来源为《[已脱敏]-73dc649d124d0d8914d3698a0254eadc.pdf》，6页、头部标90条消息，5,153,710字节，SHA-256 `2c0304ef6c4c93a11fdacf557e37c1aac6bd2466b92196211ed44c3bad43bc86`。r2时重新提取全文并查看六页；r4沿用该记录，未再次读取PDF，重复上传不增加证据权重。消息日期2026-11-02，导出没有时区；页码从1起。脱敏标签不推断GitHub审批身份。以下必要短摘录保留讨论中的修正过程，不把原话直接升级为需求/技术事实。

| 来源 | 原话/位置及讨论推进 | 蒸馏出的诉求与矛盾 | 本轮展开 |
| --- | --- | --- | --- |
| D01 | p5 21:09–21:12：Mac经SSH控制两台ECS；“执行质量明显没有本地…bash的效果好”；“我猜”前缀和包裹引号致错；“最好还是包装成工具” | Agent应直接表达目标和任务，不每次手写远程调用包装；原因只是猜测，不能把质量问题全归于SSH | [远端执行](../designs/application-paths.md#remote-exec)：目标、部署、输入/结果、对照与替代 |
| D02 | p5–6 21:19–21:28：“只打通道”“不管业务”→担心“一万种工具…一万种封装”→回应“可以不坐在loomoi里面”→“效果还是得做出一版” | 工具化与轻内核并非二选一；共同接口应止于连接，业务工具可在外部独立演进 | [远端执行](../designs/application-paths.md#remote-exec)：外部工具/执行服务与OI分责 |
| D04 | p1 00:38–04:16：“一个窗口只打开一个app”“按键精灵+应用粒度，暴露工具”；现成MCP可提供；担心逐步操作/多任务回复慢。p3 14:00提出PC替代，称ADB可后台/多设备 | 想控制的是应用工作面，不只是机器；通道并发不等于UI并行；应先复用现有操作端再决定新增包装 | [应用控制](../designs/application-paths.md#app-control)：设备/应用绑定、互斥、动作循环及平台取舍 |
| D05 | p1–2 10:57–11:06：k8s/虚拟手机集群、宿主机/Pod接入OIC；之后Loom managed agent、记忆/事件/serverless；先远控，再跨机器开发部署，“理想情况” | 同一Agent需使用异构、会变化的执行位置；远控是上层平台的连接前置，不等于OI需要拥有调度/记忆 | [主机/Pod](../designs/application-paths.md#fleet)及[上层流程](../designs/application-paths.md#harness) |
| D07 | p2 11:01–11:02希望Jev类模型统一决策；13:27–13:32反对其承担关键决策，提出规范1–10的“门”与流程设计 | 统一决策模型与分层门判断是实质分歧；模型名称/能力未核验，但控制流程问题有架构价值 | [上层流程](../designs/application-paths.md#harness)：确定性约束、模型建议与外部执行记录 |
| D03 | p3–4 14:14–14:41：删旧v0.6；概念说明是什么/谁使用；顶层架构；docs/modules/oic、ois；局部调整“不破坏其他的决策”；需要SDD避免反复手搓prompt | 分层是为了局部变更可推理，不是为了多建目录；模块详细设计也不应把未选型事项伪装成定稿 | [影响分析](../designs/application-paths.md#layers)：PC替换Android及Pod重建 |
| D06 | p2 13:52：“不出意外大概oi是用go做了” | Go倾向需要与明确选型区分；业务服务栈与OI核心栈可解耦 | 原倾向不作批准；后续H12明确确认见[ADR-001](../decisions/ADR-001-core-language.md) |

过滤：开头语气/记忆闲聊、附和、重复引用、两条撤回XML；不猜撤回内容。p3风险截图只证明讨论出现兼容/平台风险提示，不证明来源、版本、封禁概率或PC一定可用。它使我们把具体微信组合留在真实版本验证，而不是否定应用控制本身。旧v0.6文件已经删除，当前核对保留Git历史，不重复做清理工作。


| 补充来源 | 范围 | 去向 |
| --- | --- | --- |
| D08 | 10月31日原Markdown L1204–1693，15:45:18–16:19:13；本轮沿用r4读取记录，未再下载/哈希 | [原始回溯](../evidence/alpha-origin-review-001.md)与架构目标绑定 |
| H11 | [#13三项诉求](https://github.com/wyw-ai/loom-oi/issues/13#issuecomment-6586567010) | 通用接入、状态、互斥契约Review |
| H12 | [#8明确采用Go](https://github.com/wyw-ai/loom-oi/issues/8#issuecomment-6586292098) | [ADR-001](../decisions/ADR-001-core-language.md)，R3 Accepted |

原始材料的SHA、W11–W14及各需求的源关联见[机器记录](DISC-001.json)。

<a id="requirements"></a>
## 需求与确认归属

| ID | 唯一确认目标 | 当前状态 | 归属与验证 |
| --- | --- | --- | --- |
| R1 | 外部工具/有限参考例及准确目标能力消费 | Pending | #13；共用契约#3；V01–V03/V09 |
| R2 | 三类资源的应用路线、外部共享仲裁及主机/Pod组合 | Pending | #13；共用契约#3；V04/V08 |
| R3 | OIS/OIC首版统一使用Go | Accepted，仅H12原话范围 | #8；ADR-001；V05仍Blocked |
| R4 | 分层影响分析与内容优先方法的细则评审 | Pending；用户要求的治理编辑可完成 | 结构维护归DISC-002；V06/V07 |
| R5 | 通用协议/业务描述分层、OIC状态与重启边界 | Pending | #3主契约；#13应用；#4传输/恢复；V09/V10 |

R4的治理工作已按用户要求执行不等于产品采用。应用切片建议A→B→C，样本先就绪可调整，不能替代P1原生只读闭环或跳过阶段门槛。每项只按明确评论和指纹覆盖的范围记录确认，不由整单状态推断。

<a id="validation"></a>
## 验证设计与现状



| ID / 对照 | 观察与预期判定 | 实际状态/缺口 |
| --- | --- | --- |
| V01 参数/环境 | 同一固定任务比较SSH封装、结构化直连、同工具经OI；目标实际记录argv/cwd；空参数/空格/单双引号/Unicode/换行等逐项相等，脚本模式单独评价 | Blocked：实际两端工具/目标、OI未具备；没有把JSON草图当运行 |
| V02 失败/取消/替换 | 效果后丢响应、取消无确认、目标revision变更；不得未知误报未执行、无条件重发或旧请求改投新实例 | Blocked：真实提供方/OI实现与隔离故障条件；LAB-004/G3 |
| V03 Agent质量 | 相同模型/任务/环境/预算，随机顺序，运行前确定样本/阈值；分别计参数表示、选错目标、环境差异、任务完成与耗时 | Blocked：任务原件/日志/目标/模型配置；若A0足够或改进来自工具而非OI，缩小OI价值主张 |
| V04 应用/设备 | 分别检查独立资源、服务内部句柄和共享冲突资源；共享组合固定版本；同一输入域两个消费者争用、窗口切换/重开、设备离线、效果后断线；增加双入口/双Exposure争用、动作跨期、迟到命令、仲裁重启与旁路；按本轮回应核对实际动作不重叠、静止后交接及未知结果；分段测时 | Blocked：真实MCP/设备/演示应用；若后台独立输入可证实才缩小互斥域；LAB-004/G7 |
| V09 解耦替换 | 不同业务使用同一支持传输；OI二进制/依赖不变，变更仅配置/描述/revision，拒绝任意目标及未知传输；A1只到B1、C1只到B2，取消/重配/旧代通知不串路 | Blocked；缺实际实现/两业务样本，G1/G7 |
| V10 状态/重建 | 拒绝旧generation/票据/过期样本；半关闭/有界背压；重启后新认证/通道且不恢复业务锁或重发；区分单腿与共同进程故障，不按业务session分配资源 | Blocked；G1/G2/G3、真实协议组合G7 |
| V05 语言/资源 | 沿用LAB-003同契约目标预算及明确批准的比较范围 | Blocked；本次讨论不是新语言样本 |
| V08 Pod实例 | 同名不同UID替换、旧通道/任务结果处理、重新授权；外部持久结果和OI临时状态分别检查 | Blocked：测试集群/部署/实现与可清理环境；LAB-004/G7、实际传输G3 |
| V06 文档记录 | 来源/Issue/文件/图/指纹与版本闭环；只证明记录一致性 | 本修订记录检查见[DISC-002](DISC-002.md) |
| V07 内容与技能 | 原PDF对照方案；独立任务只给原始材料/基线/新技能，评审Issue正文里的应用机制和主要线程 | 原r2只读内容任务记录见[固定历史](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/docs/changes/DISC-001.md)；本次结构任务见DISC-002，不证明用户已接受 |


<a id="delivery"></a>
## 历史与本次整理

r2重做PDF应用求解，#12按用户指令关闭失效；r3补解耦/状态/共享仲裁；r4补原始目标配对和资源三分。完整历史见[整理前固定快照](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/docs/changes/DISC-001.md)。本次只搬迁/整合正文及补记H12，细节在[DISC-002](DISC-002.md)。旧V07为r2任务记录，不冒充本次产品实测。

## 旧锚点导航

<a id="origin-review"></a>
[原文回溯与期间收束](../evidence/alpha-origin-review-001.md)。

<a id="feedback13"></a>
[解耦](../architecture/alpha-v0.7.md#decoupling)、[OIC状态](../architecture/alpha-v0.7.md#oic-state)、[资源分类](../architecture/alpha-v0.7.md#target-binding)、[详细交接](../designs/application-paths.md#arbitration-detail)。

<a id="remote-exec"></a>
[远程执行应用](../designs/application-paths.md#remote-exec)。

<a id="app-control"></a>
[应用控制](../designs/application-paths.md#app-control)。

<a id="fleet"></a>
[宿主机与Pod](../designs/application-paths.md#fleet)。

<a id="harness"></a>
[未来流程接缝](../designs/application-paths.md#harness)。

<a id="references"></a>
[原有一手机制依据与版本](https://github.com/wyw-ai/loom-oi/blob/36612d7f777c177d6bb09a7ae4b83d722b4bfae2/docs/changes/DISC-001.md#references)。
