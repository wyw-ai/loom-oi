# 周期待办的固定 Linux 开发反馈

固定被测来源 `3be014afecd6b0ca78cc22b1501cc1488d213fd1`，tree `ad49ab2c2b59c26e233609f3e7905fbb5fdee738`；先行规格为 `82ea90c6bdea4ed8f6df3d7c7245aedf5a62e497`。[完整结构化原件索引与独立分析](periodic-work-linux.json)保存本轮首次结果，不覆盖[cc653e 六叶失败](late-slow-input-linux.md)。

结论：完整 relay 单测/race、全仓 vet、184 个准备辅助节点通过；G2-11 六叶仍全部失败。周期待办已实际可见，两叶越过原 final 选择，但没有完整 cycle、60 秒窗口或 settled 资源检查。此为开发反馈，不是正式 G2 或完整质量通过。

## 一次执行及原件

| 步骤 | 实际退出 | 原记录耗时 | 真实节点 |
| --- | ---: | ---: | --- |
| 完整 relay unit | 0 | 42.8676316s | 276 PASS，含新增65 |
| 完整 relay race | 0 | 67.7558683s | 同一276节点PASS |
| 全仓 vet | 0 | 36.1902781s | 退出0 |
| 准备 helpers | 0 | 51.6751636s | 184 PASS |
| G2-11 | 1 | 49.8281522s | 六叶、三个父节点及包FAIL |

parent 用时264.6056586s；五步原1800/2400/900/240/1200s、parent9000s、host9030s、integration共享5700s保持。原wait、输出落盘、hash/source后检与result写返回均在原D内，errors为空。276为109根+167子，包含4根+61子=65新节点；helpers为28根+156子，不额外相加。relay局部覆盖1872/1963=95.3642384106%，不能替代完整cmd/internal和依赖自有语句分母。

实际deploy/execute/collect为0/1/0；902源码成员逐Git路径/模式/blob/SHA一致，源码归档305428480B、SHA256 `57e85370a68d3dbf879c9cecebad08db448f1fd5d97ada77de8472db922290a9`。manifest列141原件103636699B，加manifest自身共142文件103660887B；全部与回读tar逐字相同，tar为103782400B、SHA256 `c362e9df76b7e3ba4b6512147959c613b77adc3b99316648b0c35f0b28752947`。五child均实际Gone/组空；runner只执行group=False身份检查，不能说扫过runner组。VM/PG/Go1.27.3/32MiB tmpfs身份前后保持。

## 六叶原失败及区分

| 叶 | 原首失败 | 有效观测 |
| --- | --- | --- |
| SQLite round0 | final selection | 24样本中17次Full；首拒query年龄201.1472–420.0128ms |
| SQLite round1 | final selection | 26样本中17次Full，16次在原D内；年龄202.4546–511.4922ms |
| SQLite round2 | critical后重叠输入消失 | final最大query年龄153.9299ms，critical177.1132ms；后置scrape真实200 |
| PostgreSQL round0 | provider ABORT输入超过原cycle D | final最大query年龄59.0498ms，critical81.9865ms；ABORT晚D 67.9075ms |
| PostgreSQL round1 | final selection | 26样本中19次Full，18次在D内；年龄204.0898–432.1470ms |
| PostgreSQL round2 | final selection | 24样本中16次Full，15次在D内；年龄208.4394–408.6323ms |

Full逐次按原capacity16/backlog16/pending8/P2/Q4复算；四个final失败叶共100样本、69次Full，但所有已执行的young投影仍为false。相同四叶在cc653e是2/100 Full；该出现次数变化与新增记账一致，只支持本轮待办确实可见，不证明连续满态、运行间只有一个差异或没有额外调度成本。两个final成功叶没有输出此前全部采样历史，不能补造。原32Drop按六叶只消耗16/16/16/17/16/16，不能归因为耗尽。

SQLite round2的最终输入选择在原合格429完成后继续等待437.7289ms；critical进入时原cycle尚余344.3489ms。随后实际native DATA/FIN、provider Revoked ABORT、control CloseChannel、OIC反向ABORT均有wire；后置scrape在原D内返回200，实际正文timeout=2、write_error=0、success=93、rejected=5，baseline全0。这些计数在慢socket主动Close前已经存在；后置真实200说明该时点至少一槽可用。429拒绝路径也会通过同一个write/Flush增加timeout，因此无法把两次timeout对应到最初两条200、具体占槽请求或其释放原因。原件也不能定位timeout的精确时刻和pipeline ordinal，不能以初始429证明整个关键区间持续双槽占满，更不能将该200改判成功或解释为200ms导出期限违规。

PostgreSQL round0在最后合格429完成后等待424.0577ms才完成final；critical进入时尚余355.5244ms。正常响应FIN在D前282.2452ms收到，victim provider ABORT在其后350.1527ms、即原D后67.9075ms才首次收到。该样本不存在D内已完成的provider ABORT，不能靠更改Wait的select优先级挽救资格。后续CloseChannel与反向ABORT已在失败冻结之后，不能当作成功主动release链。原日志有Revoked/config applied、六OIS均无`store write failed`；但失败cycle未保存native/撤销调用起止，不能将整个间隔归于SQL、锁、CPU或500ms控制超限。

源码进一步显示：原`g.write(DELETE)`会先同步GET配置版本，再DELETE，均用原Background上下文。现有`TokenCreated.ConfigVersion`可能支持省去这个额外GET，但须先证明本cycle没有中间配置写，并保留真实If-Match、不重试与原D；本轮数据未证明该候选足以消除失败。另需从原样本区分年轻query与真实导出占槽的时序，不能再只改一种排序盲跑。

## 证据限制与后续

完整同源expected_spec保持，严格分析命令实际exit0并输出六组FAIL；未运行的G2组仍缺失，不裁表称整个G2通过。选中103份G211/source/process原件独立核验。没有任何完成cycle、60s或实际settled资源check，缺失不能记为资源通过，也不直接推定泄漏。

本轮宿主/分析错误均保留：首次绑定把handoff映射误作列表，源仍None且库存命令拒绝；collect-01漏传deployment在SSH前拒绝，collect-02仅补齐参数收回同一次产品结果。独立auditor首稿源语句字面匹配有误，诊断首稿tuple键无法序列化；修正后的离线核验/诊断退出0，未更改冻结prepared、原件或实际业务运行。完整结果索引保留这些路径和SHA。

保留周期记账实现及其有限差分/实际relay回归结论，继续在原契约内收敛输入准备与额外调用开销。必要实现先补具体规格和反例、提交回读，再运行固定来源；不增Drop、续期、改变200/300ms年龄锚点或把过期输入当通过。CLI/API和图06的产品状态无新增变化，Accepted片段、19旧run/14ITER保持。G1 Failed、G2 Blocked、G3/G4 Inconclusive及alpha/main不推进；完整root/nested质量、Load、G2-12、fuzz、scanner、clock和最终同源正式复证仍待完成。

最终解释复审进一步收紧了timeout计数归因：独立诊断原文保留，结构化记录的`interpretation_review_correction`明确以总计数不可关联具体请求为准。该修正不更改六叶失败、原始时间线或下一轮判线。
