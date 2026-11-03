# 演进记录：仓库内使用说明

本目录是[问题与方案演进记录](../lineage.md)的机器可读补充。当前只整理#14/#15的6个有来源节点与4条关系，不表示完整项目历史。每次直接读取文件并校验，无数据库、网络调用、模型依赖或持久索引。实现为[一个标准库脚本](../../tools/issue_evolution.py)，Python 3.10及以上；Git仅用于可选的历史保真检查。

## 常用命令

在仓库根目录运行：

```bash
python tools/issue_evolution.py validate
python tools/issue_evolution.py validate --base HEAD
python tools/issue_evolution.py search '独立转发工作器' --issue 15
python tools/issue_evolution.py trace A-CHANNEL-INTEGRATED
python tools/issue_evolution.py impact C-PATH-LOAD
python tools/issue_evolution.py graph --issue 15
python tools/issue_evolution.py compare L-PATH-BEFORE-PROCESS spec/TASK-002-issue-evolution/cases/changed-conditions.json
```

`--root <仓库>`可放在子命令之前。validate不加base只检查当前数据，**检查旧阶段未改必须显式指定可信基线**；首次导入前的基线没有记录，保真数量为0属实但不能证明后续未改。该检查不保护GitHub在线评论，也不能阻止人绕过命令修改Git历史。

search支持登记的机制标识/别名、字面匹配和中英文词项候选（中文二字片段），`--limit`范围1–100。分数只是排序，不能视为概率；同义词未登记可能漏检，语义相同与否仍须核对原文。别名也可指相关研究线索，不保证机制等同。例如“独立转发工作器”找回的是旧进程专项，`process-boundary-first`表示当时先讨论进程的分析次序，不能当作所有中继隔离设计的机制ID。compare需要输入者明确正在比较的机制，不把检索第一名直接当成同一机制。

trace从判断逆向追溯来源、前提、支持和前序；impact仅沿明确的前提/支持关系向下提示复核，普通相关不传播失效。默认深度8，`--depth`范围1–50；每节点只展示第一条可达路径，并报告截断。graph按一个Issue生成Mermaid源码，跨单节点不在本视图中；命令不渲染图片、不执行内容中的指令。完整边的理由与范围须回到记录核对。

JSON输出含输入指纹、覆盖缺口和决策边界；图源码也附指纹。返回码0代表命令完成，**并非批准/拒绝方案**；错误返回1并向stderr输出JSON。命令均不写仓库，也不会关闭或修改Issue。

## 记录契约与维护

每个`records/STAGE-*.json`是一份不可覆写的阶段记录。可参考[当前实例](records/STAGE-INITIAL.json)。使用现有编辑/Git流程增加新文件；不为每个句子建立节点。未来修改解释、修正关系或撤回错误关联，新增阶段及关系/withdrawal，保留原文件。一个文件内的节点、边、来源、撤回及stage使用全局唯一ID（大写字母开头，最多80位字母/数字/下划线/横线）。

| 对象 | 必填内容与含义 |
| --- | --- |
| 阶段 | schema_version=1、stage（等于文件名）、recorded_on、recorder、coverage，以及sources/nodes/edges/withdrawals列表；未知字段会报错 |
| 来源 | id、url、snapshot、sha256、group、kind。snapshot为仓库内文件并核对原始字节；没有快照时snapshot/sha256都为null，工具明确提示仅定位。group标同一次原始来源；计数不证明独立性 |
| 节点 | id、kind、issue、title、text、sources、basis、review、mechanism、aliases、conditions、outcome、reason、revisit；无适用机制用null、无条件用空对象。重要条件未知用null，不用“未知”字符串伪装为已明确 |
| 关系 | id、from、to、relation、scope、reason、sources、review。方向是来源/前提/证据→判断，旧版本→新版本；准确scope比连线数量重要 |
| 撤回 | id、edge、reason、sources、review。撤回已核对时不再使用那条边，但保留原边；撤回误记再添加正确的新边，不编辑旧文件 |

kind可为question、attempt、claim、evidence、decision、lesson；basis为source、inference、proposal、measured、human_decision；outcome为proposed、supported、refuted、deferred、unresolved、accepted、rejected。它们是不同维度：supported的推论仍不是实测或人类采用。

来源kind为discussion、repository、experiment、human。人类决定/accepted/rejected须关联human来源且basis为human_decision；measured须有experiment来源。校验仅检查声明和字节，**无法鉴别人类身份或实验真实性**，执行AI须实际读取原文。URL不会被自动打开。recorded_on是本条整理日期，不冒充历史事件时间；原时间/作者在来源原文中保留。

review的verified表示已对照来源核实这条摘录/关系；pending表示待核对，**不表示方案已验证**。待核对或已撤回的边不参与推导；trace/impact不是全量争论地图。关系支持based_on、depends_on、supports、contradicts、supersedes、related；前提和替代关系分别不得成环，普通相关与争论允许循环。supersedes须写准确范围，不自动把新提案升级成采用。

## 再次尝试时怎样使用

输入JSON固定为mechanism、conditions、purpose三项；purpose为evaluate或replicate。先核对mechanism确实相同、条件完整，不能把未核实的值强填为确定值。对照输出为：

| suggestion | 含义 |
| --- | --- |
| reuse_prior_reasoning | 已声明的条件相同，复用旧判断及适用范围；不是禁止继续讨论 |
| reevaluate | 当前条件齐备且至少一项改变或从旧未知补齐，重新评估；newly_specified标明后来明确，不声称旧取值与现值不同 |
| continue_evidence | 旧方案仍未测/暂缓/待定，应补证而非当作失败 |
| insufficient_context | 条件缺失或未知，不能下确定结论 |
| replicate_with_plan | 明确复验目的，应制定对照与判定 |
| different_mechanism | 声明的机制不同，不能套用旧结论 |
| review_evidence | 有未核对/无快照来源、明确冲突或替代记录，先核对依据 |
| human_review | 触及既有人类决定，回到准确原确认范围，不自动覆盖 |

判断是确定性提示，依赖输入声明；工具不能发现未写出的关键条件，也不自动判断替代范围是否完全覆盖。新增认识后追加到原Issue、更新受影响文档；记录中的错误同样需要纠正，不能让本工具成为冻结旧建议的理由。
