# 增加节点部署位置

## 目的

支持新增节点时选择本机部署，让 Hysteria2 节点与管理面板运行在同一台机器上。

## 范围

- 表：`server_nodes`
- 新增列：`deploy_mode ENUM('ssh', 'local') NOT NULL DEFAULT 'ssh'`

## 执行顺序

1. 执行 `database/migrations/2026-07-02_add_server_nodes_deploy_mode.sql`
2. 部署新版面板
3. 新增节点时选择 `本机` 部署并验证安装流程

## 回滚

如需回滚，应先确认没有 `deploy_mode = 'local'` 的节点，再执行：

```sql
ALTER TABLE server_nodes DROP COLUMN deploy_mode;
```

## 验证

- 旧节点默认 `deploy_mode = 'ssh'`
- 本机节点安装时不需要 SSH 凭据
- 本机节点的安装、配置下发、服务操作在面板所在机器执行

## 风险

本机部署会在面板所在机器执行系统级安装和 systemd 操作，面板进程需要具备相应权限，或配置可用的本机 sudo 提权能力。
