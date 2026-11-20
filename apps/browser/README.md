# Browser oiapp

Browser 的 18 个 Action 共用一个 stdio worker。每个会话启动带沙箱的独立无头 Chromium，使用专用 profile。解压对应平台的发行包，将其中的 `app.yaml` 交给 OIC 加载。程序目录可以只读，状态和 profile 放在服务账户能读写的私有本机目录。

## 构建与验证

原生构建基线为 Node.js 24.13.0 x64，目标系统为 Windows 11 x64 和 Ubuntu 24.04 x64。在 Linux 上使用非 root 账户、可用的 Chromium 沙箱和系统共享库。主机管理员可用 `npx playwright-core install-deps chromium` 安装所需系统库。

```sh
npm ci
npm run install-browser
npm test
npm run package
```

安装命令将 Playwright 锁定的 Chromium 下载到本 App 的 `browsers/`。加载 App 时不下载浏览器，创建会话时启动包内程序。打包脚本针对当前原生系统生成 `dist/browser-windows-x64.zip` 或 `dist/browser-linux-x64.tar.gz`。包内包含 Node、`worker/main.cjs`、`worker/node_modules`、Chromium、契约、许可证和文件校验和。脚本实际启动浏览器核对版本，将构建输入 SHA-256 和可取得的源码 commit 写入 `release-lock.json`。在脱离 Git 的构建目录，可通过 `OI_SOURCE_COMMIT` 传入原检出版本。Node 许可证来自安装目录或随源码提供的对应版本，`OI_NODE_LICENSE` 可显式指定。

解压后，可在源码目录把 `OI_BROWSER_TEST_ROOT` 设为包含 `app.yaml` 的解压目录，再运行 `npm test`。同一组测试将调用归档内的 Node 和 worker。Linux 清单启动 `./runtime/node ./worker/main.cjs`，Windows 清单启动 `./runtime/node.exe ./worker/main.cjs`。

## 配置与调用

在 OIC 中至少配置 `app_config.browser.state_dir`，再在 OIS 绑定需要开放的 Action。启动前创建状态目录。Linux 要求当前账户拥有该目录，权限设为 0700。Windows 要求私有 DACL，目录所有者与允许访问的主体限定为当前服务账户、SYSTEM 或本机 Administrators。持久 profile 使用独立目录和别名，名单须同时匹配可信的 `access_identity` 和 `vmcp_id`。使用同一 profile 新建会话可复用网站登录态。[配置及 18 项输入输出契约](contract/schemas.json)

`screenshot` 使用 `content.v1` 交付一帧可见视口图像。显式 CSS 像素 `clip` 返回原像素无损 PNG。网页 confirm 等原生弹窗可用 `handle_dialog` 处理，再用 `list_tabs` 查询原操作的终态。网页文件上传下载、整页截图和系统桌面不在此 App 的接口范围内。
