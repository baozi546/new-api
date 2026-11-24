import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { TaskPluginChannelBadge } from '../channel-type-badge'

test('an account pool plugin uses its own channel badge', () => {
  const previous = useAuthStore.getState().auth.user
  useAuthStore.getState().auth.setUser({
    id: 1,
    role: 100,
    username: 'root',
  })
  const client = new QueryClient()
  client.setQueryData(['task-plugin-options'], [{
    key: 'gpt-account-pool', name: 'GPT account pool', accountPool: true, provider: true, models: [],
  }])
  const view = render(
    <QueryClientProvider client={client}>
      <TaskPluginChannelBadge pluginKey='gpt-account-pool' />
    </QueryClientProvider>
  )
  try {
    expect(screen.getByText('GPT account pool')).toBeVisible()
    expect(screen.getByText('Account pool')).toBeVisible()
  } finally {
    view.unmount()
    client.clear()
    useAuthStore.getState().auth.setUser(previous)
  }
})
