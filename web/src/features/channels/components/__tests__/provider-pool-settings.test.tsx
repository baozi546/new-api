import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { listPoolChannels, poolQueryKeys } from '../../lib/provider-pool-api'
import { ProviderPoolSettings } from '../provider-pool-settings'

afterEach(() => vi.restoreAllMocks())

function CustomerGroupSummary() {
  const query = useQuery({
    queryKey: poolQueryKeys.channels,
    queryFn: listPoolChannels,
  })
  return (
    <output aria-label='Customer groups'>
      {query.data?.channels[0]?.groups.join(', ')}
    </output>
  )
}

test('saving the channel account concurrency keeps customer groups intact', async () => {
  const user = userEvent.setup()
  let group = 'plus,pro'
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: {
      success: true,
      data: {
        channels: [
          {
            id: 7,
            name: 'Subscriptions',
            groups: group.split(','),
            models: [],
            plugin: { key: 'gpt-account-pool', name: 'GPT account pool' },
          },
        ],
        plugins: [],
      },
    },
  }))
  const save = vi.spyOn(api, 'put').mockImplementation(async () => {
    group = 'plus,pro'
    return { data: { success: true } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <ProviderPoolSettings
        channelId={7}
        settings={{ concurrency: 1 }}
        disabled={false}
      />
      <CustomerGroupSummary />
    </QueryClientProvider>
  )
  try {
    await waitFor(() =>
      expect(screen.getByLabelText('Customer groups')).toHaveTextContent(
        'plus,pro'
      )
    )
    await user.clear(
      screen.getByRole('spinbutton', { name: 'Concurrency per account' })
    )
    await user.type(
      screen.getByRole('spinbutton', { name: 'Concurrency per account' }),
      '3'
    )
    await user.click(
      screen.getByRole('button', { name: 'Save account pool settings' })
    )
    await waitFor(() => expect(save).toHaveBeenCalled())
    expect(save).toHaveBeenCalledWith('/api/channel/7/account_pool', {
      concurrency: 3,
    })
    expect(screen.getByLabelText('Customer groups')).toHaveTextContent(
      'plus,pro'
    )
    expect(
      screen.queryByRole('combobox', {
        name: 'Account type used by this channel',
      })
    ).not.toBeInTheDocument()
  } finally {
    view.unmount()
    client.clear()
  }
})

test('disabled account pool settings prevent changes and saving', () => {
  const client = new QueryClient()
  const view = render(
    <QueryClientProvider client={client}>
      <ProviderPoolSettings
        channelId={7}
        settings={{ concurrency: 1 }}
        disabled
      />
    </QueryClientProvider>
  )
  expect(
    screen.getByRole('spinbutton', { name: 'Concurrency per account' })
  ).toBeDisabled()
  expect(
    screen.getByRole('button', { name: 'Save account pool settings' })
  ).toBeDisabled()
  view.unmount()
  client.clear()
})
