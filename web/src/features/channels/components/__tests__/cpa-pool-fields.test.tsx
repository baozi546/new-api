import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { CPAPoolFields } from '../cpa-pool-fields'

test('channels share CPA accounts automatically and can select the actual Pro category', async () => {
  const user = userEvent.setup()
  const onCategoryChange = vi.fn()
  const view = render(
    <CPAPoolFields category='' onCategoryChange={onCategoryChange} />
  )
  try {
    expect(screen.getAllByRole('combobox')).toHaveLength(1)
    expect(
      screen.getByText('Accounts at this CPA address are shared automatically.')
    ).toBeVisible()
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
    <CPAPoolFields category='' disabled onCategoryChange={vi.fn()} />
  )
  try {
    for (const field of screen.getAllByRole('combobox')) {
      expect(field).toBeDisabled()
    }
  } finally {
    view.unmount()
  }
})
