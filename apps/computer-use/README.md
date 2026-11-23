# Computer Use 截图 App

`computer-use.screenshot({})` 在 Windows 11 x64 的当前交互会话中采集整个虚拟桌面，每次调用在配置的输出根下生成新的 PNG。返回值中的 `path` 是根相对路径，`width` 和 `height` 是原图的像素尺寸。

## 部署

在 Windows 上使用 Go 1.26 或更高版本构建：

```powershell
./build.ps1
```

将 `app.yaml` 和 `runtime/oiapp-computer-use.exe` 一同部署到 Host A，使用能访问当前用户交互桌面的 OIC 加载。为 OIC 配置已存在且运行账户可写的绝对路径：

```yaml
app_config:
  computer-use:
    output_root: 'C:\oi\exchange'
```

worker 在根内建立 `captures` 目录。Action 的输入为 `{}`，一次成功调用返回类似以下结果。

```json
{"path":"captures/EXAMPLE.png","width":2560,"height":1600}
```

将 File Store 的本机读取根也配置为该目录的父根（示例中的 `C:\oi\exchange`）。Agent 可以直接将 `path` 传给该 Host 的 `file-store.put_file`，随后用文件引用读回到另一位置，调用 `filesystem.view_image` 验收实际画面。桌面可能包含用户数据，部署方通过 OIS 的发布范围和操作系统目录权限控制访问及原件保留时间。

未连接、已锁定或无法访问当前输入桌面时，采集可能返回 `CAPTURE_FAILED`。屏幕抓取使用 Windows GDI，受系统保护的画面内容由 Windows 决定。worker 同时处理一项采集，忙时返回 `CAPACITY_EXCEEDED`。单次原图的虚拟桌面上限为 64 Mi 像素。
