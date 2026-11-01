# ImageGen 总体架构图 · alpha v0.6

本图是配套[架构文档](../alpha-v0.6.md)的总体导览，使用内置 ImageGen 生成与定向修订。精确对象、状态和时序仍以正文及六张可编辑技术视图为准。没有新增架构选择、实验数据或采用决议。

- 语义基线：提交 `020b7e437b88afdedbc9795ea75ca78680dc93c4` 的架构文档与 `01-overview.mmd`。
- 最终图片：[00-overview-imagegen-v0.6.png](00-overview-imagegen-v0.6.png)。
- 控制箭头表示连接发起或管理/观测关系；数据箭头表示双向传输。
- 生成记录：第一次图中中继数据腿误接能力服务，且数据库连线有歧义，未作为仓库产物采用；第二次定向编辑后采用。
- 校对结果：CLI ↔ OIS中继 ↔ OIC ↔ 能力服务；OIC向OIS主动发起控制连接；协议适配为条件扩展；观测与业务分责；存储仅为管理元数据；未声明已实现或HA通过。
- 数据库以文字边界说明呈现，未画详细存储依赖；外部监控等次要路径在技术视图保留。
- 栅格图不提供伪造SVG；可编辑技术拓扑保留在原Mermaid/SVG中。图片更新必须重做语义和文字校对。

## 初次生成提示词

```text
Use case: infographic-diagram.
Create a polished, publication-quality software architecture overview for the existing loom-oi project. This is a professional architecture review graphic, not decorative concept art. Landscape 16:10 canvas, high resolution, exceptionally legible Simplified Chinese text, precise connectors, ample whitespace. Flat vector-like editorial design, white background, navy text, teal data paths, blue control paths, light gray zone panels, subtle amber for conditional components. Avoid 3D, gradients, mascots, marketing claims, fake metrics, logos, and tiny text.

Exact header: "loom-oi · alpha v0.6"
Main title: "原生能力通道 · 总体架构"
Small subtitle: "推荐设计 · 待实现验证"

Use THREE clearly separated trust/deployment zones from left to right. No overlapping connectors. No direct consumer-to-provider connection.
LEFT zone heading "消费侧".
Within it, a top card "原生客户端 / 应用 / Agent", an arrow downward to a large card "oi CLI / 客户端库".
A small optional card below labeled "条件扩展：协议适配", connected by a dashed arrow to the CLI card. This is optional, not required.

CENTER zone heading "OIS · Go". Small zone caption "可信管理与中继".
Within the zone: upper card "身份 · 目录 · 策略", middle large card "授权配对 · 有界中继", lower database cylinder "PostgreSQL" with sublabel "身份映射 / 配置 / 策略".
Connect upper management card to relay with an internal authorization arrow. Connect management card bidirectionally to database in gray. No business payload flow to database. Management and relay are modules of the SAME OIS process, not separate products.

RIGHT zone heading "受控环境".
Top large card "OIC · Go" with sublabel "登记 / 本地目标校验 / 有界转发".
Below it a card "独立能力服务" with sublabel "HTTP / 桌面 / 文件 / 设备".
Join OIC and capability service with a two-way teal line labeled "原生协议".
A compact small card "可选探针" beside the service, blue arrow from probe to service labeled "无副作用检查"; show observation result returns to OIC with a thin blue line. Probe belongs to OIC observation responsibility, not business process supervision.
Below the service, small caption: "业务身份、状态与资源互斥由提供方负责".

EXACT interzone connections:
1. A thin blue control arrow from the CLI to the OIS upper management card, label "HTTPS 发现 / Open".
2. A thin blue control arrow FROM OIC TO OIS upper management card, label "WSS 控制 · OIC 主动出站". Make the arrowhead clearly point LEFT to OIS, showing connection initiation; this is an authenticated mTLS control connection, not business data.
3. A prominent horizontal TWO-WAY TEAL line between CLI and OIS relay, label "WSS 数据腿".
4. A prominent TWO-WAY TEAL line between OIS relay and OIC, label "WSS 数据腿". Nearby small note "OIC 主动建立，数据双向传输".
The consumer and provider data legs pair at the same active OIS. Routing passes through OIS, with no P2P arrow or automatic business replay.

Footer, separated from topology, three succinct principle cells:
"观测独立：online ≠ ready"
"故障恢复：新通道重新鉴权，旧业务不重放"
"部署路线：单活闭环 → 主备恢复验证"
Tiny legend: "蓝色：控制与观测   青色：业务数据   虚线：条件扩展".
Keep all Chinese characters correct. Do not add guarantees of HA, zero-copy, exactly-once, throughput, or already accepted/implemented status. All arrows must have unambiguous endpoints. Prioritize clarity over embellishment.
```

## 定向修订提示词

```text
Edit this existing architecture diagram. Preserve its clean professional style, exact title, all three zone panels, Chinese typography, footer, optional adapter, and all already correct connections. Correct ONLY the architecture wiring and storage presentation described here. This is technical, so edge endpoints are mandatory.

CRITICAL CORRECTION 1: The teal WSS data leg on the right currently INCORRECTLY connects the central relay directly to "独立能力服务". Remove that connection entirely. Draw a clearly visible two-way teal connector FROM the right edge of the central "授权配对 · 有界中继" card TO THE LEFT EDGE of the upper-right "OIC · Go" card. Use an elbow bend in the empty gap between center and right zones: go right from relay, then UP, then right into OIC. Endpoint must physically touch OIC, not the service. Keep label "WSS 数据腿", with small text "OIC 主动建立，数据双向传输". There must be NO relay-to-service connector. The existing vertical teal OIC-to-service connector remains unchanged. The end-to-end data path MUST visibly be CLI ↔ OIS relay ↔ OIC ↔ service.

CRITICAL CORRECTION 2: The gray lines currently incorrectly join relay and database, and add a gray connection between management and relay. Remove BOTH gray connectors and the old "配置与策略管理" edge label. Keep the blue one-way management-to-relay authorization arrow.
Replace the bottom database cylinder with a calm rectangular information panel, clearly a note rather than a traffic node, with the following exact three lines:
"持久化边界"
"PostgreSQL：身份映射 / 配置 / 策略"
"运行连接留内存；不保存业务载荷"
Do not draw any connector to this informational panel. This avoids suggesting that relay business data goes into PostgreSQL.

Remove the stray punctuation immediately before "OIC · Go" so it reads exactly "OIC · Go".
Keep the blue control arrow from OIC pointing left into OIS management. Do not add any new architecture component or new connectivity. Keep all other content and layout as close to original as possible. Ensure text is readable and lines don't cross cards or labels.
```
