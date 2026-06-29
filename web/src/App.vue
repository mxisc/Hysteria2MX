<script setup lang="ts">
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { init, use } from 'echarts/core'
import type { ECharts } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { fetchCurrentSession, getStoredSession, login, logout } from './services/authService'
import {
  createAdmin,
  fetchAppSettings,
  fetchAuditLogs,
  fetchAdmins,
  fetchNotificationSettings,
  fetchNodes,
  fetchOnlineClientPage,
  createNodeWithInstall,
  createUser,
  deleteAdmin,
  deleteNode,
  deleteUser,
  deployHysteriaConfig,
  fetchSystemSettings,
  fetchSystemHealth,
  fetchLogs,
  fetchPanelState,
  fetchStreamPage,
  fetchUserSubscriptionInfo,
  fetchUsers,
  fetchUserTrafficStats,
  fetchTrafficHistory,
  performServiceAction,
  saveNodeConfig,
  syncTrafficUsage,
  sendTestNotification,
  uninstallHysteria,
  uploadNodeSshKey,
  updateNotificationSettings,
  updateSystemSettings,
  updateAdmin,
  updateUser,
} from './services/hysteriaService'
import { applyMockPanelConfig, getMockNodeRuntimeStatuses, isMockPanelEnabled } from './services/mockPanelData'
import { fetchSetupStatus, initializeSetup } from './services/setupService'
import { checkVersion, upgradeVersion } from './services/versionService'
import {
  buildLoginBackgroundUrl,
  DEFAULT_LOGIN_BACKGROUND_CONFIG,
} from './services/loginBackgroundService'
import type {
  AdminRole,
  AdminStatus,
  AdminUser,
  AdminUserPayload,
  AuditLogItem,
  AuthSession,
  HysteriaNodeConfig,
  HysteriaNodePayload,
  HysteriaPanelState,
  HysteriaUser,
  HysteriaUserPayload,
  LoginBackgroundConfig,
  NotificationSettings,
  NodeTrafficHistoryItem,
  PaginationMeta,
  Permission,
  PublicAppSettings,
  RemoteCommandResult,
  ServiceStatus,
  SshKeyUploadResult,
  SetupPayload,
  SetupStatus,
  SystemHealth,
  SystemSettings,
  TrafficNodeSummary,
  UserSubscriptionInfo,
  UserTrafficStats,
} from './types'

use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const sections = [
  { id: 'overview', label: '总览' },
  { id: 'traffic', label: '流量统计' },
  { id: 'nodes', label: '节点配置' },
  { id: 'users', label: '用户管理' },
  { id: 'logsAudit', label: '日志审计' },
  { id: 'appearance', label: '综合配置' },
  { id: 'notifications', label: '邮件通知' },
  { id: 'permissions', label: '权限控制' },
] as const

const listPageSizeOptions = [10, 20, 50, 100] as const
const trafficPageSizeOptions = [6, 12, 24, 48] as const

type SectionId = (typeof sections)[number]['id']

type UserEditorForm = {
  username: string
  auth_password: string
  status: HysteriaUser['status']
  quota_gb: number
  used_gb: number
  speed_limit_mbps: number
  expires_at: string
}

type AdminEditorForm = {
  username: string
  display_name: string
  role: AdminRole
  status: AdminStatus
  password: string
}

type SetupEditorForm = SetupPayload

type ToastType = 'success' | 'error' | 'info'

const NODE_STATUS_SYNC_INTERVAL_MS = 60 * 1000

function createEmptyPanelState(): HysteriaPanelState {
  return {
    currentNodeId: null,
    node: null,
    service: {
      command: '',
      output: '未配置节点',
      exitCode: 1,
    },
    metrics: {
      nodeCount: 0,
      userCount: 0,
      activeUserCount: 0,
      quotaTotalGb: 0,
      quotaUsedGb: 0,
    },
  }
}

function createPaginationMeta(pageSize = 10): PaginationMeta {
  return {
    page: 1,
    pageSize,
    total: 0,
    totalPages: 1,
  }
}

function createDefaultObfsPassword(): string {
  return `hy2-${Math.random().toString(36).slice(2, 10)}`
}

function createEmptyNodeForm(): HysteriaNodePayload {
  return {
    name: 'default-node',
    host: '',
    ssh_port: 22,
    ssh_username: 'root',
    ssh_auth_type: 'password',
    ssh_password: '',
    ssh_private_key_path: '',
    ssh_private_key_token: '',
    ssh_private_key_uploaded: false,
    ssh_private_key_name: '',
    ssh_public_key_name: '',
    sudo_password: '',
    install_script: 'bash <(curl -fsSL https://get.hy2.sh/)',
    service_name: 'hysteria-server',
    config_path: '/etc/hysteria/config.yaml',
    listen_port: 443,
    traffic_stats_listen: '127.0.0.1:9999',
    traffic_stats_secret: '',
    tls_mode: 'self_signed',
    tls_cert_path: '/etc/hysteria/server.crt',
    tls_key_path: '/etc/hysteria/server.key',
    domain: '',
    acme_email: '',
    obfs_password: createDefaultObfsPassword(),
    masquerade_url: 'https://www.bing.com',
    bandwidth_up_mbps: 200,
    bandwidth_down_mbps: 200,
    manage_mode: 'agent',
    agent_enabled: true,
    agent_report_interval_seconds: 5,
    agent_task_poll_interval_seconds: 1,
    agent_install_path: '/usr/local/bin/mxinhy-agent',
    agent_config_path: '/etc/mxinhy-agent.json',
    agent_service_name: 'mxinhy-agent',
  }
}

function mapNodeToForm(node: HysteriaNodeConfig): HysteriaNodePayload {
  return {
    name: node.name,
    host: node.host,
    ssh_port: Number(node.ssh_port),
    ssh_username: node.ssh_username,
    ssh_auth_type: node.ssh_auth_type,
    ssh_password: node.ssh_password ?? '',
    ssh_private_key_path: '',
    ssh_private_key_token: '',
    ssh_private_key_uploaded: Boolean(node.ssh_private_key_uploaded),
    ssh_private_key_name: node.ssh_private_key_name ?? '',
    ssh_public_key_name: node.ssh_public_key_name ?? '',
    sudo_password: node.sudo_password ?? '',
    install_script: node.install_script,
    service_name: node.service_name,
    config_path: node.config_path,
    listen_port: Number(node.listen_port),
    traffic_stats_listen: node.traffic_stats_listen || '127.0.0.1:9999',
    traffic_stats_secret: node.traffic_stats_secret ?? '',
    tls_mode: node.tls_mode,
    tls_cert_path: node.tls_cert_path ?? '/etc/hysteria/server.crt',
    tls_key_path: node.tls_key_path ?? '/etc/hysteria/server.key',
    domain: node.domain,
    acme_email: node.acme_email,
    obfs_password: node.obfs_password,
    masquerade_url: node.masquerade_url,
    bandwidth_up_mbps: Number(node.bandwidth_up_mbps),
    bandwidth_down_mbps: Number(node.bandwidth_down_mbps),
    manage_mode: node.manage_mode,
    agent_enabled: Boolean(node.agent_enabled),
    agent_report_interval_seconds: Number(node.agent_report_interval_seconds),
    agent_task_poll_interval_seconds: Number(node.agent_task_poll_interval_seconds),
    agent_install_path: node.agent_install_path,
    agent_config_path: node.agent_config_path,
    agent_service_name: node.agent_service_name,
  }
}

function createEmptyUserForm(): UserEditorForm {
  return {
    username: '',
    auth_password: '',
    status: 'active',
    quota_gb: 300,
    used_gb: 0,
    speed_limit_mbps: 0,
    expires_at: '',
  }
}

function createEmptyAdminForm(): AdminEditorForm {
  return {
    username: '',
    display_name: '',
    role: 'viewer',
    status: 'active',
    password: '',
  }
}

function createEmptySetupForm(): SetupEditorForm {
  return {
    host: '127.0.0.1',
    port: 3306,
    database: 'hy2_panel',
    username: 'root',
    password: '',
    charset: 'utf8mb4',
    public_api_base_url: '',
    admin_username: 'admin',
    admin_password: '',
  }
}

function toDateTimeLocal(value: string | null): string {
  if (!value) return ''
  return value.replace(' ', 'T').slice(0, 16)
}

function normalizeDateTime(value: string): string | null {
  if (!value) return null
  const normalized = value.replace('T', ' ')
  return normalized.length === 16 ? `${normalized}:00` : normalized.slice(0, 19)
}

function sanitizeAdminUsername(value: string): string {
  return value.replace(/[^A-Za-z0-9]/g, '')
}

function assertAdminUsername(value: string): string {
  const normalized = sanitizeAdminUsername(value.trim())
  if (!normalized) {
    throw new Error('用户名不能为空')
  }
  if (!/^[A-Za-z0-9]+$/.test(normalized)) {
    throw new Error('用户名仅支持英文和数字')
  }
  return normalized
}

function handleLoginUsernameInput() {
  loginForm.value.username = sanitizeAdminUsername(loginForm.value.username)
}

function handleSetupAdminUsernameInput() {
  setupForm.value.admin_username = sanitizeAdminUsername(setupForm.value.admin_username ?? '')
}

function handleAdminUsernameInput() {
  adminForm.value.username = sanitizeAdminUsername(adminForm.value.username)
}

function isPasswordVisible(key: string): boolean {
  return Boolean(passwordVisibility.value[key])
}

function togglePasswordVisibility(key: string): void {
  passwordVisibility.value[key] = !isPasswordVisible(key)
}

function mapUserToForm(user: HysteriaUser): UserEditorForm {
  return {
    username: user.username,
    auth_password: user.auth_password,
    status: user.status,
    quota_gb: Number(user.quota_gb),
    used_gb: Number(user.used_gb),
    speed_limit_mbps: Number(user.speed_limit_mbps),
    expires_at: toDateTimeLocal(user.expires_at),
  }
}

function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '操作失败，请稍后重试'
}

function clearNodeKeyFileSelection() {
  nodePrivateKeyFile.value = null
  if (nodePrivateKeyInput.value) {
    nodePrivateKeyInput.value.value = ''
  }
}

function syncNodeKeyState(form: HysteriaNodePayload) {
  form.ssh_private_key_token = ''
  form.ssh_private_key_uploaded = Boolean(form.ssh_private_key_uploaded || form.ssh_private_key_name)
  form.ssh_private_key_name = form.ssh_private_key_name ?? ''
  form.ssh_public_key_name = form.ssh_public_key_name ?? ''
  clearNodeKeyFileSelection()
}

function handleNodePrivateKeyFileChange(event: Event) {
  const target = event.target as HTMLInputElement
  nodePrivateKeyFile.value = target.files?.[0] ?? null
  nodeForm.value.ssh_private_key_token = ''
}

function openNodePrivateKeyPicker() {
  nodePrivateKeyInput.value?.click()
}

function applyUploadedNodeKey(result: SshKeyUploadResult) {
  nodeForm.value.ssh_private_key_token = result.token
  nodeForm.value.ssh_private_key_uploaded = true
  nodeForm.value.ssh_private_key_name = result.private_key_name
  nodeForm.value.ssh_public_key_name = result.public_key_name
  nodeForm.value.ssh_password = ''
  clearNodeKeyFileSelection()
}

function hasPermission(permission: Permission): boolean {
  if (!session.value) return false
  return session.value.permissions.includes('*') || session.value.permissions.includes(permission)
}

function canAccessSection(sectionId: SectionId): boolean {
  switch (sectionId) {
    case 'overview':
      return hasPermission('panel.view')
    case 'traffic':
      return hasPermission('panel.view')
    case 'nodes':
      return hasPermission('node.view')
    case 'users':
      return hasPermission('user.view')
    case 'logsAudit':
      return hasPermission('logs.view') || hasPermission('audit.view')
    case 'appearance':
      return hasPermission('appearance.manage')
    case 'notifications':
      return hasPermission('notification.manage')
    case 'permissions':
      return hasPermission('admin.manage')
  }
}

function resolveNodeRuntimeStatus(nodeId: number): ServiceStatus | 'unknown' {
  return nodeRuntimeStatuses.value[nodeId] ?? (panel.value.currentNodeId === nodeId ? inferredServiceStatus.value : 'unknown')
}

function nodeRuntimeStatusLabel(nodeId: number): string {
  const status = resolveNodeRuntimeStatus(nodeId)
  return status === 'unknown' ? '检测中' : statusLabelMap[status]
}

function resolveAgentStatusClass(node: HysteriaNodeConfig): string {
  const status = node.agent?.status
  if (status === 'online') return 'running'
  if (status === 'error') return 'degraded'
  return 'suspended'
}

function resolveAgentStatusLabel(node: HysteriaNodeConfig): string {
  return node.agent ? agentStatusLabelMap[node.agent.status] : '未部署'
}

function resolveNodeManageLabel(node: HysteriaNodeConfig): string {
  return node.manage_mode === 'agent' ? 'Agent' : 'SSH'
}

function ensureSectionAccess() {
  const allowed = visibleSections.value
  if (!allowed.find((item) => item.id === currentSection.value)) {
    currentSection.value = allowed[0]?.id ?? 'overview'
  }
}

const panel = ref<HysteriaPanelState>(createEmptyPanelState())
const currentSection = ref<SectionId>('overview')
const session = ref<AuthSession | null>(null)
const appVersion = import.meta.env.VITE_APP_VERSION || 'dev'
const versionUpdateAvailable = ref(false)
const latestVersion = ref<string | null>(null)
const versionChecking = ref(false)
const versionUpgrading = ref(false)
const authChecked = ref(false)
const setupStatus = ref<SetupStatus | null>(null)
const setupSubmitting = ref(false)
const setupError = ref('')
const loginLoading = ref(false)
const loginError = ref('')
const authNotice = ref('')
const userFilter = ref('')
const userFilterKeyword = ref('')
const busyAction = ref('')
const logsLoaded = ref(false)
const logsText = ref('')
const nodes = ref<HysteriaNodeConfig[]>([])
const nodesLoaded = ref(false)
const nodePageSize = ref<(typeof listPageSizeOptions)[number]>(10)
const nodePage = ref(1)
const nodePagination = ref<PaginationMeta>(createPaginationMeta(nodePageSize.value))
const users = ref<HysteriaUser[]>([])
const usersLoaded = ref(false)
const userPageSize = ref<(typeof listPageSizeOptions)[number]>(10)
const userPage = ref(1)
const userPagination = ref<PaginationMeta>(createPaginationMeta(userPageSize.value))
const auditLogs = ref<AuditLogItem[]>([])
const auditLogsLoaded = ref(false)
const auditLogPageSize = ref<(typeof listPageSizeOptions)[number]>(10)
const auditLogPage = ref(1)
const auditLogPagination = ref<PaginationMeta>(createPaginationMeta(auditLogPageSize.value))
const systemHealth = ref<SystemHealth | null>(null)
const notificationSettingsLoaded = ref(false)
const onlineClientTotal = ref(0)
const streamTotal = ref(0)
const userTrafficStats = ref<UserTrafficStats[]>([])
const trafficHistory = ref<NodeTrafficHistoryItem[]>([])
const trafficNodeSummaries = ref<TrafficNodeSummary[]>([])
const trafficHistoryLoaded = ref(false)
const trafficPageSize = ref<(typeof trafficPageSizeOptions)[number]>(12)
const trafficPage = ref(1)
const trafficPagination = ref<PaginationMeta>(createPaginationMeta(trafficPageSize.value))
const trafficChartContainer = ref<HTMLDivElement | null>(null)
const logsOutputRef = ref<HTMLElement | null>(null)
const auditLogListRef = ref<HTMLElement | null>(null)
const admins = ref<AdminUser[]>([])
const adminsLoaded = ref(false)
const adminPageSize = ref<(typeof listPageSizeOptions)[number]>(10)
const adminPage = ref(1)
const adminPagination = ref<PaginationMeta>(createPaginationMeta(adminPageSize.value))
const nodeRuntimeStatuses = ref<Record<number, ServiceStatus | 'unknown'>>({})
const refreshingNodeStatuses = ref(false)
const operationOutput = ref<RemoteCommandResult | null>(null)
const nodeKeyUploading = ref(false)
const editingUserId = ref<number | null>(null)
const editingNodeId = ref<number | null>(null)
const editingAdminId = ref<number | null>(null)
const showNodeModal = ref(false)
const showUserModal = ref(false)
const showAdminModal = ref(false)
const nodeSaveLocked = ref(false)
const showUserDeleteModal = ref(false)
const showNodeDeleteModal = ref(false)
const showAdminDeleteModal = ref(false)
const showNodeAdvancedOptions = ref(false)
const showSubscriptionModal = ref(false)
const qrModalTitle = ref('')
const qrModalValue = ref('')
const subscriptionInfo = ref<UserSubscriptionInfo | null>(null)
const toastMessage = ref('')
const toastType = ref<ToastType>('success')
const selectedLogNodeId = ref<number | null>(null)
const pendingDeleteUser = ref<HysteriaUser | null>(null)
const pendingDeleteNode = ref<HysteriaNodeConfig | null>(null)
const pendingDeleteAdmin = ref<AdminUser | null>(null)
const auditLogRefreshMode = ref<'auto' | 'manual'>('auto')
let auditLogAutoRefreshTimer: number | null = null
let userFilterTimer: number | null = null

let toastTimer: number | null = null
let nodeStatusSyncTimer: number | null = null
let userTrafficStatsTimer: number | null = null
let trafficChart: ECharts | null = null

const loginForm = ref({
  username: '',
  password: '',
})
const passwordVisibility = ref<Record<string, boolean>>({
  login: false,
  setupDb: false,
  setupAdmin: false,
  nodeSsh: false,
  nodeSudo: false,
  nodeTrafficSecret: false,
  admin: false,
})
const setupForm = ref<SetupEditorForm>(createEmptySetupForm())
const systemSettingsForm = ref<SystemSettings>({
  site_title: 'Hysteria2 Panel',
  public_api_base_url: '',
  site_icon_url: '',
  login_background_url: '',
  mock_panel_enabled: false,
  mock_node_count: 6,
  mock_user_count: 32,
  mock_running_node_count: 4,
  mock_degraded_node_count: 1,
  mock_stopped_node_count: 1,
  mock_suspended_user_count: 4,
  bruteforce_enabled: true,
  bruteforce_max_attempts: 5,
  bruteforce_window_minutes: 15,
  bruteforce_lock_minutes: 15,
})
const notificationSettingsForm = ref<NotificationSettings>({
  smtp_enabled: false,
  smtp_host: '',
  smtp_port: 587,
  smtp_encryption: 'tls',
  smtp_username: '',
  smtp_password: '',
  smtp_password_configured: false,
  smtp_from_email: '',
  smtp_from_name: 'Hysteria2 Panel',
  smtp_notify_email: '',
})
const nodeForm = ref<HysteriaNodePayload>(createEmptyNodeForm())
const userForm = ref<UserEditorForm>(createEmptyUserForm())
const adminForm = ref<AdminEditorForm>(createEmptyAdminForm())
const loginBackgroundConfig = ref<LoginBackgroundConfig>(DEFAULT_LOGIN_BACKGROUND_CONFIG)
const backgroundForm = ref<LoginBackgroundConfig>({ ...DEFAULT_LOGIN_BACKGROUND_CONFIG })
const nodePrivateKeyFile = ref<File | null>(null)
const nodePrivateKeyInput = ref<HTMLInputElement | null>(null)

const statusLabelMap: Record<ServiceStatus, string> = {
  running: '运行中',
  degraded: '处理中',
  stopped: '已停止',
}

const userStatusLabelMap: Record<HysteriaUser['status'], string> = {
  active: '正常',
  suspended: '已停用',
}

const adminRoleLabelMap: Record<AdminRole, string> = {
  super_admin: '超级管理员',
  operator: '运维管理员',
  auditor: '审计员',
  viewer: '只读成员',
}

const adminStatusLabelMap: Record<AdminStatus, string> = {
  active: '启用',
  disabled: '停用',
}

const agentStatusLabelMap = {
  pending: '待注册',
  online: '在线',
  offline: '离线',
  error: '异常',
} as const

const nodeList = computed(() => nodes.value)

const visibleSections = computed(() => sections.filter((item) => canAccessSection(item.id)))
const insecureRemoteLogin = computed(() => {
  if (typeof window === 'undefined') return false
  const hostname = window.location.hostname
  return window.location.protocol !== 'https:' && !['localhost', '127.0.0.1', '::1'].includes(hostname)
})

const inferredServiceStatus = computed<ServiceStatus>(() => {
  if (busyAction.value.startsWith('service-') || busyAction.value === 'install') return 'degraded'
  if (!panel.value.node) return 'stopped'
  return panel.value.service.exitCode === 0 ? 'running' : 'stopped'
})

const filteredUsers = computed(() => users.value)

const filteredActiveUsers = computed(() => filteredUsers.value.filter((user) => user.status === 'active').length)
const filteredSuspendedUsers = computed(() => filteredUsers.value.filter((user) => user.status === 'suspended').length)
const filteredQuotaTotalGb = computed(() => filteredUsers.value.reduce((total, user) => total + Number(user.quota_gb || 0), 0))
const filteredQuotaUsedGb = computed(() => filteredUsers.value.reduce((total, user) => total + Number(user.used_gb || 0), 0))

const selectedLogNode = computed(() => nodeList.value.find((node) => node.id === selectedLogNodeId.value) ?? panel.value.node ?? null)
const selectedLogNodeUsesAgent = computed(() => {
  const node = selectedLogNode.value
  return Boolean(node && node.manage_mode === 'agent' && node.agent_enabled && node.agent)
})
const realtimeLogRefreshEnabled = computed(() => auditLogRefreshMode.value === 'auto' && selectedLogNodeUsesAgent.value)
const totalNodeCount = computed(() => panel.value.metrics.nodeCount)
const activeUserRatio = computed(() => `${panel.value.metrics.activeUserCount}/${panel.value.metrics.userCount}`)
const onlineUserRatio = computed(() => `${onlineClientTotal.value}/${panel.value.metrics.userCount}`)
const currentNodeStatusLabel = computed(() => statusLabelMap[inferredServiceStatus.value])
const appearanceSiteTitle = computed(() => systemSettingsForm.value.site_title.trim() || 'Hysteria2 Panel')
const appearanceApiBaseUrl = computed(() => systemSettingsForm.value.public_api_base_url.trim() || '未设置')
const appearanceMockSummary = computed(() => {
  if (!systemSettingsForm.value.mock_panel_enabled) return '已关闭'
  return `${systemSettingsForm.value.mock_node_count} 节点 / ${systemSettingsForm.value.mock_user_count} 用户`
})
const appearanceSecuritySummary = computed(() => {
  if (!systemSettingsForm.value.bruteforce_enabled) return '未启用'
  return `${systemSettingsForm.value.bruteforce_max_attempts} 次失败后锁定 ${systemSettingsForm.value.bruteforce_lock_minutes} 分钟`
})

const userTrafficStatsMap = computed(() => {
  const map: Record<string, UserTrafficStats> = {}
  for (const stat of userTrafficStats.value) {
    map[stat.username] = stat
  }
  return map
})

const filteredRealtimeUsers = computed(() => {
  return filteredUsers.value.filter((user) => Boolean(userTrafficStatsMap.value[user.username])).length
})

const aggregatedTrafficSeries = computed(() => {
  if (!trafficHistory.value.length) {
    return {
      timestamps: [] as string[],
      totalRxData: [] as number[],
      totalTxData: [] as number[],
      maxVal: 1,
    }
  }

  const timestamps = trafficHistory.value.map((item) => item.recorded_at)
  const totalRxData = trafficHistory.value.map((item) => item.total_rx)
  const totalTxData = trafficHistory.value.map((item) => item.total_tx)

  let maxVal = 0
  for (const val of [...totalRxData, ...totalTxData]) {
    if (val > maxVal) maxVal = val
  }

  return {
    timestamps,
    totalRxData,
    totalTxData,
    maxVal: maxVal || 1,
  }
})

const auditLogTotalPages = computed(() => auditLogPagination.value.totalPages)
const paginatedAuditLogs = computed(() => auditLogs.value)
const auditLogPageStart = computed(() => (auditLogPagination.value.total ? (auditLogPagination.value.page - 1) * auditLogPagination.value.pageSize + 1 : 0))
const auditLogPageEnd = computed(() => Math.min(auditLogPagination.value.page * auditLogPagination.value.pageSize, auditLogPagination.value.total))

const nodeModalTitle = computed(() => (editingNodeId.value ? '编辑节点' : '新增节点'))
const userModalTitle = computed(() => (editingUserId.value ? '编辑用户' : '新增用户'))
const adminModalTitle = computed(() => (editingAdminId.value ? '编辑管理员' : '新增管理员'))
const nodeSubmitLabel = computed(() => (editingNodeId.value ? '保存并同步' : '保存并安装'))

const loginBackgroundStyle = computed(() => {
  const url = buildLoginBackgroundUrl(loginBackgroundConfig.value)
  if (!url) return {}
  return {
    backgroundImage: `linear-gradient(180deg, rgba(6, 10, 20, 0.2), rgba(6, 10, 20, 0.82)), url("${url}")`,
  }
})

const backgroundPreviewStyle = computed(() => {
  const url = buildLoginBackgroundUrl(backgroundForm.value)
  if (!url) return {}
  return {
    backgroundImage: `linear-gradient(180deg, rgba(6, 10, 20, 0.16), rgba(6, 10, 20, 0.72)), url("${url}")`,
  }
})

function applySiteIcon(url: string) {
  if (typeof document === 'undefined') return
  const existing = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  const link = existing ?? document.createElement('link')
  link.rel = 'icon'
  link.href = url || '/favicon.ico'
  if (!existing) {
    document.head.appendChild(link)
  }
}

function applySiteTitle(title: string) {
  if (typeof document === 'undefined') return
  const nextTitle = title.trim() || 'Hysteria2 Panel'
  document.title = nextTitle
}

function applyPublicAppSettings(settings: PublicAppSettings) {
  applySiteTitle(settings.site_title)
  applySiteIcon(settings.site_icon_url)
  const nextBackground = {
    customUrl: settings.login_background_url?.trim() || '',
  } satisfies LoginBackgroundConfig
  loginBackgroundConfig.value = nextBackground
  backgroundForm.value = { ...nextBackground }
}

function clearToast() {
  toastMessage.value = ''
  if (toastTimer !== null) {
    window.clearTimeout(toastTimer)
    toastTimer = null
  }
}

function showToast(message: string, type: ToastType = 'success') {
  clearToast()
  toastType.value = type
  toastMessage.value = message
  toastTimer = window.setTimeout(() => {
    toastMessage.value = ''
    toastTimer = null
  }, 3200)
}

function stopNodeStatusSyncTimer() {
  if (nodeStatusSyncTimer !== null) {
    window.clearInterval(nodeStatusSyncTimer)
    nodeStatusSyncTimer = null
  }
}

function stopAuditLogAutoRefreshTimer() {
  if (auditLogAutoRefreshTimer !== null) {
    window.clearInterval(auditLogAutoRefreshTimer)
    auditLogAutoRefreshTimer = null
  }
}

function stopUserTrafficStatsTimer() {
  if (userTrafficStatsTimer !== null) {
    window.clearInterval(userTrafficStatsTimer)
    userTrafficStatsTimer = null
  }
}

function startUserTrafficStatsAutoRefresh() {
  stopUserTrafficStatsTimer()
  if (!session.value || currentSection.value !== 'users' || !hasPermission('user.view')) {
    return
  }

  userTrafficStatsTimer = window.setInterval(() => {
    void loadUserTrafficStats()
  }, 5000)
}

async function loadUserTrafficStats() {
  if (!panel.value.currentNodeId || !hasPermission('user.view') || !users.value.length) {
    userTrafficStats.value = []
    return
  }
  try {
    userTrafficStats.value = await fetchUserTrafficStats(
      panel.value.currentNodeId,
      users.value.map((user) => user.username),
    )
  } catch {
    userTrafficStats.value = []
  }
}

async function loadTrafficHistory(page = trafficPage.value) {
  try {
    const result = await fetchTrafficHistory(24, page, trafficPageSize.value)
    trafficHistory.value = result.series
    trafficNodeSummaries.value = result.items
    trafficPagination.value = result.pagination
    trafficPage.value = result.pagination.page
    trafficHistoryLoaded.value = true
    await nextTick()
    renderTrafficChart()
  } catch {
    trafficHistory.value = []
    trafficNodeSummaries.value = []
    trafficHistoryLoaded.value = true
    trafficChart?.clear()
  }
}

async function handleRefreshTrafficHistory() {
  if (busyAction.value !== '') {
    return
  }

  trafficHistoryLoaded.value = false
  if (!hasPermission('traffic.sync')) {
    await loadTrafficHistory()
    await refreshTrafficNodeRuntimeStatuses()
    return
  }

  await syncTrafficUsageForTrafficView(true)
}

async function syncTrafficUsageForTrafficView(showSuccessMessage = false) {
  const runSync = async () => {
    if (!trafficHistoryLoaded.value) {
      await loadTrafficHistory(trafficPage.value)
    }

    let lastResult: RemoteCommandResult | null = null
    const nodeIds = [...new Set(trafficNodeSummaries.value.map((item) => item.id))]
    if (!nodeIds.length) {
      lastResult = await syncTrafficUsage()
    } else {
      for (const nodeId of nodeIds) {
        lastResult = await syncTrafficUsage(nodeId)
      }
    }
    if (lastResult) {
      operationOutput.value = lastResult
    }
    await loadState()
    await loadTrafficHistory(trafficPage.value)
    await refreshTrafficNodeRuntimeStatuses()
    return lastResult
  }

  if (showSuccessMessage) {
    await withBusyAction('traffic-refresh', runSync, '已刷新当前页节点流量', false)
    return
  }

  if (!session.value || !hasPermission('traffic.sync') || busyAction.value !== '') {
    return
  }

  busyAction.value = 'traffic-sync-enter'
  try {
    await runSync()
  } catch (error) {
    showToast(getErrorMessage(error), 'error')
  } finally {
    if (busyAction.value === 'traffic-sync-enter') {
      busyAction.value = ''
    }
  }
}

function renderTrafficChart() {
  const container = trafficChartContainer.value
  if (!container) return

  const { timestamps, totalRxData, totalTxData } = aggregatedTrafficSeries.value
  if (!timestamps.length) {
    trafficChart?.clear()
    return
  }

  if (!trafficChart || trafficChart.getDom() !== container) {
    trafficChart?.dispose()
    trafficChart = init(container)
  }

  const xAxisData = timestamps.map((value) => {
    const date = new Date(value)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })

  trafficChart.setOption({
    backgroundColor: 'transparent',
    animation: false,
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(8, 17, 32, 0.92)',
      borderColor: 'rgba(148, 163, 184, 0.18)',
      borderWidth: 1,
      padding: [10, 12],
      textStyle: {
        color: '#e2e8f0',
        fontSize: 12,
      },
      formatter: (params: unknown) => {
        const items = Array.isArray(params) ? params as Array<{ axisValueLabel?: string, marker?: string, seriesName?: string, data?: number }> : []
        const title = items[0]?.axisValueLabel ?? ''
        return [
          `<div style="margin-bottom:6px;color:#f8fafc;font-weight:600;">${title}</div>`,
          ...items.map((item) => `${item.marker ?? ''}${item.seriesName ?? ''} ${formatChartBytes(Number(item.data ?? 0))}`),
        ].join('<br/>')
      },
    },
    legend: {
      data: ['总下行', '总上行'],
      top: 0,
      right: 0,
      textStyle: {
        color: 'rgba(226, 232, 240, 0.82)',
      },
      itemWidth: 18,
      itemHeight: 3,
    },
    grid: {
      left: 12,
      right: 18,
      top: 42,
      bottom: 12,
      containLabel: true,
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: xAxisData,
      axisLabel: {
        color: 'rgba(148, 163, 184, 0.82)',
      },
      axisLine: {
        lineStyle: {
          color: 'rgba(148, 163, 184, 0.16)',
        },
      },
      axisTick: {
        show: false,
      },
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        color: 'rgba(148, 163, 184, 0.82)',
        formatter: (value: number) => formatChartBytes(value),
      },
      splitLine: {
        lineStyle: {
          color: 'rgba(148, 163, 184, 0.12)',
        },
      },
      axisLine: {
        show: false,
      },
    },
    series: [
      {
        name: '总下行',
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: totalRxData,
        lineStyle: {
          width: 2.4,
          color: '#3b82f6',
        },
        areaStyle: {
          color: 'rgba(59, 130, 246, 0.14)',
        },
      },
      {
        name: '总上行',
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: totalTxData,
        lineStyle: {
          width: 2,
          color: '#f59e0b',
        },
        areaStyle: {
          color: 'rgba(245, 158, 11, 0.10)',
        },
      },
    ],
  })
}

function resizeTrafficChart() {
  if (trafficChart && trafficChart.getDom().isConnected) {
    trafficChart.resize()
  }
}

function formatChartBytes(val: number): string {
  if (val < 1024) return Math.round(val) + ' B'
  if (val < 1024 * 1024) return Math.round(val / 1024) + ' KB'
  if (val < 1024 * 1024 * 1024) return (val / 1024 / 1024).toFixed(1) + ' MB'
  return (val / 1024 / 1024 / 1024).toFixed(1) + ' GB'
}

function resolveUserQuotaPercent(user: HysteriaUser): number {
  if (user.quota_gb <= 0) return 0
  return Math.min(100, (user.used_gb / user.quota_gb) * 100)
}

function startAuditLogAutoRefresh() {
  stopAuditLogAutoRefreshTimer()
  if (!selectedLogNodeId.value || !selectedLogNodeUsesAgent.value) return

  auditLogAutoRefreshTimer = window.setInterval(() => {
    if (hasPermission('logs.view')) {
      void loadLogs()
    }
    if (hasPermission('audit.view')) {
      void loadAuditLogs()
    }
  }, 1000)
}

function scrollRealtimeLogPanels() {
  if (!realtimeLogRefreshEnabled.value || currentSection.value !== 'logsAudit') {
    return
  }

  void nextTick(() => {
    if (logsOutputRef.value) {
      logsOutputRef.value.scrollTop = logsOutputRef.value.scrollHeight
    }
    if (auditLogListRef.value) {
      auditLogListRef.value.scrollTop = 0
    }
  })
}

function handleAuditLogRefreshModeChange() {
  if (auditLogRefreshMode.value === 'auto') {
    startAuditLogAutoRefresh()
    scrollRealtimeLogPanels()
  } else {
    stopAuditLogAutoRefreshTimer()
  }
}

async function syncNodeStatusesInBackground() {
  if (!session.value || currentSection.value !== 'nodes' || busyAction.value !== '' || refreshingNodeStatuses.value) {
    return
  }

  await refreshNodeRuntimeStatuses()
}

function startNodeStatusSyncTimer() {
  stopNodeStatusSyncTimer()
  if (!session.value || currentSection.value !== 'nodes' || !hasPermission('service.view')) {
    return
  }

  nodeStatusSyncTimer = window.setInterval(() => {
    void syncNodeStatusesInBackground()
  }, NODE_STATUS_SYNC_INTERVAL_MS)
}

async function loadState() {
  const data = await fetchPanelState()
  panel.value = data
  if (isMockPanelEnabled()) {
    nodeRuntimeStatuses.value = getMockNodeRuntimeStatuses()
  }

  await loadNodes(nodePage.value, false)

  if (selectedLogNodeId.value === null) {
    selectedLogNodeId.value = data.currentNodeId ?? nodes.value[0]?.id ?? null
  } else if (!nodes.value.some((node) => node.id === selectedLogNodeId.value)) {
    selectedLogNodeId.value = data.currentNodeId ?? nodes.value[0]?.id ?? null
  }
  if (data.node) {
    nodeForm.value = mapNodeToForm(data.node)
  }
  if (!operationOutput.value) {
    operationOutput.value = data.service
  }

  const nodeId = data.currentNodeId ?? nodes.value[0]?.id ?? null
  if (nodeId && hasPermission('service.view')) {
    try {
      const [online, streams] = await Promise.all([
        fetchOnlineClientPage(nodeId, 1, 1),
        fetchStreamPage(nodeId, 1, 1),
      ])
      onlineClientTotal.value = online.pagination.total
      streamTotal.value = streams.pagination.total
    } catch {
      onlineClientTotal.value = 0
      streamTotal.value = 0
    }
  } else {
    onlineClientTotal.value = 0
    streamTotal.value = 0
  }

  ensureSectionAccess()
}

async function refreshNodeRuntimeStatuses(targetNodeIds?: number[]) {
  const nodeIds = targetNodeIds ?? nodes.value.map((node) => node.id)
  if (refreshingNodeStatuses.value || !nodeIds.length || !hasPermission('service.view')) {
    return
  }

  refreshingNodeStatuses.value = true
  const statusMap: Record<number, ServiceStatus | 'unknown'> = targetNodeIds ? { ...nodeRuntimeStatuses.value } : {}

  try {
    for (const nodeId of nodeIds) {
      try {
        const result = await performServiceAction('status', nodeId)
        statusMap[nodeId] = result.exitCode === 0 ? 'running' : 'stopped'
      } catch {
        statusMap[nodeId] = 'stopped'
      }
    }
  } finally {
    refreshingNodeStatuses.value = false
  }

  nodeRuntimeStatuses.value = statusMap
}

async function refreshTrafficNodeRuntimeStatuses() {
  const nodeIds = [...new Set(trafficNodeSummaries.value.map((item) => item.id))]
  await refreshNodeRuntimeStatuses(nodeIds)
}

async function loadLogs() {
  if (!selectedLogNodeId.value) {
    logsText.value = '请先配置节点后再查看远端日志。'
    logsLoaded.value = true
    return
  }

  const result = await fetchLogs(selectedLogNodeId.value)
  logsText.value = result.output || '暂无日志输出'
  logsLoaded.value = true
  scrollRealtimeLogPanels()
}

async function loadNodes(page = nodePage.value, syncSelectedNode = true) {
  const result = await fetchNodes(page, nodePageSize.value)
  nodes.value = result.items
  nodePagination.value = result.pagination
  nodePage.value = result.pagination.page
  nodesLoaded.value = true

  if (!syncSelectedNode) {
    return
  }

  if (selectedLogNodeId.value === null) {
    selectedLogNodeId.value = panel.value.currentNodeId ?? nodes.value[0]?.id ?? null
  } else if (!nodes.value.some((node) => node.id === selectedLogNodeId.value) && nodes.value.length) {
    selectedLogNodeId.value = panel.value.currentNodeId ?? nodes.value[0]?.id ?? null
  }
}

async function loadUsers(page = userPage.value) {
  const result = await fetchUsers(page, userPageSize.value, userFilterKeyword.value)
  users.value = result.items
  userPagination.value = result.pagination
  userPage.value = result.pagination.page
  usersLoaded.value = true
  await loadUserTrafficStats()
}

async function loadAuditLogs(page = auditLogPage.value) {
  const result = await fetchAuditLogs(selectedLogNodeId.value, page, auditLogPageSize.value)
  auditLogs.value = result.items
  auditLogPagination.value = result.pagination
  auditLogPage.value = result.pagination.page
  auditLogsLoaded.value = true
  scrollRealtimeLogPanels()
}

async function loadAdmins(page = adminPage.value) {
  const result = await fetchAdmins(page, adminPageSize.value)
  admins.value = result.items
  adminPagination.value = result.pagination
  adminPage.value = result.pagination.page
  adminsLoaded.value = true
}

async function loadSystemHealth() {
  systemHealth.value = await fetchSystemHealth()
}

async function loadNotificationSettings() {
  notificationSettingsForm.value = await fetchNotificationSettings()
  notificationSettingsLoaded.value = true
}

async function withBusyAction<T>(key: string, task: () => Promise<T>, successMessage: string, reload = true): Promise<T> {
  busyAction.value = key

  try {
    const result = await task()
    if (reload) {
      await loadState()
    }
    showToast(successMessage, 'success')
    return result
  } catch (error) {
    showToast(getErrorMessage(error), 'error')
    throw error
  } finally {
    busyAction.value = ''
  }
}

async function bootstrap() {
  loginBackgroundConfig.value = DEFAULT_LOGIN_BACKGROUND_CONFIG
  backgroundForm.value = { ...DEFAULT_LOGIN_BACKGROUND_CONFIG }
  session.value = null
  authNotice.value = ''

  try {
    setupStatus.value = await fetchSetupStatus()
  } catch (error) {
    authChecked.value = true
    loginError.value = getErrorMessage(error)
    return
  }

  authChecked.value = true

  if (setupStatus.value?.requiresSetup) {
    return
  }

  try {
    applyPublicAppSettings(await fetchAppSettings())
  } catch {
    loginBackgroundConfig.value = DEFAULT_LOGIN_BACKGROUND_CONFIG
    backgroundForm.value = { ...DEFAULT_LOGIN_BACKGROUND_CONFIG }
    applySiteTitle('Hysteria2 Panel')
  }

  session.value = getStoredSession()

  try {
    const currentSession = await fetchCurrentSession()
    session.value = currentSession
  } catch {
    if (!session.value) {
      session.value = null
    }
  }

  if (session.value) {
    try {
      await loadState()
      if (hasPermission('panel.view')) {
        void loadSystemHealth()
      }
      if (hasPermission('appearance.manage')) {
        systemSettingsForm.value = await fetchSystemSettings()
        syncMockPanelConfigFromSettings()
        applyPublicAppSettings(systemSettingsForm.value)
      }
      await setSection(currentSection.value)
    } catch (error) {
      showToast(getErrorMessage(error), 'error')
    }
  }

  // 异步检查版本更新，不阻塞任何流程
  void checkForUpdates()
}

async function checkForUpdates(): Promise<void> {
  try {
    versionChecking.value = true
    const result = await checkVersion()
    if (result.hasUpdate) {
      versionUpdateAvailable.value = true
      latestVersion.value = result.latest
    }
  } catch {
    // 静默失败，不影响主流程
  } finally {
    versionChecking.value = false
  }
}

function handleVersionClick(): void {
  if (versionUpdateAvailable.value) {
    if (confirm(`发现新版本 v${latestVersion.value}，是否立即升级？\n\n升级过程中会自动备份当前配置，升级完成后页面将自动刷新。`)) {
      void performUpgrade()
    }
  } else {
    void checkForUpdates()
  }
}

async function performUpgrade(): Promise<void> {
  try {
    versionUpgrading.value = true
    showToast('正在下载更新包...', 'info')
    const result = await upgradeVersion()
    showToast(`升级成功！${result.from} → ${result.to}，备份目录：${result.backup}`, 'success')
    setTimeout(() => {
      window.location.reload()
    }, 2000)
  } catch (error) {
    showToast(getErrorMessage(error), 'error')
  } finally {
    versionUpgrading.value = false
  }
}

onMounted(() => {
  window.addEventListener('resize', resizeTrafficChart)
  void bootstrap()
})
onBeforeUnmount(() => {
  clearToast()
  stopNodeStatusSyncTimer()
  stopAuditLogAutoRefreshTimer()
  stopUserTrafficStatsTimer()
  window.removeEventListener('resize', resizeTrafficChart)
  trafficChart?.dispose()
  trafficChart = null
})

async function setSection(id: SectionId) {
  if (!canAccessSection(id)) {
    showToast('当前账号无权访问该页面', 'error')
    ensureSectionAccess()
    return
  }

  currentSection.value = id
  stopNodeStatusSyncTimer()

  if (id === 'logsAudit' && session.value) {
    if (!nodesLoaded.value) {
      await loadNodes()
    }
    if (hasPermission('logs.view') && !logsLoaded.value) {
      try {
        await loadLogs()
      } catch {
        logsText.value = '日志加载失败'
      }
    }

    if (hasPermission('audit.view') && !auditLogsLoaded.value) {
      try {
        await loadAuditLogs()
      } catch {
        auditLogs.value = []
      }
    }

    if (auditLogRefreshMode.value === 'auto') {
      startAuditLogAutoRefresh()
    }
  }

  if (id === 'permissions' && session.value && !adminsLoaded.value) {
    try {
      await withBusyAction('admins', loadAdmins, '已刷新管理员列表', false)
    } catch {
      admins.value = []
    }
  }

  if (id === 'notifications' && !notificationSettingsLoaded.value && session.value) {
    try {
      await loadNotificationSettings()
    } catch {
      notificationSettingsLoaded.value = false
    }
  }

  if (id === 'overview' && session.value && hasPermission('panel.view')) {
    if (!trafficHistoryLoaded.value) {
      await loadTrafficHistory()
    } else {
      await nextTick()
      renderTrafficChart()
    }
  }

  if (id === 'traffic' && session.value && hasPermission('panel.view')) {
    if (!trafficHistoryLoaded.value) {
      await loadTrafficHistory()
    }
    if (hasPermission('traffic.sync')) {
      await syncTrafficUsageForTrafficView()
    } else {
      await refreshTrafficNodeRuntimeStatuses()
    }
  }

  if (id === 'users' && session.value) {
    if (hasPermission('user.view')) {
      if (!usersLoaded.value) {
        await loadUsers()
      } else {
        await loadUserTrafficStats()
      }
    }
    startUserTrafficStatsAutoRefresh()
  } else {
    stopUserTrafficStatsTimer()
  }

  if (id === 'nodes' && session.value) {
    if (!nodesLoaded.value) {
      await loadNodes()
    }
    await refreshNodeRuntimeStatuses()
    startNodeStatusSyncTimer()
  }
}

async function handleLogin() {
  if (insecureRemoteLogin.value) {
    loginForm.value.password = ''
    loginError.value = '当前站点未启用 HTTPS，已禁止登录。请先为面板配置 SSL 证书。'
    return
  }

  loginLoading.value = true
  loginError.value = ''

  try {
    const username = assertAdminUsername(loginForm.value.username)
    loginForm.value.username = username
    session.value = await login(username, loginForm.value.password)
    loginForm.value.password = ''
    operationOutput.value = null
    logsLoaded.value = false
    auditLogsLoaded.value = false
    await loadState()
    if (hasPermission('panel.view')) {
      void loadSystemHealth()
    }
    if (hasPermission('appearance.manage')) {
      systemSettingsForm.value = await fetchSystemSettings()
      syncMockPanelConfigFromSettings()
      applyPublicAppSettings(systemSettingsForm.value)
    }
    await setSection(currentSection.value)
  } catch (error) {
    loginError.value = getErrorMessage(error)
  } finally {
    loginLoading.value = false
  }
}

async function handleInitializeSetup() {
  setupSubmitting.value = true
  setupError.value = ''
  loginError.value = ''
  authNotice.value = ''

  try {
    const setupUsername = assertAdminUsername(setupForm.value.admin_username ?? '')
    setupForm.value.admin_username = setupUsername
    setupStatus.value = await initializeSetup({
      ...setupForm.value,
      admin_username: setupUsername,
    })
    loginForm.value.username = setupUsername
    loginForm.value.password = ''
    authNotice.value = `初始化完成，请使用你刚设置的管理员账号 ${setupUsername} 登录`
  } catch (error) {
    setupError.value = getErrorMessage(error)
  } finally {
    setupSubmitting.value = false
  }
}

async function handleLogout() {
  stopNodeStatusSyncTimer()
  stopUserTrafficStatsTimer()
  await logout()
  session.value = null
  panel.value = createEmptyPanelState()
  nodeForm.value = createEmptyNodeForm()
  userForm.value = createEmptyUserForm()
  editingNodeId.value = null
  editingUserId.value = null
  operationOutput.value = null
  logsText.value = ''
  logsLoaded.value = false
  nodes.value = []
  nodesLoaded.value = false
  nodePagination.value = createPaginationMeta(nodePageSize.value)
  users.value = []
  usersLoaded.value = false
  userPagination.value = createPaginationMeta(userPageSize.value)
  auditLogs.value = []
  auditLogsLoaded.value = false
  auditLogPagination.value = createPaginationMeta(auditLogPageSize.value)
  onlineClientTotal.value = 0
  streamTotal.value = 0
  trafficHistory.value = []
  trafficNodeSummaries.value = []
  trafficPagination.value = createPaginationMeta(trafficPageSize.value)
  admins.value = []
  adminsLoaded.value = false
  adminPagination.value = createPaginationMeta(adminPageSize.value)
  nodeRuntimeStatuses.value = {}
  selectedLogNodeId.value = null
  clearToast()
  currentSection.value = 'overview'
  showNodeModal.value = false
  showUserModal.value = false
  showAdminModal.value = false
  showUserDeleteModal.value = false
  showNodeDeleteModal.value = false
  showAdminDeleteModal.value = false
  showSubscriptionModal.value = false
  subscriptionInfo.value = null
  stopAuditLogAutoRefreshTimer()
}

function handleResetNodeForm() {
  const editingNode = editingNodeId.value ? nodes.value.find((item) => item.id === editingNodeId.value) : null
  nodeForm.value = editingNode ? mapNodeToForm(editingNode) : createEmptyNodeForm()
  syncNodeKeyState(nodeForm.value)
}

function openNodeModal() {
  showNodeAdvancedOptions.value = false
  showNodeModal.value = true
}

function closeNodeModal() {
  if (busyAction.value === 'node-save') return
  clearNodeKeyFileSelection()
  showNodeModal.value = false
}

function handleCreateNode() {
  editingNodeId.value = null
  nodeForm.value = createEmptyNodeForm()
  syncNodeKeyState(nodeForm.value)
  clearToast()
  openNodeModal()
}

function handleEditNode(node: HysteriaNodeConfig) {
  editingNodeId.value = node.id
  nodeForm.value = mapNodeToForm(node)
  syncNodeKeyState(nodeForm.value)
  showToast(`已载入节点 ${node.name}`, 'success')
  openNodeModal()
}

async function handleSaveNode() {
  if (nodeSaveLocked.value || busyAction.value === 'node-save') {
    return
  }

  nodeSaveLocked.value = true
  const isEditing = Boolean(editingNodeId.value)
  try {
    const node = await withBusyAction('node-save', async () => {
      if (nodeForm.value.ssh_auth_type === 'key' && nodePrivateKeyFile.value) {
        nodeKeyUploading.value = true
        try {
          const uploadResult = await uploadNodeSshKey(nodePrivateKeyFile.value)
          applyUploadedNodeKey(uploadResult)
        } finally {
          nodeKeyUploading.value = false
        }
      }

      if (isEditing) {
        const savedNode = await saveNodeConfig(nodeForm.value, editingNodeId.value)
        nodeForm.value = mapNodeToForm(savedNode)
        const deployResult = await deployHysteriaConfig(savedNode.id)
        operationOutput.value = deployResult
        return savedNode
      } else {
        const createResult = await createNodeWithInstall(nodeForm.value)
        nodeForm.value = mapNodeToForm(createResult.node)
        operationOutput.value = createResult.install
        return createResult.node
      }
    }, isEditing ? '节点已保存，配置已加入 Agent 异步同步队列' : '节点已创建并自动执行安装流程')

    nodeRuntimeStatuses.value[node.id] = 'degraded'
    await refreshNodeRuntimeStatuses()
    editingNodeId.value = null
    showNodeModal.value = false
  } finally {
    nodeSaveLocked.value = false
  }
}

async function handleDeleteNode(node: HysteriaNodeConfig) {
  pendingDeleteNode.value = node
  showNodeDeleteModal.value = true
}

function closeNodeDeleteModal() {
  if (busyAction.value === 'node-delete') {
    return
  }
  showNodeDeleteModal.value = false
  pendingDeleteNode.value = null
}

async function confirmDeleteNode() {
  if (!pendingDeleteNode.value) {
    return
  }

  const node = pendingDeleteNode.value
  await withBusyAction(
    'node-delete',
    async () => {
      await uninstallHysteria(node.id)
      await deleteNode(node.id)
    },
    `节点 ${node.name} 已卸载并删除`,
  )
  delete nodeRuntimeStatuses.value[node.id]
  if (editingNodeId.value === node.id) {
    editingNodeId.value = null
    showNodeModal.value = false
    nodeForm.value = createEmptyNodeForm()
  }
  closeNodeDeleteModal()
}

function resetUserEditor() {
  editingUserId.value = null
  userForm.value = createEmptyUserForm()
}

function openCreateUserModal() {
  resetUserEditor()
  showUserModal.value = true
}

function closeUserModal() {
  if (busyAction.value === 'user-save' || busyAction.value === 'user-create') return
  showUserModal.value = false
}

function handleEditUser(user: HysteriaUser) {
  editingUserId.value = user.id
  userForm.value = mapUserToForm(user)
  currentSection.value = 'users'
  showUserModal.value = true
}

async function handleSubmitUser() {
  const payload: HysteriaUserPayload = {
    username: userForm.value.username.trim(),
    auth_password: userForm.value.auth_password.trim(),
    status: userForm.value.status,
    quota_gb: Number(userForm.value.quota_gb),
    used_gb: Number(userForm.value.used_gb),
    speed_limit_mbps: Number(userForm.value.speed_limit_mbps),
    expires_at: normalizeDateTime(userForm.value.expires_at),
  }

  if (editingUserId.value) {
    await withBusyAction('user-save', () => updateUser(editingUserId.value as number, payload), '用户已更新', false)
  } else {
    await withBusyAction('user-create', () => createUser(payload), '用户已创建', false)
  }

  await loadUsers(userPage.value)
  resetUserEditor()
  showUserModal.value = false
}

async function handleDeleteUser(user: HysteriaUser) {
  pendingDeleteUser.value = user
  showUserDeleteModal.value = true
}

function closeUserDeleteModal() {
  if (busyAction.value === 'user-delete') {
    return
  }
  showUserDeleteModal.value = false
  pendingDeleteUser.value = null
}

async function confirmDeleteUser() {
  if (!pendingDeleteUser.value) {
    return
  }

  const user = pendingDeleteUser.value
  await withBusyAction('user-delete', () => deleteUser(user.id), '用户已删除', false)
  await loadUsers(userPage.value)
  if (editingUserId.value === user.id) {
    resetUserEditor()
  }
  closeUserDeleteModal()
}

async function handleRefreshLogs() {
  if (!selectedLogNodeId.value) {
    logsText.value = '请先选择节点后再查看远端日志。'
    logsLoaded.value = true
    return
  }

  const result = await withBusyAction('logs', () => fetchLogs(selectedLogNodeId.value), '已刷新远端日志', false)
  logsText.value = result.output || '暂无日志输出'
  logsLoaded.value = true
}

async function handleRefreshAuditLogs() {
  await withBusyAction('audit-logs', loadAuditLogs, '已刷新审计日志', false)
}

async function handleLogNodeChange() {
  logsLoaded.value = false
  auditLogsLoaded.value = false
  auditLogPage.value = 1
  stopAuditLogAutoRefreshTimer()

  if (currentSection.value !== 'logsAudit' || !session.value) {
    return
  }

  if (hasPermission('logs.view')) {
    try {
      await loadLogs()
    } catch {
      logsText.value = '日志加载失败'
    }
  }

  if (hasPermission('audit.view')) {
    try {
      await loadAuditLogs()
    } catch {
      auditLogs.value = []
    }
  }

  if (auditLogRefreshMode.value === 'auto') {
    startAuditLogAutoRefresh()
  }
}

function setAuditLogPage(page: number) {
  auditLogPage.value = Math.max(1, Math.min(auditLogTotalPages.value, page))
  void loadAuditLogs(auditLogPage.value)
}

function handleAuditPageSizeChange() {
  auditLogPage.value = 1
  void loadAuditLogs(1)
}

function handleUserFilterInput() {
  if (userFilterTimer !== null) {
    window.clearTimeout(userFilterTimer)
  }
  userFilterTimer = window.setTimeout(() => {
    userFilterKeyword.value = userFilter.value.trim()
    userPage.value = 1
    void loadUsers(1)
  }, 280)
}

function setNodePage(page: number) {
  nodePage.value = Math.max(1, Math.min(nodePagination.value.totalPages, page))
  void loadNodes(nodePage.value)
}

function handleNodePageSizeChange() {
  nodePage.value = 1
  void loadNodes(1)
}

function setUserPage(page: number) {
  userPage.value = Math.max(1, Math.min(userPagination.value.totalPages, page))
  void loadUsers(userPage.value)
}

function handleUserPageSizeChange() {
  userPage.value = 1
  void loadUsers(1)
}

function setTrafficPage(page: number) {
  trafficPage.value = Math.max(1, Math.min(trafficPagination.value.totalPages, page))
  void (async () => {
    await loadTrafficHistory(trafficPage.value)
    await refreshTrafficNodeRuntimeStatuses()
  })()
}

function handleTrafficPageSizeChange() {
  trafficPage.value = 1
  void (async () => {
    await loadTrafficHistory(1)
    await refreshTrafficNodeRuntimeStatuses()
  })()
}

function setAdminPage(page: number) {
  adminPage.value = Math.max(1, Math.min(adminPagination.value.totalPages, page))
  void loadAdmins(adminPage.value)
}

function handleAdminPageSizeChange() {
  adminPage.value = 1
  void loadAdmins(1)
}

async function handleStopNode(node: HysteriaNodeConfig) {
  const result = await withBusyAction(
    `node-stop-${node.id}`,
    () => performServiceAction('stop', node.id),
    `节点 ${node.name} 已停止`,
  )
  operationOutput.value = result
  nodeRuntimeStatuses.value[node.id] = result.exitCode === 0 ? 'stopped' : 'degraded'
  currentSection.value = 'nodes'
}

async function handleStartNode(node: HysteriaNodeConfig) {
  const result = await withBusyAction(
    `node-start-${node.id}`,
    () => performServiceAction('start', node.id),
    `节点 ${node.name} 已启动`,
  )
  operationOutput.value = result
  nodeRuntimeStatuses.value[node.id] = result.exitCode === 0 ? 'running' : 'degraded'
  currentSection.value = 'nodes'
}

async function handleRestartNode(node: HysteriaNodeConfig) {
  const result = await withBusyAction(
    `node-restart-${node.id}`,
    () => performServiceAction('restart', node.id),
    `节点 ${node.name} 已重启`,
  )
  operationOutput.value = result
  nodeRuntimeStatuses.value[node.id] = result.exitCode === 0 ? 'running' : 'degraded'
  currentSection.value = 'nodes'
}

async function handleUpgradeAgent(node: HysteriaNodeConfig) {
  const result = await withBusyAction(
    `agent-upgrade-${node.id}`,
    () => performServiceAction('upgrade-agent', node.id),
    `节点 ${node.name} 的 Agent 已升级`,
  )
  operationOutput.value = result
  currentSection.value = 'nodes'
}

function resetAdminEditor() {
  editingAdminId.value = null
  adminForm.value = createEmptyAdminForm()
}

function openCreateAdminModal() {
  resetAdminEditor()
  showAdminModal.value = true
}

function closeAdminModal() {
  if (busyAction.value === 'admin-save' || busyAction.value === 'admin-create') return
  showAdminModal.value = false
}

function handleEditAdmin(item: AdminUser) {
  editingAdminId.value = item.id
  adminForm.value = {
    username: item.username,
    display_name: item.display_name,
    role: item.role,
    status: item.status,
    password: '',
  }
  showAdminModal.value = true
}

async function handleSubmitAdmin() {
  const username = assertAdminUsername(adminForm.value.username)
  adminForm.value.username = username
  const payload: AdminUserPayload = {
    username,
    display_name: adminForm.value.display_name.trim(),
    role: adminForm.value.role,
    status: adminForm.value.status,
    password: adminForm.value.password.trim() || undefined,
  }

  if (editingAdminId.value) {
    await withBusyAction('admin-save', () => updateAdmin(editingAdminId.value as number, payload), '管理员已更新', false)
  } else {
    if (!payload.password) {
      throw new Error('新增管理员时必须设置密码')
    }
    await withBusyAction('admin-create', () => createAdmin(payload), '管理员已创建', false)
  }

  await loadAdmins()
  resetAdminEditor()
  showAdminModal.value = false
}

async function handleDeleteAdmin(item: AdminUser) {
  pendingDeleteAdmin.value = item
  showAdminDeleteModal.value = true
}

function closeAdminDeleteModal() {
  if (busyAction.value === 'admin-delete') {
    return
  }
  showAdminDeleteModal.value = false
  pendingDeleteAdmin.value = null
}

async function confirmDeleteAdmin() {
  if (!pendingDeleteAdmin.value) {
    return
  }

  const item = pendingDeleteAdmin.value
  await withBusyAction('admin-delete', () => deleteAdmin(item.id), '管理员已删除', false)
  await loadAdmins()
  closeAdminDeleteModal()
}

function buildQrCodeUrl(value: string) {
  return `https://quickchart.io/qr?text=${encodeURIComponent(value)}&size=240`
}

async function copySubscriptionLink() {
  const value = qrModalValue.value.trim()
  if (!value) {
    showToast('当前没有可复制的订阅链接', 'error')
    return
  }

  await navigator.clipboard.writeText(value)
  showToast('已复制订阅链接', 'success')
}

async function openSubscriptionModal(user: HysteriaUser) {
  busyAction.value = 'user-subscription'
  try {
    const info = await fetchUserSubscriptionInfo(user.id)
    subscriptionInfo.value = info
    qrModalTitle.value = `${info.username} · 订阅信息`
    qrModalValue.value = info.url
    showSubscriptionModal.value = true
  } catch (error) {
    showToast(getErrorMessage(error), 'error')
  } finally {
    busyAction.value = ''
  }
}

function closeSubscriptionModal() {
  showSubscriptionModal.value = false
  subscriptionInfo.value = null
  qrModalTitle.value = ''
  qrModalValue.value = ''
}

function buildSystemSettingsPayload(): SystemSettings {
  return {
    site_title: systemSettingsForm.value.site_title.trim(),
    public_api_base_url: systemSettingsForm.value.public_api_base_url.trim(),
    site_icon_url: systemSettingsForm.value.site_icon_url.trim(),
    login_background_url: backgroundForm.value.customUrl.trim(),
    mock_panel_enabled: Boolean(systemSettingsForm.value.mock_panel_enabled),
    mock_node_count: Number(systemSettingsForm.value.mock_node_count) || 6,
    mock_user_count: Number(systemSettingsForm.value.mock_user_count) || 32,
    mock_running_node_count: Number(systemSettingsForm.value.mock_running_node_count) || 0,
    mock_degraded_node_count: Number(systemSettingsForm.value.mock_degraded_node_count) || 0,
    mock_stopped_node_count: Number(systemSettingsForm.value.mock_stopped_node_count) || 0,
    mock_suspended_user_count: Number(systemSettingsForm.value.mock_suspended_user_count) || 0,
    bruteforce_enabled: Boolean(systemSettingsForm.value.bruteforce_enabled),
    bruteforce_max_attempts: Number(systemSettingsForm.value.bruteforce_max_attempts) || 5,
    bruteforce_window_minutes: Number(systemSettingsForm.value.bruteforce_window_minutes) || 15,
    bruteforce_lock_minutes: Number(systemSettingsForm.value.bruteforce_lock_minutes) || 15,
  }
}

function syncMockPanelConfigFromSettings() {
  applyMockPanelConfig({
    enabled: systemSettingsForm.value.mock_panel_enabled,
    node_count: systemSettingsForm.value.mock_node_count,
    user_count: systemSettingsForm.value.mock_user_count,
    running_node_count: systemSettingsForm.value.mock_running_node_count,
    degraded_node_count: systemSettingsForm.value.mock_degraded_node_count,
    stopped_node_count: systemSettingsForm.value.mock_stopped_node_count,
    suspended_user_count: systemSettingsForm.value.mock_suspended_user_count,
  })
}

async function handleSaveLoginBackground() {
  systemSettingsForm.value = await withBusyAction(
    'system-settings',
    () => updateSystemSettings(buildSystemSettingsPayload()),
    '背景图配置已保存',
    false,
  )
  syncMockPanelConfigFromSettings()
  applyPublicAppSettings(systemSettingsForm.value)
}

async function handleResetLoginBackground() {
  backgroundForm.value = { ...DEFAULT_LOGIN_BACKGROUND_CONFIG }
  systemSettingsForm.value = await withBusyAction(
    'system-settings',
    () => updateSystemSettings(buildSystemSettingsPayload()),
    '已恢复默认背景图',
    false,
  )
  syncMockPanelConfigFromSettings()
  applyPublicAppSettings(systemSettingsForm.value)
}

async function handleSaveSystemSettings() {
  systemSettingsForm.value = await withBusyAction(
    'system-settings',
    () => updateSystemSettings(buildSystemSettingsPayload()),
    '系统设置已保存',
    false,
  )
  syncMockPanelConfigFromSettings()
  applyPublicAppSettings(systemSettingsForm.value)
  await loadState()
}

async function handleSaveNotificationSettings() {
  const payload: NotificationSettings = {
    ...notificationSettingsForm.value,
    smtp_host: notificationSettingsForm.value.smtp_host.trim(),
    smtp_port: Number(notificationSettingsForm.value.smtp_port) || 587,
    smtp_username: notificationSettingsForm.value.smtp_username.trim(),
    smtp_password: notificationSettingsForm.value.smtp_password,
    smtp_from_email: notificationSettingsForm.value.smtp_from_email.trim(),
    smtp_from_name: notificationSettingsForm.value.smtp_from_name.trim(),
    smtp_notify_email: notificationSettingsForm.value.smtp_notify_email.trim(),
  }

  notificationSettingsForm.value = await withBusyAction(
    'notification-settings',
    () => updateNotificationSettings(payload),
    'SMTP 通知配置已保存',
    false,
  )
}

async function handleSendTestNotification() {
  await withBusyAction(
    'notification-test',
    sendTestNotification,
    '测试邮件已发送，请检查接收邮箱',
    false,
  )
}

</script>

<template>
  <div v-if="!authChecked" class="auth-shell">
    <section class="auth-card auth-loading">
      <p class="eyebrow">Session Check</p>
      <h1>正在检查登录态</h1>
    </section>
  </div>

  <div v-else-if="setupStatus?.requiresSetup" class="auth-shell anime-auth-shell" :style="loginBackgroundStyle">
    <section class="auth-card auth-setup-card">
      <div class="auth-header setup-header">
        <span class="auth-mark" aria-hidden="true"></span>
        <p class="eyebrow">First Install</p>
        <h1>首次安装</h1>
      </div>

      <form class="setup-form" @submit.prevent="handleInitializeSetup">
        <div class="setup-form-stack">
          <div class="setup-row">
            <label class="setup-field password-field">
              <span>MySQL 主机</span>
              <input v-model="setupForm.host" class="setup-input" type="text" />
            </label>

            <label class="setup-field">
              <span>端口</span>
              <input v-model.number="setupForm.port" class="setup-input" type="number" min="1" max="65535" />
            </label>

            <label class="setup-field">
              <span>数据库名</span>
              <input v-model="setupForm.database" class="setup-input" type="text" />
            </label>
          </div>

          <div class="setup-row">
            <label class="setup-field">
              <span>MySQL 用户名</span>
              <input v-model="setupForm.username" class="setup-input" type="text" autocomplete="username" />
            </label>

            <label class="setup-field password-field">
              <span>MySQL 密码</span>
              <div class="password-control">
                <input
                  v-model="setupForm.password"
                  class="setup-input password-input"
                  :type="isPasswordVisible('setupDb') ? 'text' : 'password'"
                  autocomplete="current-password"
                />
                <button
                  class="password-toggle"
                  :class="{ active: isPasswordVisible('setupDb') }"
                  type="button"
                  :aria-label="isPasswordVisible('setupDb') ? '隐藏密码' : '显示密码'"
                  :title="isPasswordVisible('setupDb') ? '隐藏密码' : '显示密码'"
                  @click="togglePasswordVisibility('setupDb')"
                >
                  <svg v-if="isPasswordVisible('setupDb')" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                    <circle cx="12" cy="12" r="2.6" />
                  </svg>
                  <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                    <circle cx="12" cy="12" r="2.6" />
                    <path d="M5 4L19 20" />
                  </svg>
                </button>
              </div>
            </label>

            <label class="setup-field">
              <span>管理员账号</span>
              <input
                v-model="setupForm.admin_username"
                class="setup-input"
                type="text"
                autocomplete="username"
                placeholder="首次安装时创建"
                inputmode="text"
                pattern="[A-Za-z0-9]+"
                title="用户名仅支持英文和数字"
                @input="handleSetupAdminUsernameInput"
              />
            </label>
          </div>

          <label class="setup-field setup-field-full password-field">
            <span>管理员密码</span>
            <div class="password-control">
              <input
                v-model="setupForm.admin_password"
                class="setup-input password-input"
                :type="isPasswordVisible('setupAdmin') ? 'text' : 'password'"
                autocomplete="new-password"
                placeholder="至少 8 位，首次安装时创建"
              />
              <button
                class="password-toggle"
                :class="{ active: isPasswordVisible('setupAdmin') }"
                type="button"
                :aria-label="isPasswordVisible('setupAdmin') ? '隐藏密码' : '显示密码'"
                :title="isPasswordVisible('setupAdmin') ? '隐藏密码' : '显示密码'"
                @click="togglePasswordVisibility('setupAdmin')"
              >
                <svg v-if="isPasswordVisible('setupAdmin')" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                  <path d="M5 4L19 20" />
                </svg>
              </button>
            </div>
          </label>
        </div>

        <div class="setup-actions">
          <button class="primary auth-submit" :disabled="setupSubmitting" type="submit">
            {{ setupSubmitting ? '初始化中...' : '初始化数据库' }}
          </button>
        </div>

        <p class="error-text setup-error" :class="{ visible: Boolean(setupError) }" aria-live="polite">
          {{ setupError || ' ' }}
        </p>
      </form>
    </section>
  </div>

  <div v-else-if="!session" class="auth-shell anime-auth-shell" :style="loginBackgroundStyle">
    <section class="auth-card auth-login-card login-card">
      <div class="auth-header login-header">
        <span class="auth-mark" aria-hidden="true"></span>
        <p class="eyebrow">Hysteria2 Console</p>
        <h1>登录</h1>
        <p v-if="insecureRemoteLogin" class="auth-note login-note">
          当前页面不是 HTTPS，登录请求已被禁用。请先在宝塔/Nginx 中为站点启用 SSL。
        </p>
      </div>

      <form class="login-form" @submit.prevent="handleLogin">
        <label class="login-field">
          <span>用户名</span>
          <input
            v-model="loginForm.username"
            class="login-input"
            type="text"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            inputmode="text"
            pattern="[A-Za-z0-9]+"
            title="用户名仅支持英文和数字"
            @input="handleLoginUsernameInput"
          />
        </label>

        <label class="login-field">
          <span>密码</span>
          <div class="password-control">
            <input
              v-model="loginForm.password"
              class="login-input password-input"
              :type="isPasswordVisible('login') ? 'text' : 'password'"
              autocomplete="current-password"
            />
            <button
              class="password-toggle"
              :class="{ active: isPasswordVisible('login') }"
              type="button"
              :aria-label="isPasswordVisible('login') ? '隐藏密码' : '显示密码'"
              :title="isPasswordVisible('login') ? '隐藏密码' : '显示密码'"
              @click="togglePasswordVisibility('login')"
            >
              <svg v-if="isPasswordVisible('login')" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                <circle cx="12" cy="12" r="2.6" />
              </svg>
              <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                <circle cx="12" cy="12" r="2.6" />
                <path d="M5 4L19 20" />
              </svg>
            </button>
          </div>
        </label>

        <div class="login-actions">
          <button class="primary auth-submit login-submit" :disabled="loginLoading" type="submit">
            {{ loginLoading ? '登录中...' : '登录' }}
          </button>
        </div>

        <p class="error-text login-error" :class="{ visible: Boolean(loginError) }" aria-live="polite">
          {{ loginError || ' ' }}
        </p>

        <p v-if="authNotice" class="auth-note auth-note-success login-note">
          {{ authNotice }}
        </p>
      </form>
    </section>
  </div>

  <div v-else class="shell">
    <Transition name="toast">
      <div v-if="toastMessage" class="toast-wrap" aria-live="polite">
        <div class="toast" :class="toastType === 'error' ? 'toast-error' : 'toast-success'">
          <span>{{ toastMessage }}</span>
          <button class="toast-close" type="button" @click="clearToast">关闭</button>
        </div>
      </div>
    </Transition>

    <aside class="sidebar">
      <div class="sidebar-top">
        <div class="sidebar-brand-card">
          <p class="sidebar-brand-text">Hysteria2 Web Console</p>
        </div>

        <div class="session-card">
          <div class="session-card-head">
            <span class="session-label">当前登录</span>
            <span class="session-role-pill">{{ adminRoleLabelMap[session.role] }}</span>
          </div>
          <strong>{{ session.displayName }}</strong>
          <p>{{ session.username }}</p>
        </div>
      </div>

      <div class="sidebar-version-row">
        <div
          class="sidebar-version"
          :class="{ 'sidebar-version-update': versionUpdateAvailable, 'sidebar-version-upgrading': versionUpgrading }"
          @click="handleVersionClick"
        >
          <span v-if="versionUpgrading">升级中...</span>
          <span v-else-if="versionChecking">检查中...</span>
          <template v-else>
            v{{ appVersion }}
            <span v-if="versionUpdateAvailable" class="version-update-badge">更新 v{{ latestVersion }}</span>
          </template>
        </div>
      </div>

      <nav class="nav-list">
        <button
          v-for="item in visibleSections"
          :key="item.id"
          class="nav-item"
          :class="{ active: currentSection === item.id }"
          @click="setSection(item.id)"
        >
          {{ item.label }}
        </button>
        <button class="nav-item logout-button" @click="handleLogout">退出登录</button>
      </nav>
    </aside>

    <main class="content">
      <section v-if="currentSection === 'overview'" class="content-page">
        <header class="hero-card" :class="{ 'health-alert-card': systemHealth && !systemHealth.ok }">
          <div v-if="systemHealth && !systemHealth.ok" class="health-alert-content">
            <p class="eyebrow">异常公告</p>
            <h2>系统依赖检查发现异常</h2>
            <div class="health-issue-list">
              <article v-for="issue in systemHealth.issues" :key="`${issue.title}-${issue.message}`" class="health-issue-row">
                <strong>{{ issue.title }}</strong>
                <span>{{ issue.message }}</span>
              </article>
            </div>
          </div>
          <div v-else>
            <p class="eyebrow">控制台概览</p>
            <h2>节点、用户与流量状态总览</h2>
          </div>
        </header>

        <section class="metrics-grid">
          <article class="metric-card">
            <p>节点总数</p>
            <strong>{{ totalNodeCount }}</strong>
            <span class="primary">已配置节点</span>
          </article>
          <article class="metric-card">
            <p>当前节点</p>
            <strong>{{ panel.node?.name || '未配置' }}</strong>
            <span :class="inferredServiceStatus === 'running' ? 'success' : 'warning'">{{ currentNodeStatusLabel }}</span>
          </article>
          <article class="metric-card">
            <p>活跃用户</p>
            <strong>{{ activeUserRatio }}</strong>
            <span class="primary">活跃 / 总用户</span>
          </article>
          <article class="metric-card">
            <p>在线用户</p>
            <strong>{{ onlineUserRatio }}</strong>
            <span class="primary">在线 / 总用户 · 连接流 {{ streamTotal }}</span>
          </article>
        </section>

        <section class="two-column">
          <article class="card full-width">
            <div class="section-head section-head-wrap">
              <div>
                <p class="eyebrow">流量总趋势</p>
                <h3>全节点 24 小时汇总趋势</h3>
              </div>
              <button class="secondary" :disabled="busyAction !== ''" @click="handleRefreshTrafficHistory">刷新</button>
            </div>
            <div v-if="trafficHistoryLoaded && trafficHistory.length" class="traffic-chart-container">
              <div ref="trafficChartContainer" class="traffic-chart-canvas"></div>
            </div>
            <div v-else-if="trafficHistoryLoaded && !trafficHistory.length" class="empty-card">
              <p>暂无流量趋势数据，等待流量同步后自动记录</p>
            </div>
            <div v-else class="empty-card">
              <p>加载流量趋势数据中...</p>
            </div>
          </article>
        </section>

      </section>

      <section v-else-if="currentSection === 'traffic'" class="content-page">
        <article class="card">
          <div class="section-head section-head-wrap">
            <div>
              <p class="eyebrow">节点流量</p>
                <h3>各节点近 30 天流量信息</h3>
            </div>
            <div class="hero-actions">
              <button class="secondary" :disabled="busyAction !== ''" @click="handleRefreshTrafficHistory">
                {{ busyAction === 'traffic-refresh' ? '刷新中...' : '刷新' }}
              </button>
            </div>
          </div>

          <div v-if="trafficNodeSummaries.length" class="traffic-node-grid">
            <article v-for="item in trafficNodeSummaries" :key="item.id" class="traffic-node-card">
              <div class="traffic-node-head">
                <div>
                  <strong>{{ item.name }}</strong>
                  <p>{{ item.host }}</p>
                </div>
                <div class="traffic-node-head-meta">
                  <span class="user-status" :class="resolveNodeRuntimeStatus(item.id) === 'unknown' ? 'suspended' : resolveNodeRuntimeStatus(item.id)">
                    {{ nodeRuntimeStatusLabel(item.id) }}
                  </span>
                </div>
              </div>

              <div class="traffic-node-metrics">
                <div class="traffic-node-metric">
                  <span>在线用户</span>
                  <strong>{{ item.onlineCount }}/{{ item.userCount }}</strong>
                </div>
                <div class="traffic-node-metric">
                  <span>30 天下行</span>
                  <strong>{{ formatChartBytes(item.totalRx) }}</strong>
                </div>
                <div class="traffic-node-metric">
                  <span>30 天上行</span>
                  <strong>{{ formatChartBytes(item.totalTx) }}</strong>
                </div>
              </div>

            </article>
          </div>
          <div v-if="trafficPagination.total > 0" class="audit-pagination">
            <div class="audit-page-size">
              <span>每页</span>
              <select v-model.number="trafficPageSize" class="compact-select" @change="handleTrafficPageSizeChange">
                <option v-for="size in trafficPageSizeOptions" :key="size" :value="size">{{ size }}</option>
              </select>
            </div>
            <span class="audit-page-info">{{ (trafficPagination.page - 1) * trafficPagination.pageSize + 1 }}-{{ Math.min(trafficPagination.page * trafficPagination.pageSize, trafficPagination.total) }} / {{ trafficPagination.total }}</span>
            <div class="audit-page-actions">
              <button class="secondary compact-button" :disabled="trafficPage <= 1" @click="setTrafficPage(trafficPage - 1)">上一页</button>
              <span>{{ trafficPage }} / {{ trafficPagination.totalPages }}</span>
              <button class="secondary compact-button" :disabled="trafficPage >= trafficPagination.totalPages" @click="setTrafficPage(trafficPage + 1)">下一页</button>
            </div>
          </div>
          <div v-else class="empty-card">
            <p>暂无节点流量数据，等待采样后展示</p>
          </div>
        </article>

      </section>

      <section v-else-if="currentSection === 'nodes'" class="content-page">
        <article class="card">
          <div class="section-head section-head-wrap">
            <div>
              <p class="eyebrow">节点列表</p>
              <h3>新增、更新、删除与节点控制</h3>
            </div>
            <div class="hero-actions">
              <button v-if="hasPermission('node.manage')" class="primary" :disabled="busyAction !== ''" @click="handleCreateNode">新增节点</button>
            </div>
          </div>

          <div v-if="nodeList.length" class="node-inline-table">
            <div class="node-inline-head">
              <span>节点名称</span>
              <span>运行状态</span>
              <span>服务器地址</span>
              <span>监听端口</span>
              <span>节点信息</span>
              <span>操作</span>
            </div>
            <article v-for="node in nodeList" :key="node.id" class="node-inline-row">
              <span class="node-inline-name">
                <strong>{{ node.name }}</strong>
              </span>
              <span class="node-inline-status">
                <span class="user-status" :class="resolveNodeRuntimeStatus(node.id) === 'unknown' ? 'suspended' : resolveNodeRuntimeStatus(node.id)">
                  {{ nodeRuntimeStatusLabel(node.id) }}
                </span>
              </span>
              <span>{{ node.host }}</span>
              <span>{{ node.listen_port }}/udp</span>
              <span>
                {{ resolveNodeManageLabel(node) }}
                ·
                {{ node.domain || '未设置域名' }}
                ·
                {{ node.tls_mode === 'self_signed' ? '自签证书' : 'ACME' }}
                <template v-if="node.manage_mode === 'agent'">
                  ·
                  <span class="user-status" :class="resolveAgentStatusClass(node)">
                    {{ resolveAgentStatusLabel(node) }}
                  </span>
                </template>
              </span>
              <span class="node-inline-actions">
                <button v-if="hasPermission('node.manage')" class="secondary compact-button" @click="handleEditNode(node)">编辑</button>
                <button
                  v-if="hasPermission('service.manage')"
                  class="secondary compact-button"
                  :disabled="busyAction !== ''"
                  @click="handleStartNode(node)"
                >
                  启动
                </button>
                <button
                  v-if="hasPermission('service.manage')"
                  class="secondary compact-button"
                  :disabled="busyAction !== ''"
                  @click="handleRestartNode(node)"
                >
                  重启
                </button>
                <button
                  v-if="hasPermission('service.manage') && node.manage_mode === 'agent' && node.agent_enabled"
                  class="secondary compact-button"
                  :disabled="busyAction !== ''"
                  @click="handleUpgradeAgent(node)"
                >
                  升级 Agent
                </button>
                <button
                  v-if="hasPermission('service.manage')"
                  class="secondary compact-button"
                  :disabled="busyAction !== ''"
                  @click="handleStopNode(node)"
                >
                  停止
                </button>
                <button
                  v-if="hasPermission('node.manage') && hasPermission('service.manage')"
                  class="secondary compact-button danger-button"
                  :disabled="busyAction !== ''"
                  @click="handleDeleteNode(node)"
                >
                  删除
                </button>
              </span>
            </article>
          </div>
          <div v-if="nodePagination.total > 0" class="audit-pagination">
            <div class="audit-page-size">
              <span>每页</span>
              <select v-model.number="nodePageSize" class="compact-select" @change="handleNodePageSizeChange">
                <option v-for="size in listPageSizeOptions" :key="size" :value="size">{{ size }}</option>
              </select>
            </div>
            <span class="audit-page-info">{{ (nodePagination.page - 1) * nodePagination.pageSize + 1 }}-{{ Math.min(nodePagination.page * nodePagination.pageSize, nodePagination.total) }} / {{ nodePagination.total }}</span>
            <div class="audit-page-actions">
              <button class="secondary compact-button" :disabled="nodePage <= 1" @click="setNodePage(nodePage - 1)">上一页</button>
              <span>{{ nodePage }} / {{ nodePagination.totalPages }}</span>
              <button class="secondary compact-button" :disabled="nodePage >= nodePagination.totalPages" @click="setNodePage(nodePage + 1)">下一页</button>
            </div>
          </div>
          <div v-else class="empty-card">
            <p>当前还没有节点记录，点击右上角“新增节点”即可创建第一台节点。</p>
          </div>
        </article>
      </section>

      <section v-else-if="currentSection === 'users'" class="content-page">
        <section class="content-stack">
          <article class="card">
            <div class="section-head section-head-wrap">
              <div>
                <p class="eyebrow">用户列表</p>
                <h3>用户管理</h3>
              </div>
              <div class="hero-actions">
                <input v-model="userFilter" class="search-input" type="text" placeholder="搜索用户名" @input="handleUserFilterInput" />
                <button v-if="hasPermission('user.manage')" class="primary" :disabled="busyAction !== ''" @click="openCreateUserModal">新增用户</button>
              </div>
            </div>

            <div v-if="filteredUsers.length" class="user-list-summary">
              <div class="user-list-stats">
                <span class="user-list-stat">结果 {{ filteredUsers.length }}/{{ userPagination.total }}</span>
                <span class="user-list-stat">启用 {{ filteredActiveUsers }}/{{ filteredUsers.length }}</span>
                <span class="user-list-stat">停用 {{ filteredSuspendedUsers }}</span>
                <span class="user-list-stat">流量 {{ filteredQuotaUsedGb }}/{{ filteredQuotaTotalGb }} GB</span>
                <span class="user-list-stat">实时 {{ filteredRealtimeUsers }}/{{ filteredUsers.length }}</span>
              </div>
            </div>

            <div v-if="filteredUsers.length" class="user-list-table">
              <div class="user-list-head">
                <span>用户名</span>
                <span>状态</span>
                <span>到期时间</span>
                <span>限速</span>
                <span>流量进度</span>
                <span>实时流量</span>
                <span>操作</span>
              </div>
              <article v-for="user in filteredUsers" :key="user.id" class="user-list-row">
                <span class="user-list-cell user-list-name" data-label="用户名">
                  <strong>{{ user.username }}</strong>
                </span>
                <span class="user-list-cell user-list-status" data-label="状态">
                  <span class="user-status" :class="user.status">{{ userStatusLabelMap[user.status] }}</span>
                </span>
                <span class="user-list-cell" data-label="到期时间">{{ user.expires_at || '长期有效' }}</span>
                <span class="user-list-cell" data-label="限速">{{ user.speed_limit_mbps || 0 }} Mbps</span>
                <span class="user-list-cell user-list-traffic" data-label="流量进度">
                  <div class="user-list-traffic-top">
                    <span class="traffic-text">{{ user.used_gb }} / {{ user.quota_gb }} GB</span>
                    <strong>{{ resolveUserQuotaPercent(user).toFixed(0) }}%</strong>
                  </div>
                  <div class="traffic-bar-wrap user-list-traffic-bar">
                    <div class="traffic-bar" :style="{ width: resolveUserQuotaPercent(user) + '%' }"></div>
                  </div>
                </span>
                <span class="user-list-cell user-list-realtime" data-label="实时流量">
                  <template v-if="userTrafficStatsMap[user.username]">
                    <span class="traffic-realtime-item" title="下行">↓ {{ userTrafficStatsMap[user.username].rx_human }}</span>
                    <span class="traffic-realtime-item" title="上行">↑ {{ userTrafficStatsMap[user.username].tx_human }}</span>
                  </template>
                  <span v-else class="traffic-text">--</span>
                </span>
                <span class="user-list-cell user-list-actions" data-label="操作">
                  <button class="secondary compact-button" :disabled="busyAction === 'user-subscription'" @click="openSubscriptionModal(user)">{{ busyAction === 'user-subscription' ? '加载中...' : '订阅信息' }}</button>
                  <button v-if="hasPermission('user.manage')" class="secondary compact-button" @click="handleEditUser(user)">编辑</button>
                  <button v-if="hasPermission('user.manage')" class="secondary compact-button danger-button" @click="handleDeleteUser(user)">删除</button>
                </span>
              </article>
            </div>
            <div v-if="userPagination.total > 0" class="audit-pagination">
              <div class="audit-page-size">
                <span>每页</span>
                <select v-model.number="userPageSize" class="compact-select" @change="handleUserPageSizeChange">
                  <option v-for="size in listPageSizeOptions" :key="size" :value="size">{{ size }}</option>
                </select>
              </div>
              <span class="audit-page-info">{{ (userPagination.page - 1) * userPagination.pageSize + 1 }}-{{ Math.min(userPagination.page * userPagination.pageSize, userPagination.total) }} / {{ userPagination.total }}</span>
              <div class="audit-page-actions">
                <button class="secondary compact-button" :disabled="userPage <= 1" @click="setUserPage(userPage - 1)">上一页</button>
                <span>{{ userPage }} / {{ userPagination.totalPages }}</span>
                <button class="secondary compact-button" :disabled="userPage >= userPagination.totalPages" @click="setUserPage(userPage + 1)">下一页</button>
              </div>
            </div>
            <div v-else class="empty-card">
              <p>当前没有用户记录。创建用户后可以直接分发账密和配额。</p>
            </div>
          </article>
        </section>
      </section>

      <section v-else-if="currentSection === 'logsAudit'" class="content-page">
        <section class="content-stack logs-audit-stack">
          <article class="card logs-filter-card">
            <div class="logs-filter-toolbar">
              <div class="logs-filter-title">
                <p class="eyebrow">日志审计</p>
                <strong>{{ selectedLogNode ? `ID #${selectedLogNode.id} · ${selectedLogNode.name}` : '未选择节点' }}</strong>
              </div>
              <div class="logs-filter-actions">
                <select v-model="selectedLogNodeId" class="search-input node-filter-select" @change="handleLogNodeChange">
                  <option :value="null">请选择节点</option>
                  <option v-for="node in nodeList" :key="node.id" :value="node.id">
                    ID #{{ node.id }} · {{ node.name }}
                  </option>
                </select>
                <div class="audit-page-actions">
                  <button class="secondary compact-button" :disabled="nodePage <= 1" @click="setNodePage(nodePage - 1)">上一页</button>
                  <span>{{ nodePage }} / {{ nodePagination.totalPages }}</span>
                  <button class="secondary compact-button" :disabled="nodePage >= nodePagination.totalPages" @click="setNodePage(nodePage + 1)">下一页</button>
                </div>
                <div class="refresh-mode-group">
                  <label class="refresh-mode-label">
                    <input type="radio" v-model="auditLogRefreshMode" value="auto" @change="handleAuditLogRefreshModeChange" />
                    实时
                  </label>
                  <label class="refresh-mode-label">
                    <input type="radio" v-model="auditLogRefreshMode" value="manual" @change="handleAuditLogRefreshModeChange" />
                    手动
                  </label>
                </div>
              </div>
            </div>
          </article>

          <section class="logs-audit-grid">
          <article class="card logs-panel-card">
            <div class="section-head section-head-wrap">
              <div>
                <p class="eyebrow">节点日志</p>
                <h3>远端运行日志</h3>
              </div>
              <button v-if="hasPermission('logs.view')" class="secondary" :disabled="busyAction !== '' || !selectedLogNodeId || realtimeLogRefreshEnabled" @click="handleRefreshLogs">刷新日志</button>
            </div>
            <pre ref="logsOutputRef" class="terminal-output large-terminal">{{
              hasPermission('logs.view') ? (logsText || '暂无日志输出') : '当前角色无权查看日志'
            }}</pre>
          </article>

          <article class="card logs-panel-card">
            <div class="section-head section-head-wrap">
              <div>
                <p class="eyebrow">审计日志</p>
                <h3>后台关键操作记录</h3>
              </div>
              <button v-if="hasPermission('audit.view')" class="secondary" :disabled="busyAction !== '' || realtimeLogRefreshEnabled" @click="handleRefreshAuditLogs">刷新审计</button>
            </div>
            <template v-if="hasPermission('audit.view') && auditLogs.length">
              <div ref="auditLogListRef" class="audit-preview-list">
                <article v-for="item in paginatedAuditLogs" :key="item.id" class="audit-preview-row">
                  <strong>{{ item.action }}</strong>
                  <span>{{ item.admin_display_name || item.admin_username || 'system' }}</span>
                  <span>{{ item.created_at }}</span>
                </article>
              </div>
              <div class="audit-pagination">
                <div class="audit-page-size">
                  <span>每页</span>
                  <select v-model.number="auditLogPageSize" class="compact-select" @change="handleAuditPageSizeChange">
                    <option v-for="size in listPageSizeOptions" :key="size" :value="size">{{ size }}</option>
                  </select>
                </div>
                <span class="audit-page-info">{{ auditLogPageStart }}-{{ auditLogPageEnd }} / {{ auditLogPagination.total }}</span>
                <div class="audit-page-actions">
                  <button class="secondary compact-button" :disabled="auditLogPage <= 1" @click="setAuditLogPage(auditLogPage - 1)">上一页</button>
                  <span>{{ auditLogPage }} / {{ auditLogTotalPages }}</span>
                  <button class="secondary compact-button" :disabled="auditLogPage >= auditLogTotalPages" @click="setAuditLogPage(auditLogPage + 1)">下一页</button>
                </div>
              </div>
            </template>
            <div v-else class="empty-card">
              <p>{{ hasPermission('audit.view') ? '当前还没有审计记录。' : '当前角色无权查看审计日志。' }}</p>
            </div>
          </article>
          </section>
        </section>
      </section>

      <section v-else-if="currentSection === 'permissions'" class="content-page">
        <section class="content-stack">
          <article class="card">
            <div class="section-head section-head-wrap">
              <div>
                <p class="eyebrow">权限控制</p>
                <h3>管理员角色与操作范围</h3>
              </div>
              <div class="hero-actions">
                <button class="secondary" :disabled="busyAction !== ''" @click="() => loadAdmins()">刷新列表</button>
                <button class="primary" :disabled="busyAction !== ''" @click="openCreateAdminModal">新增管理员</button>
              </div>
            </div>

            <div v-if="admins.length" class="entity-list">
              <article v-for="item in admins" :key="item.id" class="entity-row entity-row-wide">
                <div class="entity-main">
                  <div class="entity-title-line">
                    <strong>{{ item.display_name }}</strong>
                    <span class="pill">{{ adminRoleLabelMap[item.role] }}</span>
                    <span class="user-status" :class="item.status === 'active' ? 'active' : 'suspended'">
                      {{ adminStatusLabelMap[item.status] }}
                    </span>
                  </div>
                  <p class="entity-copy">{{ item.username }} · 最近登录 {{ item.last_login_at || '暂无' }}</p>
                  <div class="entity-meta-grid">
                    <span>创建时间: {{ item.created_at }}</span>
                    <span>更新时间: {{ item.updated_at }}</span>
                  </div>
                </div>
                <div class="table-actions">
                  <button class="secondary compact-button" @click="handleEditAdmin(item)">编辑</button>
                  <button class="secondary compact-button danger-button" @click="handleDeleteAdmin(item)">删除</button>
                </div>
              </article>
            </div>
            <div v-if="adminPagination.total > 0" class="audit-pagination">
              <div class="audit-page-size">
                <span>每页</span>
                <select v-model.number="adminPageSize" class="compact-select" @change="handleAdminPageSizeChange">
                  <option v-for="size in listPageSizeOptions" :key="size" :value="size">{{ size }}</option>
                </select>
              </div>
              <span class="audit-page-info">{{ (adminPagination.page - 1) * adminPagination.pageSize + 1 }}-{{ Math.min(adminPagination.page * adminPagination.pageSize, adminPagination.total) }} / {{ adminPagination.total }}</span>
              <div class="audit-page-actions">
                <button class="secondary compact-button" :disabled="adminPage <= 1" @click="setAdminPage(adminPage - 1)">上一页</button>
                <span>{{ adminPage }} / {{ adminPagination.totalPages }}</span>
                <button class="secondary compact-button" :disabled="adminPage >= adminPagination.totalPages" @click="setAdminPage(adminPage + 1)">下一页</button>
              </div>
            </div>
            <div v-else class="empty-card">
              <p>当前还没有额外管理员账号。</p>
            </div>
          </article>

          <article class="card">
            <div class="section-head">
              <div>
                <p class="eyebrow">角色说明</p>
                <h3>权限分层建议</h3>
              </div>
            </div>
            <ul class="check-list">
              <li>超级管理员：拥有所有节点、用户、背景与管理员管理能力。</li>
              <li>运维管理员：可管理节点、用户、安装、启停和配置下发。</li>
              <li>审计员：可查看节点、日志、审计与面板状态，但不能写入。</li>
              <li>只读成员：可查看基础状态和用户列表，敏感连接信息会被脱敏。</li>
            </ul>
          </article>
        </section>
      </section>

      <section v-else-if="currentSection === 'notifications'" class="content-page">
        <section class="content-stack">
          <article class="hero-card mail-hero-card">
            <div>
              <p class="eyebrow">邮件通知</p>
              <h2>统一配置邮件通知通道</h2>
            </div>
            <span class="mail-status-pill" :class="{ active: notificationSettingsForm.smtp_enabled }">
              {{ notificationSettingsForm.smtp_enabled ? '已启用' : '未启用' }}
            </span>
            <div class="hero-actions">
              <button class="secondary" :disabled="busyAction === 'notification-test'" @click="handleSendTestNotification">发送测试</button>
              <button class="primary" :disabled="busyAction === 'notification-settings'" @click="handleSaveNotificationSettings">保存通知</button>
            </div>
          </article>

          <section class="mail-notification-grid">
            <article class="card mail-config-card mail-enable-card">
              <label class="config-field inline-check-field">
                <input v-model="notificationSettingsForm.smtp_enabled" type="checkbox" />
                <span class="meta-label">启用邮件通知</span>
              </label>
            </article>

            <article class="card mail-config-card">
              <div class="section-head">
                <div>
                  <p class="eyebrow">服务器</p>
                  <h3>SMTP 连接配置</h3>
                </div>
              </div>
              <div class="background-config-grid mail-fields-grid">
              <label class="config-field">
                <span class="meta-label">SMTP 服务器</span>
                <input v-model="notificationSettingsForm.smtp_host" class="background-select" type="text" placeholder="smtp.example.com" />
              </label>
              <label class="config-field">
                <span class="meta-label">端口</span>
                <input v-model.number="notificationSettingsForm.smtp_port" class="background-select" type="number" min="1" max="65535" />
              </label>
              <label class="config-field">
                <span class="meta-label">加密方式</span>
                <select v-model="notificationSettingsForm.smtp_encryption" class="background-select">
                  <option value="tls">STARTTLS</option>
                  <option value="ssl">SSL</option>
                  <option value="none">不加密</option>
                </select>
              </label>
              </div>
            </article>

            <article class="card mail-config-card">
              <div class="section-head">
                <div>
                  <p class="eyebrow">账号</p>
                  <h3>认证与收发件人</h3>
                </div>
              </div>
              <div class="background-config-grid mail-fields-grid">
              <label class="config-field">
                <span class="meta-label">SMTP 用户名</span>
                <input v-model="notificationSettingsForm.smtp_username" class="background-select" type="text" autocomplete="off" />
              </label>
              <label class="config-field">
                <span class="meta-label">SMTP 密码</span>
                <input
                  v-model="notificationSettingsForm.smtp_password"
                  class="background-select"
                  type="password"
                  autocomplete="new-password"
                  :placeholder="notificationSettingsForm.smtp_password_configured ? '已保存，留空不修改' : '请输入 SMTP 密码'"
                />
              </label>
              <label class="config-field">
                <span class="meta-label">发件邮箱</span>
                <input v-model="notificationSettingsForm.smtp_from_email" class="background-select" type="email" placeholder="noreply@example.com" />
              </label>
              <label class="config-field">
                <span class="meta-label">发件名称</span>
                <input v-model="notificationSettingsForm.smtp_from_name" class="background-select" type="text" placeholder="Hysteria2 Panel" />
              </label>
              <label class="config-field">
                <span class="meta-label">接收邮箱</span>
                <input v-model="notificationSettingsForm.smtp_notify_email" class="background-select" type="email" placeholder="admin@example.com" />
              </label>
            </div>
            </article>
          </section>

        </section>
      </section>

      <section v-else-if="currentSection === 'appearance'" class="content-page">
        <section class="content-stack appearance-stack">
          <article class="hero-card appearance-hero-card">
            <div>
              <p class="eyebrow">综合配置</p>
              <h2>综合配置</h2>
              <p class="hero-copy">左侧集中编辑配置，右侧直接查看登录页效果和当前生效摘要。</p>
            </div>
            <div class="appearance-hero-pills">
              <div class="appearance-pill">
                <span>站点标题</span>
                <strong>{{ appearanceSiteTitle }}</strong>
              </div>
              <div class="appearance-pill">
                <span>Mock 面板</span>
                <strong>{{ appearanceMockSummary }}</strong>
              </div>
              <div class="appearance-pill">
                <span>登录保护</span>
                <strong>{{ appearanceSecuritySummary }}</strong>
              </div>
            </div>
          </article>

          <section class="appearance-layout">
            <div class="appearance-main">
              <article class="card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">站点配置</p>
                    <h3>基础信息</h3>
                    <p class="appearance-card-copy">维护面板标题、对外 API 地址和浏览器图标。</p>
                  </div>
                  <div class="hero-actions">
                    <button class="primary" :disabled="busyAction === 'system-settings'" @click="handleSaveSystemSettings">保存基础配置</button>
                  </div>
                </div>

                <div class="background-config-grid appearance-form-grid">
                  <label class="config-field">
                    <span class="meta-label">面板标题</span>
                    <input
                      v-model="systemSettingsForm.site_title"
                      class="background-select"
                      type="text"
                      placeholder="Hysteria2 Panel"
                    />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">面板图标</span>
                    <input
                      v-model="systemSettingsForm.site_icon_url"
                      class="background-select"
                      type="text"
                      placeholder="https://example.com/icon.png"
                    />
                  </label>
                  <label class="config-field wide-field">
                    <span class="meta-label">面板公网 API 地址</span>
                    <input
                      v-model="systemSettingsForm.public_api_base_url"
                      class="background-select"
                      type="text"
                      placeholder="https://panel.example.com/api"
                    />
                  </label>
                </div>
              </article>

              <article class="card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">登录页背景</p>
                    <h3>视觉配置</h3>
                    <p class="appearance-card-copy">支持自定义背景图，可随时恢复默认效果。</p>
                  </div>
                  <div class="hero-actions">
                    <button class="secondary" :disabled="busyAction === 'system-settings'" @click="handleResetLoginBackground">恢复默认</button>
                    <button class="primary" :disabled="busyAction === 'system-settings'" @click="handleSaveLoginBackground">保存背景</button>
                  </div>
                </div>

                <div class="background-config-grid appearance-form-grid">
                  <label class="config-field wide-field">
                    <span class="meta-label">背景图片 URL</span>
                    <input
                      v-model="backgroundForm.customUrl"
                      class="background-select"
                      type="text"
                      placeholder="https://example.com/background.jpg"
                    />
                  </label>
                </div>
              </article>

              <article class="card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">Mock 面板</p>
                    <h3>演示数据</h3>
                    <p class="appearance-card-copy">用于演示后台效果，节点和用户数据会根据下面的参数动态生成。</p>
                  </div>
                  <div class="hero-actions">
                    <button class="primary" :disabled="busyAction === 'system-settings'" @click="handleSaveSystemSettings">保存 Mock 配置</button>
                  </div>
                </div>

                <div class="background-config-grid appearance-form-grid">
                  <label class="config-field inline-check-field wide-field appearance-toggle-field">
                    <input v-model="systemSettingsForm.mock_panel_enabled" type="checkbox" />
                    <span class="meta-label">启用 Mock 数据面板</span>
                  </label>
                  <label class="config-field">
                    <span class="meta-label">Mock 节点数</span>
                    <input v-model.number="systemSettingsForm.mock_node_count" class="background-select" type="number" min="1" max="200" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">Mock 用户数</span>
                    <input v-model.number="systemSettingsForm.mock_user_count" class="background-select" type="number" min="1" max="5000" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">运行中节点数</span>
                    <input v-model.number="systemSettingsForm.mock_running_node_count" class="background-select" type="number" min="0" max="200" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">降级节点数</span>
                    <input v-model.number="systemSettingsForm.mock_degraded_node_count" class="background-select" type="number" min="0" max="200" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">停止节点数</span>
                    <input v-model.number="systemSettingsForm.mock_stopped_node_count" class="background-select" type="number" min="0" max="200" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">停用用户数</span>
                    <input v-model.number="systemSettingsForm.mock_suspended_user_count" class="background-select" type="number" min="0" max="5000" />
                  </label>
                </div>
              </article>

              <article class="card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">登录安全</p>
                    <h3>防暴力破解</h3>
                    <p class="appearance-card-copy">控制失败次数、统计窗口和锁定时长。</p>
                  </div>
                  <div class="hero-actions">
                    <button class="primary" :disabled="busyAction === 'system-settings'" @click="handleSaveSystemSettings">保存安全配置</button>
                  </div>
                </div>

                <div class="background-config-grid security-config-grid appearance-form-grid">
                  <label class="config-field inline-check-field wide-field appearance-toggle-field">
                    <input v-model="systemSettingsForm.bruteforce_enabled" type="checkbox" />
                    <span class="meta-label">启用登录防暴力破解</span>
                  </label>
                  <label class="config-field">
                    <span class="meta-label">最大失败次数</span>
                    <input v-model.number="systemSettingsForm.bruteforce_max_attempts" class="background-select" type="number" min="1" max="30" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">统计窗口（分钟）</span>
                    <input v-model.number="systemSettingsForm.bruteforce_window_minutes" class="background-select" type="number" min="1" max="1440" />
                  </label>
                  <label class="config-field">
                    <span class="meta-label">锁定时长（分钟）</span>
                    <input v-model.number="systemSettingsForm.bruteforce_lock_minutes" class="background-select" type="number" min="1" max="1440" />
                  </label>
                </div>
              </article>
            </div>

            <aside class="appearance-side">
              <article class="card appearance-preview-card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">实时预览</p>
                    <h3>登录页效果</h3>
                  </div>
                </div>

                <div class="login-background-preview appearance-login-preview" :style="backgroundPreviewStyle">
                  <div class="preview-login-card appearance-preview-login-card">
                    <div class="appearance-preview-brand">
                      <img
                        v-if="systemSettingsForm.site_icon_url.trim()"
                        class="appearance-brand-icon"
                        :src="systemSettingsForm.site_icon_url.trim()"
                        alt="站点图标预览"
                      />
                      <span v-else class="preview-mark"></span>
                      <div>
                        <strong>{{ appearanceSiteTitle }}</strong>
                        <p>{{ appearanceApiBaseUrl }}</p>
                      </div>
                    </div>
                  </div>
                </div>
              </article>

              <article class="card appearance-summary-card">
                <div class="section-head section-head-wrap">
                  <div>
                    <p class="eyebrow">当前摘要</p>
                    <h3>生效状态</h3>
                  </div>
                </div>

                <div class="appearance-status-grid">
                  <div class="appearance-status-item">
                    <span class="meta-label">站点标题</span>
                    <strong>{{ appearanceSiteTitle }}</strong>
                    <small>{{ systemSettingsForm.site_icon_url.trim() ? '已配置自定义图标' : '使用默认图标' }}</small>
                  </div>
                  <div class="appearance-status-item">
                    <span class="meta-label">公网 API</span>
                    <strong>{{ appearanceApiBaseUrl }}</strong>
                    <small>节点回调与安装流程依赖该地址</small>
                  </div>
                  <div class="appearance-status-item">
                    <span class="meta-label">Mock 状态</span>
                    <strong>{{ systemSettingsForm.mock_panel_enabled ? '已启用' : '未启用' }}</strong>
                    <small>{{ appearanceMockSummary }}</small>
                  </div>
                  <div class="appearance-status-item">
                    <span class="meta-label">登录保护</span>
                    <strong>{{ systemSettingsForm.bruteforce_enabled ? '已启用' : '未启用' }}</strong>
                    <small>{{ appearanceSecuritySummary }}</small>
                  </div>
                </div>
              </article>
            </aside>
          </section>
        </section>
      </section>
    </main>

    <div v-if="showNodeModal" class="modal-overlay">
      <section class="modal-card">
        <div class="section-head section-head-wrap">
          <div>
            <p class="eyebrow">节点编辑</p>
            <h3>{{ nodeModalTitle }}</h3>
          </div>
          <div class="hero-actions">
            <button class="secondary" :disabled="busyAction === 'node-save'" @click="handleResetNodeForm">重置表单</button>
            <button class="secondary" :disabled="busyAction === 'node-save'" @click="closeNodeModal">关闭</button>
            <button class="primary" :disabled="busyAction === 'node-save'" @click="handleSaveNode">{{ nodeSubmitLabel }}</button>
          </div>
        </div>

        <div class="form-grid two-up-grid modal-form-grid">
          <label class="form-field">
            <span>节点名称</span>
            <input v-model="nodeForm.name" type="text" placeholder="default-node" />
          </label>
          <label class="form-field">
            <span>服务器地址</span>
            <input v-model="nodeForm.host" type="text" placeholder="1.2.3.4 或 server.example.com" />
          </label>
          <label class="form-field">
            <span>SSH 端口</span>
            <input v-model.number="nodeForm.ssh_port" type="number" min="1" />
          </label>
          <label class="form-field">
            <span>SSH 用户</span>
            <input v-model="nodeForm.ssh_username" type="text" placeholder="root" />
          </label>
          <label class="form-field">
            <span>认证方式</span>
            <select v-model="nodeForm.ssh_auth_type">
              <option value="password">密码</option>
              <option value="key">私钥</option>
            </select>
          </label>
          <label class="form-field password-field" v-if="nodeForm.ssh_auth_type === 'password'">
            <span>SSH 密码</span>
            <div class="password-control">
              <input v-model="nodeForm.ssh_password" class="password-input" :type="isPasswordVisible('nodeSsh') ? 'text' : 'password'" />
              <button
                class="password-toggle"
                :class="{ active: isPasswordVisible('nodeSsh') }"
                type="button"
                :aria-label="isPasswordVisible('nodeSsh') ? '隐藏密码' : '显示密码'"
                :title="isPasswordVisible('nodeSsh') ? '隐藏密码' : '显示密码'"
                @click="togglePasswordVisibility('nodeSsh')"
              >
                <svg v-if="isPasswordVisible('nodeSsh')" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                  <path d="M5 4L19 20" />
                </svg>
              </button>
            </div>
          </label>
          <div v-else class="form-field key-upload-field">
            <span>SSH 私钥</span>
            <div class="key-upload-panel">
              <input ref="nodePrivateKeyInput" class="key-upload-native" type="file" accept=".pem,.key,.rsa" @change="handleNodePrivateKeyFileChange" />
              <div class="key-upload-summary" :class="{ 'key-upload-summary-ready': nodeForm.ssh_private_key_uploaded || Boolean(nodePrivateKeyFile) }">
                <p class="key-upload-name">{{ nodePrivateKeyFile ? nodePrivateKeyFile.name : (nodeForm.ssh_private_key_name || '未选择任何文件') }}</p>
                <button
                  class="secondary key-upload-trigger"
                  :disabled="nodeKeyUploading || busyAction === 'node-save'"
                  type="button"
                  @click="openNodePrivateKeyPicker"
                >
                  点击上传
                </button>
              </div>
            </div>
          </div>
          <label class="form-field password-field">
            <span>Sudo 密码</span>
            <div class="password-control">
              <input
                v-model="nodeForm.sudo_password"
                class="password-input"
                :type="isPasswordVisible('nodeSudo') ? 'text' : 'password'"
                placeholder="如与 SSH 密码一致可重复填写"
              />
              <button
                class="password-toggle"
                :class="{ active: isPasswordVisible('nodeSudo') }"
                type="button"
                :aria-label="isPasswordVisible('nodeSudo') ? '隐藏密码' : '显示密码'"
                :title="isPasswordVisible('nodeSudo') ? '隐藏密码' : '显示密码'"
                @click="togglePasswordVisibility('nodeSudo')"
              >
                <svg v-if="isPasswordVisible('nodeSudo')" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                  <path d="M5 4L19 20" />
                </svg>
              </button>
            </div>
          </label>
          <label class="form-field">
            <span>监听端口</span>
            <input v-model.number="nodeForm.listen_port" type="number" min="1" />
          </label>
          <label class="form-field">
            <span>证书模式</span>
            <select v-model="nodeForm.tls_mode">
              <option value="self_signed">自签证书</option>
              <option value="acme">ACME</option>
            </select>
          </label>
          <label class="form-field">
            <span>运维模式</span>
            <select v-model="nodeForm.manage_mode">
              <option value="agent">Agent</option>
              <option value="ssh">SSH</option>
            </select>
          </label>
          <label class="form-field">
            <span>Agent 状态</span>
            <select v-model="nodeForm.agent_enabled" :disabled="nodeForm.manage_mode !== 'agent'">
              <option :value="true">启用</option>
              <option :value="false">停用</option>
            </select>
          </label>
          <label class="form-field">
            <span>域名</span>
            <input v-model="nodeForm.domain" type="text" placeholder="hy2.example.com" />
          </label>
          <label class="form-field" v-if="nodeForm.tls_mode === 'acme'">
            <span>ACME 邮箱</span>
            <input v-model="nodeForm.acme_email" type="email" placeholder="admin@example.com" />
          </label>
        </div>

        <div class="advanced-toggle-wrap">
          <button class="secondary advanced-toggle-button" :disabled="busyAction === 'node-save'" @click="showNodeAdvancedOptions = !showNodeAdvancedOptions">
            {{ showNodeAdvancedOptions ? '收起高级选项' : '展开高级选项' }}
          </button>
        </div>

        <div v-if="showNodeAdvancedOptions" class="advanced-panel">
          <div class="section-head">
            <div>
              <p class="eyebrow">高级选项</p>
              <h3>完整节点配置</h3>
            </div>
          </div>

          <div class="form-grid two-up-grid modal-form-grid advanced-form-grid">
            <label class="form-field">
              <span>服务名称</span>
              <input v-model="nodeForm.service_name" type="text" placeholder="hysteria-server" />
            </label>
            <label class="form-field">
              <span>配置路径</span>
              <input v-model="nodeForm.config_path" type="text" placeholder="/etc/hysteria/config.yaml" />
            </label>
            <label class="form-field wide-field">
              <span>安装脚本</span>
              <input v-model="nodeForm.install_script" type="text" />
            </label>
            <label class="form-field">
              <span>统计监听</span>
              <input v-model="nodeForm.traffic_stats_listen" type="text" placeholder="127.0.0.1:9999" />
            </label>
            <label class="form-field password-field">
              <span>统计密钥</span>
              <div class="password-control">
                <input
                  v-model="nodeForm.traffic_stats_secret"
                  class="password-input"
                  :type="isPasswordVisible('nodeTrafficSecret') ? 'text' : 'password'"
                  placeholder="留空则沿用旧值或回退为 Obfs 密码"
                />
                <button
                  class="password-toggle"
                  :class="{ active: isPasswordVisible('nodeTrafficSecret') }"
                  type="button"
                  :aria-label="isPasswordVisible('nodeTrafficSecret') ? '隐藏密码' : '显示密码'"
                  :title="isPasswordVisible('nodeTrafficSecret') ? '隐藏密码' : '显示密码'"
                  @click="togglePasswordVisibility('nodeTrafficSecret')"
                >
                  <svg v-if="isPasswordVisible('nodeTrafficSecret')" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                    <circle cx="12" cy="12" r="2.6" />
                  </svg>
                  <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                    <circle cx="12" cy="12" r="2.6" />
                    <path d="M5 4L19 20" />
                  </svg>
                </button>
              </div>
            </label>
            <label class="form-field">
              <span>Obfs 密码</span>
              <input v-model="nodeForm.obfs_password" type="text" placeholder="用于 salamander 混淆" />
            </label>
            <label class="form-field" v-if="nodeForm.tls_mode === 'self_signed'">
              <span>证书路径</span>
              <input v-model="nodeForm.tls_cert_path" type="text" placeholder="/etc/hysteria/server.crt" />
            </label>
            <label class="form-field" v-if="nodeForm.tls_mode === 'self_signed'">
              <span>TLS 私钥路径</span>
              <input v-model="nodeForm.tls_key_path" type="text" placeholder="/etc/hysteria/server.key" />
            </label>
            <label class="form-field wide-field">
              <span>Masquerade URL</span>
              <input v-model="nodeForm.masquerade_url" type="text" placeholder="https://www.cloudflare.com" />
            </label>
            <label class="form-field">
              <span>上行带宽 Mbps</span>
              <input v-model.number="nodeForm.bandwidth_up_mbps" type="number" min="1" />
            </label>
            <label class="form-field">
              <span>下行带宽 Mbps</span>
              <input v-model.number="nodeForm.bandwidth_down_mbps" type="number" min="1" />
            </label>
            <template v-if="nodeForm.manage_mode === 'agent'">
              <label class="form-field">
                <span>上报频率秒</span>
                <input v-model.number="nodeForm.agent_report_interval_seconds" type="number" min="1" />
              </label>
              <label class="form-field">
                <span>拉任务频率秒</span>
                <input v-model.number="nodeForm.agent_task_poll_interval_seconds" type="number" min="1" />
              </label>
              <label class="form-field wide-field">
                <span>Agent 二进制路径</span>
                <input v-model="nodeForm.agent_install_path" type="text" placeholder="/usr/local/bin/mxinhy-agent" />
              </label>
              <label class="form-field wide-field">
                <span>Agent 配置路径</span>
                <input v-model="nodeForm.agent_config_path" type="text" placeholder="/etc/mxinhy-agent.json" />
              </label>
              <label class="form-field">
                <span>Agent 服务名</span>
                <input v-model="nodeForm.agent_service_name" type="text" placeholder="mxinhy-agent" />
              </label>
            </template>
          </div>
        </div>
      </section>
    </div>

    <div v-if="showUserModal" class="modal-overlay">
      <section class="modal-card">
        <div class="section-head section-head-wrap">
          <div>
            <p class="eyebrow">用户编辑</p>
            <h3>{{ userModalTitle }}</h3>
          </div>
          <div class="hero-actions">
            <button class="secondary" :disabled="busyAction === 'user-save' || busyAction === 'user-create'" @click="resetUserEditor">清空表单</button>
            <button class="secondary" :disabled="busyAction === 'user-save' || busyAction === 'user-create'" @click="closeUserModal">关闭</button>
            <button class="primary" :disabled="busyAction === 'user-save' || busyAction === 'user-create'" @click="handleSubmitUser">
              {{ editingUserId ? '保存修改' : '创建用户' }}
            </button>
          </div>
        </div>

        <div class="form-grid two-up-grid modal-form-grid">
          <label class="form-field">
            <span>用户名</span>
            <input v-model="userForm.username" type="text" placeholder="client-001" />
          </label>
          <label class="form-field">
            <span>认证密码</span>
            <input v-model="userForm.auth_password" type="text" placeholder="分发给客户端的密码" />
          </label>
          <label class="form-field">
            <span>状态</span>
            <select v-model="userForm.status">
              <option value="active">正常</option>
              <option value="suspended">停用</option>
            </select>
          </label>
          <label class="form-field">
            <span>限速 Mbps</span>
            <input v-model.number="userForm.speed_limit_mbps" type="number" min="0" />
          </label>
          <label class="form-field">
            <span>总配额 GB</span>
            <input v-model.number="userForm.quota_gb" type="number" min="0" />
          </label>
          <label class="form-field">
            <span>已用流量 GB</span>
            <input v-model.number="userForm.used_gb" type="number" min="0" />
          </label>
          <label class="form-field wide-field">
            <span>到期时间</span>
            <input v-model="userForm.expires_at" type="datetime-local" />
          </label>
        </div>
      </section>
    </div>

    <div v-if="showAdminModal" class="modal-overlay">
      <section class="modal-card">
        <div class="section-head section-head-wrap">
          <div>
            <p class="eyebrow">管理员编辑</p>
            <h3>{{ adminModalTitle }}</h3>
          </div>
          <div class="hero-actions">
            <button class="secondary" :disabled="busyAction === 'admin-save' || busyAction === 'admin-create'" @click="closeAdminModal">关闭</button>
            <button class="primary" :disabled="busyAction === 'admin-save' || busyAction === 'admin-create'" @click="handleSubmitAdmin">
              {{ editingAdminId ? '保存管理员' : '创建管理员' }}
            </button>
          </div>
        </div>

        <div class="form-grid two-up-grid modal-form-grid">
          <label class="form-field">
            <span>用户名</span>
            <input
              v-model="adminForm.username"
              type="text"
              inputmode="text"
              pattern="[A-Za-z0-9]+"
              title="用户名仅支持英文和数字"
              @input="handleAdminUsernameInput"
            />
          </label>
          <label class="form-field">
            <span>显示名称</span>
            <input v-model="adminForm.display_name" type="text" />
          </label>
          <label class="form-field">
            <span>角色</span>
            <select v-model="adminForm.role">
              <option value="super_admin">超级管理员</option>
              <option value="operator">运维管理员</option>
              <option value="auditor">审计员</option>
              <option value="viewer">只读成员</option>
            </select>
          </label>
          <label class="form-field">
            <span>状态</span>
            <select v-model="adminForm.status">
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </select>
          </label>
          <label class="form-field wide-field password-field">
            <span>{{ editingAdminId ? '新密码（留空则不修改）' : '登录密码' }}</span>
            <div class="password-control">
              <input v-model="adminForm.password" class="password-input" :type="isPasswordVisible('admin') ? 'text' : 'password'" />
              <button
                class="password-toggle"
                :class="{ active: isPasswordVisible('admin') }"
                type="button"
                :aria-label="isPasswordVisible('admin') ? '隐藏密码' : '显示密码'"
                :title="isPasswordVisible('admin') ? '隐藏密码' : '显示密码'"
                @click="togglePasswordVisibility('admin')"
              >
                <svg v-if="isPasswordVisible('admin')" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                </svg>
                <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M2 12C4.8 7.8 8.2 5.7 12 5.7C15.8 5.7 19.2 7.8 22 12C19.2 16.2 15.8 18.3 12 18.3C8.2 18.3 4.8 16.2 2 12Z" />
                  <circle cx="12" cy="12" r="2.6" />
                  <path d="M5 4L19 20" />
                </svg>
              </button>
            </div>
          </label>
        </div>
      </section>
    </div>

    <div v-if="showUserDeleteModal" class="modal-overlay">
      <section class="modal-card confirm-modal-card">
        <div class="confirm-modal-body">
          <p class="entity-copy">是否删除用户：{{ pendingDeleteUser?.username || '-' }}</p>
        </div>

        <div class="card-actions confirm-modal-actions">
          <button class="secondary" :disabled="busyAction === 'user-delete'" @click="closeUserDeleteModal">取消</button>
          <button class="secondary danger-button" :disabled="busyAction === 'user-delete'" @click="confirmDeleteUser">{{ busyAction === 'user-delete' ? '确认中...' : '确认' }}</button>
        </div>
      </section>
    </div>

    <div v-if="showNodeDeleteModal" class="modal-overlay">
      <section class="modal-card confirm-modal-card">
        <div class="confirm-modal-body">
          <p class="entity-copy">是否删除节点：{{ pendingDeleteNode?.name || '-' }}</p>
        </div>

        <div class="card-actions confirm-modal-actions">
          <button class="secondary" :disabled="busyAction === 'node-delete'" @click="closeNodeDeleteModal">取消</button>
          <button class="secondary danger-button" :disabled="busyAction === 'node-delete'" @click="confirmDeleteNode">{{ busyAction === 'node-delete' ? '确认中...' : '确认' }}</button>
        </div>
      </section>
    </div>

    <div v-if="showAdminDeleteModal" class="modal-overlay">
      <section class="modal-card confirm-modal-card">
        <div class="confirm-modal-body">
          <p class="entity-copy">是否删除管理员：{{ pendingDeleteAdmin?.username || '-' }}</p>
        </div>

        <div class="card-actions confirm-modal-actions">
          <button class="secondary" :disabled="busyAction === 'admin-delete'" @click="closeAdminDeleteModal">取消</button>
          <button class="secondary danger-button" :disabled="busyAction === 'admin-delete'" @click="confirmDeleteAdmin">{{ busyAction === 'admin-delete' ? '确认中...' : '确认' }}</button>
        </div>
      </section>
    </div>

    <div v-if="showSubscriptionModal" class="modal-overlay">
      <section class="modal-card qr-modal-card">
        <div class="section-head section-head-wrap">
          <div>
            <p class="eyebrow">订阅信息</p>
            <h3>{{ qrModalTitle }}</h3>
          </div>
          <div class="hero-actions">
            <button class="secondary" @click="copySubscriptionLink">复制链接</button>
            <button class="secondary" @click="closeSubscriptionModal">关闭</button>
          </div>
        </div>

        <div class="qr-modal-body">
          <div v-if="subscriptionInfo" class="user-list-summary">
            <div class="user-list-stats">
              <span class="user-list-stat">节点 {{ subscriptionInfo.node_count }}</span>
              <span class="user-list-stat">{{ subscriptionInfo.nodes.map((item) => item.name).join(' / ') }}</span>
            </div>
          </div>
          <img class="qr-image" :src="buildQrCodeUrl(qrModalValue)" alt="订阅二维码" />
          <textarea class="config-editor qr-link-box" :value="qrModalValue" readonly spellcheck="false"></textarea>
        </div>
      </section>
    </div>
  </div>
</template>
