# 原生主机 G2-11 六叶最小诊断计划

先行实施规格：当前仅完成只读盘点；本页提交、推送并回读后，按已授权非生产验证范围准备并执行。沿用户已授权目标 `target-host-1`。诊断参照源码固定 `3be014afecd6b0ca78cc22b1501cc1488d213fd1`，最终实际执行SHA由主线先行固定；若要加入新诊断或使用后续提交，两环境比较应明确各自源码差异，不能把HEAD替代这个固定引用。

目标只是一次同源原生Linux G2-11，SQLite/PostgreSQL各round_0/1/2，共6叶、3个父节点。原P2/Q4、16待办/8query、Drop32、3s准备、200/300ms资格、1s慢输入、500ms控制、14slow、60s与2s资源清理全部保持；所有实际失败保留。不是G5/G7部署或整套G2/clock/formal，不根据更快/通过就归因TCG。

## 1. 真实入口约束与最小选择

固定源码 `tests/integration/main_test.go:37` 无条件执行 `go list -m -f {{.Dir}}`，`:48`无条件buildAll；`:73–85`用原 `CGO_ENABLED=0`、`-trimpath`构建ois/oic/oi。`OI_IT_BIN`只是输出目录，**不是预编译跳过开关**。所以复制VM的integration.test/ois/oic不能让原六叶在无Go环境直接运行；不造go shim、不修改TestMain、不将这类适配混成同源。

采用真实Go路线：原生任务目录私有Go1.27.3、精确同源Git archive（包括本地replace的完整`third_party/websocket`）、所需完整module/file proxy缓存；执行原go test，TestMain原样构建。编译设`GOFLAGS=-p=1`，测试保留`GOMAXPROCS=2`，匹配当前两个逻辑CPU，不用CPU配额伪装整机1C1G。`CGO_ENABLED=1`沿原集成测试构建事实，子产品仍由TestMain强制0。原生gcc实际路径已见，但版本、开发头文件与链接依赖尚未验证。

VM预编译制品可以另作制品/ABI参考：Go1.27.3+linux/amd64一致，目标和VM均glibc2.39；但这只满足基础条件，不能证明每个ELF的interpreter/DT_NEEDED/版本化符号已满足。实际执行前可核受信固定制品的`go version -m`、SHA、ELF信息/依赖；纯Go的产品通常少外部依赖，仍以实物核验为准。Windows Go build缓存不能当Linux build缓存。最小路线不依赖迁移任何旧测试binary或活动PG data。

## 2. 已有缓存与真正缺口

路径均为已有记录或本机只读确认，未重新采VM。宿主仓库根为 `C:/Users/c6321/GolandProjects/copilot-worktrees/loom-oi/codex-task007-g2-readiness`。

| 输入 | 可复用来源与精确事实 | 还需准备 |
| --- | --- | --- |
| Go1.27.3 Linux amd64 | `coverage/local-linux-vm/downloads/go1.27.3.linux-amd64.tar.gz`，70,588,678B，SHA `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40`；官方URL/provenance同目录 | 用户目录解包、原tar/解包清单回读；不写/usr/local或系统PATH |
| PG18.6 server | 同目录 `postgresql-18_18.6-1.pgdg24.04+2_amd64.deb`，7,576,708B，SHA `c0773357df8769abb70e92722196d9b53422d71c0f29a4f39c834b94276cfa3d` | 只解包，不执行dpkg维护脚本/apt install |
| PG18.6 client | 同目录 `postgresql-client-18_18.6-1.pgdg24.04+2_amd64.deb`，2,114,236B，SHA `b9d10d99a73bf7aa375be2fe36626c40499cfdf553d0ad174674a88a1b256b43` | 同一私有prefix；不依赖系统pg_wrapper |
| Go module/file proxy | 本机 `C:/Users/c6321/go/pkg/mod/cache/download` 与其父modulecache实际存在；VM原记录 `/home/oi-vm/.local/task007/go/pkg/mod`及`cache/download` | 根据固定source真实module图挑必要完整版本，含`.mod/.info/.zip/.ziphash`与提取源码；逐件SHA。旧ba的61项图不能冒称当前3be完整 |
| VM Linux build缓存 | 原记录 `/home/oi-vm/.local/task007/cache/go-build` | 可选加速，不是来源权威；移植前需真实收集。最低方案可以不用，不能承诺已热缓存/同冷启动成本 |
| PG动态依赖包 | VM `/home/oi-vm/.local/task007/downloads/apt-debs`有逐件SHA记录 | 该目录尚未整体收回宿主；不能把“VM已安装”写成目标已有或完整官方deb均已缓存 |

固定3be的`go.mod`明确`toolchain go1.27.3`和本地websocket replace。优先完整固定source archive，只复用实际依赖包；无需570b兼容源/旧依赖、漏洞库、staticcheck、Node、tmpfs/diskfull、Docker或其他质量步骤。离线环境固定 `GOTOOLCHAIN=local`、`GOWORK=off`、`GOPROXY=file://<private-cache>/cache/download`、`GOSUMDB=off`（原go.sum/zip仍核）、`GOAUTH=off`；没有公网fallback。只复制依赖，不复制其他项目源码/私有凭据。缺图/包先作为准备失败补齐原件，不能正式执行时偷联网。

**现有主包不用新下载。** Go原tar和PG两个deb本轮已本机逐字核SHA。PG依赖的官方包若不在已缓存原件且目标缺所需ABI，才由宿主从官方Ubuntu/PGDG下载精确版本并保留签名元数据/URL/SHA；缺依赖时按此来源规则补齐，不扩大系统安装范围。另一选择是将VM受控安装文件按真实package ownership/逐件SHA收成私有runtime闭包，但须说明是已安装文件快照，不假称原deb。不把VM活动数据库或配置/凭据带来。

## 3. PG私有解包与闭包（最主要待证实项）

PGDG noble包清单本机原字节明确server需`libc6>=2.38`、libgssapi-krb5-2、libicu74、libldap2、liblz4-1、libnuma1、libpam0g、libpq5、libselinux1、libssl3t64、libsystemd0、liburing2、libuuid1、libxml2、libxslt1.1、libzstd1、zlib1g；client另需libreadline8t64、libpq5>=18.6。包管理还依赖locales/common/ssl-cert/tzdata等；不用系统包管理时必须用实际工具/数据依赖验证代替，不能直接忽略。

VM downloaded-debs记录确有libpq5 18.6、libicu74、libnuma1、liburing2、libxml2、libxslt1.1；OpenSSL/Kerberos/LDAP/readline/lz4/zstd/systemd/PAM/SELinux/uuid/zlib等在VM安装清单存在，但原deb缓存清单未证明齐全。目标目前只核了glibc2.39，其他库未知。

目录仅限 `/home/admin/.local/task007/native-g211/<run-UUID>/`，先验证home/每级父目录真实路径、ownership/非symlink与新目录独占，0700。建议子目录：`input/ source/ toolchains/ cache/ pgroot/ pgdata/ pgsocket/ run/ work/ logs/`。不修改现有frp/nc、系统PG配置、systemd/sshd/防火墙/DNS/时钟，不sudo安装。

宿主或受控Linux提取器将官方deb的data archive解入`pgroot/`，保留`usr/lib/postgresql/18/bin`、`usr/lib/postgresql/18/lib`及`usr/share/postgresql/18`相对树，并记录所有member路径/size/SHA；校验拒越界链接/绝对路径/特殊设备，不执行包postinst。缺的非glibc依赖解到同prefix，以**仅该PG子进程**的`LD_LIBRARY_PATH`引用；不覆盖系统libc/loader、不给全测试环境注入LD_PRELOAD/替代glibc。版本号相同不代表闭包已验证。

后续准备时以绝对路径运行`postgres/pg_ctl/initdb/psql --version`并核实际ELF需要的库与符号；`initdb -L <pgroot>/usr/share/postgresql/18`明确share位置，PG动态模块路径指向私有`.../lib`。验证其share/extension/timezone路径真实从私有tree解析，没有依赖缺失后回落到另一个系统PG。保持PG18.6-1.pgdg24.04+2；不能为装配方便换版本。

新集群仅UID1000拥有；先确认TCP55432与run专用socket目录未占用，再用绝对私有bin初始化全新pgdata，监听**127.0.0.1:55432**、Unix socket仅pgsocket0700。临时随机口令/scram及权限文件0600仅run内，DSN不公开日志。隔离集群内创建测试数据库的权限满足`env_test.go:257–272`每叶CREATE DATABASE；没有权限访问/删除其他数据库或连接既有33306。PG role属此集群的临时测试身份，非系统权限提升。

PG共享缓冲、连接上限、fsync/synchronous_commit等先回读并记录与VM原实值的比较；不自行减弱持久性或调参让测试通过。当前未有本方案对应PG配置完整事实，不用猜默认替代。PG初始ready、版本、实际端口、PID/start_ticks/exe/datadir、配置hash与基线日志全部完成后才进入一次六叶。资源不能满足或库/路径失败则保存准备失败、不换有利设置继续。

## 4. 唯一运行及生命周期

以下是将来执行命令形状，当前不执行：

```text
cwd=<run>/source
GOMAXPROCS=2 GOFLAGS=-p=1 CGO_ENABLED=1
OI_IT=1 OI_IT_BACKENDS=sqlite,postgres OI_IT_CLOCK=0 OI_IT_LOAD=0
OI_IT_RUNDIR=<run>/run/it OI_IT_BIN=<run>/run/bin
OI_TEST_PG_DSN=<private loopback DSN>
go test -json -p=1 -count=1 -timeout 90m -run ^TestTVG2_11_ControlExportCompetition$ ./tests/integration
```

`OI_IT_BIN`在run内是为保留TestMain实际构建产物，**仍执行原buildAll**；Go不做第二次隐式业务运行。SQLite使用原modernc驱动/每叶本地文件，无外部SQLite安装。G2-11不依赖pg_dump/restore调用，但匹配client包/初始化工具仍需保留；不为此运行G4。

准备/构建与实际六叶分开：准备构建阶段固定单独900s绝对D，执行固定source的`go test -c -p=1 -o <run>/input/integration.test ./tests/integration`及原flags的三个`go build`，仅编译、不运行TestMain/业务；保存原argv/退出/制品SHA/buildinfo与完整失败。首次缓存闭包不全/构建超时就停止依赖执行，不续期或假装已热缓存。该900s是本页固定的开发准备watchdog，不是产品SLO。传输/PG装配也各用先行有限上界并保留原件，准备耗时单独记录。

实际六叶仍沿原stage1200s、普通parent9000s、host9030s覆盖本次远端执行、integration共享5700s绝对上界；只有一个产品stage，不叠加续期。保留上述原go test入口，因此执行时仍有cache校验及原TestMain无条件buildAll；这部分实际执行开销仍计入1200s，不利用准备阶段把执行deadline后移。原业务3s/1s/500ms/2s在真实输入位置照常起算，准备不混入其含义。原Go90m是内层watchdog，不能压过外层1200s。超时即保留失败，不因编译慢补跑一次。普通产品exit1可自然完整收集，同一次测试由Go正常继续余下叶；不单独挑失败叶重试。

执行前刷新原SSH严格身份、boot、UID、资源/FDlimit、55432状态、已有服务基线。现有约1.1GiB可用内存与无swap是瞬时值；不设置新CPU/内存配额声称1C1G、不迁移/停止已有服务。磁盘预算按固定payload+解包+编译缓存+PG/run日志实际size预核足量并留系统余量；没有现成数字就准备未就绪，不能把28.96GiB旧瞬时余量当永久保证。OOM/进程消失保留并判不完整，不调阈值隐去。

runner独立session/PGID；记录host SSH PID与每个guest runner/test/ois/oic/oi/自有PG的PID、start_ticks、boot、exe和run归属。普通退出先join原测试子进程；异常/断线先保留状态，只有身份精确匹配且仍属该run才SIGTERM/有界SIGKILL其组，未知身份不kill。PG最后以**该run私有pg_ctl+data**执行受限停止并核postmaster及全部子进程退出，不向任意postgres/frp进程发送信号。原业务2s资源检查先完成/失败保留，外层回收不能倒填为通过。禁止重启整机/改钟/重放unknown run。

## 5. 原件与分析交接

保留source完整文件SHA、Git对象来源、toolchain/tar/deb与私有闭包清单、实际argv与白名单env、root/PG/测试PID身份、起止/原D、完整GoJSONL/stderr、所有OIS/OIC/健康目标/wire/history日志、实际构建binary SHA/buildinfo、PGlog、source/输入前后核对与采集file set。原件可能含临时DSN/token，保持受限目录，公开仅按已有有限报告字段；不删原件后称逐字。

collect原字节tar及每文件SHA、采前后稳定和process-group Gone证明；出错留部分包和明确缺项，不因正常SSH exit便称产品成功。host按本次固定source加载同目录分析模块，D16拼行，保持完整expected_spec，再分别报告这6叶/3父严格行为与资源；不把选择性六叶诊断称整个G2通过。对应QEMU原失败永不改写，环境/source不同点列出；一次原生结果只说明该次环境，不能单独证实调度或TCG是原因。

最低执行前待办仅五项：①固定最终同源SHA和完整源码/缓存输入；②实际私有Go/gcc构建闭包；③PG18.6私有运行依赖/share路径/权限与端口；④不影响既有服务的唯一run生命周期/有限资源检查；⑤预先冻结单次driver、6叶库存与原件/严格分析接缝。无新产品协议、TestMain开关或资源审批议题，本页固定回读后可准备隔离环境；业务执行仍以最终源码、固定库存和准备核验全部满足为前提。

## 本次只读依据

- fresh原件：`fresh-01/{stdout.json,transport.json,validation.json}`；旧原件定位见`review.json`。
- 固定3be：`tests/integration/main_test.go:36–85`、`env_test.go:250–272`、`vectors_g2_competition_test.go`；`go.mod/go.sum`及本地replace。
- 已有VM准备：`coverage/formal-environment-preparation-01/handoff.md`；`coverage/local-linux-vm/downloads/{go.provenance.json,postgres-package-selection.json,pgdg-noble-Packages.gz}`；`logs/guest-provenance/{downloaded-debs.sha256,installed-packages.tsv}`。旧bootstrap脚本仅解释VM安装历史，**不得在目标机照抄其sudo/apt/system配置操作**。

## 固定执行边界

私有依赖装配与检查600s、固定源码预构建900s、完整现行184个辅助节点加新增诊断节点240s，之后原G2-11六叶1200s。预构建不运行测试体；各步骤保留首次退出状态，准备失败停止依赖业务并修正具体缺口后记录新的准备尝试。Go辅助测试失败时仍可收集，但不进入G2-11。唯一业务stage启动后不得重放；host断线先读取原PID/start_ticks/退出记录，不推定未执行。停止自有PG与采集独立300s；任何外层清理不回填原2s资源结果。部署传输1800s单独有界，不包含已开始的产品stage。

源包与原件必须在目标本地逐文件SHA核验并收回，再声明同步完成。只在任务目录扩展缓存、编译产物和隔离PG，不改用户现有服务；如实际内存/磁盘不足，在输入前记录准备失败。原生运行新增[有限失败诊断](joint-failure-diagnostics.md)，其余产品源码和输入顺序与3be保持；两环境并非逐字同测试源码对照，不作唯一原因结论。
