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

function normalizeVersion(version: string): string {
  return version.trim().replace(/^v/, '')
}

function isNewerVersion(current: string, latest: string | null): boolean {
  const numericVersion = /^\d+(?:\.\d+)*$/
  if (!latest || !numericVersion.test(current) || !numericVersion.test(latest)) {
    return false
  }
  const left = latest.split('.').map(Number)
  const right = current.split('.').map(Number)
  for (let index = 0; index < Math.max(left.length, right.length); index += 1) {
    const difference = (left[index] ?? 0) - (right[index] ?? 0)
    if (difference !== 0) return difference > 0
  }
  return false
}

export async function checkVersion(): Promise<VersionCheckResult> {
  const response = await fetch(`${API_BASE}/version/check`, {
    method: 'GET',
    credentials: 'include',
    cache: 'no-store',
  })
  const data = await response.json()
  if (!response.ok || !data.success) {
    throw new Error(data.message || '检查版本失败')
  }
  const result = data.data as VersionCheckResult
  if (result.error) {
    throw new Error('检查更新失败，请稍后重试')
  }
  const current = normalizeVersion(result.current)
  const latest = result.latest ? normalizeVersion(result.latest) : null
  return {
    ...result,
    current,
    latest,
    hasUpdate: result.hasUpdate && isNewerVersion(current, latest),
  }
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
