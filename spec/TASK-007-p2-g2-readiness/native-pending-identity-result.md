# 原生 pending 身份诊断结果

固定 `dd62daab4f86db940be2c3d1752b7efc008398fb` 已在授权非生产资源 `target-host-1` 完成一次原定七步。五步构建及 298 个 helpers 通过，G2-11 为三叶 PASS、三叶 FAIL；原 root 和六个 strict 组仍 FAIL，没有门槛晋级。

| 叶 | 实际结果 | 原业务窗口与资源 |
|---|---|---|
| SQLite 0、1 | FAIL，pre_dial_full | 未建立输入、未创建慢 socket，无完成 B4 |
| SQLite 2 | FAIL，slow_full | 已过前置；后置 Full 未成立，未完成业务窗口/B4 |
| PostgreSQL 0 | PASS | 117 cycles，60.242454046s，B4 PASS |
| PostgreSQL 1 | PASS | 117 cycles，60.354403253s，B4 PASS |
| PostgreSQL 2 | PASS | 116 cycles，60.465928648s，B4 PASS |

958 个源码文件、164 项收集原件加 manifest 逐字核验；七步完整退出，原期限保持。产品流水线378.292276725s，专用PG已按身份停止且组空。短命准备器也已完成实际Linux机制测试及装配复核：14临时命令均真实wait0，gcc-runtime的exe缺失由真实WNOWAIT退出证明；旧装配失败没有补判成功。历史路径扫描的权限可见性缺口仍保留。

## 离线解析修复

固定dd62解析器错误地只接受 `preload_joint`，真实Go阶段为 `preload_probe`，导致六叶首条请求冲突。原拒绝结果完整保存；本提交仅修正该枚举，新增真实三阶段序列和错误阶段拒绝回归。旧代码真实失败，修复后9项诊断与44项原准备测试共53项通过。产品、Go夹具、主G2分析器及既有业务判线未变。

870次实际请求（225/227/220/65/67/66）经仅此枚举修正全部完整配对，无缺失、迟到、重复或overflow。这是诊断完整性，不是业务通过；运行source、metadata和日志始终绑定dd62，离线解析修复单独标明。PID来自原资源基线，run_id/epoch来自启动及Welcome，完整Supported绑定来自真实Register/wire。时间只用同次HTTP区间夹定，不相减不同进程时钟。

## 新的实际反例

SQLite0/1的119/128个实际Full样本中，真实pending八项的最大年龄分别158.978047–546.750039ms、252.892513–548.818437ms。对应八项的80ms必要窗口与原OIS HTTP区间无交集；只覆盖这些采样，不外推整个3s。

SQLite2前置Stats的八pending与随后young八项仅交4个ID；PG1成功叶的前置交集为0，后置final又为8。因此先Full后young可以跨批次，既不能默认同批，也不能把换批本身判为失败。

SQLite2前置接受时原prepare剩431.025519ms，32Drop已耗尽。随后八条未Drop Snapshot完成真实转发；首次slow_full约104.115ms后采到backlog/pending4/4。25次后置采样依次7次4/4、12次2/2、6次0/0，P2/Q4均成立。后置资格正确拒绝。proxy写完成不等于精确服务端归约时刻，后端差异不构成唯一原因。

下一步先论证同一原3s、16Set、32Drop单批及80/200/300/500ms下的具体输入时机。调换采样只能排除混合见证，不能创造年轻批次；晚arm可保留额度，却不保证同步已有refresh/query。本结果不批准原样重跑、扩大Drop或放宽判线。默认配置的完整Linux质量检查已另外收回、独立复核与语义核验继续，正式G1/G3/G4/G2及授权SSH/Bash交付继续；19旧run、14ITER与alpha/main保持。

原始结果、修复前后命令、独立诊断和生命周期来源见[结构化记录](native-pending-identity-result.json)。大原件保存在明确的本地ignored路径，未冒充正式证据已入库。
