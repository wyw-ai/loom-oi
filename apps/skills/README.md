# Skills

在存有 Skill 的主机运行此 OIApp。Windows 使用 `./build.ps1`，Linux 使用 `sh ./build.sh`，从对应的 `dist/skills-<平台>/app.yaml` 执行 `oic --config <oic.yaml> load <app.yaml>`。构建需要 Go 1.26，包内 worker 在目标机独立运行。

```yaml
# oic.yaml 节选，路径使用目标机的绝对路径
app_config:
  skills:
    workspace: /srv/project
    home: /home/worker
    allowed_callers:
      - service: workbench
        identity: consumer:workbench:WORKBENCH_TOKEN
```

`workspace` 指定目标工作区；省略 `home` 使用 worker 所属账户的主目录。每个位置默认注册 `.agents/skills`（shared）、`.codex/skills`（codex），稳定 ID 为 `workspace-shared`、`workspace-codex`、`home-shared`、`home-codex`。缺失目录跳过。若设置 `roots`，它完整替换默认清单。示例如下，路径按对应范围的基目录解析。

```yaml
roots:
  - {root_id: project-extra, scope: workspace, product: extra, path: .extra/skills}
```

每个准入条目核对 OIS 提供的 Service 和消费身份。可选的 `root_ids` 将这个调用者限制为指定根。省略时允许读取所有已注册根，空数组表示没有可读根。

注册根的每一级目录以及 Skill 包目录须为实际目录。包内指向同一包中普通文本文件的相对链接可以读取。公开 Service 的调用身份为 `anonymous:<service>`，须在 `allowed_callers` 中单独获授才能读取。

- `list_skills({product?,limit?,cursor?})` 返回 `items`，每项含名称、简介、scope、product、root_id 和 ref。默认每页 50，最多 200。游标绑定到 worker、调用身份和 product，worker 重启后从首页重新列举。
- `read_skill({ref})` 返回 `text`（含 frontmatter 的完整 SKILL.md）、`resource` 和可按需读取的 `resources` 路径列表。指定 `resource` 读取单份 UTF-8 文本材料。读取每次重新检查包、实际文件和调用权限。跨 Service 保存引用时同时保存工具的固定来源。
- 保留 OIC 的单帧上限。无法装入结果帧的内容返回 `RESULT_TOO_LARGE`。App 不执行或写入包内文件。
