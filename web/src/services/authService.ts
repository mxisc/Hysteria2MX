import CryptoJS from 'crypto-js'
import type { AdminRole, AuthSession, Permission } from '../types'

const SESSION_KEY = 'hy2-console-session'
const SESSION_TTL_MS = 12 * 60 * 60 * 1000
const API_BASE = '/api'

const ROLE_PERMISSIONS: Record<AdminRole, Permission[]> = {
  super_admin: ['*'],
  operator: [
    'panel.view', 'node.view', 'node.manage', 'user.view', 'user.manage',
    'config.view', 'config.manage', 'service.view', 'service.manage',
    'logs.view', 'traffic.sync', 'audit.view', 'appearance.manage', 'notification.manage', 'system.upgrade', 'job.view',
  ],
  auditor: [
    'panel.view', 'node.view', 'user.view', 'config.view', 'service.view',
    'logs.view', 'audit.view', 'job.view',
  ],
  viewer: [
    'panel.view', 'node.view', 'user.view', 'config.view', 'service.view',
  ],
}

type AuthEnvelope = {
  success: boolean
  message?: string
  data?: {
    user?: {
      id: number
      username: string
      displayName: string
      role: AdminRole
      status: 'active' | 'disabled'
      permissions: Permission[]
    }
  }
}

type LoginChallengeEnvelope = {
  success: boolean
  message?: string
  data?: {
    challenge?: {
      nonce: string
      expiresAt: number
      derivedSeed: string
    }
  }
}

function encryptLoginPassword(password: string, derivedSeed: string): string {
  const keyHex = CryptoJS.MD5(derivedSeed).toString(CryptoJS.enc.Hex)
  const ivHex = CryptoJS.SHA1(keyHex).toString(CryptoJS.enc.Hex).substring(0, 16)
  const key = CryptoJS.enc.Utf8.parse(keyHex)
  const iv = CryptoJS.enc.Utf8.parse(ivHex)

  return CryptoJS.AES.encrypt(password, key, {
    iv,
    mode: CryptoJS.mode.CBC,
    padding: CryptoJS.pad.Pkcs7,
  }).ciphertext.toString(CryptoJS.enc.Hex)
}

async function fetchLoginChallenge(): Promise<{ nonce: string; expiresAt: number; derivedSeed: string }> {
  const response = await fetch(`${API_BASE}/auth/login-challenge`, {
    method: 'GET',
    credentials: 'include',
  })
  const data = (await response.json()) as LoginChallengeEnvelope
  if (!response.ok || !data.success || !data.data?.challenge?.nonce || !data.data.challenge.derivedSeed) {
    throw new Error(data.message || '获取登录 challenge 失败')
  }

  return data.data.challenge
}

function persistSession(session: AuthSession) {
  localStorage.setItem(SESSION_KEY, JSON.stringify(session))
}

export function getStoredSession(): AuthSession | null {
  const raw = localStorage.getItem(SESSION_KEY)
  if (!raw) return null

  try {
    const session = JSON.parse(raw) as AuthSession
    if (session.expiresAt <= Date.now()) {
      clearSession()
      return null
    }
    return session
  } catch {
    clearSession()
    return null
  }
}

async function parseAuthResponse(response: Response): Promise<AuthEnvelope> {
  const data = (await response.json()) as AuthEnvelope
  if (!response.ok || !data.success || !data.data?.user) {
    throw new Error(data.message || '认证请求失败')
  }
  return data
}

function toSession(user: {
  id: number
  username: string
  displayName: string
  role: AdminRole
  status: 'active' | 'disabled'
}): AuthSession {
  return {
    id: user.id,
    username: user.username,
    displayName: user.displayName,
    role: user.role,
    status: user.status,
    permissions: ROLE_PERMISSIONS[user.role] ?? ROLE_PERMISSIONS['viewer'],
    token: 'session-cookie',
    expiresAt: Date.now() + SESSION_TTL_MS,
  }
}

export async function login(username: string, password: string): Promise<AuthSession> {
  const challenge = await fetchLoginChallenge()

  const response = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      username,
      password: encryptLoginPassword(password, challenge.derivedSeed),
      password_encrypted: true,
      login_challenge: challenge.nonce,
    }),
  })
  const result = await parseAuthResponse(response)
  const session = toSession(result.data!.user!)

  persistSession(session)
  return session
}

export async function fetchCurrentSession(): Promise<AuthSession | null> {
  const response = await fetch(`${API_BASE}/auth/me`, {
    method: 'GET',
    credentials: 'include',
  })

  if (response.status === 401) {
    clearSession()
    return null
  }

  const result = await parseAuthResponse(response)
  const session = toSession(result.data!.user!)
  persistSession(session)
  return session
}

export async function logout() {
  try {
    await fetch(`${API_BASE}/auth/logout`, {
      method: 'POST',
      credentials: 'include',
    })
  } finally {
    clearSession()
  }
}

export function clearSession() {
  localStorage.removeItem(SESSION_KEY)
}
