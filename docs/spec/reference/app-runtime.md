# App 定义、参数绑定与 worker 运行时

本文规定当前 beta 的本地 App 格式、OIC 参数适配、stdio worker 协议及加载生命周期。OIS 中的 Tool 定义与固定来源见 [OIS API](ois-api.md)，OIC 本机配置见[配置参考](configuration.md)，管理命令见 [CLI](cli.md)。Bash / PowerShell 的业务参数、执行句柄与资源限额见[终端 Actions](terminal-actions.md)。

OIC 根据声明加载、绑定和转交，不包含 Bash / PowerShell 的业务分支。App 可以使用不同语言实现；现有 Bash / PowerShell App 各自是可移出主工程构建的 Go 模块，运行入口分别为 `oiapp-bash` / `oiapp-powershell.exe`。YAML 与实现、依赖、测试、构建入口同属一个 App 包；它们不引用 OIC 内部代码，也不要求未来 App 采用相同语言或内部结构。

## 1. App 包与 YAML

一个 App 包至少包含一个定义文件及其运行入口。下面是仓库示例的交付形状，目录名可由部署者选择：

```text
apps/
  bash/
    app.yaml
    runtime/oiapp-bash
  powershell/
    app.yaml
    runtime/oiapp-powershell.exe
```

完整定义分别见 [Bash App](../../../apps/bash/app.yaml) 和 [PowerShell App](../../../apps/powershell/app.yaml)。App YAML 必须只有一个文档；根、`app` 和 Action 结构中的未知字段会被拒绝。Schema 和 runtime 是独立的映射结构，其支持边界见下文。

| 字段 | 类型 | 必填 / 默认 | 含义与约束 |
| --- | --- | --- | --- |
| `version` | integer | 必填，`1` | App 定义版本 |
| `app` | object | 必填 | App 标识 |
| `app.code` | string | 必填，非空白 | OIC 内的 App 标识；用于查找 `app_config.<code>`，不是 OIS Tool 展示名 |
| `runtime` | object | 可省略 | 各 Action 的默认执行入口；若省略，每个 Action 必须能从自身声明得到有效 `command` |
| `runtime.command` | string[] | 每个有效 runtime 必须具有 | 非空参数数组，第一项为非空程序入口，后续项为原样传入的参数 |
| `actions` | object | 必填，至少一项 | 键为本地 Action 名 |
| `actions.<name>.description` | string | 必填，非空白 | 本地能力说明，可随登记提供给 OIS 查询 |
| `actions.<name>.input_schema` | object | 必填 | 描述本地实现接收的参数；根 `type` 必须是 `object` |
| `actions.<name>.input_mapping` | object<string,string> | 可省略 | 顶层改名映射，形式为“本地目标参数名: 调用来源参数名” |
| `actions.<name>.handler` | string | 省略或空字符串时等于 Action 名 | worker 实际暴露的入口名，必须出现在 `ready.handlers` 中 |
| `actions.<name>.runtime` | object | 可省略或 `{}` | 覆盖根 runtime，并影响实例归属；见第 3 节 |

当前执行器只使用 `runtime.command` 启动 worker。`type`、`protocol`、`cwd`、`env` 等并非已实现的 runtime 配置项；即使额外键能进入 runtime 映射，也不能据此期待相应启动行为。stdio JSON Lines 是当前运行时协议，不由 YAML 的 `protocol` 字段选择。当前 Action 格式没有 `output_schema` 字段；业务结果由 App 产生，通用宿主不校验其业务 Schema。

OIC 不把 `command` 拼成 shell 命令，也不展开变量或解释重定向。程序名包含 `/` 或 `\` 且为相对路径时，相对于 App YAML 所在目录解析；不带路径分隔符的程序名由系统 `PATH` 查找。worker 的初始工作目录为该 YAML 所在目录，参数数组中的其他字符串不会被自动改写成绝对路径。

## 2. 本地 Schema 与轻量参数适配

OIS 的 Tool Schema 描述消费端输入；本地 `input_schema` 描述 Action 接收的数据。两者不要求严格相等，`load` 不做中心与本地 Schema 一致性门禁。Action 的业务范围、目录可用性、参数组合等仍由 App 校验。

### Schema 字段

| 字段 | 当前处理 |
| --- | --- |
| `type` | 每个已声明 Schema 节点必须具有；支持 `string`、`integer`、`boolean`、`number`、`object`、`array`；Action 根必须为 `object` |
| `properties` | `object` 上的属性 Schema 映射；已声明的属性递归适配 |
| `required` | 字符串数组；对象完成默认值与适配后检查字段是否存在 |
| `default` | 仅在字段缺失时采用，并经过该字段的相同类型适配；加载时检查默认值能否适配 |
| `description` | 说明信息，不参与类型转换 |
| 其他 JSON Schema 关键字 | 当前绑定器不执行，例如 `minimum`、`maximum`、`enum`、`additionalProperties`、`items`、`$ref`；不能把它当成完整 JSON Schema 校验器 |

### 类型转换

| 目标类型 | 接受的输入 | 不执行的隐式转换 |
| --- | --- | --- |
| `string` | 原字符串；数值上为整数、可落在 int64 范围内的数字转为十进制字符串 | 布尔、对象、数组不会被序列化为字符串 |
| `integer` | int64 范围内的整数、有限且恰为整数的数字、可完整解析的十进制整数字符串 | 不截断小数，不把布尔变成 0/1 |
| `boolean` | 布尔；忽略大小写的字符串 `true` / `false` | 不按非空字符串或非零数字判断真假 |
| `number` | 有限数字，包括整数 | 不解析数字字符串 |
| `object` | 对象；递归处理其已声明的 `properties`、默认值与必填字段 | 不把 JSON 文本解析为对象 |
| `array` | 原数组 | 当前不适配或验证数组元素的 `items` |

数字和布尔字符串转换可忽略两端空白；普通字符串内容保持原样。显式 `null` 不能转换为上述任何类型，也不等于省略。未声明的对象属性保留并交给 App；`additionalProperties: false` 当前不会改变此行为。命令 App 会进一步拒绝自身不支持的参数。

### `input_mapping` 顺序与冲突

例如消费端提供 `command`，本地实现接收 `cmd`：

```yaml
input_mapping:
  cmd: command
input_schema:
  type: object
  properties:
    cmd: {type: string}
    yield_time_ms: {type: integer, default: 10000}
  required: [cmd]
```

以上是 Action 内的字段片段。完整 Action 仍须包含 `description`，并继承或声明有效 runtime。

绑定按以下顺序发生：

1. 从原始输入快照读取所有映射来源；存在的来源改名，原来源键移除。来源不存在时不生成目标字段。
2. 同一来源可用于多个目标，交换和循环映射始终读取原始快照。若调用者同时提供来源和目标，而目标又不是另一条映射正在迁出的来源，返回 `INVALID_ARGUMENT`，不静默覆盖。
3. 按本地 Schema 补固定默认、适配类型，再检查 `required`。绑定失败发生在本次命令创建或输入写入之前。

映射不支持表达式、拼接、条件或脚本。OIC 不按类型相似性猜测字段关系，也不在此层实现 shell 参数规则。

## 3. runtime 继承与实例归属

有效 runtime 采用顶层字段覆盖：先取根 `runtime`，再用 Action 的同名顶层字段覆盖。数组和对象作为整个值替换，不递归合并；`command` 数组不会拼接。

| Action 声明 | 有效配置 | 本次 App 加载中的实例 |
| --- | --- | --- |
| 不写 `runtime` | 继承根默认 | 与其他继承根默认的 Action 共用根 worker |
| `runtime: {}` | 继承根默认 | 同样共用根 worker |
| 非空 `runtime` | 根默认加 Action 顶层覆盖 | 该 Action 独立 worker |
| 仅 Action 声明 `runtime.command` | 使用自身入口 | 该 Action 独立 worker，不要求存在根入口 |

实例不按 `command` 文本相等自动合并。不同 App 加载也不会共享根 worker。现有 Bash 与 PowerShell App 各自的两个 Action 都继承本 App 根 runtime，才能访问同一内存执行后端；把某一个 Action 改为非空 runtime 覆盖，不会自动获得另一个实例的 session。

每个 worker 启动时收到同一 App 对应的 `app_config.<app.code>`，不按 Action 再拆分配置。OIC YAML 和 `app_config` 在 OIC 启动时读取；修改这些配置须重启 OIC，单独 `unload/load` 不会重新读取 OIC 配置文件。App 定义文件本身在每次 `load` 时读取。

## 4. stdio JSON Lines 协议

每行一个完整 UTF-8 JSON 对象，字符串中的换行使用 JSON 转义。worker 的 stdout 只用于协议，stderr 用于诊断；业务命令输出放入 Action 结果或 App 自己的有界缓冲，不直接写到协议 stdout。OIC 持续读取两条流，worker 必须串行化输出帧，避免并发响应交错。

### 帧与字段

| 方向 / `type` | 字段 | 约定 |
| --- | --- | --- |
| OIC → worker：`start` | `protocol: 1`，可选 `config` 对象 | 第一帧，交付本机 App 配置；缺少配置时按空配置处理 |
| worker → OIC：`ready` | `protocol: 1`，`handlers: string[]` | 初始化完成及实际可分发入口；声明版本不符或缺少 YAML 所引用 handler，则加载失败 |
| OIC → worker：`invoke` | `call_id`、`handler`、`input`、`context` | 每次 Action 调用；`input` 已经过本地映射与适配 |
| worker → OIC：`result` | `call_id`、`result` | 本次 Action 的业务 JSON 结果，不等于整个长执行已经结束 |
| worker → OIC：`error` | `call_id`、`error` | 结构化调用错误 |
| OIC → worker：`cancel` | `call_id` | 尽力取消仍在处理的对应调用；不新增公开 Action |

| 公共字段 | 类型 / 含义 |
| --- | --- |
| `call_id` | 非空字符串，关联一次调用及其响应；不能把它当作执行句柄 |
| `handler` | 从已加载 Action 查出的入口名，不采用业务参数自报的任意入口 |
| `input` | 本地输入对象；不能覆盖 runtime、启动 config 或可信 context |
| `context.access_identity` | OIS 提供的非空可信访问身份 |
| `context.vmcp_id` | OIS 提供的非空 vMCP 范围 |
| `error.code` | 稳定错误码字符串 |
| `error.message` | 错误说明 |
| `error.details` | 可选对象，例如绑定字段或已写入输入字节数 |

内部 `invoke.input` 包装是传输结构，Agent 调用 Tool 仍直接提供其业务参数。OIS—OIC 的 `app/action` 固定来源由 OIC 查表转换成此处的 `handler`，不是直接透传一个用户可选择的程序入口。

一次命令调用的本地帧示例：

```jsonl
{"type":"start","protocol":1,"config":{"shell_kind":"bash","shell_path":"/usr/bin/bash","default_cwd":"/srv/workspace"}}
{"type":"ready","protocol":1,"handlers":["exec_command","write_stdin"]}
{"type":"invoke","call_id":"call-001","handler":"exec_command","input":{"cmd":"pwd","yield_time_ms":1000},"context":{"access_identity":"identity-1","vmcp_id":"workbench"}}
{"type":"result","call_id":"call-001","result":{"output":"/srv/workspace\n","exit_code":0,"termination_reason":"exited","output_truncated":false,"omitted_bytes":0,"output_incomplete":false}}
```

worker 必须允许不同调用并发处理，不能用一个长等待阻塞协议输入、取消或其他 session。每个已接受调用提交一个 `result` 或 `error`；本地 worker 协议没有逐字节 stdout 通知或公开日志游标。

### 当前宿主边界

以下是当前实现的固定边界，不是 `runtime` YAML 可调字段：

| 项目 | 当前行为 |
| --- | --- |
| 单帧 | 1 MiB 上限，发送端按包含行尾的帧大小检查；超长、非法或不完整响应会使 worker 不可用并触发回收 |
| `ready` 等待 | 每个 worker 的 `start` 写入成功后，等待 `ready` 最多 10 秒；写入另有 5 秒期限。多个 runtime 串行初始化，没有整次 `load` 共用的 10 秒总截止时间 |
| 协议写入 | 单次写入等待 5 秒；卡住时标记 worker 不可用并关闭、回收 |
| OIC 在途调用 | 同一 OIC 最多 256 个；到限拒绝新调用，不建立后台业务队列 |
| 已完成调用去重 | OIC 保留有限响应，按 128 个 / 10 分钟清理；在途记录不会被已完成缓存的淘汰规则删除 |
| worker 业务并发 | 由 worker 实现控制；现有命令 worker 最多 64 个在途调用，详见终端参考。OIC 的 256 上限不等于任意第三方 worker 自动获得相同内部限制 |
| 调用期限 | OIC 单次调用 60 秒；到期转内部取消，取消请求之后最多再等待确认结果 5 秒；未确认时为 `CALL_RESULT_UNKNOWN` |
| stderr 诊断 | 每个 worker 日志保留首 1 MiB 诊断内容，日志头另计；后续继续读取并丢弃，避免反压阻塞 |

`call_id` 去重同时核对原 App/Action、原输入、可信 context 和原实现实例。同 ID 不同内容或归属为 `CALL_ID_CONFLICT`；相同调用复用首次绑定及已有响应，不重新应用新默认值。缓存有限且不跨 OIC/worker 重启，不承诺跨重启“恰好执行一次”。更换 `call_id` 是新调用，不能作为未知执行结果的安全重试机制。

## 5. load、登记与配置变化

`oic load <path>` 的成功条件是本地 App 的全部必要入口可用，并成功保存加载记录：

1. 读取单文档 YAML，校验 App/Action、基本 Schema、映射和有效入口。
2. 按实例规则初始化全部 worker，逐个等 `ready` 并核对 handler。
3. 任一步失败时回收本次新建资源，不发布部分 App，不影响其他已加载 App。
4. 全部入口仍然可用后，保存加载记录，建立本地 App/Action 索引。
5. 独立向 OIS 登记。OIS 离线时本地加载仍可成功，登记为 `pending`；收到确认后为 `registered`，重连后重新登记。

本地 `ready` 与中心 `registered` 是不同状态；OIC 已连接不代表某个 App 已可执行。登记不会创建或覆盖 OIS 的 Tool 定义，调用者仍需在 OIS 配置 Tool 的固定来源。

同一 App code 的重复加载以解析后的定义、YAML 所在目录和本机 App 配置计算有效标识。相同标识返回当前状态，不新建 worker；仅调整 YAML 空白不重启。标识变化时返回 `APP_ALREADY_LOADED`，应先 `unload` 再 `load`。实现程序文件内容不参与该标识，不监听代码文件变化或热替换运行中的 worker。

worker 崩溃后，同定义的重复 `load` 不充当自动重启；正常恢复流程为显式 `unload/load`。OIC 启动恢复加载记录失败时保留不可用条目及错误；修正文件后可再显式 `load` 重试该未成功恢复的条目。记录中的 App code 若改变，恢复会失败，不把旧记录自动迁移成另一个 App。

## 6. 卸载、停止、恢复与回收

| 操作 / 事件 | 本地状态与资源 |
| --- | --- |
| `unload` | 先持久删除加载记录、撤去本地索引和中心登记、取消该 App 在途调用，再关闭其全部 worker；清理报错不会恢复已经删除的记录 |
| OIC `stop` | 停止接入并关闭全部托管 worker，保留加载记录 |
| OIC `start` | 重新读取配置和记录中的 YAML，校验并创建新 worker；不恢复原进程、输出缓冲或 session |
| worker 意外退出 / 协议失效 | 标记不可用，通知中心并回收其托管后代；不会自动重启 |
| OIS—OIC 暂时断连 | 不自动卸载 App；原 worker 和本地执行可以继续，仍受本机期限约束 |

worker stdin 的 EOF 是本地协议的关闭信号，没有额外 `shutdown` 帧。worker 收到 EOF 后停止新调用、清理拥有的执行、排空输出并退出。OIC 在继续读取 stdout/stderr 的同时先给出 5 秒退出宽限，随后用通用进程托管兜底，并最多再等待 3 秒；不能把关闭方法报错解释为“仍保留可继续加载的旧 App”。

当前通用托管在 Windows 使用 Job Object，子进程执行用户逻辑前加入 Job；Linux 使用独立监督进程和 subreaper，覆盖普通 PTY 子进程创建新会话的情况。业务 App 自己管理执行状态，OIC 在 worker 崩溃后仍负责托管范围内的回收。该机制不是通用系统隔离或沙箱，不承诺管理主动逃离受控范围的任意外部服务。

## 7. 实现依据

- [App 配置与实例解析](../../../internal/oic/config.go)、[参数绑定](../../../internal/oic/binding.go)。
- [加载、登记、调用及记录生命周期](../../../internal/oic/host.go)、[worker 托管](../../../internal/oic/worker.go)、[公共帧类型](../../../internal/protocol/protocol.go)。
- [Linux 进程托管](../../../internal/procgroup/group_linux.go)、[Windows 进程托管](../../../internal/procgroup/group_windows.go)。
- [OIC 运行契约测试](../../../internal/oic/host_test.go)、[绑定测试](../../../internal/oic/binding_test.go)、[进程树回收测试](../../../internal/procgroup/group_test.go)。

这些链接用于定位当前实现和已有测试，不表示维护本文时重新执行了功能验收。返回[规范入口](../README.md)。
