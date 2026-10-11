# Brutal 丢包补偿开关

新增节点编辑开关，控制服务端 bandwidth.disableLossCompensation，仅影响服务端发送的 Brutal 流量。默认补偿开启，保留现有行为。保存沿用现有配置下发和服务重启流程，客户端无需改动。

## 数据变更
server_nodes.disable_loss_compensation TINYINT(1) NOT NULL DEFAULT 0。先备份配置及数据库结构，执行幂等迁移，再部署面板和静态资源。重复执行不会重复添加列。新安装 schema 包含字段。

## 验证与回滚
验证默认 false、保存 true/false、旧请求省略字段保留已有值，以及配置布尔值正确。关闭补偿可能降低丢包链路速度。回滚先还原旧面板和静态资源；新字段可保留，若删除先备份字段数据。已下发的节点配置需要重新下发原配置才能恢复，不自动改变现有节点。
