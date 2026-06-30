import type {
  AuditLogItem,
  HysteriaNodeConfig,
  HysteriaOnlineClient,
  HysteriaPanelState,
  HysteriaStreamItem,
  HysteriaUser,
  NodeTrafficHistoryItem,
  RemoteCommandResult,
  ServiceAction,
  ServiceStatus,
  TrafficOverviewResponse,
  UserTrafficStats,
} from '../types'

type MockPanelConfig = {
  enabled: boolean
  node_count: number
  user_count: number
  running_node_count: number
  degraded_node_count: number
  stopped_node_count: number
  suspended_user_count: number
}

const DEFAULT_MOCK_PANEL_CONFIG: MockPanelConfig = {
  enabled: false,
  node_count: 6,
  user_count: 32,
  running_node_count: 4,
  degraded_node_count: 1,
  stopped_node_count: 1,
  suspended_user_count: 4,
}

const nowText = '2026-06-04 20:30:00'
const NODE_PRESETS = [
  { region: 'tokyo', role: 'edge', country: 'jp' },
  { region: 'singapore', role: 'core', country: 'sg' },
  { region: 'hongkong', role: 'relay', country: 'hk' },
  { region: 'losangeles', role: 'edge', country: 'us' },
  { region: 'frankfurt', role: 'core', country: 'de' },
  { region: 'seoul', role: 'edge', country: 'kr' },
  { region: 'london', role: 'relay', country: 'uk' },
  { region: 'sydney', role: 'edge', country: 'au' },
]

let defaultMockNodeId = 101
let mockPanelConfig: MockPanelConfig | null = null

export function isMockPanelEnabled(): boolean {
  return getMockPanelConfig().enabled
}

export function applyMockPanelConfig(config: Partial<MockPanelConfig>): void {
  const nextConfig = normalizeMockPanelConfig({
    ...(mockPanelConfig ?? DEFAULT_MOCK_PANEL_CONFIG),
    ...config,
  })
  mockPanelConfig = nextConfig
  const nodes = buildMockNodes(nextConfig)
  if (!nodes.length) {
    defaultMockNodeId = 101
    return
  }
  if (!nodes.some((node) => node.id === defaultMockNodeId)) {
    defaultMockNodeId = nodes[0].id
  }
}

export async function resolveMockPanelEnabled(fetcher: <T>(path: string) => Promise<T>): Promise<boolean> {
  if (mockPanelConfig !== null) {
    return mockPanelConfig.enabled
  }

  try {
    const result = await fetcher<Partial<MockPanelConfig>>('/mock-mode')
    applyMockPanelConfig(result)
  } catch {
    applyMockPanelConfig({ enabled: false })
  }

  return isMockPanelEnabled()
}

export function getMockNodeRuntimeStatuses(): Record<number, ServiceStatus> {
  const statuses: Record<number, ServiceStatus> = {}
  for (const node of getMockNodes()) {
    statuses[node.id] = deriveNodeStatus(node)
  }
  return statuses
}

export function getMockPanelState(): HysteriaPanelState {
  const nodes = getMockNodes()
  const users = getMockUsers()
  const node = nodes.find((item) => item.id === defaultMockNodeId) ?? nodes[0]
  defaultMockNodeId = node.id

  return {
    node: { ...node, current_node: 0 },
    service: mockServiceResult('status', defaultMockNodeId),
    metrics: {
      nodeCount: nodes.length,
      userCount: users.length,
      activeUserCount: users.filter((user) => user.status === 'active').length,
      quotaTotalGb: users.reduce((total, user) => total + user.quota_gb, 0),
      quotaUsedGb: users.reduce((total, user) => total + user.used_gb, 0),
    },
  }
}

export function getMockNodes(): HysteriaNodeConfig[] {
  const nodes = buildMockNodes(getMockPanelConfig())
  return nodes.map((item) => ({ ...item, current_node: 0 }))
}

export function getMockUsers(): HysteriaUser[] {
  return buildMockUsers(getMockPanelConfig(), getMockNodes()).map((item) => ({ ...item }))
}

export function mockServiceResult(action: ServiceAction, nodeId = defaultMockNodeId): RemoteCommandResult {
  const nodes = getMockNodes()
  const users = getMockUsers()
  const node = nodes.find((item) => item.id === nodeId) ?? nodes[0]
  const status = deriveNodeStatus(node)
  const isHealthy = action === 'status' ? status === 'running' : status !== 'stopped'

  return {
    command: `mock hysteria ${action} ${node.name}`,
    output: [
      `[mock] node=${node.name}`,
      `[mock] action=${action}`,
      `[mock] status=${status}`,
      `[mock] users=${users.filter((user) => user.node_id === node.id).length}`,
      `[mock] active_users=${users.filter((user) => user.node_id === node.id && user.status === 'active').length}`,
      '[mock] this output is generated locally and no remote command was executed',
    ].join('\n'),
    exitCode: isHealthy ? 0 : 1,
  }
}

export function getMockLogs(nodeId?: number | null): RemoteCommandResult {
  const nodes = getMockNodes()
  const users = getMockUsers()
  const node = nodes.find((item) => item.id === (nodeId ?? defaultMockNodeId)) ?? nodes[0]
  const status = deriveNodeStatus(node)
  const lines = Array.from({ length: 80 }, (_, index) => {
    const level = index % 17 === 0 ? 'WARN' : status === 'degraded' && index % 9 === 0 ? 'ERROR' : 'INFO'
    const user = users[index % users.length]
    return `2026-06-04 20:${String(index % 60).padStart(2, '0')}:12 ${level} ${node.name} accepted udp session user=${user.username} rx=${(index + 3) * 3}MB tx=${(index + 2) * 2}MB`
  })

  return {
    command: `mock tail -n 80 ${node.service_name}`,
    output: lines.join('\n'),
    exitCode: status === 'stopped' ? 1 : 0,
  }
}

export function getMockOnlineClients(nodeId?: number | null): HysteriaOnlineClient[] {
  const nodes = getMockNodes()
  const users = getMockUsers()
  const targetNodeId = nodeId ?? defaultMockNodeId
  const node = nodes.find((item) => item.id === targetNodeId)
  const status = node ? deriveNodeStatus(node) : 'stopped'
  if (status === 'stopped') {
    return []
  }

  const activeUsers = users.filter((user) => user.node_id === targetNodeId && user.status === 'active')
  const divisor = status === 'degraded' ? 3 : 2
  const limit = Math.max(1, Math.min(activeUsers.length, Math.ceil(activeUsers.length / divisor)))
  return activeUsers.slice(0, Math.min(limit, 24)).map((user, index) => ({
    id: user.username,
    connections: status === 'degraded' ? 1 + (index % 2) : 1 + (index % 4),
  }))
}

export function getMockStreams(nodeId?: number | null): HysteriaStreamItem[] {
  const clients = getMockOnlineClients(nodeId)
  return clients.flatMap((client, index) =>
    Array.from({ length: client.connections }, (_, streamIndex) => ({
      state: streamIndex % 2 === 0 ? 'active' : 'idle',
      auth: client.id,
      connection: index + 1,
      stream: streamIndex + 1,
      req_addr: `198.51.100.${(index + streamIndex) % 200}:443`,
      hooked_req_addr: `203.0.113.${(index + streamIndex + 10) % 200}:443`,
      tx: (index + 1) * 1024 * 1024,
      rx: (streamIndex + 2) * 1024 * 1024,
      initial_at: nowText,
      last_active_at: nowText,
    })),
  )
}

export function getMockUserTrafficStats(nodeId?: number | null): UserTrafficStats[] {
  return getMockUsers()
    .filter((user) => !nodeId || user.node_id === nodeId)
    .slice(0, 18)
    .map((user, index) => ({
      node_id: user.node_id,
      username: user.username,
      rx: (index + 1) * 1024 * 1024 * 128,
      tx: (index + 1) * 1024 * 1024 * 72,
      rx_human: `${((index + 1) * 0.13).toFixed(2)} GB`,
      tx_human: `${((index + 1) * 0.07).toFixed(2)} GB`,
      total_human: `${((index + 1) * 0.2).toFixed(2)} GB`,
    }))
}

export function getMockTrafficHistory(hours = 24, page = 1, pageSize = 12): TrafficOverviewResponse {
  const nodes = getMockNodes()
  const users = getMockUsers()
  const samples = Math.max(6, Math.min(hours, 24))
  const nodeSeriesByNode = nodes.map((_, nodeIndex) =>
    Array.from({ length: samples }, (_, index) => {
      const wave = Math.sin((index + nodeIndex) / 3) * 0.18 + 1
      const baseRx = (nodeIndex + 4) * 1024 * 1024 * 1024
      const baseTx = (nodeIndex + 2) * 1024 * 1024 * 1024
      return {
        total_rx: Math.round(baseRx * (index + 1) * wave),
        total_tx: Math.round(baseTx * (index + 1) * (2 - wave)),
        recorded_at: `2026-06-04 ${String(index).padStart(2, '0')}:00:00`,
      }
    }),
  )
  const nodeSeries = nodeSeriesByNode.flat()

  const series: NodeTrafficHistoryItem[] = Array.from({ length: samples }, (_, index) => {
    const recordedAt = `2026-06-04 ${String(index).padStart(2, '0')}:00:00`
    return nodeSeries
      .filter((item) => item.recorded_at === recordedAt)
      .reduce<NodeTrafficHistoryItem>(
        (total, item) => ({
          recorded_at: recordedAt,
          total_rx: total.total_rx + item.total_rx,
          total_tx: total.total_tx + item.total_tx,
        }),
        {
          recorded_at: recordedAt,
          total_rx: 0,
          total_tx: 0,
        },
      )
  })

  const items = nodes.map((node, index) => {
    const nodeSeries = nodeSeriesByNode[index] ?? []
    const totals = nodeSeries.reduce(
      (summary, item, sampleIndex) => {
        const previous = nodeSeries[sampleIndex - 1]
        summary.totalRx += Math.max(0, item.total_rx - (previous?.total_rx ?? 0))
        summary.totalTx += Math.max(0, item.total_tx - (previous?.total_tx ?? 0))
        return summary
      },
      { totalRx: 0, totalTx: 0 },
    )

    return {
      id: node.id,
      name: node.name,
      host: node.host,
      onlineCount: getMockOnlineClients(node.id).length,
      userCount: users.filter((user) => user.node_id === node.id).length,
      totalRx: totals.totalRx,
      totalTx: totals.totalTx,
      recordedAt: `2026-06-04 ${String(samples - 1).padStart(2, '0')}:00:00`,
    }
  })

  const safePage = Math.max(1, page)
  const safePageSize = Math.max(1, pageSize)
  const start = (safePage - 1) * safePageSize

  return {
    series,
    items: items.slice(start, start + safePageSize),
    pagination: {
      page: safePage,
      pageSize: safePageSize,
      total: items.length,
      totalPages: Math.max(1, Math.ceil(items.length / safePageSize)),
    },
  }
}

export function getMockAuditLogs(nodeId?: number | null): AuditLogItem[] {
  const nodes = getMockNodes()
  const actions = ['node.update', 'user.create', 'hysteria.service.restart', 'system_settings.update', 'notification_settings.test']
  const targetNodes = nodeId ? nodes.filter((node) => node.id === nodeId) : nodes
  return Array.from({ length: 86 }, (_, index) => {
    const node = targetNodes[index % targetNodes.length] ?? nodes[0]
    return {
      id: 5000 + index,
      admin_id: 1,
      admin_username: 'admin',
      admin_display_name: '系统管理员',
      action: actions[index % actions.length],
      target_type: index % 2 === 0 ? 'node' : 'system',
      target_id: String(node.id),
      ip_address: '203.0.113.10',
      details: { node: node.name, mock: true },
      created_at: `2026-06-04 ${String(20 - (index % 8)).padStart(2, '0')}:${String(index % 60).padStart(2, '0')}:00`,
    }
  })
}

function getMockPanelConfig(): MockPanelConfig {
  if (mockPanelConfig === null) {
    mockPanelConfig = { ...DEFAULT_MOCK_PANEL_CONFIG }
  }
  return mockPanelConfig
}

function normalizeMockPanelConfig(config: Partial<MockPanelConfig>): MockPanelConfig {
  const nodeCount = clampNumber(config.node_count, 1, 200, DEFAULT_MOCK_PANEL_CONFIG.node_count)
  const userCount = clampNumber(config.user_count, 1, 5000, DEFAULT_MOCK_PANEL_CONFIG.user_count)
  let runningNodeCount = clampNumber(config.running_node_count, 0, nodeCount, DEFAULT_MOCK_PANEL_CONFIG.running_node_count)
  let degradedNodeCount = clampNumber(config.degraded_node_count, 0, nodeCount, DEFAULT_MOCK_PANEL_CONFIG.degraded_node_count)
  let stoppedNodeCount = clampNumber(config.stopped_node_count, 0, nodeCount, DEFAULT_MOCK_PANEL_CONFIG.stopped_node_count)
  const totalNodeStatuses = runningNodeCount + degradedNodeCount + stoppedNodeCount
  if (totalNodeStatuses > nodeCount) {
    let overflow = totalNodeStatuses - nodeCount
    const reduce = (value: number) => {
      if (overflow <= 0 || value <= 0) {
        return value
      }
      const nextValue = Math.max(0, value - overflow)
      overflow -= value - nextValue
      return nextValue
    }
    stoppedNodeCount = reduce(stoppedNodeCount)
    degradedNodeCount = reduce(degradedNodeCount)
    runningNodeCount = reduce(runningNodeCount)
  }

  return {
    enabled: config.enabled === true,
    node_count: nodeCount,
    user_count: userCount,
    running_node_count: runningNodeCount,
    degraded_node_count: degradedNodeCount,
    stopped_node_count: stoppedNodeCount,
    suspended_user_count: clampNumber(config.suspended_user_count, 0, userCount, DEFAULT_MOCK_PANEL_CONFIG.suspended_user_count),
  }
}

function clampNumber(value: unknown, minValue: number, maxValue: number, fallback: number): number {
  const parsed = Number(value)
  const safeValue = Number.isFinite(parsed) ? Math.trunc(parsed) : fallback
  return Math.max(minValue, Math.min(maxValue, safeValue))
}

function buildMockNodes(config: MockPanelConfig): HysteriaNodeConfig[] {
  const statuses = buildNodeStatuses(config)
  return Array.from({ length: config.node_count }, (_, index) => {
    const preset = NODE_PRESETS[index % NODE_PRESETS.length]
    const nodeId = 101 + index
    const order = String(Math.floor(index / NODE_PRESETS.length) + 1).padStart(2, '0')
    const name = `${preset.region}-${preset.role}-${order}`
    const domain = `${preset.country}-${String(index + 1).padStart(2, '0')}.example.com`
    const host = `node-${String(index + 1).padStart(2, '0')}.example.com`
    const status = statuses[index] ?? 'running'
    return createNode(nodeId, name, host, domain, 8443 + (index % 3) * 1000, index % 4 === 2 ? 'self_signed' : 'acme', status)
  })
}

function buildMockUsers(config: MockPanelConfig, nodes: HysteriaNodeConfig[]): HysteriaUser[] {
  return Array.from({ length: config.user_count }, (_, index) => {
    const node = nodes[index % nodes.length]
    const quota = [80, 120, 200, 300][index % 4]
    const used = Math.round(quota * (0.18 + (index % 7) * 0.09))
    return {
      id: 2000 + index,
      public_id: `usr_mock_${String(index + 1).padStart(4, '0')}`,
      node_id: node.id,
      username: `user${String(index + 1).padStart(3, '0')}`,
      auth_password: `mock-pass-${index + 1}`,
      status: index < config.suspended_user_count ? 'suspended' : 'active',
      quota_gb: quota,
      used_gb: Math.min(used, quota),
      speed_limit_mbps: [50, 80, 120, 200][index % 4],
      expires_at: index % 5 === 0 ? null : `2026-0${(index % 6) + 1}-28 23:59:59`,
      created_at: nowText,
      updated_at: nowText,
    }
  })
}

function buildNodeStatuses(config: MockPanelConfig): ServiceStatus[] {
  const statuses: ServiceStatus[] = []
  statuses.push(...Array.from({ length: config.running_node_count }, () => 'running' as const))
  statuses.push(...Array.from({ length: config.degraded_node_count }, () => 'degraded' as const))
  statuses.push(...Array.from({ length: config.stopped_node_count }, () => 'stopped' as const))
  while (statuses.length < config.node_count) {
    statuses.push('running')
  }
  return statuses.slice(0, config.node_count)
}

function deriveNodeStatus(node: HysteriaNodeConfig): ServiceStatus {
  if (node.agent?.last_service_status === 'degraded') {
    return 'degraded'
  }
  if (node.agent?.last_service_status === 'stopped') {
    return 'stopped'
  }
  return 'running'
}

function createNode(
  id: number,
  name: string,
  host: string,
  domain: string,
  listenPort: number,
  tlsMode: 'acme' | 'self_signed',
  status: ServiceStatus,
): HysteriaNodeConfig {
  return {
    id,
    current_node: 0,
    name,
    host,
    ssh_port: 22,
    ssh_username: 'root',
    ssh_auth_type: id % 2 === 0 ? 'key' : 'password',
    ssh_password: null,
    ssh_private_key_path: null,
    ssh_private_key_uploaded: id % 2 === 0,
    ssh_private_key_name: id % 2 === 0 ? `${name}.pem` : null,
    ssh_public_key_name: id % 2 === 0 ? `${name}.pem.pub` : null,
    sudo_password: null,
    install_script: 'https://example.com/install.sh',
    service_name: 'hysteria-server.service',
    config_path: '/etc/hysteria/config.yaml',
    listen_port: listenPort,
    traffic_stats_listen: '127.0.0.1:9999',
    traffic_stats_secret: `mock-secret-${id}`,
    tls_mode: tlsMode,
    tls_cert_path: null,
    tls_key_path: null,
    domain,
    acme_email: 'admin@example.com',
    obfs_password: `mock-obfs-${id}`,
    masquerade_url: 'https://example.com',
    bandwidth_up_mbps: 300 + (id % 4) * 100,
    bandwidth_down_mbps: 600 + (id % 4) * 200,
    manage_mode: id % 3 === 0 ? 'ssh' : 'agent',
    agent_enabled: true,
    agent_report_interval_seconds: 2,
    agent_task_poll_interval_seconds: 1,
    agent_install_path: '/usr/local/bin/mxinhy-agent',
    agent_config_path: '/etc/mxinhy-agent.json',
    agent_service_name: 'mxinhy-agent',
    agent:
      id % 3 === 0
        ? null
        : {
            agent_id: `agent-${id}`,
            status: status === 'stopped' ? 'offline' : 'online',
            version: '0.1.0',
            report_interval_seconds: 2,
            task_poll_interval_seconds: 1,
            installed_at: nowText,
            last_seen_at: status === 'stopped' ? '2026-06-04 19:58:00' : nowText,
            last_ip: `198.51.100.${id % 255}`,
            last_error: status === 'degraded' ? '最近一次任务回执超时' : null,
            last_service_status: status,
            last_service_message: status === 'running' ? 'Agent 心跳正常' : status === 'stopped' ? 'Agent 离线' : 'Agent 在线，服务状态待确认',
            last_total_rx: id * 1024 * 1024 * 16,
            last_total_tx: id * 1024 * 1024 * 8,
            last_user_count: 4 + (id % 5),
            last_online_count: status === 'running' ? 2 + (id % 3) : 0,
          },
    created_at: nowText,
    updated_at: nowText,
  }
}
