import type { SetupPayload, SetupStatus } from '../types'

const API_BASE = '/api'

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data?: T
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers ?? {}),
    },
    ...init,
  })

  const text = await response.text()
  const payload = (text ? JSON.parse(text) : {}) as Partial<ApiEnvelope<T>>

  if (!response.ok || !payload.success || !payload.data) {
    throw new Error(payload.message || '请求失败')
  }

  return payload.data
}

export async function fetchSetupStatus(): Promise<SetupStatus> {
  return request<SetupStatus>('/setup/status')
}

export async function initializeSetup(payload: SetupPayload): Promise<SetupStatus> {
  return request<SetupStatus>('/setup/init', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
