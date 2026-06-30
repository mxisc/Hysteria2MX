export type ServiceStatus = 'running' | 'degraded' | 'stopped'
export type UserStatus = 'active' | 'suspended'
export type ServiceAction = 'start' | 'stop' | 'restart' | 'status' | 'upgrade-agent'
export type TlsMode = 'acme' | 'self_signed'
export type AdminRole = 'super_admin' | 'operator' | 'auditor' | 'viewer'
export type AdminStatus = 'active' | 'disabled'
export type NodeManageMode = 'agent' | 'ssh'
export type AgentStatus = 'pending' | 'online' | 'offline' | 'error'
export type Permission =
  | '*'
  | 'panel.view'
  | 'node.view'
  | 'node.manage'
  | 'user.view'
  | 'user.manage'
  | 'config.view'
  | 'config.manage'
  | 'service.view'
  | 'service.manage'
  | 'logs.view'
  | 'traffic.sync'
  | 'audit.view'
  | 'appearance.manage'
  | 'notification.manage'
  | 'system.upgrade'
  | 'admin.manage'
  | 'job.view'

export interface RemoteCommandResult {
  command: string
  output: string
  exitCode: number
}

export interface CommandJob {
  id: string
  action: 'install' | 'uninstall'
  status: 'pending' | 'running' | 'done' | 'error'
  message: string
  result: RemoteCommandResult | null
  created_at: string
  updated_at: string
}

export interface PanelMetrics {
  nodeCount: number
  userCount: number
  activeUserCount: number
  quotaTotalGb: number
  quotaUsedGb: number
}

export interface PaginationMeta {
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export interface PaginatedResult<T> {
  items: T[]
  pagination: PaginationMeta
}

export interface NodeAgentInfo {
  agent_id: string
  status: AgentStatus
  version: string
  report_interval_seconds: number
  task_poll_interval_seconds: number
  installed_at: string | null
  last_seen_at: string | null
  last_ip: string | null
  last_error: string | null
  last_service_status: string
  last_service_message: string
  last_total_rx: number
  last_total_tx: number
  last_user_count: number
  last_online_count: number
}

export interface HysteriaNodeConfig {
  id: number
  current_node: number
  name: string
  host: string
  ssh_port: number
  ssh_username: string
  ssh_auth_type: 'password' | 'key'
  ssh_password: string | null
  ssh_private_key_path: string | null
  ssh_private_key_uploaded?: boolean
  ssh_private_key_name?: string | null
  ssh_public_key_name?: string | null
  sudo_password: string | null
  install_script: string
  service_name: string
  config_path: string
  listen_port: number
  traffic_stats_listen: string
  traffic_stats_secret: string
  tls_mode: TlsMode
  tls_cert_path: string | null
  tls_key_path: string | null
  domain: string
  acme_email: string
  obfs_password: string
  masquerade_url: string
  bandwidth_up_mbps: number
  bandwidth_down_mbps: number
  manage_mode: NodeManageMode
  agent_enabled: boolean
  agent_report_interval_seconds: number
  agent_task_poll_interval_seconds: number
  agent_install_path: string
  agent_config_path: string
  agent_service_name: string
  agent?: NodeAgentInfo | null
  created_at: string
  updated_at: string
}

export interface HysteriaNodePayload {
  name: string
  host: string
  ssh_port: number
  ssh_username: string
  ssh_auth_type: 'password' | 'key'
  ssh_password: string
  ssh_private_key_path: string
  ssh_private_key_token?: string
  ssh_private_key_uploaded?: boolean
  ssh_private_key_name?: string
  ssh_public_key_name?: string
  sudo_password: string
  install_script: string
  service_name: string
  config_path: string
  listen_port: number
  traffic_stats_listen: string
  traffic_stats_secret: string
  tls_mode: TlsMode
  tls_cert_path: string
  tls_key_path: string
  domain: string
  acme_email: string
  obfs_password: string
  masquerade_url: string
  bandwidth_up_mbps: number
  bandwidth_down_mbps: number
  manage_mode: NodeManageMode
  agent_enabled: boolean
  agent_report_interval_seconds: number
  agent_task_poll_interval_seconds: number
  agent_install_path: string
  agent_config_path: string
  agent_service_name: string
}

export interface SshKeyUploadResult {
  token: string
  private_key_name: string
  public_key_name: string
}

export interface HysteriaUser {
  id: number
  public_id: string
  node_id: number
  node_name?: string
  username: string
  auth_password: string
  status: UserStatus
  quota_gb: number
  used_gb: number
  speed_limit_mbps: number
  expires_at: string | null
  created_at: string
  updated_at: string
}

export interface HysteriaUserPayload {
  node_id: number
  username: string
  auth_password: string
  status: UserStatus
  quota_gb: number
  used_gb: number
  speed_limit_mbps: number
  expires_at: string | null
}

export type LogicalUserStatus = 'normal' | 'partial_abnormal'

export interface LogicalUserNode {
  id: number
  name: string
}

export interface HysteriaLogicalUser {
  username: string
  status: LogicalUserStatus
  node_count: number
  quota_gb: number
  used_gb: number
  abnormal_count: number
  nodes: LogicalUserNode[]
  details: HysteriaUser[]
}

export interface HysteriaPanelState {
  node: HysteriaNodeConfig | null
  service: RemoteCommandResult
  metrics: PanelMetrics
}

export interface AuditLogItem {
  id: number
  admin_id: number | null
  admin_username: string | null
  admin_display_name: string | null
  action: string
  target_type: string
  target_id: string | null
  ip_address: string | null
  details: Record<string, unknown>
  created_at: string
}

export interface HysteriaOnlineClient {
  id: string
  connections: number
}

export interface UserTrafficStats {
  node_id: number
  username: string
  rx: number
  tx: number
  rx_human: string
  tx_human: string
  total_human: string
}

export interface NodeTrafficHistoryItem {
  recorded_at: string
  total_rx: number
  total_tx: number
}

export interface TrafficNodeSummary {
  id: number
  name: string
  host: string
  onlineCount: number
  userCount: number
  totalRx: number
  totalTx: number
  recordedAt: string
}

export interface TrafficOverviewResponse extends PaginatedResult<TrafficNodeSummary> {
  series: NodeTrafficHistoryItem[]
}

export interface HysteriaStreamItem {
  state?: string
  auth?: string
  connection?: number
  stream?: number
  req_addr?: string
  hooked_req_addr?: string
  tx?: number
  rx?: number
  initial_at?: string
  last_active_at?: string
}

export interface AuthSession {
  id: number
  username: string
  displayName: string
  role: AdminRole
  status: AdminStatus
  permissions: Permission[]
  token: string
  expiresAt: number
}

export interface AdminUser {
  id: number
  username: string
  display_name: string
  role: AdminRole
  status: AdminStatus
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export interface AdminUserPayload {
  username: string
  display_name: string
  role: AdminRole
  status: AdminStatus
  password?: string
}

export interface LoginBackgroundConfig {
  customUrl: string
}

export interface SetupStatus {
  configured: boolean
  databaseReady: boolean
  requiresSetup: boolean
  message: string
  setupMode?: 'fresh' | 'ready'
}

export interface SetupPayload {
  host: string
  port: number
  database: string
  username: string
  password: string
  charset?: string
  public_api_base_url?: string
  admin_username?: string
  admin_password?: string
}

export interface SystemSettings {
  site_title: string
  public_api_base_url: string
  site_icon_url: string
  login_background_url: string
  mock_panel_enabled: boolean
  mock_node_count: number
  mock_user_count: number
  mock_running_node_count: number
  mock_degraded_node_count: number
  mock_stopped_node_count: number
  mock_suspended_user_count: number
  bruteforce_enabled: boolean
  bruteforce_max_attempts: number
  bruteforce_window_minutes: number
  bruteforce_lock_minutes: number
}

export interface PublicAppSettings {
  site_title: string
  site_icon_url: string
  login_background_url: string
}

export interface UserSubscriptionInfo {
  url: string
  username: string
  node_count: number
  nodes: Array<{
    id: number
    name: string
  }>
}

export interface SystemHealthIssue {
  level: 'error' | 'warning' | 'info'
  title: string
  message: string
}

export interface SystemHealth {
  ok: boolean
  checked_at: string
  issues: SystemHealthIssue[]
}

export interface NotificationSettings {
  smtp_enabled: boolean
  smtp_host: string
  smtp_port: number
  smtp_encryption: 'none' | 'ssl' | 'tls'
  smtp_username: string
  smtp_password: string
  smtp_password_configured: boolean
  smtp_from_email: string
  smtp_from_name: string
  smtp_notify_email: string
}
