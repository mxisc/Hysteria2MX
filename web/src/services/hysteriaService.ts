import type {
  AdminUser,
  AdminUserPayload,
  AuditLogItem,
  CommandJob,
  HysteriaNodeConfig,
  HysteriaOnlineClient,
  HysteriaNodePayload,
  HysteriaPanelState,
  PaginatedResult,
  HysteriaStreamItem,
  NotificationSettings,
  PublicAppSettings,
  SystemHealth,
  SystemSettings,
  SshKeyUploadResult,
  TrafficOverviewResponse,
  UserSubscriptionInfo,
  HysteriaUser,
  HysteriaUserPayload,
  RemoteCommandResult,
  ServiceAction,
  UserTrafficStats,
} from '../types'
import {
  getMockAuditLogs,
  getMockLogs,
  getMockNodes,
  getMockOnlineClients,
  getMockPanelState,
  getMockStreams,
  getMockTrafficHistory,
  getMockUsers,
  getMockUserTrafficStats,
  mockServiceResult,
  resolveMockPanelEnabled,
  selectMockNode,
} from './mockPanelData'

const API_BASE = '/api'

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data: T
}

async function requestRaw<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    credentials: 'include',
    ...init,
  })

  const text = await response.text()
  const payload = (text ? JSON.parse(text) : {}) as Partial<ApiEnvelope<T>>

  if (!response.ok || !payload.success) {
    throw new Error(payload.message || '请求失败')
  }

  return payload.data as T
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  return requestRaw<T>(path, {
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers ?? {}),
    },
    ...init,
  })
}

async function shouldUseMockPanel(): Promise<boolean> {
  return resolveMockPanelEnabled(request)
}

function withNodeQuery(path: string, nodeId?: number | null): string {
  if (!nodeId) {
    return path
  }
  return `${path}${path.includes('?') ? '&' : '?'}node_id=${nodeId}`
}

export async function fetchPanelState(): Promise<HysteriaPanelState> {
  if (await shouldUseMockPanel()) {
    return getMockPanelState()
  }
  return request<HysteriaPanelState>('/panel/state')
}

export async function createNode(payload: HysteriaNodePayload): Promise<HysteriaNodeConfig> {
  if (await shouldUseMockPanel()) {
    return {
      ...selectMockNode(101),
      ...payload,
      id: Date.now(),
      current_node: 0,
      ssh_password: null,
      ssh_private_key_path: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
  }
  const result = await request<{ node: HysteriaNodeConfig }>('/nodes', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return result.node
}

export async function createNodeWithInstall(payload: HysteriaNodePayload): Promise<{ node: HysteriaNodeConfig; install: RemoteCommandResult }> {
  if (await shouldUseMockPanel()) {
    const node = await createNode(payload)
    return {
      node,
      install: mockServiceResult('start', node.id),
    }
  }

  return request<{ node: HysteriaNodeConfig; install: RemoteCommandResult }>('/nodes/install', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function updateNode(id: number, payload: HysteriaNodePayload): Promise<HysteriaNodeConfig> {
  if (await shouldUseMockPanel()) {
    return {
      ...selectMockNode(id),
      ...payload,
      id,
      current_node: id === getMockPanelState().currentNodeId ? 1 : 0,
      ssh_password: null,
      ssh_private_key_path: null,
      updated_at: new Date().toISOString(),
    }
  }
  const result = await request<{ node: HysteriaNodeConfig }>(`/nodes/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
  return result.node
}

export async function saveNodeConfig(payload: HysteriaNodePayload, id?: number | null): Promise<HysteriaNodeConfig> {
  return id ? updateNode(id, payload) : createNode(payload)
}

export async function uploadNodeSshKey(privateKeyFile: File): Promise<SshKeyUploadResult> {
  const formData = new FormData()
  formData.append('private_key', privateKeyFile)
  return requestRaw<SshKeyUploadResult>('/nodes/ssh-key-upload', {
    method: 'POST',
    body: formData,
  })
}

export async function deleteNode(id: number): Promise<void> {
  if (await shouldUseMockPanel()) {
    void id
    return
  }
  await request<Record<string, never>>(`/nodes/${id}`, {
    method: 'DELETE',
    body: JSON.stringify({}),
  })
}

export async function selectNode(id: number): Promise<HysteriaNodeConfig> {
  if (await shouldUseMockPanel()) {
    return selectMockNode(id)
  }
  const result = await request<{ node: HysteriaNodeConfig }>(`/nodes/${id}/select`, {
    method: 'POST',
    body: JSON.stringify({}),
  })
  return result.node
}

export async function installHysteria(nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return mockServiceResult('start', nodeId ?? undefined)
  }
  const result = await request<{ job: CommandJob }>(withNodeQuery('/hysteria/install', nodeId), {
    method: 'POST',
    body: JSON.stringify({}),
  })

  if (result.job.status === 'done' && result.job.result) {
    return result.job.result
  }
  if (result.job.status === 'error') {
    throw new Error(result.job.message || '后台任务执行失败')
  }

  return waitForJob(result.job.id)
}

export async function uninstallHysteria(nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return mockServiceResult('stop', nodeId ?? undefined)
  }
  const result = await request<{ job: CommandJob }>(withNodeQuery('/hysteria/uninstall', nodeId), {
    method: 'POST',
    body: JSON.stringify({}),
  })

  if (result.job.status === 'done' && result.job.result) {
    return result.job.result
  }
  if (result.job.status === 'error') {
    throw new Error(result.job.message || '后台任务执行失败')
  }

  return waitForJob(result.job.id)
}

export async function deployHysteriaConfig(nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return mockServiceResult('restart', nodeId ?? undefined)
  }
  return request<RemoteCommandResult>(withNodeQuery('/hysteria/deploy-config', nodeId), {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

export async function performServiceAction(action: ServiceAction, nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return mockServiceResult(action, nodeId ?? undefined)
  }
  return request<RemoteCommandResult>(withNodeQuery(`/hysteria/service/${action}`, nodeId), {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

export async function fetchLogs(nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return getMockLogs(nodeId)
  }
  const suffix = nodeId ? `?node_id=${nodeId}` : ''
  return request<RemoteCommandResult>(`/hysteria/logs${suffix}`)
}

export async function fetchOnlineClients(nodeId?: number | null): Promise<HysteriaOnlineClient[]> {
  if (await shouldUseMockPanel()) {
    return getMockOnlineClients(nodeId)
  }
  const suffix = nodeId ? `?node_id=${nodeId}` : ''
  const result = await request<PaginatedResult<HysteriaOnlineClient>>(`${`/hysteria/online${suffix}${suffix ? '&' : '?'}page=1&page_size=1`}`)
  return result.items
}

export async function fetchStreams(nodeId?: number | null): Promise<HysteriaStreamItem[]> {
  if (await shouldUseMockPanel()) {
    return getMockStreams(nodeId)
  }
  const suffix = nodeId ? `?node_id=${nodeId}` : ''
  const result = await request<PaginatedResult<HysteriaStreamItem>>(`${`/hysteria/streams${suffix}${suffix ? '&' : '?'}page=1&page_size=1`}`)
  return result.items
}

export async function fetchOnlineClientPage(nodeId?: number | null, page = 1, pageSize = 10): Promise<PaginatedResult<HysteriaOnlineClient>> {
  if (await shouldUseMockPanel()) {
    const items = getMockOnlineClients(nodeId)
    return paginateMock(items, page, pageSize)
  }
  const params = new URLSearchParams()
  if (nodeId) params.set('node_id', String(nodeId))
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  return request<PaginatedResult<HysteriaOnlineClient>>(`/hysteria/online?${params.toString()}`)
}

export async function fetchStreamPage(nodeId?: number | null, page = 1, pageSize = 10): Promise<PaginatedResult<HysteriaStreamItem>> {
  if (await shouldUseMockPanel()) {
    const items = getMockStreams(nodeId)
    return paginateMock(items, page, pageSize)
  }
  const params = new URLSearchParams()
  if (nodeId) params.set('node_id', String(nodeId))
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  return request<PaginatedResult<HysteriaStreamItem>>(`/hysteria/streams?${params.toString()}`)
}

export async function fetchUserTrafficStats(nodeId?: number | null, usernames: string[] = []): Promise<UserTrafficStats[]> {
  if (await shouldUseMockPanel()) {
    const items = getMockUserTrafficStats(nodeId)
    if (!usernames.length) return items
    const allowed = new Set(usernames)
    return items.filter((item) => allowed.has(item.username))
  }
  const params = new URLSearchParams()
  if (nodeId) params.set('node_id', String(nodeId))
  params.set('page', '1')
  params.set('page_size', String(Math.max(usernames.length || 10, 1)))
  if (usernames.length) {
    params.set('usernames', usernames.join(','))
  }
  const result = await request<PaginatedResult<UserTrafficStats>>(`/hysteria/traffic-stats?${params.toString()}`)
  return result.items
}

export async function fetchTrafficHistory(hours = 24, page = 1, pageSize = 12): Promise<TrafficOverviewResponse> {
  if (await shouldUseMockPanel()) {
    return getMockTrafficHistory(hours, page, pageSize)
  }
  const params = new URLSearchParams()
  params.set('hours', String(hours))
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  return request<TrafficOverviewResponse>(`/hysteria/traffic-history?${params.toString()}`)
}

export async function syncTrafficUsage(nodeId?: number | null): Promise<RemoteCommandResult> {
  if (await shouldUseMockPanel()) {
    return mockServiceResult('status', nodeId ?? undefined)
  }
  return request<RemoteCommandResult>(withNodeQuery('/hysteria/sync-traffic', nodeId), {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

export async function fetchAuditLogs(nodeId?: number | null, page = 1, pageSize = 10): Promise<PaginatedResult<AuditLogItem>> {
  if (await shouldUseMockPanel()) {
    const items = getMockAuditLogs(nodeId)
    return paginateMock(items, page, pageSize)
  }
  const params = new URLSearchParams()
  if (nodeId) params.set('node_id', String(nodeId))
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  return request<PaginatedResult<AuditLogItem>>(`/audit-logs?${params.toString()}`)
}

export async function fetchAdmins(page = 1, pageSize = 10): Promise<PaginatedResult<AdminUser>> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return request<PaginatedResult<AdminUser>>(`/admins?${params.toString()}`)
}

export async function createAdmin(payload: AdminUserPayload): Promise<AdminUser> {
  const result = await request<{ item: AdminUser }>('/admins', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return result.item
}

export async function updateAdmin(id: number, payload: AdminUserPayload): Promise<AdminUser> {
  const result = await request<{ item: AdminUser }>(`/admins/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
  return result.item
}

export async function deleteAdmin(id: number): Promise<void> {
  await request<Record<string, never>>(`/admins/${id}`, {
    method: 'DELETE',
    body: JSON.stringify({}),
  })
}

export async function createUser(payload: HysteriaUserPayload): Promise<HysteriaUser> {
  if (await shouldUseMockPanel()) {
    return {
      id: Date.now(),
      public_id: `usr_mock_${Date.now().toString(36)}`,
      node_id: getMockPanelState().currentNodeId ?? 101,
      ...payload,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
  }
  const result = await request<{ item: HysteriaUser }>('/users', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  return result.item
}

export async function fetchNodes(page = 1, pageSize = 10): Promise<PaginatedResult<HysteriaNodeConfig>> {
  if (await shouldUseMockPanel()) {
    return paginateMock(getMockNodes(), page, pageSize)
  }
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return request<PaginatedResult<HysteriaNodeConfig>>(`/nodes?${params.toString()}`)
}

export async function fetchUsers(page = 1, pageSize = 10, keyword = ''): Promise<PaginatedResult<HysteriaUser>> {
  if (await shouldUseMockPanel()) {
    let items = getMockUsers()
    if (keyword.trim()) {
      const lowered = keyword.trim().toLowerCase()
      items = items.filter((item: HysteriaUser) => item.username.toLowerCase().includes(lowered))
    }
    return paginateMock(items, page, pageSize)
  }
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  if (keyword.trim()) {
    params.set('keyword', keyword.trim())
  }
  return request<PaginatedResult<HysteriaUser>>(`/users?${params.toString()}`)
}

function paginateMock<T>(items: T[], page: number, pageSize: number): PaginatedResult<T> {
  const safePage = Math.max(1, page)
  const safePageSize = Math.max(1, pageSize)
  const start = (safePage - 1) * safePageSize
  return {
    items: items.slice(start, start + safePageSize),
    pagination: {
      page: safePage,
      pageSize: safePageSize,
      total: items.length,
      totalPages: Math.max(1, Math.ceil(items.length / safePageSize)),
    },
  }
}

export async function updateUser(id: number, payload: HysteriaUserPayload): Promise<HysteriaUser> {
  if (await shouldUseMockPanel()) {
    return {
      id,
      public_id: `usr_mock_${id}`,
      node_id: getMockPanelState().currentNodeId ?? 101,
      ...payload,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
  }
  const result = await request<{ item: HysteriaUser }>(`/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
  return result.item
}

export async function deleteUser(id: number): Promise<void> {
  if (await shouldUseMockPanel()) {
    void id
    return
  }
  await request<Record<string, never>>(`/users/${id}`, {
    method: 'DELETE',
    body: JSON.stringify({}),
  })
}

export async function fetchUserSubscriptionInfo(id: number): Promise<UserSubscriptionInfo> {
  if (await shouldUseMockPanel()) {
    return {
      url: `${window.location.origin}/subscription/usr_mock_${id}?token=mock-subscription-token`,
      username: `mock-user-${id}`,
      node_count: getMockNodes().length,
      nodes: getMockNodes().map((node) => ({
        id: node.id,
        name: node.name,
      })),
    }
  }
  return request<UserSubscriptionInfo>(`/users/${id}/subscription-info`)
}

export async function fetchSystemSettings(): Promise<SystemSettings> {
  return request<SystemSettings>('/system-settings')
}

export async function fetchAppSettings(): Promise<PublicAppSettings> {
  return request<PublicAppSettings>('/app-settings')
}

export async function fetchSystemHealth(): Promise<SystemHealth> {
  return request<SystemHealth>('/system-health')
}

export async function updateSystemSettings(payload: SystemSettings): Promise<SystemSettings> {
  return request<SystemSettings>('/system-settings', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function fetchNotificationSettings(): Promise<NotificationSettings> {
  return request<NotificationSettings>('/notification-settings')
}

export async function updateNotificationSettings(payload: NotificationSettings): Promise<NotificationSettings> {
  return request<NotificationSettings>('/notification-settings', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function sendTestNotification(): Promise<void> {
  await request<{ message: string }>('/notification-settings/test', {
    method: 'POST',
    body: JSON.stringify({}),
  })
}

async function fetchJob(id: string): Promise<CommandJob> {
  const result = await request<{ job: CommandJob }>(`/jobs/${id}`)
  return result.job
}

async function waitForJob(id: string): Promise<RemoteCommandResult> {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const job = await fetchJob(id)
    if (job.status === 'done') {
      if (!job.result) {
        throw new Error('任务已完成，但没有返回结果')
      }
      return job.result
    }

    if (job.status === 'error') {
      throw new Error(job.message || '后台任务执行失败')
    }

    await new Promise((resolve) => window.setTimeout(resolve, 1000))
  }

  throw new Error('任务执行超时，请稍后刷新状态确认结果')
}
