# Disable obfuscation for selected nodes

Set `HYSTERIA_OBFS_DISABLED_NODES=22` in the panel environment file and restart the panel. Multiple node IDs may be comma-separated. Unlisted nodes retain Salamander. This is an operator configuration; the node editor's password remains stored for rollback and existing auth/stats dependencies.

The panel omits the obfs block from newly generated server configurations and both URI/Clash subscriptions for these nodes. Apply the server configuration and refresh client subscriptions together. QUIC/TLS encryption remains enabled. Old subscriptions still contain obfs and cannot connect until refreshed.

No database/schema changes are required. Back up the panel environment, panel binary and node configuration before deploying. Rollback: remove the node ID from the environment setting, restart the panel, reapply server configuration and refresh subscriptions. Keep the original password to preserve auth signatures and traffic stats secrets.

Validate that server configuration and new subscriptions omit obfs, TLS/auth/stats remain intact, the service starts and clients can connect. Networks blocking ordinary QUIC may require obfuscation to remain enabled; CPU savings require measurement at equal throughput.
