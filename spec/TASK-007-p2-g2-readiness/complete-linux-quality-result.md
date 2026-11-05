# 完整 Linux 开发质量 02：质量子项通过，G2-11 仍失败

固定源码 `dd62daab4f86db940be2c3d1752b7efc008398fb`、tree `d649b25d838de00e7a7bd62ed9acb04a3e6428c2`。这是一次开发检查，不是新的正式 run、manifest 或 ITER。16 步完整结束，guest 实际耗时 **2183.9811289 秒**，仅 G2-11 退出 1，其余 15 步退出 0；parent、host execute 和完整质量分析的整体失败结果保留。

## 质量与来源

组合覆盖率为 **7886/8293 = 95.09224647292898%**，真实整数门槛达到 95%。其中完整根 atomic profile 是 7875/8281，自有依赖补丁是 11/12；JS 不支持分支的 1 个语句仍计为未覆盖。该结果不使用旧局部覆盖或后续 Windows 数据补齐。

完整 root unit/race 各 2057 个具名节点唯一通过，435 个必需测试根和 39 个 pending 专项节点均实际存在，无测试节点 skip。race 中三个 cmd 包的“无测试”包终态单独允许，不等于跳过已有测试。完整嵌套 unit/race 各 281 PASS，`TestAutobahn`、`TestWasm` 两个原允许 skip 保留；原无测试包终态也分别核对。298 个新旧 helpers 全部通过。

根与依赖的 vet/staticcheck 通过。根 govulncheck 与依赖指定版本 query 使用同一固定官方离线库，完整 4715 成员、索引、归档与前后 fact 一致；root 没有发现漏洞，`github.com/coder/websocket@v1.8.15` query 的 OSV 数为 0，未以 exit0 单独认定通过。frame/control fuzz 分别真实运行 61.12/61.18 秒、304080/158958 次执行，均记录 4 workers 和唯一 PASS；末段及 control 中间的原 0/sec 观察保持，不能称一直有进展。

958 份 Git 源码与输入归档、执行前后来源和最终回读精确一致。回收 **339 份带哈希原件共 243238748 B，另加一份 manifest**；tar 为 243568640 B，SHA256 `ebaa68d7e2c1fcc15cf17a0aca8b89da33774c5b762a0fcbe7767b675ec039f7`。Host deploy/execute/collect 实际退出为 0/1/0；同一 VM HANDLE/launch 与 SSH HANDLE 身份已核。runner 仅按原身份检查 Gone，16 个 child 的进程组在退出及收集时均为空，不把 runner 的 identity-only 检查说成扫描整个组。

原 guest 9000s、host execute 9030s、integration 共享 5700s 及各步绝对 D 保持；实际 wait、源后检和结果写回都在原截止内，无监督错误、超时或续期。没有为本轮重启 VM/PG或改钟。

## 业务与资源结果

**Load 严格通过。** 双后端完整 12 组合、164/164 条 flow 实收 5588910080 B，即 5330 MiB；逐 worker、目标 tuple、两端终态和实际字节核准。12 次资源检查、36 样本均通过，清理耗时范围 188.514800–213.336600ms，保持原 2s 边界，没有新增吞吐 SLO。所有 164 个 worker 仍记录 `failed to immediately close WebSocket: use of closed network connection`。原夹具将 CloseError 与 DATA/FIN 的 FirstError 分开保存；这里没有删除错误或声称所有 Close 均返回 nil。

**G2-11 六叶全部失败。** SQLite 三轮及 PostgreSQL round0 在 cycle0 critical 后 scrape 返回 200，原前后两角色的 exporter Active 均为 0；post200 只说明该观察时刻可接纳，累计 timeout 计数不能逐一归属最初两个 writer。PostgreSQL round1/2 在 pre_dial_full 阶段失败，分别为原 context deadline 和原 Stats 请求 deadline，保留 input_not_qualified/stats_error 的区别。没有完成竞争 cycle、完整 60s 或本叶 B4 资源检查；缺少完整资源检查不等于已经证明泄漏。不将 CPU、数据库、网络或调度断言为唯一根因。

**G2-12 四叶严格业务与 B4 资源通过。** EntityCardinality 与 SlowReaders 各两个后端的固定组判定均通过，每叶一次完整资源检查；四次清理分别为 206.8014、204.2327、193.4259、192.4176ms。完整 expected_spec 保留，其它未运行 G2 组仍缺证，不能据四叶晋级整个 G2。

## 不能合并的证据与后续目标

本轮在专用 Linux VM 执行，parent 的三路来源与全部 16 个实际 Popen 环境都明确没有 `OI_IT_G211_PENDING_DIAGNOSTICS`。独立 native pending 诊断轮虽使用同一 dd62 代码，却在另一运行配置、主机与原件集合中显式启用该开关；其叶结果、日志及因果分析不拼入本轮。

后续离线诊断 parser 将实际 preload 阶段误拼写 `preload_joint` 修正为 `preload_probe`，原 red 实际语义失败与 green 53 方法通过另有记录。这些是运行结束后的工具修复验证，不是 dd62 执行时的测试，不增加本轮 2057/298 等库存，也不改变原业务终态。

继续完成首版门槛与原已采用 SSH/Bash 主机范围。旧失败与原件全部保留；下一受控输入或实现必须先固定具体规格，再以同源完整证据核验。正式 criteria、预算、run/ITER、验收状态和 alpha/main 不变。本轮证明若干质量子项、Load 与 G2-12 在该配置下通过，同时明确 G2-11 尚未完成。

## 可核查来源

- 生命周期、覆盖和 fuzz：`coverage/liveness-complete-linux-02/implementation/independent-review-01/review.json`，SHA256 `f129688b48e1adfbc277972ff6ab6e96c598cf7ee080bc950df4fddc03c09848`。
- Load/G2 严格业务与资源：`coverage/liveness-complete-linux-02/implementation/semantic-handoff-01/handoff.json`；其原件、分析器和完整分析 SHA 逐项见[原件摘要](complete-linux-quality-result.json)。
- `quality-analysis-01.json` 整体仍为 fail，未改写成通过。所有来源只读归纳，没有新执行、重试或远端动作。

[下一先行规格](early-arm-input.md)已固定于312bc235并完成远端逐字回读：只提前原唯一32Drop到原prepare起点、首8Set前，保留P2/Q4及全部80/200/300与原时限。实现与下一native结果独立记录，不回填本轮失败。
