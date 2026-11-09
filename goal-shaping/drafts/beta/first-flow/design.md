# loom-oi beta：首个完整使用流程草稿

状态：讨论中。以[当前共识](../../../current/beta/design.md)为已确认基线；首版按 Agent → vMCP → Tool → Action → 通道 → OIC → oiapp → 目标 cmd 的固定链路收敛，不增加多目标选择或额外调度。具体字段、通道载体、执行方式、CLI 和 Done 仍为候选。来源与被替代提议见[沟通笔记](notes.md)。

配套文章：[从 App YAML 到 Agent 调用](app-to-agent-walkthrough.md)，补充目录、配置和执行结果的示例。首版以本文的固定通道链路为准；OIS 维护消费侧工具定义，OIC 按本地 YAML 中的 Action、参数 Schema 和执行映射识别数据并转交 App 实现。

Bash 的参数、默认值、返回结构、托管进程、输出、并发及失败规则见 [Bash Action、参数与执行机制详稿](bash-action-design.md)。它是本轮具体候选，不表示新增机制均已采纳。下文的 Bash 运行方式与该详稿保持一致。

## 已确认的终态骨架

执行侧使用 Host、App、Action；消费侧由 OIS 维护 Tool 定义及其 Action 映射，再组合成 vMCP。一个 App 一份 YAML，包含 Action、参数 Schema 等基本信息及执行映射，配套 actions 目录。OIC 根据这些描述识别输入并交给 App 实现；本地 Action 描述与 OIS 的消费侧 Tool 定义各有用途，不要求两侧严格一致。调用方式由项目统一，不要求应用作者逐个设计 HTTP/TCP/MCP 接入配置。

runtime 可以作为 App 根级默认配置，也可以放在某个 Action 下，Action 更具体的配置优先。省略 handler 时使用同名 Action 入口；省略 handler 不等于省略 Action 的参数 Schema。runtime 的具体字段与覆盖粒度仍按下文候选讨论。

OIC 保留为轻量 CLI 宿主。本期不做独立消费端 oi。首版每个 Tool 固定绑定一个 App.Action 及其通道，Agent 选择 Tool 并提交业务参数，不再承担执行目标的查询与选择。Host 仍可表示 OIC 所在宿主机，但不作为本轮 Agent 调用的选择层；这一简化不意味着整个平台永远只能连接一个 OIC。

首版按终态设计职责和接口，只实现 Bash App，不实现 CU。公共接入、固定映射转交与消费适配不写成 Bash 专用逻辑，CU 继续作为后续场景检验概念边界。

通道先按 OIS 对接某个 OIC 的通信入口理解，这是当前候选解释，不新增独立通道服务或调度器。Tool 的绑定决定调用经过哪条通道；通道具体采用什么协议、如何标识及建立连接仍需设计。

## 推荐的 Bash 首个流程

1. 在执行机器上准备 Bash App 的 YAML 与实现，并具备实际执行 Bash 的环境。
2. OIC 加载 YAML，解析 App、Actions、基本参数 Schema 与有效 runtime，连接 OIS 并通过对应通道登记可提供的能力和映射。两侧参数定义差异至多提示，不强制阻止加载。
3. OIS 配置 terminal_exec、terminal_write_stdin 两个 Tool，维护参数定义和说明，分别固定绑定 Bash App 的 exec_command、write_stdin，并使用同一通道。
4. OIS 将这两个 Tool 加入一个 vMCP；使用者配置支持的 Agent 连接该入口。
5. Agent 向 Tool 提交业务参数。OIS 根据固定绑定把调用交给指定通道；OIC 找到对应 App.Action，按 YAML 参数 Schema 识别和轻量适配输入，再按执行映射转交 App 实现。
6. OIC 收集 Action 结果，经 OIS 返回 Agent。Bash 未结束则返回 session_id，Agent 调用已绑定同一执行链路的 terminal_write_stdin 继续读写。

Tool 展示名可以与 Action 名不同。Agent 的 Tool 参数建议直接采用扁平业务对象；通道和 Action 来自 OIS 配置，不由调用参数覆盖。OIS—OIC 的内部 invoke 仍可使用 input 封装业务数据，便于与可信调用上下文分开。具体参数映射与可转换类型仍按下文候选讨论。

## App 的交付候选

以普通目录作为首个参考 App 的交付形式：

```text
bash/
  bash.yaml
  actions/
    exec_command.py
    write_stdin.py
  runtime/
    worker.py
    executor.py

computer-use/
  computer-use.yaml
  actions/
    click.py
    screenshot.py
```

目录结构体现 App 包含多个 Action，不规定每个脚本必须是 Python；首版只实现 Bash，CU 为后续示例。runtime/executor.py 示意 Bash App 的共享执行后端，是本项目实现建议，不是当前 Codex 工具内部结构的声明。解释器加脚本与直接可执行程序都可以作为入口，语言及安装机制另定。

App 是描述与实现的组织单位，不要求所有 App 拥有独立常驻服务。本轮建议 Bash 由 OIC 直接托管一个共享 worker，两个 Action 在其中分发，执行后端保留进程与输出。普通无状态 App 的逐调用入口仍可作为后续运行形式，但首个 Bash 闭环无需同时实现所有模式。

## YAML 结构候选

以下仅为字段讨论草图，Python/Bash 路径假定示例环境已准备：

```yaml
version: 1
app:
  code: bash
runtime:
  command: ["python", "./runtime/worker.py"]
actions:
  exec_command:
    description: 启动一次 Bash 执行并返回本轮输出。
    input_schema:
      type: object
      properties:
        cmd: {type: string}
        workdir: {type: string}
        tty: {type: boolean, default: false}
        yield_time_ms: {type: integer, default: 10000}
        max_output_tokens: {type: integer, default: 10000}
      required: [cmd]
  write_stdin:
    description: 向已有执行输入，或仅等待并读取后续输出。
    input_schema:
      type: object
      properties:
        session_id: {type: string}
        chars: {type: string, default: ""}
        yield_time_ms: {type: integer}
        max_output_tokens: {type: integer, default: 10000}
      required: [session_id]
```

- version 描述加载文件格式版本，不与 App 发布版本混同。
- app.code 和 Action 标识供执行侧识别、登记和引用。actions.<name>.description 描述本地 Action；对外 Tool 名称及面向 Agent 的说明仍由 OIS 配置。
- actions.<name>.input_schema 建议采用基本 JSON Schema 表达 type、properties、required 和固定 default。OIC 据此识别字段、采用固定默认值并进行轻量转换；这不意味着在 load 时要求本地与 OIS 的 Schema 相同，也不要求先支持完整 JSON Schema 校验器。
- input_schema 描述本地实现接收的参数名和类型，本例字段按原名传递。此前 input 中的 as/required/default 等类型与参数信息统一由 Schema 表达；需要重命名时，可另加 input_mapping，采用“本地目标名: 调用来源名”的字符串映射，例如 {cmd: command}。OIC 先按映射重命名，再按本地 Schema 处理默认值、必填和轻量类型绑定。具体字段和转换细节仍是候选，不引入第二套类型定义或转换表达式语言。
- runtime.command 是入口程序和参数列表；Bash 示例指向共享 worker。相对资源以 App 描述文件所在目录解析，不经字符串拼接的 shell 间接启动。handler 指向实际动作入口；省略时使用 Action 名，异名时才需要显式映射。同名 handler 可省略的约定保留，但 Action 现在仍须声明基本参数信息，不再用空对象作为完整示例。
- 启动解释器是执行入口问题，不是要求各 App 选择传输协议。公共接入方式由项目统一，YAML 不再推荐 runtime.transport/http headers 等逐 App 配置。
- 本地配置、执行环境和超时等字段按实际执行需要设计。OIC 的本地 Schema 服务于执行识别与适配，OIS 的 Tool 定义服务于消费侧展示和调用，两者不强制保持完全一致。

runtime.command 仅表达本地启动，不恢复此前每个 App 自选 transport 或通过 provides.tool/action 绑定中心 Tool 的方案。Tool 到 App.Action 的映射仍由 OIS 维护。

根级 runtime 与 Action.runtime 的合并建议采用简单规则：先取根级默认，再由 Action.runtime 覆盖同名顶层字段；command 数组整体替换，不拼接命令参数。嵌套对象同样作为一个字段整体替换，不做隐式递归合并。未覆盖的根字段保留；没有根 runtime 时，Action 必须给出足以解析执行入口的配置。更具体配置优先的方向已确认，这一字段合并粒度只是候选。

本例两个 Bash Action 均不覆盖根 runtime，复用本次 App 加载的同一个受托管 worker。实例规则候选是：runtime: {} 与未配置一样继承根实例；非空 Action.runtime 在覆盖默认字段后使用该 Action 的独立 worker，不因 command 文本相同就自动合并。具体实例规则见 [Bash 详稿](bash-action-design.md)，不是用户已确认的 runtime 优先级所必然推出的结论。

若给某个 Action 配置不同 runtime，不能据此假定它仍能访问原 worker 的 session；要延续 Bash 执行，两个 Action 必须对接同一个共享后端。runtime 优先级解决的是入口选择，不自动提供跨进程状态共享，也不要求所有 App 采用 Bash 的常驻 worker 形态。本轮要求基本输入参数 Schema，不强制增加 output_schema。

write_stdin 的等待默认值依赖 chars 是否为空，因此本例不在该字段写固定 default，由 App 在收到已适配输入后决定。静态字段定义放在 YAML，业务语义、本机状态和条件默认仍由 App 实现负责。

## OIC 如何控制 Action：统一运行约定候选

推荐由 OIC 提供通用 Action 运行能力：根据已加载描述选择入口，按统一方式交付输入，跟踪调用并收集结果。应用作者实现业务模块及薄协议适配，不自建网络服务；共同协议可由辅助库复用。

首个 Bash 的可验证候选：

- OIC 加载时启动 worker；使用固定 JSON Lines 协议传递启动配置及带 call_id 的调用帧，stdout 只承载协议帧，stderr 输出诊断。
- exec_command 入口将 cmd 交给 Bash 执行后端，按等待窗口返回已有输出及运行中句柄或退出状态；write_stdin 入口访问同一个后端继续读写。
- 调用时 OIC 先按可选 input_mapping 将调用字段对应到本地参数名，再按 YAML 的 input_schema 识别参数、应用固定默认值并作通用绑定和轻量转换，随后将数据交给有效 runtime 中的 Action 实现。可转换的字符串与整数允许传递，无法转换的实际值报错，不先逐项比较两套 Schema；业务语义和依赖本机状态的默认值仍由 App 处理。
- worker 按统一 result/error 约定响应；一次 Action 返回不导致共享 worker 或 Bash 命令自动退出。
- OIC 管 worker 进程及公共调用记录；Bash App 管业务进程、执行期限、缓冲及回收，平台进程管理能力需实际验证。
- 结果至少要区分正常业务结果与执行器失败，不能把一个进程的退出码自动等同于所有业务操作结果。

详稿给出了本地协议、分帧和限额候选，尚未确认采用。截图、文件和流式输出仍需清楚的结果表示，不能由文本 Bash 示例推定所有 Action 只返回字符串。

共同机制由 OIC 提供，业务执行留在脚本。SDK 可在实际重复代码出现后提供辅助读写与校验，不将它作为最初的强制接入方式。常驻进程、JSON-RPC 和 HTTP 仍可用于适合的边界，但不是每个 App 必须自行实现的服务协议。

## CLI 与加载候选

OIC 是 CLI 宿主已经明确；首版建议保留以下六个命令。默认后台运行、命令名称及标志均为候选，尚未实现或整体采纳：

```sh
oic start
oic status
oic load ./apps/bash/app.yaml
oic apps
oic unload bash
oic stop
```

| 命令 | 建议行为 |
| --- | --- |
| start | 启动本机 OIC，默认后台运行；主动连接配置的 OIS。重复启动不创建第二个相同实例。 |
| status | 查看本机 OIC 是否运行、OIS 连接和固定通道状态、已加载 App 数量。 |
| load <yaml> | 请求已运行的本机 OIC 加载一份 App 描述，准备执行入口，不执行用户 cmd。 |
| apps | 列出已加载 App、Actions、本地就绪状态及 OIS 登记状态。 |
| unload <app.code> | 停止接收该 App 的新调用，撤销登记并关闭执行入口，回收该 App 的已有执行；对应 session 失效，不删除 App 文件。 |
| stop | 停止本机 OIC，停止新调用，回收所有托管 App 和执行，再关闭连接并退出；不能仅因发送关闭请求就报告资源已回收。 |

配置默认读取当前目录的 oic.yaml；全局 --config 可显式指定，管理命令必须通过相同的本机实例配置定位运行中的 OIC。start --foreground 用于前台运行和观察日志，其他管理命令可从另一终端发起；具体本地控制传输仍待选定。start 完成本机启动与 OIS 已连接分别显示，避免把后台进程存在误认为工具已经远端可用。示例：

```sh
oic --config ./oic.yaml start
oic --config ./oic.yaml load ./apps/bash/app.yaml
oic --config ./oic.yaml apps
# 调试时可改用前台启动
oic --config ./oic.yaml start --foreground
```

unload/stop 的上述资源回收语义是本轮建议，不是已经采纳的生命周期规则；执行会话不会因重新启动自动恢复。首版命令集不增加远程目标选择、通用 exec 消费入口或系统服务安装管理。

load 的推荐含义调整为：读取 App、Action 及参数 Schema，检查描述与执行入口的基本可用性，解析根级和 Action 级 runtime，按有效 runtime 及明确的实例归属分组初始化，建立本地索引并登记可提供的执行入口。已确认不将本地参数与 OIS 定义严格一致作为加载条件；能识别的差异至多警告。加载成功不要求提前执行一次有副作用的 Action；是否启动受托管 worker 由所选执行形态决定，Bash 的共享 worker 是本轮候选，不假定每个 App 永远只有一个进程。

具体登记成功是否属于 load 成功条件、是否自动重载文件、重复加载如何处理、重启后如何恢复尚未确定。OIC 加载与 OIS 创建 Tool/vMCP 映射是独立步骤，不隐式替使用者创建全部消费侧工具。

## OIS 定义与执行时适配

具体端点与请求示例见 [OIS 首版 API 草案](ois-api-design.md)。管理 CRUD、HTTP 执行入口和 MCP 共用固定绑定；路径、配置权威及更新规则均为候选，未改变本节已确认边界。

已确认：OIS 维护消费侧工具定义、参数描述、Prompt 与组合；本地 YAML 需要包含 Action、参数 Schema 等基本信息和执行映射。OIC 按 YAML 识别输入并转交 App 实现，不以两侧结构完全一致作为加载或发布门槛。

首版 Tool 采用固定绑定，候选配置片段如下：

```yaml
tools:
  terminal_exec:
    source: {app: bash, action: exec_command, channel: bash-local}
  terminal_write_stdin:
    source: {app: bash, action: write_stdin, channel: bash-local}
vmcps:
  workbench:
    tools: [terminal_exec, terminal_write_stdin]
```

两个 Tool 分别调用不同 Action，但通过同一通道到达同一个 OIC 及其 Bash App。Tool 的消费侧输入定义另由 OIS 维护；上面只展示固定绑定，不让 Agent 提交或选择通道。

OIC 一侧的通道配置候选为：

```yaml
ois:
  url: https://ois.example.com
channel:
  id: bash-local
```

channel.id 在这里用于标识这条接入关系，不替代连接认证。通道注册、凭据以及重复连接的处理细节仍未冻结；这些配置也不意味着新增一个独立通道服务。

执行时做轻量序列化、反序列化及参数适配，可转换就传递，实际无法适配才返回错误。具体规则建议保持简单：目标类型相同则直传；目标需要整数且实际值为可解析的数字字符串时转换；对象或数组没有对应映射却要传给整数参数时返回参数错误。不能因为参数类型与中心描述不同就在适配前拒绝调用。

上述例子的目标类型由本地 input_schema 提供，不假设 OIC 能自动推断任意脚本的函数签名。建议按同名字段传递；需要重命名时单独使用 input_mapping，不在其中重复声明类型、必填和默认值。Schema 在这里提供基本数据描述，OIC 的首版适配仍保持轻量，不因此引入通用表达式语言或复杂跨 Schema 一致性检查。无法转为整数的文本不能靠填零等方式伪装成成功，具体转换表和错误形式另定。

首版每次调用沿 Tool 已配置的 App.Action 与通道转交，本地适配面向这条固定链路的实际实现。通道不可用时返回不可用，不再寻找其他执行目标，也不添加自动切换或负载均衡。

vMCP 可以包含一个或多个已配置 Tool。工具集合与每个 Tool 的执行绑定分开管理，不增加自动业务规划、广播或额外调度。

## 无独立 Host 的 App

对应远程 HTTP 服务的 App YAML 可以位于 OIS，这是已明确的接入方向。仍保留 App/Action 描述和消费侧映射，不为表达它而虚构物理 Host。

推荐将 App 描述与执行位置分开理解；是否由 OIS 执行适配脚本、复用某种 OIC 运行能力或交给其他明确执行入口，目前均未确定。该扩展也不意味着重新引入逐 Action 任意 HTTP 模板作为必选 YAML 结构。

## 对之前技术选项的处理

OIC 保留，但其后台进程与服务安装方式未定。Go、Rust、Python、SDK、HTTP 都是不同维度的候选，未因原始讨论中的建议而定案。

优先比较 App 描述和输入输出契约，再选择能实现它的 OIC 和执行方式。Bash 目前推荐由 OIC 托管共享 worker 来保存持续执行状态；这不要求所有 App 都拥有常驻适配服务，也不要求应用作者自建网络协议。OIS—OIC 的通道通信仍需独立定义，不能由 OIC—worker 的本地调用协议代替。

## 运行约定与待收束问题

现有 App/Action/Tool/vMCP 关系加上固定通道可以承载首个完整流程，Host 保留为宿主机概念。连接方向、CU 单控制者范围和 OIS/OIC 控制边界已确认；调用状态、更新处理及具体通道机制仍为候选。下文分别标明，不能因部分确认而将全部运行机制视为定案。

### OIS—OIC 的连接与投递

已确认由 OIC 主动连接配置的 OIS，首版只经当前可用通道向对应 OIC 投递，重连后重新登记能力。通道的存活检测及重连细节仍需设计；持久保存的通道或 App 定义不等于当前在线状态。

离线请求直接返回不可用，不缓存成恢复连接后才执行的任务。具体传输协议仍待选定，不从这一方向推定 HTTP、WebSocket 或 alpha 的 TCP 转发方案。

### 一次调用的状态、结果与停止

已确认 Bash 使用启动及后续读写两种调用，结果区分仍在运行与已结束。更完整的投递、取消和断连状态尚未确认。建议一个 call_id 贯穿 OIS、OIC 和 Action 执行记录，区分尚未投递、已接收、执行中和已结束，以及无法确认结果的情况。Bash 已执行但结果回传失败时，不能声称没有执行，也不能自动重发。

首版可以只在同一运行周期及规定保留范围内识别重复 call_id；不承诺跨重启恢复或恰好执行一次。明确记录保留范围，过期请求不得因去重记录消失而自动重新执行。取消当前 Action 的等待与终止已有 Bash 执行分开定义；仅在实际执行停止后报告进程已停止，不能由请求取消推断业务已经回滚。

将输入、结果和错误表达整理为一份共同执行契约，补上超时、输出大小限制及执行环境规则。结构化调用记录建议关联 call_id、Tool、通道、App.Action、状态和耗时，便于确定问题发生在固定映射、通信、入口还是业务执行；无需先建设独立监控服务。图片等非文本结果保留扩展边界，不因首版 Bash 提前实现 CU。

### Computer-use 的控制权与并发

desktop_id 只能确认桌面上下文，不能保证多个 Agent 的动作不会交错。即使 click 和 type_text 各自串行执行，也可能出现 Agent A 截图后，Agent B 切换焦点，随后 A 输入到错误窗口。

已确认首个 CU 场景限定同一桌面同一时段只有一个控制者，控制范围覆盖观察—点击—输入—复核这一段操作。具体占用身份、拒绝其他控制者的响应、释放及失联回收规则待定；desktop_id 不兼任控制权凭据。暂不扩展通用工作流或会话平台。

### 配置权威、更新与正在执行的调用

已确认消费侧工具定义由 OIS 维护，OIC 本地 YAML 维护 Action、基本参数 Schema 与执行映射；加载或组合不强制匹配两侧参数定义，实际调用时按本地 Schema 适配。此前建议的 Schema 一致性门禁及依赖它的重新校验流程已被替代；本地需要参数 Schema 并不恢复该门禁。

首版建议明确一种配置编辑权威：例如 YAML 导入形成 OIS 的持久配置，管理 API 修改该配置，YAML 用于导入/导出；若选择持续以文件为准，则管理 API 不应再独立维护另一份可变定义。具体采用哪种仍待确定。

更新和卸载时如何处理运行中的调用仍未确认。可继续比较拒绝变更、等待执行结束等简单行为，但不把接口更新等同于必须通过严格 Schema 比较。执行入口变更的生效时机也需单独明确；本地停用后拒绝新调用这一控制边界已经确认。

### 提供方与消费方的控制边界

OIS 负责消费方可以访问哪些 Tool，调用沿该 Tool 的固定绑定执行；OIC 只执行本机已加载且启用的 Action，停用后本地立即拒绝新调用，不依赖 OIS 的缓存及时刷新。既有消费方与提供方的控制边界保留，首版不增加 Agent 选择 Host 的授权流程。OIC 连接并信任配置的 OIS，具体凭据及授权字段待定。

普通 Tool 输入不能指定任意入口程序、覆盖 App 本地配置或取得接入凭据。App 明确提供的业务能力仍按其语义执行，例如 Bash 本身允许执行脚本；本地范围检查不等于为 Bash 建立沙箱。完整账号体系、多租户和逐次审批不因此成为首版前提。

### 首个实现范围与验证顺序

首版范围已确定为 Bash，按终态架构保持解耦。建议先选定一个执行平台和一个 Agent 客户端，使用一条通道及其 Bash App 验证固定绑定、长短执行、持续读写与失败行为。CU 留作后续接入，不要求首版额外证明多目标路由。

任务队列、跨重启执行恢复、负载均衡、热更新、App 商店及多实例可以继续后置。CU 的桌面后端和具体控制机制留到后续接入时设计。

## Bash 两动作交互与 MCP 出口

已确认参照当前助手可见的命令工具，采用 exec_command / write_stdin 两个核心 Action。取其启动、有限等待、返回执行句柄及继续读写的行为；具体默认值、本地执行后端实现及恢复规则仍为候选。

直接依据是本会话提供的工具接口及一次无副作用进程交互验证：启动返回 session_id，空 chars 继续等待，写入一行后收到后续输出及 exit_code。该验证使用 Windows 环境中的 Python 测试程序，不代表已经实现 Bash App，也不证明 Bash/PTY 的跨平台行为。官方公开说明同样描述了这两个工具用于持续执行、输入和轮询的模式。[OpenAI Codex Prompting Guide](https://developers.openai.com/cookbook/examples/gpt-5/codex_prompting_guide)

### MCP 能提供什么

截至 2026-11-09，官方 latest 指向 2026-07-28。普通 tools/call 提供最终工具结果；Streamable HTTP 可以在 SSE 流上先发送相关通知，再发送最终响应。传输流本身不定义命令 stdout/stderr 的分块、stdin 和终端会话。[Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)、[Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)

Progress 是可选进度通知，依赖请求中的 progressToken；不能据此假设所有客户端都会把每段输出交给模型持续推理。建议将其用于运行提示，业务输出另有可读取的来源。[Progress](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/progress)

Tasks 是需要双方声明支持的官方扩展，可以承载异步任务状态和结果；它不自动定义终端输入或日志游标。建议以后按客户端能力接入，不将其作为首版 Bash 可用的前提。实现该扩展还需满足其耐久性等要求，不能只把一个易失的 Bash session_id 改名为 taskId。[Tasks](https://modelcontextprotocol.io/extensions/tasks/overview)

### 两个核心 Actions

| Action | 核心行为 |
| --- | --- |
| exec_command | 启动一次 Bash 执行；等待窗口内结束则返回 output 和 exit_code，否则返回已有 output 与 session_id。 |
| write_stdin | 使用 session_id 继续操作；chars 为空时等待读取，非空时写入输入后收取后续输出，结果仍区分运行中与已结束。 |

两者是 Bash App 的业务 Actions，由 OIS 配置成对外 Tools，例如 terminal_exec 和 terminal_write_stdin，并固定到同一通道及 Bash App。Agent 调用只提交该 Tool 的业务参数；两次调用之间由 Agent 读取结果并决定等待、输入或结束。普通 MCP 工具调用即可承载这个循环，不要求一个 tools/call 持续追加最终结果。

字段草案沿用可见接口中与 Bash 有关的部分：exec_command 接受 cmd、可选 workdir、tty、yield_time_ms、max_output_tokens；write_stdin 接受 session_id、可选 chars、yield_time_ms、max_output_tokens，chars 默认空。具体必填、默认值、限额和编码约定已在详稿提出候选，尚未冻结。当前通用工具的 shell 选择和宿主专用配置不照搬为 Bash App 的业务参数；Bash 程序由本地配置决定。

yield_time_ms 是本次等待窗口，不是执行期限；返回 session_id 后执行可以继续。max_output_tokens 限制本次返回输出，不能充当进程资源限额；如何估算预算和标明截断仍待明确。workdir 选择本次业务执行目录，不修改 OIC 的本机配置，也不绕过本机执行范围。

每次 exec_command 启动新执行，不自动继承另一次执行的 cd/export。若启动的本身是一个持续交互的 Bash，则后续 write_stdin 操作同一个进程，可以在这次执行内保留状态。session_id 只代表这次持续执行，不是与进程无关的永久 shell 工作区，也不是 MCP 会话标识。

tty 表达可选终端模式；首版不因此承诺终端界面渲染、resize 或所有交互程序兼容。当前可见结果是 output，不保证分别暴露 stdout、stderr，Bash App 也先按这个消费形状设计。[PTY 手册](https://man7.org/linux/man-pages/man7/pty.7.html)

以下是产品调用示例，数值与 ID 为示意，尚未实现：

```json
{
  "cmd": "make",
  "workdir": "/srv/workspace",
  "yield_time_ms": 1000,
  "max_output_tokens": 2000
}
```

若仍在运行，业务结果可为：

```json
{"output": "Building...\n", "session_id": "s_b1_7ef4"}
```

通过 terminal_write_stdin 继续读取：

```json
{
  "session_id": "s_b1_7ef4",
  "chars": "",
  "yield_time_ms": 1000,
  "max_output_tokens": 2000
}
```

运行结束时业务结果可为：

```json
{"output": "Build finished.\n", "exit_code": 0}
```

需要输入时将 chars 改为待输入文字，例如带换行的一行文本。会话已失效、输入端已关闭或该模式不支持输入时，返回明确错误。session_id 作为不透明句柄使用，示例采用字符串，具体生成方式不由这些示例冻结。

OIS 在内部根据 Tool 绑定选择通道，再形成调用帧；内部 input 封装不改变上面面向 Agent 的扁平参数形状，例如：

```json
{
  "type": "invoke",
  "call_id": "call-001",
  "app": "bash",
  "action": "exec_command",
  "input": {
    "cmd": "make",
    "yield_time_ms": 1000
  }
}
```

这个帧经 Tool 固定的通道送到 OIC，App、Action 和可信调用上下文由系统确定，不允许业务参数改写。具体帧字段仍是协议候选。

### 状态归属与解耦

建议 Bash App 的共享本地执行后端持有 Bash 进程、stdin、输出缓冲和执行状态；OIC 负责通用托管、连接、调用转交及轻量适配，OIS 负责工具定义、权限、固定绑定与 MCP 转换。公共层不出现按 bash.exec_command/bash.write_stdin 名称处理业务的分支，也不将 Bash 作业状态升级成 OIS 必须内置的通用任务系统。共享后端是本项目实现建议，未查看或宣称复制了 Codex 内部实现。

每个 Action 的一次输入、一份结果可以保留；一次 Action 返回不必等于 Bash 进程结束。普通短脚本、带共享后端的 App 使用同一接入概念，托管模式可按实际需要演进，不强制每个 App 自建 HTTP 服务。

session_id 关联一次 Bash 执行，call_id 用于关联一次动作调用；多次 write_stdin 引用同一个 session_id，但各有自己的调用记录。session_id 关联原通道对应的 OIC、App 执行实例和访问者范围，后续调用通过既定 Tool 继续到达同一后端。修改 Tool 绑定不会迁移旧 session；原链路不可用时返回错误，不在别处重建执行。详稿建议使用不透明字符串句柄、worker 内存状态和有界记录，不承诺跨重启恢复；这些具体机制仍待采纳及验证。

### 输出、停止和恢复的最低体验

建议持续消费进程输出，采用有界缓冲；通过 write_stdin 返回后续内容及结束状态。详稿提出“本批输出被显示截断后不补读”的消费语义，并区分缓冲丢弃和输出收尾不完整。完整历史保留、可重复读取游标和断点回放不在首版范围。output 的展示顺序不等于可以还原 stdout/stderr 的精确全局时序。

应区分命令非零退出与入口启动失败、参数无法适配、会话失效或 stdin 不可写。适用的终端可以通过 write_stdin 接收 Ctrl+C 等控制输入；这只是请求中断，必须观察执行结果，不能宣称已经完成强制停止。并发数、最长执行期限、输出上限和回收规则已在详稿提出默认建议，仍需结合首个平台验证。

网络中断不证明进程停止，也不能自动重跑来假装恢复。创建请求尚未取得句柄时的断连、后续输入是否已送达、OIC/App 重启后的会话失效需要明确处理；这些投递和恢复规则仍是候选。

此前建议的独立 read/write/cancel/list、对外 cursor、resize 和强制 kill 不再作为首版核心方案。需要的进程回收可以先由本地执行后端和宿主生命周期承担；是否另加公开停止或管理接口，只能按后续明确需要补充。

MCP 请求取消应按所支持协议版本处理：2026-07-28 的 HTTP 响应流关闭表示取消对应请求，不能统一解释为 Bash 已停止，也不能无视该取消语义。启动作业、等待或读取输出、停止作业各自的请求范围需要在实现契约中明确。[MCP Cancellation](https://modelcontextprotocol.io/specification/2026-07-28/basic/patterns/cancellation)

## 候选 Done

- OIC 加载包含 exec_command、write_stdin 及基本参数 Schema 的 Bash App，解析有效 runtime，并通过对应通道登记执行映射。
- OIS 将两个 Action 固定映射为 Tool，绑定同一通道并组成一个 vMCP；支持的 Agent 能发现并调用它们。
- Agent 只提交业务参数，调用沿配置链路到达 Bash；无需目标查询、通道选择或额外调度，后续读写仍访问原执行实例。
- YAML 无法解析、入口不可用、执行时参数无法适配、业务失败与执行器失败有可解释结果；OIC 按本地 Schema 将可转换的类型差异适配后转交 App，不因它与 OIS 定义不同就在加载时拒绝。
- 验证根 runtime 默认、Action runtime 优先及同名 handler 省略；不同执行入口不被误认为自动共享 Bash session。
- 公共结果表达保留结构化与非文本内容的扩展边界，可用契约测试验证；首版不实现 CU。
- 验证短命令直接结束、长命令返回句柄、空输入继续读取、非空输入交互及最终退出状态，并覆盖可选终端模式；资源回收和异常行为按进一步明确的契约验收。
- 从准备 App 到 Agent 调用的步骤可复现。取消、超时、断连等验收需要在具体承诺确定后补齐。

## 关键未决事项

2026-11-09 收敛审查：首版产品目标、固定调用链、Bash 两动作及 OIS/OIC/App 的职责已经基本收敛，没有发现需要推翻主架构的矛盾。尚未达到实现规格冻结；许多细节已有推荐方案但未采纳，少数行为还没有闭合。下面区分这些状态，不将所有候选字段或后续产品方向都视为开工前置条件。

### 首版需要收尾的决定

| 事项 | 当前状态与影响 | 最小收束建议，仍待采纳 |
| --- | --- | --- |
| 首发环境与交付 | 执行平台、Bash/PTY 后端、首个验收 Agent 客户端及 MCP 兼容版本未选；Linux 路径、Python 和 VS Code 只是示例。OIS/OIC 的 CLI 常驻形态也仍是候选。 | 首版选定一套部署和客户端组合，交付可复现的 OIS、OIC 与 Bash App 启动流程；其余平台、客户端和服务安装器后置。具体语言、框架由实现设计选择，无需全部作为产品问题讨论。 |
| 配置权威与管理范围 | API 稿推荐首次 YAML 导入、后续 API 修改持久配置，但总稿仍保留文件权威方案；完整 CRUD 和 Host/Action 管理查询是候选交付清单。 | 采用一份配置权威。推荐 OIS 持久配置由 API 维护，旧 YAML 不在重启时覆盖；本地 Action 描述仍由 YAML/OIC 维护，管理查询不创建第二份可写执行定义。明确 API 清单哪些纳入首版验收。 |
| Host、通道与接入身份 | 已有管理查询路径，但稳定 Host ID、首次绑定、重复接入及重装处理尚无完整契约；简单凭据引用只是示例。 | 定义最小稳定登记身份和受认证约束的通道关联，先验证单个 OIC 接入，不实现硬件识别、自动归并或迁移。管理、消费、OIC 接入凭据分别授权，不能靠自报 ID 覆盖他人登记。 |
| 执行与通信契约 | Bash 共用 worker、JSON Lines、本地 Schema 子集和 runtime 实例规则已有详细候选；OIS—OIC 的载体未定。 | 首版实现一种明确的托管形式，Bash 两动作访问同一后端；保留根 runtime 默认和 Action 覆盖，冻结帧结构、基础参数转换和结果形状。OIS—OIC 选一种通信实现，不与本地 worker 协议混为一谈。 |
| load、卸载和重启 | 本地 ready/中心 registered 已有候选；load 的退出口径、重复 load、多个 runtime 部分失败，以及加载记录恢复仍缺完整行为。 | 建议全部本地入口 ready 即本地加载成功，登记另报 pending/registered；重复相同定义为无操作，变更先卸载再加载；初始化失败回收本次新建资源，不发布半个 App。unload 回收执行并移除加载记录；stop 回收执行但保留记录；start 重新校验加载记录，不恢复旧 session。首版不做热更新或 worker 崩溃后自动恢复。 |
| Bash 体验与请求结束 | 非 TTY stdin 为 EOF、超预算输出截断后不补读、闲置回收、没有单独强制停止工具均已有候选，但会影响使用体验。MCP 取消已有分阶段建议，REST /exec 断开与上层超时尚未闭合。 | 明确是否接受上述行为；保留两动作时，后续输入与 Ctrl-C 需使用 TTY，非 TTY 的单次执行不能靠 write_stdin 强制停止。建议 REST 与 MCP 接到同一内部取消契约，仍区分取消等待、请求回收新执行及网络失联；失联结果未知不重跑。网络请求期限与 yield_time_ms 分别设置。 |
| 配置变更对在途调用的影响 | API 稿已有不原位换 source、引用资源不能直接删、变更只影响新请求等推荐；尚未成为统一契约。 | 采用明确且简单的一组变更规则。已接受调用保留当次绑定，删除 vMCP 不等于终止 Bash，实际执行由本地生命周期管理；不引入旧 session 迁移或执行入口热替换。 |

其中需要补齐具体方案的重点是 Host 首次登记/绑定、App 多 runtime 初始化失败处理，以及 REST 请求中断/超时语义；其余大多可从已有候选中一次性选择，不必重开目标讨论。Action 本地 Schema 的定义来源已经明确，Host 查询也不等于恢复 Agent 选目标，不将这两点重复列为产品方向疑问。

### 首版 Done 的建议收口

完整 Done 尚未确认，建议在选定平台和客户端上至少覆盖：

1. 从准备配置、启动 OIS/OIC、加载 App、固定绑定两个 Tool 到 Agent 接入均可复现，Host/Action 管理查询与实际登记状态一致。
2. 短命令直接返回输出与退出码；约十分钟且可以长时间无输出的命令，在 Agent 持续通过 write_stdin 等待时能最终取得结果，不把等待窗口当作执行期限。
3. TTY 输入、空输入读取、非零退出、执行时可转换参数和无法转换参数均符合公开契约；根 runtime 默认、Action 更具体配置优先及同名 handler 省略有可验证结果。
4. 通道不可用、交付后失联、App 崩溃和管理卸载有明确结果，不自动重复命令；普通托管范围内的进程资源能够回收。
5. 验证所选输出截断、取消、重启和旧句柄失效规则；不把“消息已发出”当作“命令已停下”，也不将小范围去重宣传成跨重启恰好执行一次。

十分钟持续执行与十分钟无人读取不是同一承诺；现有闲置期限方案会回收后者。是否接受该体验需要明确，具体分钟数、缓冲容量及心跳间隔可以在实现验证时集中冻结，不逐项升级为产品目标争议。

### 不作为首版收敛门槛的事项

- CU 控制权、多 Host 合并/选路/负载均衡、无独立 Host 的远程 HTTP App 是后续范围，不再混入本版关键未决清单。
- App 商店、通用 SDK、热更新、执行迁移、跨重启恢复、完整输出历史与多种执行器后端无需作为本版前置目标。
- 具体语言/存储实现、路径拼写、Schema 字段最终命名和限额常量需要工程冻结，但通常不改变已确定的产品目标；实现时按一份契约和验收结果确定即可。

本审查仅判断设计收敛程度，没有运行实现验证，也不代表用户采纳全部推荐、结束讨论或授权开始开发。

本文建议尚未整体采纳，不自动开始实现。
