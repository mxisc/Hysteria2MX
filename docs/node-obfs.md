# Node obfuscation

Use the independent 流量混淆 switch in the node editor. Turning it off removes obfs from generated server configurations and URI/Clash subscriptions while retaining QUIC/TLS encryption. Save deploys the node configuration; clients must refresh subscriptions together. Old clients configured with obfs cannot connect to a server with obfs disabled.

The password remains stored for rollback and existing auth/stats dependencies. New nodes with obfs off do not require a password. The earlier HYSTERIA_OBFS_DISABLED_NODES operator override is automatically migrated once to the node setting and removed from the panel environment. See node-monitor-data-change.md for migration, validation and rollback details.

Networks blocking ordinary QUIC may require obfuscation. CPU savings must be measured at equal throughput.
