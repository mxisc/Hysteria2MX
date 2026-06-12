import type { LoginBackgroundConfig } from '../types'

export const DEFAULT_LOGIN_BACKGROUND_CONFIG: LoginBackgroundConfig = {
  customUrl: '',
}

export function buildLoginBackgroundUrl(config: LoginBackgroundConfig) {
  return config.customUrl?.trim() || ''
}
