# 固定指标前缀的等字节渲染优化

2026-11-05，编码前固定。只优化已采用有限指标 schema 的重复格式化，不改导出契约、请求输入或验收期限。当前产品文件与固定 `7da7159a339986659b18842350ae1edc242e958a` 的 `internal/metrics/render.go` 相同。

## 依据与不能推出的结论

单次 Linux HTTP 机制中，OIS/OIC 的真实200路径从handler入口到首次Write分别取得273/243个样本，中位6.1658/7.5291ms；这段还包含deadline穿透、取槽、Snapshot、调度和仪器成本，不能称为Render独占时间。slow连接的实际网络Write/Flush均很快返回，不曾形成200ms持续阻塞；原输入未成立及OIC observer迟到保持，不能提前宣称本优化修复了G2。

源码可直接核实的重复工作是：固定13族、35 counter series和26 histogram series，每次重复构造HELP/TYPE与标签/bucket前缀，295行逐行fmt格式化。schema不接受动态注册或实体label。这些固定字节可以仅生成一次，降低每次抓取的工作量，且不改变可见输出。

## 唯一实施范围

1. 包初始化时，沿原descriptor/series顺序生成不可变HELP/TYPE、counter、bucket、sum/count前缀。仍使用原HELP/label转义规则、bucket文本的`g,-1,64`及`+Inf`。缓存为进程内固定有限表，不接受运行时注册，不读取或缓存Registry/Snapshot数值。
2. 每次Render继续接收原Snapshot值并分配独立输出。动态uint64使用十进制AppendUint，float使用AppendFloat的`g,-1,64`；先写固定栈scratch，再按原剩余容量加入输出。不能先扩过limit再报错。维持原uint64累计行为与所有float字面量，不改样本顺序、LF、ContentType或返回类型。
3. 原公开64KiB上限和私有render(limit)语义保持：负值、容量不足均返回nil/ErrTooLarge，精确长度允许成功；不返回部分响应。前缀缓存不暴露可变切片；一次返回body被调用方修改不得污染下一次或并发Render。
4. Registry、Snapshot锁范围、Handler/Activity、两槽、200ms、Write/Flush、HTTP错误路径与所有G2输入均不变。没有新增writer goroutine、队列、锁、请求、重试或期限。初始化缓存的固定内存成本需报告，不能说零成本。

范围限于`internal/metrics/render.go`及有判别力的测试/benchmark。原转义函数的直接回归保留；不为提高覆盖率删除既有断言或调整分母。CLI/API、协议、图和已采用片段无语义变化。

## 验证与停止条件

测试内保留明确注明固定7da来源的旧渲染算法作字节对照，不调用新前缀表。比较零值、全部family/label非零、bucket边界、最大uint64及累计溢出、极小/极大/指数float，以及包内可构造的负零/NaN/Inf。原13族解析器仍验证可解析的正常输入；解析成功不能代替字节相等。

以旧输出长度L核`-1/0/L-1/L/L+1/64KiB`，并覆盖不足时nil返回、HELP/label特殊字符转义、返回body独立性及并发只读缓存。保持既有metrics包和Handler真实失败/慢写回归。先运行Windows聚焦单测、vet和静态检查；固定源码后再在本地Linux检查该包unit/race，不把Windows或局部覆盖冒充完整交付检查。

新增小benchmark同时调用旧reference和新Render，使用同一Go进程、相同零/非零/极值Snapshot及原输出长度，报告实际ns/op、B/op、allocs/op，不新增产品SLO。首次有限测量为每子项200ms、count=3、整个命令上限30s；若不足或失败保留，不靠延长到有利结果。基准只检验是否减少重复格式化成本；没有改善则重新评估这一候选。

此处不预注册新的G2正式结果。后续是否已经形成真实慢写/竞争输入，仍由新固定源码上的原判据、原原件与独立分析决定。不得复用7da机制的通过/失败去替代新版本，也不修改任何旧run、门槛或alpha。

## 本地实现检查点

2026-11-05，按先行提交 `0e343c2d04e65babb025b6d6948b112707331bec` 实现固定前缀，Handler/Registry不变。Windows Go1.27.3完整metrics包79项PASS、220/223语句覆盖（98.6547%），vet/staticcheck通过；独立复核11份源与27份原件SHA及旧算法618tokens一致，未见高置信阻挡。唯一200ms×3基准在同一30s内8.608s完成，新输出均65536B/1次分配；固定缓存逻辑25724B（不含分配器取整/初始化暂存）。精确argv、原件hash、库存和18样本见[本地记录](render-prefix-local.json)。这只支持本机格式化成本降低；Linux/race、完整产品和G2输入仍待新固定源实测。

## Linux 定向复核完成

固定 `d222f35eb6c8543094a2e74a1af409cec3452563` 的本地QEMU一次unit/race各79节点、15根PASS，无skip/fail或DATA RACE；完整metrics覆盖220/223=98.6547%，render73/73。原stage120/180s内分别18.0723/22.8118s，共享360s至最终结果fsync54.7095s；完整720 Git文件及22原件168584B核对一致，runner和两子组均Gone/empty，PG/boot/Go/小盘不变。root另独立核原GoJSON、覆盖与21个manifest成员、三个进程检查。具体SHA、过程及限制见[Linux记录](render-prefix-linux.json)。只支持该包的新实现与race，不替代全产品95%或G2实际输入；没有benchmark重跑、正式run或门槛晋级。
