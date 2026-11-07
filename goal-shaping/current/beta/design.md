# loom-oi beta：当前设计共识

本文记录已确认的产品方向及已核对的跨会话共识，不代表代码现状、已交付能力或验收结果。候选方案见[活跃草稿](../../drafts/beta/first-flow/design.md)，讨论来源与演进见[沟通笔记](../../drafts/beta/first-flow/notes.md)。

## 目的与范围

让分散在不同机器、应用和设备上的功能可以被 Agent、脚本或其他程序发现、选中并使用，同时保留提供方的控制权。

beta 从头定义产品。alpha 用于理解原始意图，不自动继承其职责划分、实现和技术方案。先明确目标、概念和首个完整流程，再设计、实现和验证。本期不做独立消费端 oi。

## 核心概念

| 概念 | 含义 |
| --- | --- |
| Host | 提供受控功能的宿主机；宿主机统一使用 Host 命名 |
| App | 一组相关 Action 的组织与接入单位，由一份 YAML 描述，例如 bash、computer-use |
| Action | App 中具体可调用的操作，例如 exec、click、screenshot |
| vMCP | 面向消费者的 MCP 入口，将选定 Action 映射成 Tool，并组成工具集 |

App 包含 Action，Host 承载 App。同一种 App 可以由多个 Host 提供，一个 Host 可以提供多个 App。这些是关联关系，不固定为一条部署或请求转发层级。

Tool 是 OIS 面向 MCP 消费者配置的操作呈现；执行侧的 App、Action 标识与消费侧的 Tool 展示名称分开。原先用于宿主机的 Node 名称不再使用。未来如引入负载池或逻辑路由目标，需要另行定义，不再用 Node 混指宿主机。

## OIC 与 App 描述

OIC 保留为轻量 CLI 宿主，负责受控侧接入，加载 App YAML，识别本机提供的 App、Actions、参数约定及相应执行脚本。接入已有应用也是统一接入模型的变体。

一个 App 一份 YAML，配套 actions 目录保存执行脚本。computer-use 的 click、screenshot 等操作属于同一个 App；bash、browser-use、wechat 等可以分别有自己的 App 描述。具体目录名、字段、脚本入口格式和打包规范尚未冻结。

YAML 至少表达 App 标识、Action 清单、参数 Schema 和执行脚本对应关系。它不以消费侧 Tool 展示名称、描述或 Prompt 作为必填职责；App 和 Action 自身的标识仍然需要。

项目统一接入和调用方式，App 声明各 Action 的参数结构。接入方式统一不意味着所有 Action 接受相同参数。具体传输、输入输出、脚本启动和控制契约仍需设计。

OIC 是否采用常驻后台、是否按调用启动脚本、是否支持常驻执行器和如何取消或回收资源，尚未确定；App 不被定义为必须存在一个独立常驻协议进程。

## OIS 与消费侧映射

OIS 提供 API，管理 MCP 转换和工具组合。它配置哪些 Action 映射成哪些 MCP Tool，以及哪些 Tool 组成一个 vMCP；映射范围可以按 App、Host 或单个 Action 选择。

OIC 提供执行侧声明与可执行接口，不负责组装 MCP。工具本身仍由 OIS 面向消费者配置，执行侧 YAML 描述实际 Action 及参数约定。Tool 与 Action 的结构兼容、是否允许参数映射、版本及展示信息的具体字段尚未确定。

一个 vMCP 可以暴露一个 Action、一个 App 的 Actions，或跨多个受控方组合。一个 MCP Server 与一个 App 或一项所谓能力之间没有固定一一对应要求。

## 工具合并与目标路由

多个 Host 提供 Bash 时，OIS 可以将实现映射到同一个 Bash Tool，保留各 Host 的执行目标。

Agent 能查询可调用目标的 ID，并在调用这种合并工具时明确传入目标 ID。工具及目标共同决定执行位置；必填目标不表示所有调用永久选择同一台 Host。查询接口、参数名和权限过滤规则仍未确定。

多 Host 选择及未来负载均衡是需要继续设计的方向。逻辑路由目标、成员互换条件、会话保持和调度策略尚未冻结，不自动纳入首版。

## 没有独立实体 Host 的接入

App 描述层仍需保留。如果接入对象只是一组远程 HTTP 接口、没有独立受控机器，对应 YAML 可以放在 OIS。

这确定的是统一 App 描述及其可放置位置，不代表已经设计 OIS 执行器、通用 HTTP 适配字段、凭据管理或具体执行部署。完整 MCP 接入、单 Action 接入及多受控方组合也使用统一描述和映射思路，具体适配方式尚未确定。

## 工作方式与 Done

开发及设计文档使用 beta 分支的独立 worktree；main、alpha 和历史实验 worktree 保留，alpha 的未提交改动不修改或删除。

SDK、Go、Rust、混合实现和具体传输都未定案。用户对目标讨论的授权不等于开始实现、提交或发布。

完整 Done 尚未形成。需要明确 App 准备与加载、Action 登记、OIS Tool/vMCP 映射、Agent 接入、目标选择、实际执行与结果核对，以及失败处理的完整场景。

## 尚未确定

- 完整 App YAML Schema、App 定义与各 Host 上实例的标识方式。
- Action 脚本的输入、输出、错误、非文本结果、取消和执行环境契约。
- OIC CLI 与后台形态、加载/卸载、持续会话、并发、恢复与资源管理。
- Action 到 Tool 的兼容和映射规则、目标查询、路由字段与授权。
- 多 Host 选择及未来负载均衡的具体范围和策略。
- 无独立 Host 的 App 在 OIS 一侧如何接入与执行。
- 首个支持的客户端、平台、交付形式，以及完整验收标准。
