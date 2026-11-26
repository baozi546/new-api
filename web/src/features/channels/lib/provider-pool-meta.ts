import type { PoolAccount } from './provider-pool-api'

export type AccountVisualKind = 'active' | 'warning' | 'error' | 'disabled'

export type ExpirySource = 'manual' | 'subscription' | 'token' | 'none'

export type EffectiveExpiry = {
  timestamp: number
  source: ExpirySource
}

export type ExpiryCountdown = {
  expired: boolean
  urgent: boolean
  soon: boolean
  remainingSeconds: number
  days: number
  hours: number
  minutes: number
}

export const EXPIRING_SOON_SECONDS = 7 * 24 * 3600
export const EXPIRING_URGENT_SECONDS = 3 * 24 * 3600

export function getEffectiveExpiry(account: PoolAccount): EffectiveExpiry {
  if ((account.state.custom_expires_at || 0) > 0) {
    return {
      timestamp: account.state.custom_expires_at!,
      source: 'manual',
    }
  }
  if ((account.profile.subscription_expires_at || 0) > 0) {
    return {
      timestamp: account.profile.subscription_expires_at!,
      source: 'subscription',
    }
  }
  if ((account.expires_at || 0) > 0) {
    return {
      timestamp: account.expires_at,
      source: 'token',
    }
  }
  return { timestamp: 0, source: 'none' }
}

export function computeExpiryCountdown(
  unixSeconds: number,
  nowMs: number
): ExpiryCountdown | null {
  if (!unixSeconds || unixSeconds <= 0) return null
  const diffSec = Math.floor(unixSeconds - nowMs / 1000)
  const expired = diffSec <= 0
  const absSec = Math.abs(diffSec)
  const totalMinutes = Math.max(1, Math.ceil(absSec / 60))
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  return {
    expired,
    urgent: expired || diffSec <= EXPIRING_URGENT_SECONDS,
    soon: !expired && diffSec <= EXPIRING_SOON_SECONDS,
    remainingSeconds: diffSec,
    days,
    hours,
    minutes,
  }
}

export function formatDurationCompact(
  days: number,
  hours: number,
  minutes: number,
  locale: string
): string {
  const parts: string[] = []
  const fmt = (value: number, unit: 'day' | 'hour' | 'minute') =>
    new Intl.NumberFormat(locale, {
      style: 'unit',
      unit,
      unitDisplay: 'short',
    }).format(value)
  if (days > 0) parts.push(fmt(days, 'day'))
  if (hours > 0 && parts.length < 2) parts.push(fmt(hours, 'hour'))
  if (minutes > 0 && parts.length < 2 && days === 0) {
    parts.push(fmt(minutes, 'minute'))
  }
  if (parts.length === 0) parts.push(fmt(1, 'minute'))
  return parts.join(' ')
}

export function isAccountExpiringOrExpired(
  account: PoolAccount,
  nowMs: number
): boolean {
  const expiry = getEffectiveExpiry(account)
  if (expiry.source === 'none' || expiry.source === 'token') {
    // Only flag subscription/manual expiry as expiring unless token itself is already expired
    if (expiry.source === 'token' && expiry.timestamp > 0) {
      return expiry.timestamp * 1000 <= nowMs
    }
    return false
  }
  const cd = computeExpiryCountdown(expiry.timestamp, nowMs)
  return !!cd && (cd.expired || cd.soon)
}

export function getAccountVisualStatus(
  account: PoolAccount,
  nowMs: number
): {
  kind: AccountVisualKind
  labelKey: string
  stripeClass: string
  cardClass: string
  dotClass: string
  badgeClass: string
} {
  const expiry = getEffectiveExpiry(account)
  const subCountdown =
    expiry.source === 'manual' || expiry.source === 'subscription'
      ? computeExpiryCountdown(expiry.timestamp, nowMs)
      : null

  if (account.state.disabled) {
    return {
      kind: 'disabled',
      labelKey: 'Disabled',
      stripeClass: 'bg-zinc-400 dark:bg-zinc-600',
      cardClass:
        'border-zinc-300/80 bg-muted/35 opacity-85 hover:opacity-100 dark:border-zinc-700/80 dark:bg-zinc-900/50',
      dotClass: 'bg-zinc-400 dark:bg-zinc-500',
      badgeClass:
        'border-zinc-300 bg-zinc-100 text-zinc-700 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300',
    }
  }

  const severeError =
    account.needs_login ||
    account.state.health === 'account_banned' ||
    account.state.health === 'login_required' ||
    (subCountdown?.expired ?? false)

  if (severeError) {
    const labelKey =
      account.state.health === 'account_banned'
        ? 'Account banned'
        : account.needs_login || account.state.health === 'login_required'
          ? 'Login required'
          : 'Expired'
    return {
      kind: 'error',
      labelKey,
      stripeClass: 'bg-red-500',
      cardClass:
        'border-red-300/85 bg-red-50/35 dark:border-red-900/80 dark:bg-red-950/20',
      dotClass: 'bg-red-500',
      badgeClass:
        'border-red-300/80 bg-red-500/10 text-red-700 dark:border-red-800 dark:text-red-300',
    }
  }

  const warningIssue =
    !!account.state.degraded ||
    !!account.profile.quota_error ||
    account.state.health === 'wrong_answer' ||
    account.state.health === 'rate_limited' ||
    account.state.health === 'inconclusive' ||
    !account.profile.plan_verified ||
    (subCountdown?.urgent ?? false)

  if (warningIssue) {
    const labelKey = account.state.degraded
      ? 'Reduced quality'
      : account.state.health === 'rate_limited'
        ? 'Rate limited'
        : account.state.health === 'wrong_answer'
          ? 'Answer mismatch'
          : account.profile.quota_error
            ? 'Quota error'
            : subCountdown?.urgent
              ? 'Expiring soon'
              : 'Attention'
    return {
      kind: 'warning',
      labelKey,
      stripeClass: 'bg-amber-500',
      cardClass:
        'border-amber-300/85 bg-amber-50/35 dark:border-amber-800/80 dark:bg-amber-950/20',
      dotClass: 'bg-amber-500',
      badgeClass:
        'border-amber-300/80 bg-amber-500/12 text-amber-800 dark:border-amber-800 dark:text-amber-300',
    }
  }

  return {
    kind: 'active',
    labelKey: 'Enabled',
    stripeClass: 'bg-emerald-500',
    cardClass:
      'border-emerald-300/75 bg-emerald-50/25 dark:border-emerald-900/70 dark:bg-emerald-950/15',
    dotClass: 'bg-emerald-500',
    badgeClass:
      'border-emerald-300/80 bg-emerald-500/12 text-emerald-700 dark:border-emerald-800 dark:text-emerald-300',
  }
}

export function getPlanStyle(planRaw: string, categoryRaw?: string): {
  key: string
  label: string
  isPro: boolean
  badgeClass: string
  cardRingClass: string
} {
  const plan = (planRaw || categoryRaw || 'unknown').trim().toLowerCase()
  if (plan === 'pro') {
    return {
      key: 'pro',
      label: 'PRO',
      isPro: true,
      badgeClass:
        'border-violet-500/60 bg-gradient-to-r from-violet-600 to-fuchsia-600 text-white shadow-xs',
      cardRingClass:
        'ring-1 ring-violet-400/35 dark:ring-violet-500/35',
    }
  }
  if (plan === 'prolite' || plan === 'pro_lite') {
    return {
      key: 'prolite',
      label: 'PRO LITE',
      isPro: true,
      badgeClass:
        'border-violet-400/60 bg-violet-500/15 text-violet-700 dark:border-violet-500/50 dark:bg-violet-500/20 dark:text-violet-200',
      cardRingClass:
        'ring-1 ring-violet-300/30 dark:ring-violet-600/30',
    }
  }
  if (plan === 'max' || plan === 'ultra') {
    return {
      key: plan,
      label: plan.toUpperCase(),
      isPro: true,
      badgeClass:
        'border-amber-500/60 bg-gradient-to-r from-amber-500 to-orange-500 text-white shadow-xs',
      cardRingClass: 'ring-1 ring-amber-400/35 dark:ring-amber-500/35',
    }
  }
  if (plan === 'plus') {
    return {
      key: 'plus',
      label: 'PLUS',
      isPro: false,
      badgeClass:
        'border-sky-400/50 bg-sky-500/12 text-sky-700 dark:border-sky-700 dark:bg-sky-500/20 dark:text-sky-300',
      cardRingClass: '',
    }
  }
  if (plan === 'team' || plan === 'business' || plan === 'enterprise') {
    return {
      key: plan,
      label: plan.toUpperCase(),
      isPro: false,
      badgeClass:
        'border-indigo-400/50 bg-indigo-500/12 text-indigo-700 dark:border-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-300',
      cardRingClass: '',
    }
  }
  return {
    key: plan || 'unknown',
    label: plan ? plan.toUpperCase() : 'STANDARD',
    isPro: false,
    badgeClass:
      'border-border bg-muted text-muted-foreground',
    cardRingClass: '',
  }
}

export function getProviderVisual(pluginKey?: string): {
  shortLabel: string
  badgeClass: string
  dotClass: string
} {
  const key = (pluginKey || '').toLowerCase()
  if (key.includes('gpt') || key.includes('codex') || key.includes('openai')) {
    return {
      shortLabel: 'GPT / Codex',
      badgeClass:
        'border-teal-400/50 bg-teal-500/10 text-teal-700 dark:border-teal-700 dark:bg-teal-500/20 dark:text-teal-300',
      dotClass: 'bg-teal-500',
    }
  }
  if (key.includes('claude') || key.includes('anthropic')) {
    return {
      shortLabel: 'Claude',
      badgeClass:
        'border-orange-400/50 bg-orange-500/10 text-orange-700 dark:border-orange-700 dark:bg-orange-500/20 dark:text-orange-300',
      dotClass: 'bg-orange-500',
    }
  }
  if (key.includes('gemini') || key.includes('google')) {
    return {
      shortLabel: 'Gemini',
      badgeClass:
        'border-blue-400/50 bg-blue-500/10 text-blue-700 dark:border-blue-700 dark:bg-blue-500/20 dark:text-blue-300',
      dotClass: 'bg-blue-500',
    }
  }
  if (key.includes('antigravity')) {
    return {
      shortLabel: 'Antigravity',
      badgeClass:
        'border-purple-400/50 bg-purple-500/10 text-purple-700 dark:border-purple-700 dark:bg-purple-500/20 dark:text-purple-300',
      dotClass: 'bg-purple-500',
    }
  }
  if (key.includes('kimi')) {
    return {
      shortLabel: 'Kimi',
      badgeClass:
        'border-cyan-400/50 bg-cyan-500/10 text-cyan-700 dark:border-cyan-700 dark:bg-cyan-500/20 dark:text-cyan-300',
      dotClass: 'bg-cyan-500',
    }
  }
  if (key.includes('grok') || key.includes('xai')) {
    return {
      shortLabel: 'Grok',
      badgeClass:
        'border-slate-400/50 bg-slate-500/10 text-slate-700 dark:border-slate-700 dark:bg-slate-500/20 dark:text-slate-300',
      dotClass: 'bg-slate-500',
    }
  }
  return {
    shortLabel: pluginKey || 'Provider',
    badgeClass: 'border-border bg-secondary/60 text-secondary-foreground',
    dotClass: 'bg-primary',
  }
}

export function getQuotaBarColorClass(usedPercent: number): string {
  if (usedPercent >= 90) return 'bg-red-500'
  if (usedPercent >= 70) return 'bg-amber-500'
  return 'bg-emerald-500'
}
