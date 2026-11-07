# loom-oi beta：首个完整使用流程草稿

状态：讨论中。以[当前共识](../../../current/beta/design.md)为已确认基线；本页的字段、执行方式、CLI 和 Done 为候选，尚未整体采纳。来源与被替代提议见[沟通笔记](notes.md)。

配套文章：[从 App YAML 到 Agent 调用](app-to-agent-walkthrough.md)，用机器 A 的 computer-use 与机器 B 的 Bash 展开目录、完整配置和执行结果。文章进一步提出 config_schema、统一输入输出、host_id/input 分层、目标发现等候选约定，尚未同步为已确认共识。

## 已确认的终态骨架

执行侧使用 Host、App、Action；消费侧由 OIS 把选定 Action 映射为 Tool，再组合成 vMCP。一个 App 一份 YAML，声明 Action 参数及执行脚本，配套 actions 目录。调用方式由项目统一，不要求应用作者逐个设计 HTTP/TCP/MCP 接入配置。

OIC 保留为轻量 CLI 宿主。本期不做独立消费端 oi。执行侧与消费侧可分别演进，同一 App 能由多个 Host 提供，多个实现可归入同一个 Tool。Agent 查询目标并在调用时明确指定目标 ID。

## 推荐的 Bash 首个流程

1. 在两台 Host 上准备 Bash App 的 YAML 与脚本，并具备实际执行 Bash 的环境。
2. OIC 加载 YAML，校验 App、Actions、参数结构与执行入口，向 OIS 登记本 Host 可提供的 App/Actions。
3. OIS 配置一个 Tool，例如 terminal_exec，绑定 App bash 的 Action exec，并限定可选 Host。
4. OIS 将该 Tool 加入一个 vMCP；使用者配置支持的 Agent 连接该入口。
5. Agent 查询可调用 Host，向 Tool 提交目标 ID 和脚本。OIS 解析 Tool 映射及目标，OIC 找到对应 App.Action 并按公共约定运行。
6. OIC 收集执行结果，经 OIS 返回 Agent。切换目标 ID 可以明确选择另一台 Host。

Tool 展示名可以与 Action 名不同。参数如何继承或变换、系统路由 ID 如何与业务参数组合仍需定义。建议目标 ID 用于 OIS/OIC 路由，Action 脚本只接收自己的业务输入。

## App 的交付候选

以普通目录作为首个参考 App 的交付形式：

```text
bash/
  bash.yaml
  actions/
    exec.py

computer-use/
  computer-use.yaml
  actions/
    click.py
    screenshot.py
```

目录结构体现 App 包含多个 Action，不规定每个脚本必须是 Python，也不承诺这些 App 都属于首版。解释器加脚本与直接可执行程序都可以作为执行入口候选，语言及安装机制另定。

App 是描述与实现的组织单位，不要求它拥有一个独立常驻服务。首个 Bash 可以按调用运行 Action 脚本；需要持续浏览器、终端或桌面状态时，再比较持久执行上下文或常驻执行器，不把每次新进程扩大为所有 Action 的规则。

## YAML 结构候选

以下仅为字段讨论草图，Python/Bash 路径假定示例环境已准备：

```yaml
version: 1
app:
  code: bash

actions:
  exec:
    input_schema:
      type: object
      properties:
        script:
          type: string
      required: [script]
    run: ["python3", "./actions/exec.py"]
```

- version 描述加载文件格式版本，不与 App 发布版本混同。
- app.code 和 Action 标识供执行侧识别、登记和引用；对外 Tool 名称与描述由 OIS 配置。
- input_schema 描述这个 Action 实际接受的输入。结构形式是否采用完整 JSON Schema、支持哪些约束仍需确定。
- run 是程序和参数列表的候选表达；相对资源建议以 App 描述文件所在目录解析，不经字符串拼接的 shell 间接启动。
- 启动解释器是执行入口问题，不是要求各 App 选择传输协议。公共接入方式由项目统一，YAML 不再推荐 runtime.transport/http headers 等逐 App 配置。
- 输出 Schema、环境参数、业务配置和超时等字段只有在责任及行为明确后再加入，避免为示例预设完整框架。

此前以 runtime.command/transport 启动常驻 bash-oic、再通过 provides.tool/action 关联中心 Tool 的例子不再作为主方案。它可以作为特殊常驻执行器的技术备选，但不能定义所有 App 的形态。

## OIC 如何控制 Action：统一运行约定候选

推荐由 OIC 提供通用 Action 运行能力：根据已加载描述选择入口，按统一方式交付输入，跟踪调用并收集结果。应用作者实现具体业务脚本，不必重复实现一个 initialize/invoke/cancel/shutdown 服务。

首个 Bash 的可验证候选：

- OIC 将业务输入编码为一份结构化输入，通过约定的输入通道交给脚本。
- exec 脚本读取 script，启动 Bash，收集业务 stdout、stderr 和退出状态。
- 脚本按统一结果约定输出；脚本自己的诊断与业务结果分开。
- OIC 负责入口进程、期限和运行记录，取消如何覆盖脚本启动的子进程仍需验证和明确。
- 结果至少要区分正常业务结果与执行器失败，不能把一个进程的退出码自动等同于所有业务操作结果。

具体是 stdin/stdout、文件或其他通道尚未采纳；即使采用 JSON，也需另外定义分帧和输出大小。截图、文件和流式输出需要清楚的结果表示，不能由文本 Bash 示例推定所有 Action 只返回字符串。

共同机制由 OIC 提供，业务执行留在脚本。SDK 可在实际重复代码出现后提供辅助读写与校验，不将它作为最初的强制接入方式。常驻进程、JSON-RPC 和 HTTP 仍可用于适合的边界，但不是每个 App 必须自行实现的服务协议。

## CLI 与加载候选

OIC 是 CLI 宿主已经明确；常驻后台配合短时管理命令仍是候选。可讨论的操作入口：

```sh
oic start
oic load ./bash/bash.yaml
oic apps
oic unload bash
oic stop
```

load 的推荐含义调整为：加载 App 描述，校验 Action Schema 和执行入口，建立本地 App/Action 索引，并发布可提供的执行接口。加载成功不要求提前执行一次有副作用的 Action，也不要求为了每个 App 启动一个空闲常驻进程。

具体登记成功是否属于 load 成功条件、是否自动重载文件、重复加载如何处理、重启后如何恢复尚未确定。OIC 加载与 OIS 创建 Tool/vMCP 映射是独立步骤，不隐式替使用者创建全部消费侧工具。

## OIS 的映射与 Schema 候选规则

执行侧 YAML 的 Action Schema 描述实际接口，OIS 管理消费侧 Tool 的展示、参数说明、Prompt 与组合。

建议 OIS 在映射时引用并校验 Action 的输入结构，补充面向模型的说明和系统路由字段；若允许重命名或变换业务参数，应有显式映射，不能静默产生与执行接口不一致的另一套契约。具体结构权威、版本与更新规则仍需收束。

同一个 Tool 可对应多个 Host 上的 App.Action。兼容性需要超过名称相同；同一负载池还需实际环境适合互换。宿主机统一称 Host，直接选择宿主机时可考虑 host_id 字段；此前 node_id 不再作为默认推荐。未来逻辑池需要另行命名与设计，不默认加入首版。

vMCP 可以选择一个 Action、一个 App 的 Actions 或跨 App/Host 组合。展示工具集合不意味着系统必须自动规划业务流程或把一次 Tool 调用广播给所有目标。

## 无独立 Host 的 App

对应远程 HTTP 服务的 App YAML 可以位于 OIS，这是已明确的接入方向。仍保留 App/Action 描述和消费侧映射，不为表达它而虚构物理 Host。

推荐将 App 描述与执行位置分开理解；是否由 OIS 执行适配脚本、复用某种 OIC 运行能力或交给其他明确执行入口，目前均未确定。该扩展也不意味着重新引入逐 Action 任意 HTTP 模板作为必选 YAML 结构。

## 对之前技术选项的处理

OIC 保留，但其后台进程与服务安装方式未定。Go、Rust、Python、SDK、HTTP 都是不同维度的候选，未因原始讨论中的建议而定案。

优先比较 App 描述和脚本输入输出契约，再选择能实现它的 OIC 和脚本运行方式。独立常驻适配服务可以解决持久状态需求，但不再是首个 Bash 示例的默认推荐。OIS—OIC 的通信仍需独立定义，不能由本地脚本输入通道代替。

## 候选 Done

- OIC 加载一个含 exec Action 的 Bash App，并登记 Action 结构和实际 Host。
- OIS 将该 Action 映射为 Tool，组成一个 vMCP；支持的 Agent 能发现并调用它。
- 两台 Host 提供相同 App.Action 时，Agent 可通过同一个 Tool 及不同目标 ID 分别在两台 Host 上执行并核对结果。
- YAML/Schema 错误、入口不可用、业务失败与执行器失败有可解释结果。
- 用一个返回非文本结果的 Action 检验结果契约能否表达截图或文件；是否将 computer-use 实现纳入首版另行确定。
- 从准备 App 到 Agent 调用的步骤可复现。取消、超时、断连等验收需要在具体承诺确定后补齐。

## 关键未决事项

- App 与 Host 上实例的标识、版本和更新关系。
- 完整 YAML Schema 与 Action 入口表达。
- 输入输出、错误、非文本结果、取消、会话与资源管理。
- OIC 的前台/后台形态、加载状态和登记成功标准。
- Action 到 Tool 的 Schema、描述和参数映射规则。
- 目标查询、权限、多 Host 选择与未来路由池。
- 无独立 Host App 的执行入口。
- 首个客户端、平台与最终 Done。

本文建议尚未整体采纳，不自动开始实现。
