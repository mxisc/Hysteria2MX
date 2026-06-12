const API_BASE = '/api'

type VersionCheckResult = {
  current: string
  latest: string | null
  hasUpdate: boolean
  error: string | null
}

type UpgradeResult = {
  message: string
  from: string
  to: string
  backup: string
}

export async function checkVersion(): Promise<VersionCheckResult> {
  const response = await fetch(`${API_BASE}/version/check`, {
    method: 'GET',
    credentials: 'include',
  })
  const data = await response.json()
  if (!response.ok || !data.success) {
    throw new Error(data.message || '检查版本失败')
  }
  return data.data as VersionCheckResult
}

export async function upgradeVersion(): Promise<UpgradeResult> {
  const response = await fetch(`${API_BASE}/version/upgrade`, {
    method: 'POST',
    credentials: 'include',
  })
  const data = await response.json()
  if (!response.ok || !data.success) {
    throw new Error(data.message || '升级失败')
  }
  return data.data as UpgradeResult
}
