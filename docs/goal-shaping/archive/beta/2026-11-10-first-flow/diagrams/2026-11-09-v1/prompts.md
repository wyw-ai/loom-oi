# loom-oi beta 交互图生成提示词

日期：2026-11-09。使用内置 image_gen，分类 infographic-diagram。共五张独立图片；以下为实际提交的完整提示词。图片描述当前共识与候选 Bash 托管实现，不代表已实现。

## 01-top-level：顶层模块交互

```text
Use case: infographic-diagram. Create a precise software architecture diagram for Chinese-speaking engineers. This is one standalone image in a coordinated five-image series for loom-oi beta. High-resolution landscape 3:2 canvas, crisp flat vector-like graphics, white background, large readable Simplified Chinese typography, exact Latin identifiers, generous whitespace, aligned rounded boxes and clean orthogonal arrows. Technical clarity is more important than decoration. Use a consistent restrained palette: Agent slate, OIS blue, OIC teal, Bash App amber, Bash process dark gray. Solid blue arrows mean calls; green arrows mean returned results; thin gray dashed arrows mean configuration or registration, not business execution. Explicit arrowheads, avoid crossings. Put the page number at upper left and a clear Chinese title. No photographic objects, no 3D, no watermark, no invented modules, no source code screenshots, no decorative gibberish. Render all requested text faithfully. Do not insert CU, SSH, oi client, database, load balancer, automatic retries, or extra public Bash tools. Label shared-worker internals as candidate design. A small footer: "loom-oi beta · 2026-11-09 · 设计示意，尚未实现".
Title exactly: "01  顶层模块交互".
Primary purpose: show distinct module ownership, a primary invocation on Host B, and the same two Tools also mapping to Host A.
Main left-to-right chain: "Agent" -> big "OIS" container -> large "Host B" container. Inside Host B, chain "OIC" -> "Bash App" -> "Bash 进程".
Agent small text: "发现目标 / 调用工具".
OIS contains three readable labels: "vMCP / Tool 定义", "权限与 Host 路由", "terminal_exec / terminal_write_stdin".
OIC contains: "加载 YAML", "参数绑定 / 入口选择".
Bash App contains: "exec_command / write_stdin", "进程、session、输出缓冲", plus small amber badge "共享 worker：候选实现".
Bash process text: "执行 cmd".
Main call arrows label Agent→OIS "host_id + input"; OIS→OIC "App.Action + input"; OIC→App "handler + 已适配参数"; App→Bash "创建 / 输入".
Show green return arrows through the same modules in reverse, labeled collectively "output + session_id 或 exit_code". Do NOT draw a direct App→Agent bypass.
Below OIC inside Host B place a document card "App YAML", with short lines "Action + 参数 Schema" and "runtime 默认 / Action 覆盖". A thin dashed gray arrow FROM this document TO OIC labeled "加载声明".
Below primary Host B, show smaller secondary "Host A" card containing "OIC → Bash App → Bash". Draw a branching route FROM OIS TO Host A labeled "可选目标 host-a", while primary route to B is labeled "本次选择 host-b". One shared pair of Tools in OIS, not a duplicated Tool per Host.
Show one separate dashed control arrow FROM Host B OIC TO OIS with label "主动连接 / 能力登记", clearly distinguish it from business call direction.
Bottom two concise callouts: "OIS 定义消费接口；本地 YAML 描述执行接口" and "两侧 Schema 不要求完全相同".
Legend unobtrusive: "蓝：调用   绿：结果   灰虚线：声明 / 登记".
```

## 02-load-register：加载与能力登记

```text
Use case: infographic-diagram. Create a precise software architecture diagram for Chinese-speaking engineers. This is one standalone image in a coordinated five-image series for loom-oi beta. High-resolution landscape 3:2 canvas, crisp flat vector-like graphics, white background, large readable Simplified Chinese typography, exact Latin identifiers, generous whitespace, aligned rounded boxes and clean orthogonal arrows. Technical clarity is more important than decoration. Use a consistent restrained palette: Agent slate, OIS blue, OIC teal, Bash App amber, Bash process dark gray. Solid blue arrows mean calls; green arrows mean returned results; thin gray dashed arrows mean configuration or registration, not business execution. Explicit arrowheads, avoid crossings. Put the page number at upper left and a clear Chinese title. No photographic objects, no 3D, no watermark, no invented modules, no source code screenshots, no decorative gibberish. Render all requested text faithfully. Do not insert CU, SSH, oi client, database, load balancer, automatic retries, or extra public Bash tools. Label shared-worker internals as candidate design. A small footer: "loom-oi beta · 2026-11-09 · 设计示意，尚未实现".
Title exactly: "02  加载与能力登记".
Create a vertical-time sequence diagram with five swimlanes, left to right: "操作者", "OIC", "App YAML", "Bash App worker", "OIS". Worker lane must have small badge "候选实现". No Bash command process lane: loading does not execute cmd.
Use numbered messages in chronological order, enough vertical spacing:
1 操作者→OIC: "oic start"
2 OIC→OIS: "主动连接 / 身份认证"
3 操作者→OIC: "oic load app.yaml"
4 OIC→App YAML: "读取 Action / 参数 Schema / runtime"
5 App YAML→OIC return: "声明与执行映射"
6 OIC self-action box: "解析有效 runtime 与 handler"
   next to it a clear short note "根配置默认；Action 更具体优先"
7 OIC→Bash App worker: "启动入口 / 交付本机配置"
8 Bash App worker→OIC: "ready + handlers"
9 OIC self-action box: "建立 Action → Schema / 入口索引"
10 OIC→OIS: "登记 Host / App / Action 可用状态"
11 OIS→OIC: "登记确认"
12 OIC→操作者: "ready / registered"
Place three short bottom callout cards:
"load 不执行 cmd"
"本地就绪 ≠ 中心已登记"
"登记不自动覆盖 OIS Tool 定义"
Place a smaller footer note above standard footer: "参数差异至多提示；入口不可用仍需报错".
Do not reverse the active connection arrow. Do not make App YAML an executing service. Do not show the worker directly registering itself with OIS.
```

## 03-exec-command：启动执行与返回分支

```text
Use case: infographic-diagram. Create a precise software architecture diagram for Chinese-speaking engineers. This is one standalone image in a coordinated five-image series for loom-oi beta. High-resolution landscape 3:2 canvas, crisp flat vector-like graphics, white background, large readable Simplified Chinese typography, exact Latin identifiers, generous whitespace, aligned rounded boxes and clean orthogonal arrows. Technical clarity is more important than decoration. Use a consistent restrained palette: Agent slate, OIS blue, OIC teal, Bash App amber, Bash process dark gray. Solid blue arrows mean calls; green arrows mean returned results; thin gray dashed arrows mean configuration or registration, not business execution. Explicit arrowheads, avoid crossings. Put the page number at upper left and a clear Chinese title. No photographic objects, no 3D, no watermark, no invented modules, no source code screenshots, no decorative gibberish. Render all requested text faithfully. Do not insert CU, SSH, oi client, database, load balancer, automatic retries, or extra public Bash tools. Label shared-worker internals as candidate design. A small footer: "loom-oi beta · 2026-11-09 · 设计示意，尚未实现".
Title exactly: "03  exec_command：启动执行".
Create a readable vertical-time sequence diagram with five lanes: "Agent", "OIS", "OIC", "Bash App worker", "Bash 进程". Add amber "候选实现" badge to the worker lane.
Top request callout: 'host_id=host-b · cmd="./long-task.sh" · yield_time_ms=1000'.
Messages:
Agent→OIS: "terminal_exec"
OIS self box: "权限检查 / Tool → App.Action"
OIS→OIC: "bash.exec_command + input"
OIC self box: "按 YAML 绑定参数 / 选择入口"
OIC→Bash App worker: "invoke(call_id=C1, handler=exec_command)"
Bash App worker→Bash process: "创建执行"
Bash process→Bash App worker green: "持续采集 stdout / stderr"
Worker vertical activity bar labeled "等待本轮窗口，最多 1000ms".
Below this place a UML-style mutually exclusive ALT frame with TWO separate rows, no sequential implication:
Branch label "已结束并完成输出收尾": worker→OIC→OIS→Agent green return chain, exact result text "output + exit_code".
Branch label "尚未完成": worker→OIC→OIS→Agent green return chain, exact result text "output + session_id"; process lifeline continues downward with label "命令继续运行".
Add a compact lower-right note near worker: "进程与 session 状态保留在 Bash App".
Bottom callout exactly: "等待窗口结束 ≠ 命令被终止".
Do not return both exit_code and session_id simultaneously. Do not show OIC interpreting Bash syntax. Do not create a new Bash process on a response. Tool response must travel through OIC and OIS.
```

## 04-continue-session：持续读写与十分钟等待

```text
Use case: infographic-diagram. Create a precise software architecture diagram for Chinese-speaking engineers. This is one standalone image in a coordinated five-image series for loom-oi beta. High-resolution landscape 3:2 canvas, crisp flat vector-like graphics, white background, large readable Simplified Chinese typography, exact Latin identifiers, generous whitespace, aligned rounded boxes and clean orthogonal arrows. Technical clarity is more important than decoration. Use a consistent restrained palette: Agent slate, OIS blue, OIC teal, Bash App amber, Bash process dark gray. Solid blue arrows mean calls; green arrows mean returned results; thin gray dashed arrows mean configuration or registration, not business execution. Explicit arrowheads, avoid crossings. Put the page number at upper left and a clear Chinese title. No photographic objects, no 3D, no watermark, no invented modules, no source code screenshots, no decorative gibberish. Render all requested text faithfully. Do not insert CU, SSH, oi client, database, load balancer, automatic retries, or extra public Bash tools. Label shared-worker internals as candidate design. A small footer: "loom-oi beta · 2026-11-09 · 设计示意，尚未实现".
Title exactly: "04  write_stdin：持续读写".
Subtitle: "十分钟命令只启动一次".
Draw five swimlanes: "Agent", "OIS", "OIC", "Bash App worker", "原 Bash 进程". Worker marked "候选实现".
A condition strip at top: "已有 session_id=s1，Host=host-b".
Main large sequence LOOP frame label: "重复等待：每轮最多 30 秒，约 20 轮".
Inside loop, sequence:
Agent→OIS: 'terminal_write_stdin(host-b, s1, chars="", yield=30000)'
OIS→OIC: "权限检查 / 原 Host 路由"
OIC→Bash App worker: "绑定参数 / 新 call_id"
Bash App worker self box: "查找原 session，等待本轮输出"
Original Bash process continues a long active bar, green arrow to worker "后台持续采集输出".
Worker→OIC→OIS→Agent green return chain: 'output="" + session_id=s1'.
Below loop, final event strip "约 10 分钟后，进程结束并完成收尾".
Worker→OIC→OIS→Agent green return chain: "output + exit_code".
Lower inset, visually separate from ten-minute no-input case, titled "需要交互时（tty=true）":
"Agent → OIS → OIC → Bash App → 原进程" with label 'chars="hello\n"：先输入，再等输出'. This is an alternate input operation, not another exec.
Two clear footer callouts: "空输出不代表结束；exit_code 表示完成" and "新 call_id，同一 session_id；不要重复 exec_command".
Ensure the loop sends one new write_stdin request per round, NEVER automatically resubmits exec_command. The process stays on its original Host. Do not imply a single call waits ten minutes.
```

## 05-finish-failure：结束断连与回收

```text
Use case: infographic-diagram. Create a precise software architecture diagram for Chinese-speaking engineers. This is one standalone image in a coordinated five-image series for loom-oi beta. High-resolution landscape 3:2 canvas, crisp flat vector-like graphics, white background, large readable Simplified Chinese typography, exact Latin identifiers, generous whitespace, aligned rounded boxes and clean orthogonal arrows. Technical clarity is more important than decoration. Use a consistent restrained palette: Agent slate, OIS blue, OIC teal, Bash App amber, Bash process dark gray. Solid blue arrows mean calls; green arrows mean returned results; thin gray dashed arrows mean configuration or registration, not business execution. Explicit arrowheads, avoid crossings. Put the page number at upper left and a clear Chinese title. No photographic objects, no 3D, no watermark, no invented modules, no source code screenshots, no decorative gibberish. Render all requested text faithfully. Do not insert CU, SSH, oi client, database, load balancer, automatic retries, or extra public Bash tools. Label shared-worker internals as candidate design. A small footer: "loom-oi beta · 2026-11-09 · 设计示意，尚未实现".
Title exactly: "05  结束、断连与回收".
Subtitle exactly: "候选运行规则 · 各类事件分别处理".
Create a clean 2×2 grid of four large scenario panels. Each panel must contain a small explicit module flow with arrows and two short notes. Keep labels large and readable. Same consistent role colors.
Top-left panel title "正常完成":
Flow "Bash 退出" → "App 排空输出" → "OIC → OIS → Agent".
Result label "output + exit_code".
Notes "确认退出后才给最终结果" and "最终结果领取后关闭句柄".
Top-right panel title "取消当前读取":
Flow "Agent 取消请求" → "OIS / OIC 转交" → "App 停止本次等待".
Below a distinct still-active box "原 Bash 执行继续".
Notes "未提交输出保留" and "已输入字符不能撤回".
Bottom-left panel title "OIS—OIC 断连":
Draw OIS and OIC boxes with a clearly broken connection, and on OIC side show "App → Bash" still running.
Notes "断连不等于执行停止" and "结果不明：不自动重跑".
Small extra label "原实例仍存活，可用原句柄接续".
Bottom-right panel title "卸载、崩溃与本地期限":
Use separate compact rows:
"卸载：OIC → App → 回收执行"
"崩溃：OIC 兜底清理托管进程"
"期限：App 终止执行并收尾"
Notes "覆盖托管子进程与终端资源" and "worker 重启后旧 session 失效".
Do not make request cancellation always kill Bash. Do not equate disconnection with process termination. Do not promise cross-restart session restoration or automatic re-execution. Distinguish normal final result from uncertain failure state.
```

## 定向修订提示词

初次生成后检查了文字与箭头。01、04、05 使用以下定向修订；02、03 沿用初次生成结果。

### 01-top-level

```text
Edit this architecture diagram for arrow correctness. Preserve the title, all Chinese text, white background, node identities, Host A/B boundaries, blue request paths, gray registration/configuration arrows, and overall visual style. Change ONLY the incorrect green return wiring. Delete the long green bus that currently connects the Bash process directly to OIS and branches upward into OIC. Replace it with FOUR SEPARATE green directed connections, each terminating at the NEXT module: (1) Bash process -> Bash App, (2) Bash App -> OIC, (3) OIC -> OIS, (4) OIS -> Agent. Route these under the blue calls using clean orthogonal lines, with visible arrowheads at each receiving box; move local spacing if needed. No green connection may skip Bash App, OIC, or OIS. Label process->App "输出 / 退出状态"; label App->OIC "output + session_id 或 exit_code"; the two remaining green legs may simply say "结果". Avoid connecting return lines to the App YAML arrow. This is the essential correction. All other meanings, labels, and layout should remain the same.
```

### 04-continue-session

```text
Edit this diagram with three precise technical corrections; preserve its style, title, colors, five module columns, ten-minute scenario and bottom interaction inset.
1. In the request box replace "yield=30000" with the exact identifier "yield_time_ms=30000". Widen/reflow that box if needed without tiny text.
2. In BOTH green response rows (ongoing and final), draw three distinct consecutive leftward arrows with clear receive/send stops: Bash App worker -> OIC, OIC -> OIS, OIS -> Agent. A line that directly bypasses either OIC or OIS is incorrect. Place small green labeled stops "OIC" and "OIS" at the corresponding columns in each response row if needed to make the forwarding unambiguous. The ongoing payload stays 'output="" + session_id=s1'; the final payload stays 'output + exit_code'.
3. The bottom input example must show chars="hello\n" using exactly one visible backslash followed by n, not two backslashes. Retain "先输入，再等输出".
Keep all other content unchanged. Do not introduce new executions or any extra tools.
```

### 05-finish-failure

```text
Edit this diagram, preserve the four scenario cards, title, all substantive lifecycle notes, layout and colors. Make these exact corrections:
1. In the top legend change the OIS Chinese subtitle from "会话服务" to "工具与路由". Change the OIC subtitle from "会话控制" to "接入与适配". Keep the App as owner of Bash execution state.
2. In the "正常完成" panel, remove the curved green arrow that bypasses OIC and OIS and goes from App straight to Agent. Keep the existing row of modules "Bash 退出" -> "App 排空输出" -> "OIC" -> "OIS" -> "Agent". Color the three response arrows App->OIC, OIC->OIS, OIS->Agent green, with a visible arrowhead on EACH receiving module. The "output + exit_code" box below is a simple payload annotation with NO bypass arrow attached.
3. In the disconnected panel refine the little label to "重连且原实例仍存活，可用原句柄接续" so it does not imply calls work while disconnected.
Do not alter the cancellation meaning or make OIC own the Bash session state. All other text remains intact.
```

### 01 顶层返回端口的最终修订

```text
Make one tiny correction to the supplied diagram; preserve ALL text, all boxes, layout, colors and other arrows. The green result connection FROM OIC TO OIS currently has two arrowheads: a leftward head at OIS (correct), and an upward head at the LEFT bottom port of OIC, approximately image position x=786,y=400 (incorrect). Remove ONLY that upward green triangle at the LEFT bottom OIC port; leave a plain line connected to OIC as the source. Retain the leftward green arrowhead entering OIS. Do NOT remove the separate upward green arrowhead at the RIGHT bottom OIC port, approximately x=840,y=400: that belongs to Bash App -> OIC and is correct. The final result route is exactly Bash process -> Bash App -> OIC -> OIS -> Agent. No other change.
```
