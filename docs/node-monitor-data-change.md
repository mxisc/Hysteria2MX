# Node monitoring and obfuscation switch

## Scope and behavior

Node edit now has an independent obfuscation switch. Turning it off preserves TLS, credentials, traffic-stat secrets and auth signatures, and removes obfs from generated server configurations and subscriptions. New nodes may be created with the switch off and no obfs password; an internal random fallback secret is generated. Existing empty-password edits retain the stored secret.

Agent 0.1.4 collects whole-machine CPU, available-memory-based usage, swap, root disk, load, uptime and all non-loopback network counters every 5 seconds. Collection and upload run independently from heartbeat/tasks, with a bounded latest-sample queue and a 4-second upload timeout. Network failure drops samples instead of growing memory or delaying commands. First samples and counter resets have null rates. Counters restart after host reboot; these are system counters, not Hysteria user traffic.

The monitoring page polls asynchronously every 5 seconds, skips overlapping requests and hidden pages, cancels obsolete node/range requests, and releases charts/timers on leaving. Offline gaps remain gaps, and old values are marked stale after 20 seconds. One-hour history retains 5-second points; longer windows aggregate averages and CPU peaks.

## Database and execution order

1. Back up the panel binary, environment, schema and node settings; preserve node config and Agent binary for rollback.
2. Deploy the idempotent migration `2026-10-11_add_node_monitor_and_obfs.sql`: add `server_nodes.obfs_enabled` (default 1) if absent and create `node_system_metrics` if absent. Unique `(node_id,sampled_at_ms)` prevents duplicate samples; a timestamp index supports retention, and the node FK supports removal.
3. Restart the panel. Legacy `HYSTERIA_OBFS_DISABLED_NODES` IDs are migrated to `obfs_enabled=0`; the old override is then removed from the environment. Stored passwords remain untouched. This ensures the editor can later re-enable obfs without an environment override defeating it.
4. Deploy Agent 0.1.4 and restart only the Agent. Monitoring does not require restarting Hysteria. Refresh browser assets. Existing obfs-off node configurations remain off.

History is retained for 7 days; background hourly cleanup removes up to 50,000 expired rows per batch. Reads return at most 1,500 aggregated points. At 5 seconds this is about 120,960 rows per node per week; storage and retention deletion should be monitored when adding many nodes. No historical CPU data can be reconstructed from before deployment.

## Validation and rollback

Run Go tests and frontend build. Verify the migration twice without duplicates, legacy node 22 remains off, signed Agent uploads arrive 5 seconds apart, anonymous monitor reads fail, and UI switching/refresh/chart gaps work. Verify TLS and both subscription formats with the obfs switch off. Compare CPU at equal throughput before attributing performance improvements.

Rollback: stop/revert the new Agent and panel binary and restore the backed-up environment; additive schema may remain. If restoring the old panel while a node should stay off, retain the legacy environment override. Monitor table can be exported and removed later only with explicit authorization. Never roll back passwords or unrelated CPU protection settings blindly.
