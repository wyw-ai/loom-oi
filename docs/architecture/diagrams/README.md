# alpha v0.6 架构图册

配套[架构与实现方案](../alpha-v0.6.md)，2026-11-02。全部为当前推荐设计视图，尚非已实现拓扑；正式采用、实现验证与设计建议分别记录。

## 专业绘制总体导览

![ImageGen总体架构图](00-overview-imagegen-v0.6.png)

使用内置ImageGen生成并修订，已逐条校对箭头、信任边界和文字；[绘图说明与完整提示词](imagegen-overview.md)及[图片校验清单](imagegen-manifest.json)一并保存。该图供整体阅读；接口、状态、恢复顺序的精确细节见下列可编辑技术视图和正文。

## 六张技术视图

| 图 | 阅读问题 | 预览 / 矢量 / 源文件 |
| --- | --- | --- |
| 1 总体职责 | 从消费者到受控能力的控制与数据路径，哪些组件可选 | [PNG](01-overview.png) · [SVG](01-overview.svg) · [Mermaid](01-overview.mmd) |
| 2 建连时序 | 谁认证、谁授权、双数据腿何时开始转发 | [PNG](02-open-sequence.png) · [SVG](02-open-sequence.svg) · [Mermaid](02-open-sequence.mmd) |
| 3 观测状态 | 执行状态与有效就绪观测如何分开 | [PNG](03-observation-state.png) · [SVG](03-observation-state.svg) · [Mermaid](03-observation-state.mmd) |
| 4 故障恢复 | 活动OIS失效后如何恢复新通道而不重做旧业务 | [PNG](04-recovery-sequence.png) · [SVG](04-recovery-sequence.svg) · [Mermaid](04-recovery-sequence.mmd) |
| 5 部署形态 | OIS单活/热备、数据库与OIC故障域 | [PNG](05-deployment.png) · [SVG](05-deployment.svg) · [Mermaid](05-deployment.mmd) |
| 6 实施依赖 | 先走通哪条链，再补哪些正确性与恢复验证 | [PNG](06-delivery-dependencies.png) · [SVG](06-delivery-dependencies.svg) · [Mermaid](06-delivery-dependencies.mmd) |

六张技术视图的Mermaid源文件是绘图输入，生成的PNG/SVG与源一起版本化。[渲染清单](manifest.json)记录输入/输出哈希和渲染器版本；ImageGen图片另记生成清单。后续改图必须同步受影响的版本并重新校对，不能只改单独一份。源码中未包含生产地址或密钥。
