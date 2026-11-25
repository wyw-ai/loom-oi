# Loom OI 管理 App

在能访问 OIS 管理地址的 OIC 加载此 App。Windows 使用 `./build.ps1`，Linux 使用 `sh ./build.sh`，再执行 `oic --config <oic.yaml> load dist/loom-oi-<平台>/app.yaml`。构建需要 Go 1.26；运行包自带 worker。

```yaml
# 管理主机的 oic.yaml 节选
app_config:
  loom-oi:
    base_url: http://127.0.0.1:25220
    admin_token_file: /run/secrets/ois-admin
    callers:
      - service: oi-catalog
        identity: consumer:oi-catalog:CATALOG_TOKEN
        visible_services: [oi-creator, workbench]
      - service: oi-creator
        identity: consumer:oi-creator:CREATOR_TOKEN
        hosts: [host-a]
        sources:
          - {channel: host-a, app: computer-use, action: screenshot}
          - {channel: host-a, app: filesystem, action: view_image}
          - {channel: host-a, app: skills, action: list_skills}
          - {channel: host-a, app: skills, action: read_skill}
        managed_services: [workbench]
```

`base_url` 固定为 OIS 的 HTTP(S) origin。`admin_token_file` 是该 worker 所在主机的绝对路径，内容为 OIS 管理令牌。OIC 的通道凭据、目录/创造者/业务 Service 的访问凭据分别配置。Service 和消费身份取自 OIS 可信上下文。部署方通过 OIC 的 `load/unload` 管理 App，修改启动 YAML 后重启 OIC。

`hosts` 决定可盘点的主机，`sources` 明确可列举和发布的固定来源。`visible_services` 授予查看权，`managed_services` 同时授予查看、创建、替换定义与访问规则、删除的权限。各列表是精确 ID。将业务 Service 纳入管理范围前，应同时列入更新后要保留的每一个成员来源。示例只给出四个来源，若维护已有六成员的示例 workbench，还要把其 file-store 来源加入授权清单。

变更 Service 时，`access.credential_env` 为消费凭据的环境变量名。提交空的 `access: {}` 会将该 Service 设为公开，OIS 对所有调用者使用 `anonymous:<service>` 身份。管理 App 只匹配 `callers` 明确授权的 Service 和身份。部署示例中的管理和目录身份均要求凭据。

| Action | 入参和结果 |
| --- | --- |
| list_hosts | `{}` 返回已登记可见 Host 的 id、name、os、arch、online。 |
| list_apps | `{host_id}` 返回该 Host 已加载 App 的 ready、available、stale 等目录状态。 |
| list_actions | `{host_id?,app?}` 返回允许发布的 Action、输入契约、可用性与可直接复用的 source。 |
| list_services | `{}` 返回 `items`，每项含 id、publish、成员摘要（name、source），发布 MCP 时含 endpoint。 |
| get_service | `{id}` 返回用户可见的完整 Service 定义。 |
| apply_service | `{id, service:{actions,access,publish}}` 完整创建或替换并返回 `{created,service}`。先用 get_service 读回并保留所需成员和访问规则。 |
| delete_service | `{id}` 删除已授权的 Service，返回被删 id。 |

部署时在 OIS 为管理 OIC 配置固定 channel。用预设目录 Service 发布 `loom-oi.list_services`，用创造者 Service 发布管理 Action，然后将两个入口分别授权给目录同步器与创造者。调用业务 Service 时仍需要该 Service 的消费身份。
