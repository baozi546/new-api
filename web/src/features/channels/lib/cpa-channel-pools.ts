import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

const route = '/api/cpa/accounts'
export async function request<T>(
  method: 'GET' | 'PATCH' | 'POST',
  path = '',
  data?: unknown
): Promise<T> {
  const r = await api.request({ method, url: route + path, data })
  if (!r.data?.success) {
    throw new Error(
      typeof r.data?.message === 'string' ? r.data.message : 'Request failed'
    )
  }
  return r.data.data as T
}

export type Binding = {
  id: number
  name: string
  pool: string
  category?: string
}
export function useCPABindings(enabled: boolean) {
  return useQuery({
    queryKey: ['cpa-channel-bindings'],
    queryFn: () =>
      request<{ endpoints: string[]; channels: Binding[] }>('GET', '/bindings'),
    enabled,
    staleTime: 30000,
    refetchInterval: 30000,
  })
}
export function isCPARelayURL(raw: string, endpoints: string[]) {
  try {
    const url = new URL(raw.trim())
    if (url.username || url.password || url.search || url.hash) return false
    return endpoints.includes(
      url.origin + url.pathname.replace(/\/+$/, '').replace(/\/v1$/, '')
    )
  } catch {
    return false
  }
}
