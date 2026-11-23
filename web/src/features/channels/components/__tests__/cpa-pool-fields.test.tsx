import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { CPAPoolFields } from '../cpa-pool-fields'

test('editing a channel can select an existing shared pool and the actual Pro category', async () => {
  const user = userEvent.setup()
  const onPoolChange = vi.fn()
  const onCategoryChange = vi.fn()
  const view = render(
    <CPAPoolFields
      pools={[{ id: 2, name: 'Plus channel', pool: 'shared' }]}
      pool='own'
      category=''
      onPoolChange={onPoolChange}
      onCategoryChange={onCategoryChange}
    />
  )
  try {
    await user.click(screen.getByRole('combobox', { name: 'CPA channel pool' }))
    await user.click(
      await screen.findByRole('option', {
        name: /Share pool with Plus channel/,
      })
    )
    expect(onPoolChange).toHaveBeenCalledWith('shared')
    await user.click(
      screen.getByRole('combobox', { name: 'Route accounts by category' })
    )
    await user.click(await screen.findByRole('option', { name: 'Pro' }))
    expect(onCategoryChange).toHaveBeenCalledWith('pro')
  } finally {
    view.unmount()
  }
})

test('pool controls cannot be changed when the channel is locked', () => {
  const view = render(
    <CPAPoolFields
      pools={[]}
      pool=''
      category=''
      disabled
      onPoolChange={vi.fn()}
      onCategoryChange={vi.fn()}
    />
  )
  try {
    for (const field of screen.getAllByRole('combobox')) {
      expect(field).toBeDisabled()
    }
  } finally {
    view.unmount()
  }
})
