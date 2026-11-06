# 专业做法调研与本任务取舍

核对日期：2026-11-04。按用户“Git仓库级别、不要过重”的要求，采用成熟方法的必要部分，最终为Git阶段JSON＋一个Python标准库只读脚本。没有部署数据库、服务或完整知识管理平台。

| 原始资料 | 核对范围 | 本任务采用与边界 |
| --- | --- | --- |
| [QOC：Questions, Options, and Criteria，1991](https://www.tandfonline.com/doi/abs/10.1080/07370024.1991.9667168)；[gIBIS，1988](https://doi.org/10.1145/62266.62278) | 作者论文摘要中的设计空间/类型化讨论关系；未声称全文复现 | 问题、候选、判断理由分开记录，以支持后来修改与复用；不声称符合某套完整本体或能自动推理 |
| [W3C PROV-DM，2013推荐版](https://www.w3.org/TR/2013/REC-prov-dm-20130430/) | 来源、责任主体、派生/修订的概念 | 保留原来源、版本和明确关系；连线不自行证明因果。未引入RDF或全量PROV兼容层 |
| [Nygard：Documenting Architecture Decisions，2011](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) | 决定背景、状态、后果和替代记录 | 延续已有ADR与准确批准范围；AI推荐不因入库变成人类决定 |
| [Microsoft：Event Sourcing pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/event-sourcing) | 追加历史、派生视图及其复杂性 | 只借鉴历史与当前读视图分离。按Git阶段追加并在查询时读取，不建设事件总线、服务或完整事件溯源系统 |
| [SQLite FTS5](https://www.sqlite.org/fts5.html) | trigram分词、短于3字符的查询限制 | 曾比较本地索引，但当前小规模语料和用户轻量约束下不引入SQLite。用直接扫描与已登记别名；未来只有测出真实查询瓶颈才重评 |
| [JSON Schema 2020-12](https://json-schema.org/draft/2020-12) | 标准化结构验证与format语义 | 当前固定小格式用严格字段/类型/引用检查，减少安装依赖；不将自有校验器称作完整JSON Schema实现 |

没有一个上述方法单独保证“不重蹈覆辙”。本项目新增的是：先找相关尝试，明确机制与条件，区分可复用/可重试/需补证，再根据新证据复核影响。语义正确性仍需人/AI读原文，不能由排序分数或格式通过替代。

本轮可测的是具体文件/命令的行为、历史保真和已给定案例的输出。没有足够独立样本衡量真实团队重复探索减少比例；已登记别名的检索收益包含人工整理投入。未知同义词、遗漏条件和错误来源分组仍是限制，后续依据真实漏判/误判再迭代，而非先增加模型、嵌入或图数据库。
