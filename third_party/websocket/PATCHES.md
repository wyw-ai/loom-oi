# 本地补丁清单与验证范围

依据 TASK-007 的 `liveness-implementation.md` 和已采用 `data-leg-liveness.md`，本补丁新增有限帧写 API；不改上游默认 Reader、Ping、自动 PONG、Close 或控制会话行为。

## 自有生产代码

| 文件 | 内容 |
| --- | --- |
| `control_write.go` | `WritePing(ctx, payload)` / `WritePong(ctx, payload)`；共同校验仅允许 PING/PONG、payload 最大 125 字节。 |
| `control_write_notjs.go` | 直接调用原 `writeFrame(ctx, true, false, opcode, payload)`，保留原错误链；不经过原 `writeControl` 的额外 5 秒包装。 |
| `control_write_js.go` | 浏览器无法发送控制帧，返回可用 `errors.Is` 判定的 `errors.ErrUnsupported`；原模拟 Ping 未改。 |

两个方法同步完成真实帧写，不等待回复、不登记 `activePings`、不建立队列/worker、不保留调用者 payload。调用者在返回前不得修改该 slice。原接收回调借用的 buffer 仍须由上层 owner 在返回前复制。超过边界或非法操作在写入前拒绝；真实写失败由上层按原终态合同处理，API 不吞错或重试。

取消仍遵循原 `writeFrame`：等待帧锁时退出不主动关闭传输；已在 socket 写阶段的 context 取消可能关闭整连接。因此上层必须传连接生命周期 context，不能用探测观察窗口去取消仍在途的写。这里未改变原已取消 context 与空闲锁的 select 语义。

## 测试与统计

`control_write_test.go` 使用真实 HTTP 升级和两端 Reader，覆盖两个方向 0/8/125 字节、无回复等待/无 payload 别名改写、拒绝 126 字节及其他 opcode、帧锁与活动 socket 写的取消/关闭、真实 socket 错误链，以及原 5 秒包装与新调用者期限的差异。门闩在 Write 入口选中，握手完成后才 arm；每条真实 case 的操作与 cleanup 共用同一 10 秒绝对截止。没有使用放宽库期限、假网络替代或后台遗留来获得通过。

`control_write_js_test.go` 实际验证 JS/wasm 明确不支持和校验次序，不声明浏览器真实帧能力。宿主 Node 的覆盖报告限制与原失败日志必须单列。

补丁有效测试分母在实施前已声明为上述三个生产文件的全部可执行语句；当前 native 11 条，JS 1 条，共 12 条。原生和 JS coverage 应分开实际采集后合并，不能用原上游文件或第三方路径排除来掩盖自有代码。仅原生 11/11 覆盖不能冒称跨平台 12/12。原上游回归覆盖另列。

本目录具有自己的 go.mod，根 `go test ./cmd/... ./internal/...` 和根 `./...` 不会自动运行此嵌套模块的测试。最终验证须显式在本目录运行原上游与补丁 tests/vet，Windows 无 gcc 时不宣称 race 已执行；Linux race 及项目完整产品验证由固定来源质量步骤承担。正式源码归档、漏洞扫描与 runner source manifest 也必须纳入此本地依赖，不能仍只记录上游版本号。

本地实测原件与独立错误负对照保存在 ignored `coverage/websocket-control-patch-01/`；它们是开发验证，不是正式 run 或新的门槛通过结论。
