# Service 与 File Store 实机验收

## Host A 桌面读回

验收时，Windows Host A 已登录交互桌面。Linux 服务器独立运行 OIS 和 AList v3.66.0，AList 使用可写的 Local 存储。Windows OIC 各自加载 Computer Use、File Store 和 Filesystem 三个普通 App。Agent 只用 `workbench` 发布的 MCP 成员发起业务调用。

测试窗口在桌面显示一次性随机字符串，另存独立对照值。Agent 先查看 MCP 返回的图，再与对照值核对。截图、凭据和本机配置只保存在隔离测试环境。

第一轮运行于 2026-11-23 16:07 UTC，运行编号为 `1d76e352532247b097ee9239eaa0642a`。Agent 在深蓝底上读到白字 `HOST A / LIVE SCREEN` 和浅青色的 `SILVER-CF454115`。与独立对照值逐字匹配。

| 调用 | 本轮结果 |
| --- | --- |
| `capture_a` | 捕获 2560 × 1600 的实际桌面，生成 `captures/SPFFS7NRL5ER6J3U32MUSSIM6I.png`。 |
| `put_from_a` | 上传至 `shots/host-a/1d76e352532247b097ee9239eaa0642a/desktop.png`。 |
| `lookup_on_a`、`list_store_a` | 完整路径查得同一份文件引用，列举本次目录也找到了这张截图。 |
| `get_on_a` | 从 AList 下载至新的本地路径 `returned-1d76e352532247b097ee9239eaa0642a.png`。 |
| `view_on_a` | Filesystem 对取回文件生成原生 MCP image 内容，调用 ID 为 `call_3380f61d12d98e7d9e023f3ca242c0bb`。 |

本轮源文件、上传回执、取回文件和 MCP 返回的 PNG 均为 29,904 字节，SHA-256 均为 `4164685bc29e7fd368edad329e0bb8b6a29732e0f9239d74b3f12c43484a6db9`。Filesystem 在需要时可以缩放或重新编码供查看的预览，原件比对应使用上传回执和取回文件。

部署配置、构建命令及复验步骤见[操作指南](../../guides/service-files.md)。[验收脚本](../../../scripts/acceptance-service-files.ps1)保存每步调用 ID、存储引用和返回的图像，供 Agent 读图及现场核对。


## 最终包复验

在提交 `f885482` 上重新构建并加载 File Store，保留同一 Service，换一张随机桌面验收卡。该轮于 2026-11-23 16:14 UTC 完成，运行编号为 `0e4be09da8254e438dbeb685a979bdf1`。

- 通过 MCP 依次完成发现、截图、上传、按路径查询、目录列举、下载及看图。最终的看图调用 ID 为 `call_296ca5d000f4db17a8920d62ad565904`。
- Agent 在返回的深蓝色画面里读到 `HOST A / LIVE SCREEN` 和 `ORBIT-0972EE13`，随后与本轮独立保存的对照值逐字核对，一致。
- Host A 的截图原件、独立路径下的下载文件和 MCP 返回 PNG 均为 30,254 字节，与上传回执的 SHA-256 `a8f658d2b873a0d135ef61c1273cbde3baedd065eb67df4ddb83911a193da0ee` 一致。

## 仅凭目录在 Host B 取回

Windows A 和 Linux B 是两台机器。B 使用独立的 OIC、File Store worker 和本地文件目录。此次 B 与服务器上的 OIS、AList 共机。两个 App 实例连接相同的 WebDAV 挂载，并使用相同的逻辑存储、Service 与可信调用身份。

| 在 B 的操作 | 观察结果 |
| --- | --- |
| 将第一轮的存储目录传给 `list_on_b` | 返回一个文件条目 `desktop.png` 和它的完整存储路径。调用 ID 为 `call_f6c8df4577be82376667b96768209d94`。 |
| 将列举所得路径传给 `lookup_on_b` | 找回与 Host A 上传回执完全一致的 `file_ref`。调用 ID 为 `call_5c2421bf7aaac1de172ba43e6f9f536c`。 |
| 将引用传给 `get_on_b` | 收到独立 Linux 目录中的 `inbox/host-b-v03j03s9m.png`。调用 ID 为 `call_5aa8732c69dcb2e48419b6fff2f71969`。 |

在 B 的本地文件系统上独立计算得到 29,904 字节及 SHA-256 `4164685bc29e7fd368edad329e0bb8b6a29732e0f9239d74b3f12c43484a6db9`，与第一轮 A 的原件相符。

## 非图片文件存取

运行编号为 `formats-mwthef72`。Windows Host A 通过同一 workbench 的 File Store 将六个样本上传到服务端 AList，按完整路径查回引用，再下载到独立目录。每份下载文件均与本地源文件逐字节相同，上传与下载回执的大小和 SHA-256 均与源文件一致。

| 样本 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `中文 说明.txt` | 58 | `442e24e21f2796fa3032c0cb47a0e366c11d8da830a00a4406e8a80a1aa11232` |
| `document.pdf` | 613 | `10fb68171a8c64e8b4ad27daad2454d642811203fdec15df4c2b1fdda7dc745a` |
| `empty.dat` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `random.bin` | 2097169 | `9e841d521497270f25750cd3d0bde012965bc3851cf7e5b1dc70e77d0fd2d201` |
| `sample.zip` | 599 | `01119a39c1b91d778f1bb8fef6f93112afbc27365576b1f1969ccbdb55c0e818` |
| `tone.wav` | 4044 | `c92748e4cd6e369b78a87da89bf2aeb05dd0edd599f0f707ed80bcad3315a98f` |

存储目录按每页两个条目列举，三页共找回全部六个文件。下载后的中文文本按 UTF-8 解码，单页 PDF 可渲染，ZIP 的两个条目均通过 CRC 与内容检查，WAV 可按 8 kHz 单声道 PCM 读取。
