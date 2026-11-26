import { api } from '@/lib/api'
import type { LocalizedTextValue } from '@/lib/localized-text'
import { requireServerSuccess } from '@/lib/server-error-message'

export type PoolSettings = {
  concurrency: number
}

export type PoolPlugin = {
  key: string
  name: LocalizedTextValue
  supports_reset_quota?: boolean
}

export type PoolChannel = {
  id: number
  name: string
  groups: string[]
  models: string[]
  plugin: PoolPlugin
  supports_reset_quota?: boolean
  account_count?: number
  active_count?: number
  problem_count?: number
  disabled_count?: number
}

export type ResetCreditItem = {
  id?: string
  title?: string
  status?: string
  reset_type?: string
  granted_at?: number
  expires_at?: number
  supported_by_plan?: boolean
}

export type AccountResetCredits = {
  available_count: number
  applicable_available_count: number
  credits?: ResetCreditItem[]
  error?: string
}

export type PoolAccount = {
  owner_id: number
  index: number
  plugin_key?: string
  plugin_name?: LocalizedTextValue
  supports_reset_quota?: boolean
  account_id: string
  label: string
  expires_at: number
  needs_login: boolean
  state: {
    disabled: boolean
    health?: string
    last_error?: string
    checked_at?: number
    wrong_answers?: number
    degraded?: boolean
    custom_expires_at?: number
  }
  profile: {
    plan: string
    category: string
    plan_verified: boolean
    subscription_expires_at?: number
    subscription_start_at?: number
    subscription_plan?: string
    will_renew?: boolean
    supports_reset_credits?: boolean
    reset_credits?: AccountResetCredits | null
    quota_checked_at: number
    quota_error: string
    windows:
      | {
          name: LocalizedTextValue
          used: number
          limit: number
          unit: string
          resetAt: number
        }[]
      | null
  }
  channels: { id: number; name: string; groups: string[]; concurrency?: number }[]
}

export type PoolData = { items: PoolAccount[]; settings: PoolSettings }

export const poolQueryKeys = {
  channels: ['provider-account-pools'] as const,
  accounts: (id: number) => ['provider-account-pool', id] as const,
  allAccounts: (plugin?: string) =>
    ['provider-account-pool', 'all', plugin || 'all'] as const,
}

export async function listPoolChannels(): Promise<{
  channels: PoolChannel[]
  plugins: PoolPlugin[]
}> {
  const response = await api.get('/api/channel/account_pools')
  return requireServerSuccess(response.data).data
}

export async function getAllProviderPoolAccounts(
  plugin?: string
): Promise<{ items: PoolAccount[] }> {
  const response = await api.get('/api/channel/account_pools/accounts', {
    params: plugin ? { plugin } : undefined,
    disableDuplicate: true,
  })
  return requireServerSuccess(response.data).data
}

export async function getProviderPool(id: number): Promise<PoolData> {
  const response = await api.get(`/api/channel/${id}/account_pool`, {
    disableDuplicate: true,
  })
  return requireServerSuccess(response.data).data
}

export async function saveProviderPool(
  id: number,
  settings: PoolSettings
): Promise<void> {
  const response = await api.put(`/api/channel/${id}/account_pool`, settings)
  requireServerSuccess(response.data)
}

export async function importPoolAccount(
  id: number,
  account: Record<string, unknown>
): Promise<{
  status: string
  quota_error?: string
  models_error?: string
  models_updated?: number
}> {
  const response = await api.post(`/api/channel/${id}/account_pool/import`, {
    account,
  })
  return requireServerSuccess(response.data).data
}

export type InspectAccountResult = {
  account_id: string
  model: string
  result: string
  reason?: string
  time: number
}

export type ResetQuotaResult = {
  account_id: string
  code: string
  message?: string
}

export async function operatePoolAccount(
  id: number,
  accountId: string,
  action: string,
  targetChannelId?: number
): Promise<void> {
  const response = await api.post(
    `/api/channel/${id}/account_pool/accounts/${encodeURIComponent(accountId)}`,
    { action, target_channel_id: targetChannelId }
  )
  requireServerSuccess(response.data)
}

export async function inspectPoolAccount(
  id: number,
  accountId: string
): Promise<InspectAccountResult> {
  const response = await api.post(
    `/api/channel/${id}/account_pool/accounts/${encodeURIComponent(accountId)}`,
    { action: 'inspect' },
    { timeout: 190000 }
  )
  return requireServerSuccess(response.data).data as InspectAccountResult
}

export async function resetPoolAccountQuota(
  id: number,
  accountId: string
): Promise<ResetQuotaResult> {
  const response = await api.post(
    `/api/channel/${id}/account_pool/accounts/${encodeURIComponent(accountId)}`,
    { action: 'reset_quota' },
    { timeout: 60000 }
  )
  return requireServerSuccess(response.data).data as ResetQuotaResult
}

export async function setPoolAccountExpiry(
  id: number,
  accountId: string,
  customExpiresAt: number
): Promise<{ account_id: string; custom_expires_at: number }> {
  const response = await api.post(
    `/api/channel/${id}/account_pool/accounts/${encodeURIComponent(accountId)}`,
    { action: 'set_expiry', custom_expires_at: customExpiresAt }
  )
  return requireServerSuccess(response.data).data
}

export async function createPoolChannel(input: {
  plugin: string
  name: string
  groups?: string[]
  settings: PoolSettings
}): Promise<{ id: number }> {
  const response = await api.post('/api/channel/account_pools', input)
  return requireServerSuccess(response.data).data
}
