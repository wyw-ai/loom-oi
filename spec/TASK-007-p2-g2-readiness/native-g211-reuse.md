# 下一次原生 G2-11：依赖复用先行规格

本页保留814c先行的准备规则；现已绑定7b5f750与独占UUID 424cf3d8-bfac-4f40-98e8-40902ec0cf0d完成一次现场执行及停止回收，见[实际结果](native-before-write-result.md)。准备和来源核验通过，六业务叶全部失败；不是尚未执行或业务通过。目标仍是同一已授权 `target-host-1` 的一次开发六叶，不是新正式门槛、clock 或 G5/G7。

## 1. 建议：同机复制已验依赖，新建所有可写运行状态

旧根固定为 `/home/admin/.local/task007/native-g211/ae91f47d-8d14-434b-8057-4d13347f5b1a`。旧数据、原件、配置、凭据文件均保留，绝不重启旧 runner 或续旧 parent。其 PG 已由真实 private pg_ctl 停止、原 PID3948584/start2006434303 组空；这只是已有事实，下一次启动前还需 fresh 身份与空闲核验。

新根固定为 `/home/admin/.local/task007/native-g211/<全新UUID>`，独占0700。**不直接把新环境的GOROOT/PGbin指向旧run，也不用hardlink或跨run目录symlink。** 当前 guest_run.py:platform 要求 Go 在本次ROOT内，prepare_guest.py:stop也固定本次root/pgroot。复用做成远端本地独立复制（普通copy；如文件系统支持可CoW独立inode），既省上传/重复解包，又不需要把原路径校验放宽为任意外部目录。

| 输入 | 已有可追溯内容 | 新run处理 |
|---|---|---|
| 私有Go1.27.3 | 旧prepared的tools_manifest：15,657普通文件，244,139,206B，0 symlink；实际go SHA `5f5eb6d0b2fb6aa0bb5d5f8c5f1c944e0de955c925f0111af92a9d8f79680b5b` | fresh核旧树与原manifest exact，再本地复制到新toolchains；新树再次全清单核验。运行Go来自新ROOT，不写旧GOROOT |
| 私有PG18.6 prefix | pg_manifest：651项=638普通文件/63,589,616B+13相对链接；原官方deb及闭包来源已保存 | 本地复制到新pgroot并核同一清单/链接字面值；链接解析不得越出新prefix。无需再ar/解包8个deb，不改系统库 |
| module/file proxy | cache-provenance固定46版本/167文件，missing=[]，原go.sum/ziphash已验 | 优先读取旧 `input/unpacked/modules.tar.gz`（63,225,977B，SHA `0cea0686606ce8b31bfae55e5ec8d0fc42a4058110abbaa0ae7e4ffb62d22b42`），核原字节后仅解入新cache/modules。也可按原167文件白名单复制旧proxy种子；不整树盲复制运行后新增文件 |
| 可写cache | 旧运行曾使用的expanded modules、GOCACHE含运行状态 | 新GOMODCACHE/GOPATH/GOCACHE独占本次ROOT，默认新建GOCACHE；不把旧可写cache直接作为本次缓存。不复用旧测试或产品binary跳过任何构建 |
| 产品源 | 上轮9c源包及910清单只是历史 | 新最终SHA对应完整Git archive、module/local replace、source清单、helpers库存，独占新source；不能从dirty目录或旧制品反推新源码 |

原155,699,200B依赖payload不用重新经SSH发送。复用不是信任“路径还在”：远端旧工具树/proxy种子须与已保全原manifest/hash逐件匹配；新副本也须exact。只需传新plan/helper脚本、引用清单、固定新源包及必要依赖增量。新source的go.mod/go.sum若与原种子不一致，必须先证明新增闭包并上传缺件；不能只因版本号相同就沿用46版本结论。原cache参考go.mod SHA `f645490ea1906fea924ca8cbd8af33c2884f641c0c6400a0fd6e2b6fdc1017c1`，go.sum SHA `f1838014cbad6b08c2cb707b22ec64faf2ab70643e5ed4bfc9e3e5cb868d81d4`。本地replace仍由新完整source提供，不走proxy。

这是有意选择的最少入口变更方案；直接跨run只读引用工具prefix虽然少复制约308MB，却需新增受限shared-root清单/平台与stop验证分支，没有必要为本轮省这次同机复制扩大边界。

## 2. PG使用全新data，不重用旧cluster

新PGdata、pgsocket、随机role/password和0600环境文件；固定127.0.0.1:55432，先fresh确认端口空闲。initdb使用新prefix的绝对bin及`-L` share路径，PG专用LD_LIBRARY_PATH仅给PG进程，dynamic_library_path也指新prefix。测试主环境继续无LD_PRELOAD/LD_LIBRARY_PATH/GODEBUG/RES_OPTIONS。旧PGdata和原日志不改不清空。

私有PG配置沿原真实值：18.6-1.pgdg24.04+2、shared_buffers128MB、max_connections100、fsync/synchronous_commit均on；必须实际回读版本、share/ELF依赖、data/port、配置hash。标准startup等待≤10s且被assembly/parentD截短，保存每次ready原返回；这是同一启动的ready等待，不是重跑业务。启动后记录全新PID/start_ticks/PGID/SID/boot/exe/data/socket，不能沿用3948584。

不选“同一pgdata新启动”：仅换PID/密码并不能清除旧catalog/WAL/统计/可能残留的测试数据库。原env_test的强制DROP返回被忽略，也未保存足以证明旧cluster完全空的目录清单。重用会是已用cluster的后续样本，必须另说明这种状态；没有必要为省一次initdb引入此差异。新data消除了持久数据库状态复用，但同机文件页缓存/系统负载仍可能已热，不能宣称物理冷启动或控制变量实验。

## 3. 新锚点、原7步及所有期限

新bootstrap第一次执行前即建立本次parent单调起点，9000s覆盖fresh资源核验、复制/传输、装配、源码、7child、stop/collect；host为该次独立起点9030s。**不得使用旧prepared/anchor的期限，不得由“重用依赖”延续或刷新旧run。** 新单次传输窗口1800s从本次首次receive起算，新source包仍消费它；本地复制/依赖验证/新PG装配统一600s，均min(parentD)。源码未固定就不启动这个parent，避免准备等待消耗后又续期。

保留既有实际7child顺序：

1. `go test -c -p=1 -o <new>/prebuilt/integration.test ./tests/integration`
2. `go build -trimpath -o <new>/prebuilt/ois ./cmd/ois`
3. 同flags构建oic。
4. 同flags构建oi。
5. `go version -m`核4制品并保存binary SHA。以上5命令共享900s绝对D。
6. 最终source实际helpers精确库存，原240s；225是上轮库存，若新实现增减须按同源码实际JSON/定义重新固定，不能自动假定仍225。
7. 原 `go test -json -p=1 -count=1 -timeout 90m -run ^TestTVG2_11_ControlExportCompetition$ ./tests/integration`，SQLite/PostgreSQL各round0/1/2，原1200s。

helpers与G211共享5700s，所有child均被parent截短；Go90m不压过stageD。原TestMain无条件buildAll不跳过；CGO测试1、产品3bin0、GOMAXPROCS2、GOFLAGS=-p=1、CLOCK0/LOAD0、offline file proxy及原输入/资源判线保持。helper失败停止依赖业务；业务一旦启动，普通Go exit1保留完整原六叶及自然退出，不挑失败叶重试。未知SSH结果不重放；真正需要修准备缺口须另记录新尝试，不能覆盖本次结果。

## 4. 最少脚本差异与证据

只需派生新的bootstrap/assembly输入路径：接受固定旧根与精确reuse清单作为只读种子，copy/verify代替Go/deb重新解包；module种子仍按原安全解包规则。新环境所有执行/可写路径仍在新ROOT，guest_run的source、PG、deadline、step、write-boundary和collector身份检查可原样复用。host仅新UUID/source/helper清单与输入hash，不把旧执行stage记录复制为新结果。不会移动旧目录去做共享工具目录，也不改变旧权限。

`reuse-inputs.json`最小内容：旧root/旧prepared SHA、原manifest哈希/精确条目、来源版本；实际复用前旧内容核对、copy后新内容核对、源/目的路径、起止及原D。保留实际旧种子哈希和新副本哈希，而非仅声明“reused”。新prepared/environment模式与上轮一致，但绑定新root/boot/UID/工具路径/PG身份及新parent。若copy或hash失败，保留partial新目录和首次错误，不退化为未经核验直接引用。

执行前fresh核主机身份/boot、UID、资源/FDlimit、55432监听、旧runner/7child/PG记录及当前可读进程归属；旧Gone不能代替现时观察。检查现有frp/nc等原服务仍不受干预。沿原准备guard空闲磁盘≥6GiB、MemAvailable≥768MiB；按新source/依赖副本/cache/PG/日志实际size核预算并留余量。旧估算3,034,836,194B只作历史参照，不假定当前足够；不足就停止依赖准备，不删旧证据/停止既有服务来凑条件。

各child沿原PID/start/session/group、原argv、源码before/after、完整stdout/stderr、结果与fsync返回边界。最后只用新PG精确身份+新私有pg_ctl fast stop，stop与collect共享300s/min(parentD)，核新PG及子进程组Gone。使用已修collect_final固定收PG完整最终日志、stop控制/输出以及所有run文件，两遍set/hash一致；runner仅group=False身份核验，勿夸大成单独runner组扫描。旧采集原件、旧PGdata和所有旧失败均不改。

## 5. 目前可复用的依据与待绑定

- 上轮准备：`coverage/native-g211-preparation-01/handoff.json`、`collect-01/artifacts/prepared.json`（SHA `8455dfffa0114fe3e7fdef7ac32a368979524fb3a0814d280aed857cfca68d1c`）以及`inputs-01/cache-provenance.json`（SHA `281a25471b09d8ae9cbbd6536c774fe566bd81b3f538c2b92063087090bf4538`）。
- 最终退出/采集：`coverage/native-g211-execution-01/recovery-final-review-01/handoff.json`（SHA `fb660c513a18c37ea572e5d9ab8b6888d0d1158aaca97806608181d469a6da1b`）；旧source与164+manifest原件保持。
- 现有入口约束来自`guest_run.py:77–108`和`prepare_guest.py:193–247,272–299`；实际脚本hash由上轮本地review保存。

实施前仅待主线：最终SOURCE/真实helpers库存与源依赖对照；新UUID和冻结reuse清单；派生脚本窄diff/必要纯mock审查；按既有Autopilot授权完成一次fresh核验与新parent启动。本稿不绑定未定源、不开始任何现场步骤，也不预告六叶或正式门槛通过。

本规格只派生[原原生执行规格](native-g211.md)的依赖准备方式，业务侧使用[beforeWrite联合输入规格](before-write-input.md)固定后新源码。原完整运行见[结果](native-g211-result.md)，不会重放或更改其原件。复用清单、派生脚本及最终源码经核验后继续执行，不请求新的用户许可。
