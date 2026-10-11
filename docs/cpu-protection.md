# 节点 CPU 动态保护

在 Agent JSON 配置中加入以下字段，然后重启 Agent。功能默认关闭，已有节点升级后不会自动改变资源限制。

```json
"cpu_protection": {
  "enabled": true,
  "high_percent": 90,
  "low_percent": 70,
  "max_quota_percent": 80,
  "min_quota_percent": 20
}
```

要求 Linux、systemd，Agent 有设置服务运行时资源属性的权限。无需新依赖、数据库变更或重启 Hysteria。

每 5 秒读取 `/proc/stat` 的整机计数，计入用户、内核及软中断处理；不重复计入 guest，不把 iowait 和虚拟机 steal 时间视为本机执行。4 核各使用 30% 时整机利用率为 30%。

连续 3 次超过 90% 后，将 Hysteria 的 CPU 配额降为整机算力的 80%；仍持续超限时，每 15 秒乘以 0.8，最低为整机算力的 20%。连续 12 次低于 70% 后，每分钟恢复 5 个百分点，达到恢复上限后回到启用前的配额。区间内不恢复，避免短时波动反复调整。启用前已经存在的更严格配额不会被放宽。

CPU 配额以运行时 systemd 属性实现，减少服务可执行的处理量，转发吞吐会随之下降。这不是固定 Mbps 限速，也不是提高每字节处理效率；加密、混淆及内核网络处理仍有成本。其他服务占满 CPU、内核软中断过高或流量攻击时，不能保证整机始终低于阈值。配额降低也可能增加延迟或重传，必须对真实负载进行验证。

原配额保存在 `/run/mxinhy-agent/cpu-protection.json`，Agent 重启不会遗忘原上限；主机重启清除运行时限制。将 `enabled` 改为 `false` 并重启 Agent，会恢复启用前配额。运行期间不要同时由其他程序调整同一服务的 CPUQuota；卸载 Agent 前应先关闭保护并验证恢复。采样失败不会解除已有保护，调整失败会保留当前状态并重试。日志仅记录调整及失败，不按采样持续刷日志。

验证：查看 Agent 日志中的 `CPU protection`，以及 `systemctl show hysteria-server -p CPUQuotaPerSecUSec` 和服务 cgroup 的 `cpu.max`。同时观察整机 CPU、有效下载吞吐与延迟。不得通过长时间满载验证来触发供应商封停。
