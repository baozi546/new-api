import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  CPAAccountsDialog,
  CPAChannelPoolProvider,
  CPAChannelPoolStatus,
} from '../cpa-accounts-dialog'

test('filtering problem accounts keeps a limit edit attached to its credential', async () => {
  const accounts = ['healthy', 'needs-review'].map((id, index) => ({
    id,
    label: id,
    disabled: false,
    requires_sign_in: false,
    account_concurrency: 2,
    account_pool: 'plus',
    account_check: {
      time: 0,
      result: index ? 'wrong_answer' : 'passed',
      question: '',
      model: '',
      consecutive: index,
    },
  }))
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (options) => ({
      data: { success: true, data: options.method === 'GET' ? accounts : null },
    }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <CPAAccountsDialog />
    </QueryClientProvider>
  )
  try {
    fireEvent.click(screen.getByRole('button', { name: 'CPA accounts' }))
    await screen.findByRole('row', { name: /healthy/ })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Problem accounts' }))
    expect(screen.queryByRole('row', { name: /healthy/ })).toBeNull()
    const row = within(screen.getByRole('row', { name: /needs-review/ }))
    fireEvent.change(row.getByRole('spinbutton'), { target: { value: '3' } })
    fireEvent.click(row.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(request).toHaveBeenCalledWith({
        method: 'PATCH',
        url: '/api/cpa/accounts',
        data: { id: 'needs-review', limit: 3, pool: 'plus' },
      })
    )
  } finally {
    view.unmount()
    client.clear()
    request.mockRestore()
  }
})

test('the selected destination is sent at sign-in and locked for that flow', async () => {
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (options) => ({
      data: {
        success: true,
        data:
          options.method === 'GET'
            ? []
            : { flow: 'one', url: 'https://auth.example.test/sign-in' },
      },
    }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <CPAAccountsDialog />
    </QueryClientProvider>
  )
  try {
    fireEvent.click(screen.getByRole('button', { name: 'CPA accounts' }))
    const destination = screen.getByRole('combobox', {
      name: 'Add account to pool',
    }) as HTMLSelectElement
    fireEvent.change(destination, { target: { value: 'pro' } })
    fireEvent.click(screen.getByRole('button', { name: 'Authorize account' }))
    await screen.findByRole('link', { name: 'Open sign-in page' })
    expect(request).toHaveBeenCalledWith({
      method: 'POST',
      url: '/api/cpa/accounts/login',
      data: { pool: 'pro' },
    })
    expect(destination.disabled).toBe(true)
  } finally {
    view.unmount()
    client.clear()
    request.mockRestore()
  }
})

test('shared channels display the same issue count and open affected accounts', async () => {
  const accounts = [
    { id: 'healthy' },
    { id: 'limited', quota_blocked: true, unavailable: true },
    { id: 'relogin', disabled: true, requires_sign_in: true },
  ].map((a) => ({
    ...a,
    account_pool: 'plus',
    account_concurrency: null,
    account_check: {},
  }))
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (options) => ({
      data: {
        success: true,
        data: options.url?.endsWith('/bindings')
          ? {
              endpoints: [],
              channels: [7, 8].map((id) => ({
                id,
                name: `Channel ${id}`,
                pool: '',
              })),
            }
          : accounts,
      },
    }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <CPAChannelPoolProvider enabled>
        {[7, 8, 9].map((id) => (
          <CPAChannelPoolStatus key={id} id={id} />
        ))}
      </CPAChannelPoolProvider>
    </QueryClientProvider>
  )
  try {
    const buttons = await screen.findAllByRole('button', {
      name: /Shared CPA pool.*2 of 3 accounts need attention/,
    })
    expect(buttons).toHaveLength(2)
    fireEvent.click(buttons[0])
    await screen.findByRole('row', { name: /limited.*Quota blocked/ })
    expect(
      screen.getByRole('row', { name: /relogin.*Login required/ })
    ).toBeTruthy()
    expect(screen.queryByRole('row', { name: /healthy/ })).toBeNull()
    expect(
      request.mock.calls.every(([options]) => options.method === 'GET')
    ).toBe(true)
  } finally {
    view.unmount()
    client.clear()
    request.mockRestore()
  }
})

test('an empty channel opens account sign-in bound to its own pool', async () => {
  const request = vi
    .spyOn(api, 'request')
    .mockImplementation(async (options) => {
      let data: unknown = { flow: 's', url: 'https://auth.example.test/login' }
      if (options.url?.endsWith('/bindings')) {
        data = {
          endpoints: [],
          channels: [{ id: 7, name: 'Pro', pool: 'first' }],
        }
      } else if (options.method === 'GET') {
        data = [{ id: 'other', channel_pool_id: 'second', account_check: {} }]
      }
      return { data: { success: true, data } }
    })
  const client = new QueryClient()
  const view = render(
    <QueryClientProvider client={client}>
      <CPAChannelPoolProvider enabled>
        <CPAChannelPoolStatus id={7} />
      </CPAChannelPoolProvider>
    </QueryClientProvider>
  )
  try {
    fireEvent.click(
      await screen.findByRole('button', { name: 'No accounts · Add account' })
    )
    await screen.findByRole('dialog', { name: /Pro/ })
    expect(screen.queryByRole('row', { name: /other/ })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Authorize account' }))
    await screen.findByRole('link', { name: 'Open sign-in page' })
    expect(request).toHaveBeenCalledWith({
      method: 'POST',
      url: '/api/cpa/accounts/login',
      data: { channel_id: 7 },
    })
  } finally {
    view.unmount()
    client.clear()
    request.mockRestore()
  }
})
