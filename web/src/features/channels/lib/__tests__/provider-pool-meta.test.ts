import { describe, expect, test } from 'vitest'

import type { PoolAccount } from '../provider-pool-api'
import {
  computeExpiryCountdown,
  getAccountVisualStatus,
  getEffectiveExpiry,
  getPlanStyle,
  getProviderVisual,
  isAccountExpiringOrExpired,
} from '../provider-pool-meta'

function makeAccount(overrides: Partial<PoolAccount> = {}): PoolAccount {
  return {
    owner_id: 11,
    index: 0,
    plugin_key: 'gpt-account-pool',
    account_id: 'acct-1',
    label: 'user@example.com',
    expires_at: 1_800_000_000,
    needs_login: false,
    state: {
      disabled: false,
      health: 'passed',
    },
    profile: {
      plan: 'pro',
      category: 'pro',
      plan_verified: true,
      subscription_expires_at: 1_810_000_000,
      quota_checked_at: 1_795_000_000,
      quota_error: '',
      windows: [],
    },
    channels: [{ id: 11, name: 'GPT Pro', groups: ['pro'] }],
    ...overrides,
  }
}

describe('provider-pool-meta', () => {
  const nowSec = 1_800_000_000
  const nowMs = nowSec * 1000

  test('prioritizes manual expiry over subscription expiry over token expiry', () => {
    const manual = makeAccount({
      state: { disabled: false, custom_expires_at: 1_820_000_000 },
    })
    expect(getEffectiveExpiry(manual)).toEqual({
      timestamp: 1_820_000_000,
      source: 'manual',
    })

    const sub = makeAccount()
    expect(getEffectiveExpiry(sub)).toEqual({
      timestamp: 1_810_000_000,
      source: 'subscription',
    })

    const tokenOnly = makeAccount({
      profile: {
        plan: 'plus',
        category: 'plus',
        plan_verified: true,
        subscription_expires_at: 0,
        quota_checked_at: 0,
        quota_error: '',
        windows: [],
      },
    })
    expect(getEffectiveExpiry(tokenOnly)).toEqual({
      timestamp: 1_800_000_000,
      source: 'token',
    })
  })

  test('computes expiry countdown states accurately', () => {
    const expired = computeExpiryCountdown(nowSec - 3600 * 5, nowMs)
    expect(expired?.expired).toBe(true)
    expect(expired?.urgent).toBe(true)

    const urgent = computeExpiryCountdown(nowSec + 3600 * 24, nowMs)
    expect(urgent?.expired).toBe(false)
    expect(urgent?.urgent).toBe(true)
    expect(urgent?.days).toBe(1)

    const soon = computeExpiryCountdown(nowSec + 3600 * 24 * 5, nowMs)
    expect(soon?.urgent).toBe(false)
    expect(soon?.soon).toBe(true)

    const healthy = computeExpiryCountdown(nowSec + 3600 * 24 * 25, nowMs)
    expect(healthy?.soon).toBe(false)
    expect(healthy?.urgent).toBe(false)
    expect(healthy?.days).toBe(25)
  })

  test('distinguishes active, disabled, warning, and error visual statuses', () => {
    const active = makeAccount()
    expect(getAccountVisualStatus(active, nowMs).kind).toBe('active')

    const disabled = makeAccount({ state: { disabled: true, health: 'wrong_answer' } })
    expect(getAccountVisualStatus(disabled, nowMs).kind).toBe('disabled')

    const banned = makeAccount({ state: { disabled: false, health: 'account_banned' } })
    expect(getAccountVisualStatus(banned, nowMs).kind).toBe('error')

    const expiredSub = makeAccount({
      profile: {
        plan: 'pro',
        category: 'pro',
        plan_verified: true,
        subscription_expires_at: nowSec - 60,
        quota_checked_at: nowSec,
        quota_error: '',
        windows: [],
      },
    })
    expect(getAccountVisualStatus(expiredSub, nowMs).kind).toBe('error')
    expect(isAccountExpiringOrExpired(expiredSub, nowMs)).toBe(true)

    const rateLimited = makeAccount({ state: { disabled: false, health: 'rate_limited' } })
    expect(getAccountVisualStatus(rateLimited, nowMs).kind).toBe('warning')
  })

  test('maps Pro, ProLite, Plus, and multi-provider badges', () => {
    expect(getPlanStyle('pro').isPro).toBe(true)
    expect(getPlanStyle('pro').label).toBe('PRO')
    expect(getPlanStyle('prolite').isPro).toBe(true)
    expect(getPlanStyle('prolite').label).toBe('PRO LITE')
    expect(getPlanStyle('plus').label).toBe('PLUS')

    expect(getProviderVisual('gpt-account-pool').shortLabel).toBe('GPT / Codex')
    expect(getProviderVisual('claude-account-pool').shortLabel).toBe('Claude')
    expect(getProviderVisual('gemini-account-pool').shortLabel).toBe('Gemini')
    expect(getProviderVisual('custom-oauth').shortLabel).toBe('custom-oauth')
  })
})
