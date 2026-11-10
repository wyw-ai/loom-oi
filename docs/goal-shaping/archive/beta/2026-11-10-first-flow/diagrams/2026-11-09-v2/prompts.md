# 首版固定调用链图提示词

日期：2026-11-09。使用内置 image_gen。用户明确取消首版多 Host 路由，本图据此展示固定调用链；不冻结具体通道协议或实现语言。

```text
Use case: infographic-diagram. Create one clean, accurate Chinese software architecture diagram for loom-oi beta, superseding a more complex multi-host design. White background, high-resolution wide landscape 16:9, generous whitespace, crisp flat graphics, large readable Chinese sans-serif text and exact English identifiers. Minimal engineering diagram, no 3D, no decorative icons, no extra modules, no watermark.
Title exactly: "loom-oi beta · 首版固定调用链".
Subtitle exactly: "工具预先绑定 Action 和通道，调用时直接转交".
The main diagram is ONE straight horizontal chain with exactly eight clearly labeled stages in this exact order:
"Agent" → "vMCP" → "Tool" → "Action" → "通道" → "OIC" → "oiapp" → "目标 cmd".
Place "vMCP", "Tool", and "Action" together INSIDE a thin blue grouping container labeled "OIS". Action here is the configured reference to the application action, not a separate business execution process. The channel is a connecting communication stage, not an independent routing/scheduling service. Place OIC, oiapp, and cmd inside a thin neutral grouping container labeled "受控侧". Agent is outside both containers.
Each adjacent stage has ONE solid blue arrow pointing to the next; no branch or alternate route. Small secondary text inside selected boxes:
vMCP: "工具集合"
Tool: "terminal_exec"
Action: "bash.exec_command"
通道: "固定连接"
OIC: "参数适配 / 转交"
oiapp: "Bash App"
目标 cmd: "bash 执行命令"
Use slate for Agent and target cmd, blue for OIS, teal for OIC, amber for oiapp. Keep the labels readable; labels may wrap sensibly.
Under the OIC box place a small document card "App YAML" with two lines "Action + 参数 Schema" and "runtime：根默认 / Action 优先". A thin gray dashed arrow from that document UP to OIC means "加载声明". Keep this configuration arrow visually separate from the main business chain.
Below the main flow, include one plain green caption with NO skipping/bypass arrow: "结果沿原调用链逐级返回".
At the bottom, add a compact two-row example table with columns "工具", "固定 Action", "接续方式":
row 1: "terminal_exec" | "bash.exec_command" | "启动命令，返回 session_id 或 exit_code"
row 2: "terminal_write_stdin" | "bash.write_stdin" | "同一通道、同一实例，使用 session_id"
A concise footnote: "Agent 只传业务参数；不传 host_id 或 channel_id".
Another small note: "首版不做多 Host 合并、目标发现、自动选路或负载均衡".
Standard footer: "2026-11-09 · 固定链路已确认 · 字段与执行实现仍为候选".
Absolutely do not show Host A/Host B, a host pool, list_targets, allowed_hosts, permission-check workflow boxes, automatic retry, a new dispatcher layer, CU, SSH, or an oi client. Do not display a direct App→Agent bypass. Do not suggest that the channel is chosen by Agent. Keep the whole image much simpler than a lifecycle sequence diagram.
```
