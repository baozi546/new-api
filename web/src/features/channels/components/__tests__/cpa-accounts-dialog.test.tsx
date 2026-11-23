import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { CPAAccountsDialog } from '../cpa-accounts-dialog'

test('filtering problem accounts keeps a limit edit attached to its credential', async () => {
  const accounts = ['healthy', 'needs-review'].map((id, index) => ({
    id,
    label: id,
    disabled: false,
    requires_sign_in: false,
    account_concurrency: 2,
    account_pool: 'plus',
    account_check: { time: 0, result: index ? 'wrong_answer' : 'passed', question: '', model: '', consecutive: index },
  }))
  const request = vi.spyOn(api, 'request').mockImplementation(async (options) => ({
    data: { success: true, data: options.method === 'GET' ? accounts : null },
  }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(<QueryClientProvider client={client}><CPAAccountsDialog /></QueryClientProvider>)
  try {
    fireEvent.click(screen.getByRole('button', { name: 'CPA accounts' }))
    await screen.findByRole('row', { name: /healthy/ })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Problem accounts' }))
    expect(screen.queryByRole('row', { name: /healthy/ })).toBeNull()
    const row = within(screen.getByRole('row', { name: /needs-review/ }))
    fireEvent.change(row.getByRole('spinbutton'), { target: { value: '3' } })
    fireEvent.click(row.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(request).toHaveBeenCalledWith({
      method: 'PATCH', url: '/api/cpa/accounts', data: { id: 'needs-review', limit: 3, pool: 'plus' },
    }))
  } finally {
    view.unmount()
    client.clear()
    request.mockRestore()
  }
})
