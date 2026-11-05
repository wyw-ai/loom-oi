# TASK-007 嵌套依赖测试的解析器环境

本页先行登记测试环境增量，承接[完整依赖适配计划](liveness-evidence-adapter.md)与固定183b的DNS泄漏反例。它不修改产品DNS、主机配置、64份上游源码或原用例5s上下文，也不改变H17及95%门槛。

## 已取得的窄证据

[固定183b完整TestBadDials检查](resolver-local.json)在同一Linux VM执行一次，9节点及原TestMain包终结全部PASS，真实日志记录强制cgo选择。该次主机实际为glibc 2.39，NSS为`hosts: files dns`，resolv.conf经systemd-resolved的127.0.0.53 stub；保存了完整NSS/resolv及实际Go1.27.3解析器源码原件，前后字节一致。只对该Go子进程设置`GODEBUG=netdns=cgo+1`、`RES_OPTIONS=attempts:1 timeout:1`与CGO=1，原5s/Go120s/父180s不变。此前原DNS包失败及race120s开发失败不改写。

这个结果仅支持在相同环境中继续验证候选；cgo底层getaddrinfo可能在上下文取消后继续，timeout/attempts不是整个API的总期限保证。依据为[Go解析器选择说明](https://pkg.go.dev/net#hdr-Name_Resolution)及[resolv.conf参数说明](https://man7.org/linux/man-pages/man5/resolv.conf.5.html)。不得据一次根测试通过宣称完整模块、race或产品门槛通过。

## 完整套件接入范围

下一版本的显式`dependency_quality`规格增加固定测试环境；只在`dependency-unit`和`dependency-race`子进程设置`CGO_ENABLED=1`、`GODEBUG=netdns=cgo+1`、`RES_OPTIONS=attempts:1 timeout:1`。根产品unit/race、其他工具及主机环境不继承这项覆盖。两个步骤保留完整`./...`、原上游及全部自有测试，禁止short/run过滤；unit/race原300/600s工具预算与Go默认watchdog保持，未完成或泄漏仍失败。

实际原始start记录必须包含精确环境，完整Go事件必须包含实际强制cgo选择日志。缺声明、旧环境、无实际选择见证、任何失败/额外skip或不完整包均不能通过。CI的Linux依赖unit/race同步这个环境；Windows原生开发检查仍单列，不声称在Windows取得Linux解析器见证。

变更前已将3fc5db8完整分析器及7个相邻依赖逐字归档，旧run与已固定开发结果保持原来源。新增正式manifest仍待最终同源产品通过后预注册，不改写旧13/16步或历史summary。

## 后续与限制

先以同一最终源码执行完整根/嵌套质量，再核验Load及G2-11/12库存；开发结果不得拼接成正式通过。若完整套件仍泄漏、超时、来源改变或环境不匹配，就保存该失败并继续定位；不会靠跳过原测试、吞掉TestMain或延长产品截止来完成验收。G1 Failed、G2 Blocked、G3/G4 Inconclusive暂保持。

下一次完整开发范围及共享执行预算见[同源Linux开发检查计划](liveness-complete-linux.md)，不是已完成结果或正式验收清单。
