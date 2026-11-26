import { api } from '@/lib/api'
import type { LocalizedTextValue } from '@/lib/localized-text'
import { requireServerSuccess } from '@/lib/server-error-message'

export type PoolSettings = {
  concurrency: number
}
export type PoolChannel = {
  id: number
  name: string
  groups: string[]
  models: string[]
  plugin: { key: string; name: string }
}
export type PoolAccount = {
  owner_id: number
  index: number
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
  }
  profile: {
    plan: string
    category: string
    plan_verified: boolean
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
  channels: { id: number; name: string; groups: string[] }[]
}
export type PoolData = { items: PoolAccount[]; settings: PoolSettings }
export const poolQueryKeys = {
  channels: ['provider-account-pools'] as const,
  accounts: (id: number) => ['provider-account-pool', id] as const,
}

export async function listPoolChannels(): Promise<{
  channels: PoolChannel[]
  plugins: { key: string; name: string }[]
}> {
  const response = await api.get('/api/channel/account_pools')
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

export async function createPoolChannel(input: {
  plugin: string
  name: string
  settings: PoolSettings
}): Promise<{ id: number }> {
  const response = await api.post('/api/channel/account_pools', input)
  return requireServerSuccess(response.data).data
}
