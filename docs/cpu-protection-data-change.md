# CPU保护配置数据变更

目的：由面板统一保存并下发每个节点的CPU保护配置，不添加表或列。

范围：既有`system_settings(setting_key, setting_value)`。键为`node_cpu_protection_<节点ID>`，值为包含enabled、high_percent、low_percent、max_quota_percent、min_quota_percent的JSON。既有`node_agents.last_payload_json`保存Agent实际配置回报，不添加新列。

执行顺序：先部署包含新接口/心跳协议的面板及Agent构建产物，再升级Agent；从节点CPU保护对话框保存配置。配置通过INSERT ... ON DUPLICATE KEY UPDATE写入，重复保存同一节点不会产生重复行。缺失配置不会强制覆盖旧节点的本地设置。

回滚：先在面板关闭保护并等待Agent确认，验证Hysteria原CPU配额恢复；再恢复旧二进制和静态资源。配置键可保留供再次升级使用；如果需要撤销数据，先导出对应键值，再通过受控数据库操作移除该键。不得在保护仍启用时直接删除Agent，以免运行时配额遗留。

验证：无节点管理权限无法保存；无效阈值拒绝写入；保存后GET返回期望配置，后续心跳实际回报匹配且Agent在线后applied为true；关闭配置验证原配额恢复；Agent重启后验证配置持久化。

风险：配置保存成功不等于Agent在线或已应用，界面必须保留等待状态。已有更严格CPU配额不可被放宽。CPU配额不能保证系统其他服务或软中断负载始终低于阈值。该配置不含密钥。
