# G2-12 一次 Linux HTTP 机制诊断

2026-11-05，编码前固定，尚未运行。本页只规定开发诊断，不修改产品、正式判线或旧失败。产品源固定 `7da7159a339986659b18842350ae1edc242e958a`；来源核对原件为 coverage/post-seal-linux-dev-01/metrics-http-mechanism-design-01/source-verification.json。

## 已查证的旧证据与缺口

旧目录 `coverage/g2-ci-9f-integration/trace-01` 的 209 源文件全匹配保存 manifest；原 trace 有 209 事件/21 observer，Windows Go1.27.3。前三个 200 响应已经完成，第四个 200 才分别进入长 Write，重叠约198.66ms后 timeout；并未复现当前 Linux 失败。

旧 `g2_competition_trace_test.go:109–164` 记录 handler、WriteHeader、Write、FlushError；`:121` 的 Unwrap 直接返回 inner，没有记录实际调用，也没有底层 net.Conn deadline/Read/Write。故它不能回答 deadline 被何时设置/清除、handler 外 flush 是否在写。其固定容量65536、原子占位/发布与结束后输出可复用；不能原样复制旧清理超时后未 join 就声称完整 trace。

旧 trace 与当前失败还存在三项明确差异，禁止抹平：

- 旧 trace `:340–342` 是 httptest 默认 http.Server，额外超时字段均0。7da OIS `server.go:135`：ReadHeader/Read/Write=200ms、Idle=1s、MaxHeader=8KiB；OIC `reload.go:180`：仅 ReadHeader=5s，其余0。两端不能合称同一配置。
- 旧 trace 调用 g2CompetitionDialer，在 connect 前设 SO_RCVBUF1024；当前 G2-12 `vectors_g2_export_test.go:542` 用普通 net.Dialer，helper `:282` 在 connect 后 SetReadBuffer1024。Linux 握手时的窗口条件可能不同；这只是候选解释，不是根因结论。
- 当前 helper 原写D在首次Dial前设1s，G2-12 observer的 stopSet+1s在helper返回后设；旧 trace 的输入D是较早 connected+1s。新记录必须保留各自真实起点，不能混称同一个截止。

## 最小固定执行对象

采用一个显式 diagnostic-only Linux 命令，两子场景顺序各一次：`ois_profile`、`oic_profile`，每个都用真实 `metrics.NewHandler(&metrics.Registry{})`、回环TCP和标准 http.Server，严格按上述各自真实配置。不运行SQLite/PG、不虚构backend标签、不运行两端完整OI，不把该机制测试纳正式G2通过。两个profile均须保留实际结果，不删去未复现或失败的一端。

保持真实Handler/Registry/Render代码、两导出槽、入口200ms、原响应内容与64KiB限制；不替换render或注入大响应、不修改server/client buffer、TCP_NODELAY、keepalive、WriteBudget。两个真实slow socket，每个2048条现有GET，普通dial后SetReadBuffer1024，原writer1s绝对D；两条流不读响应。保留原observer的HTTP调用、200/429检查、10ms已有节拍与原1s观察D；不能新增sleep、制造126ms间隔、按结果改变观察频率、增加连接重试或再跑到成功。

范围须明确：纯HTTP机制没有原G2-12的native OI交换，不能伪造其响应/耗时。首429后直接继续观察，记录该区别；因此这是底层写/期限机制诊断，不声称逐包复现G2-12完整负载。本次选择纯HTTP机制，不接native回调、不引入数据库或额外OI进程；不得将结果外推为完整G2-12复现或通过。

首次baseline正常GET、两个slow、原observer、末尾恢复GET之外不增加HTTP请求。直接Registry快照与Handler.Activity只在原输入前/关闭前/全部join后三个边界读取，不增远端stats轮询或逐帧锁。容量/默认全族与响应大小原样记录；纯registry的数值不同于完整OI也须声明。

## 两层透明仪器

1. **底层TCP。** listener.Accept仅把真实 *net.TCPConn 包成透传conn；不加bufio、限速、门闩或重试。为Read、Write、SetDeadline、SetReadDeadline、SetWriteDeadline、Close分别记真实入口/返回、请求长度/返回n/有限错误类别。传给原conn的参数必须是同一原切片/同一time.Time；不自行修正deadline。实际deadline记录 `zero` 与 `deadline.Sub(origin)`，所有at也用同一进程monotonic origin，禁止UnixNano换算。getter仅用于准备期记录socket实际SO_RCVBUF/SO_SNDBUF；不修改它们，也不从期望1024猜内核实际值。
2. **HTTP writer。** 实际ConnContext绑定连接ID、tuple和每连接request ordinal，wrapper在handler入口/退出及WriteHeader/Write/FlushError前后记录。`Unwrap`必须记真实被调用的事件并立即返回原inner；wrapper不要自行实现SetWriteDeadline/SetReadDeadline，否则会截断ResponseController真实Unwrap路径。FlushError只调用原inner的ResponseController.Flush一次。Header返回原map；不注入额外Header或自行调用隐式WriteHeader。
3. **归属。** 固定conn状态保存last_request和handler_active/current_write_or_flush_op。底层Write事件附真实active操作ID；没有该ID时明确`outside_handler_or_writer`，不可硬归给最近200。Read可能是HTTP parser或后台读，按实际时间/连接记录，不把一次Read等于一个请求。每次新的handler ordinal直接区分同两条长期阻塞与同socket持续处理新请求；只有实际200+尚未退出的Write/Flush才是本次持槽写的直接证据，429写从来不算owner。
4. **接口透明。** 不改变实际可用的ReaderFrom/WriteTo/SyscallConn等TCP可选接口；若net/http实际走该旁路，同样记入口/返回并标明，不假称已被Write事件逐块覆盖。当前Handler调用普通Write/Flush，应由轨迹确认该事实。不能因wrapper省略接口而悄悄换掉标准库优化路径。

记录仅用启动前预分配的有界结构：可沿用65536事件槽；固定连接表256项/请求表4352项。事件为数字/枚举/固定错误分类，无每次fmt、日志、JSON、栈展开、payload复制或动态append；原子占位，写完后ready发布。任何容量溢出/缺发布/缺配对均让诊断无效并保留完整可读前缀，禁止扩容或丢掉异常再判通过。全局index不是独立goroutine的因果总序；相同timestamp和重叠deadline调用区间要如实保留，无法归属就记不确定。

## Go 1.27.3 的实际对照路径

读取本机已下载官方Linux归档，未执行Go；归档SHA256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40`。

- `net/http/responsecontroller.go:105–113`：SetWriteDeadline先找真实setter，否则逐层Unwrap；Flush `:47–57`先找FlushError，再Unwrap。透传层须保留原分支。
- `net/http/server.go:501–506`：response的read/write deadline直接传到底层conn。
- `:377–403`、`:1780–1789`、`:4278–4284`：HTTP Write/Flush经过buffer/chunk writer，底层写错误保存并取消连接ctx。HTTP方法返回并不等于对端已接收全部字节。
- `:2157–2165`：调用handler后 cancelCtx，再finishRequest，再SetWriteDeadline(zero)。`finishRequest:1714–1724`还会flush。故新仪器必须活到HTTP worker结束，不能handler_exit即停止记录或说deadline清除发生在handler内部。

据此只定义可区分结果，不预判产品：①同200 request持续阻塞且实际底层deadline已设、未清除；②ordinal持续递增，每次都获得新200ms，离散429不能证明同两次写；③deadline/Unwrap没有触达或错误返回；④handler已经释放槽，但net/http后续flush仍在写；⑤Write没有进入，耗时在render/调度；⑥只出现client Close后的write_error，不算关闭前timeout成功。即使①成立，仍需实际返回和调度时点解释，不凭一条晚采样断定内核定时器错误。

## 结束、保全和判读

原关闭前快照必须先于slow.Close，记录关闭实际开始/结束和每条连接关闭事件；保持原2s清理总窗，不每个join另开2s。记录器保留handler、server连接、slow writer和observer的全部退出边界；Server.Close本身不能冒充所有自有goroutine已join。只有所有owner确已结束后才汇总JSON；超时则保留已发布项/未发布计数、失败和组状态，不能从仍在写的slot取未发布结构。加入一次受限top-level清理使失败也能关闭两socket/observer/server并保全原始输出。

未来运行前固定全部产品/仪器/Go archive来源hash、Linux/boot、两个Server配置、实际argv/环境、唯一输出目录；结束再核源不变。原始文件包含全部事件、observer原始结果、三个Registry/Activity边界、连接tuple映射、实际write/observation/cleanup D和errors/overflow。没有完整轨迹就不归因；没有复现只记未复现。不得覆盖209事件原件、7da失败或已有正式run；该诊断不直接恢复G2/G1门槛。

实施前必要窄仪器测试（本轮不运行）：原net.Conn参数/返回逐值透传；真实ResponseController确经Unwrap到TCP setter；deadline zero与原time.Time保持；相同连接多request与handler外write分开；错误/n>0+err/panic原值保持；并发ready发布/overflow不丢错；cleanup尚未join拒绝完整性通过。测试需验证行为接缝，不用配置值镜像充数。


## 实施与来源固定

仪器放在ignored独立嵌套Go模块，不进入产品或普通正式测试清单。模块路径须满足现有internal包可见性，replace仅指向上述真实固定Git源checkout；不能拿正在编辑的工作树冒充7da。实际helper可以从固定文件字节复制必需小段，但必须保存对应来源与差异，不依赖未登记临时函数。产品和仪器分列source_kind：产品为fixed_git_commit，仪器为pre-execution-byte-snapshot；后者没有Git身份时不得伪造。

先完成窄仪器测试及独立静态审查，再固定完整仪器目录、go.mod/go.sum、原依赖、执行脚本与源清单的字节快照。Linux执行前逐SHA回读，使用专用VM的新排他目录，记录boot和实际进程身份；原产品checkout只读且执行前后全部核验。一次命令内按ois_profile、oic_profile顺序各一次，不因结果重试。宿主保留前台命令/退出、全部原件及身份绑定；只清理本次已验证身份的进程。全局诊断命令上限120s（含实际两个profile及输出），每profile仍是原writer1s、observer1s及共享cleanup2s，不以外层上限续任何内层期限。Windows窄仪器自检不能替代这次Linux观察。

事件原件只能在实际自有worker、HTTP handler及所有底层记录调用退出后读取；HTTP Server.Serve退出或Conn.Close返回不能单独当连接handler已完成。使用真实ConnState Closed/全部活跃操作结束等可验证边界，并说明标准库goroutine与自有worker的证据差异；如果无法证明记录已停止，保留已发布前缀并标不完整，不从活跃槽取值。不得为避免仪器失败改动产品、时钟、SO_SNDBUF/窗口或导出预算。

结果只帮助决定下一项最小修复或观测。旧209事件、本次7da慢读失败、G1正式失败与G2关闭状态均保持；原请求未知结果不得重放。没有新增产品承诺、权限或对外接口，不改变Accepted范围或alpha。
