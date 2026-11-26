import {
  Activity,
  AlertTriangle,
  ArrowRightLeft,
  CalendarClock,
  Check,
  Copy,
  Crown,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Power,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Ticket,
  Trash2,
  Zap,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { resolveLocalizedText } from '@/lib/localized-text'
import { cn } from '@/lib/utils'

import type { PoolAccount } from '../lib/provider-pool-api'
import {
  computeExpiryCountdown,
  formatDurationCompact,
  getAccountVisualStatus,
  getEffectiveExpiry,
  getPlanStyle,
  getProviderVisual,
  getQuotaBarColorClass,
} from '../lib/provider-pool-meta'

const healthLabels: Record<string, string> = {
  passed: 'Passed',
  wrong_answer: 'Answer mismatch',
  rate_limited: 'Rate limited',
  login_required: 'Login required',
  account_banned: 'Account banned',
  inconclusive: 'Inconclusive',
}

function formatShortDate(unixSeconds: number, locale: string): string {
  if (!unixSeconds || unixSeconds <= 0) return ''
  try {
    return new Intl.DateTimeFormat(locale, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date(unixSeconds * 1000))
  } catch {
    return formatTimestampToDate(unixSeconds)
  }
}

export function ProviderAccountCard(props: {
  account: PoolAccount
  disabled: boolean
  inspecting?: boolean
  resetting?: boolean
  showChannelBadge?: boolean
  onInspect?: (account: PoolAccount) => void
  onResetQuota?: (account: PoolAccount) => void
  onEditExpiry?: (account: PoolAccount) => void
  onAction: (account: PoolAccount, action: string) => void
  onLogin: (account: PoolAccount) => void
  onTransfer?: (account: PoolAccount) => void
}) {
  const { t, i18n } = useTranslation()
  const { copy, copied } = useCopyToClipboard()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [nowMs, setNowMs] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 30000)
    return () => window.clearInterval(timer)
  }, [])

  const account = props.account
  const visual = getAccountVisualStatus(account, nowMs)
  const planStyle = getPlanStyle(account.profile.plan, account.profile.category)
  const providerVisual = getProviderVisual(account.plugin_key)
  const providerDisplayName =
    resolveLocalizedText(account.plugin_name, i18n.language) ||
    providerVisual.shortLabel

  const effectiveExpiry = getEffectiveExpiry(account)
  const expiryCountdown =
    effectiveExpiry.timestamp > 0
      ? computeExpiryCountdown(effectiveExpiry.timestamp, nowMs)
      : null
  const expiryDurationText = expiryCountdown
    ? formatDurationCompact(
        expiryCountdown.days,
        expiryCountdown.hours,
        expiryCountdown.minutes,
        locale
      )
    : ''

  const supportsReset = !!(
    account.supports_reset_quota || account.profile.supports_reset_credits
  )
  const resetCredits = account.profile.reset_credits
  const availableResetCount = resetCredits?.available_count ?? null
  const earliestResetExpiry =
    resetCredits?.credits?.find((c) => (c.expires_at || 0) > 0)?.expires_at || 0
  const resetExpiryCountdown =
    earliestResetExpiry > 0
      ? computeExpiryCountdown(earliestResetExpiry, nowMs)
      : null

  const windows = account.profile.windows || []
  const shortId =
    account.account_id.length > 16
      ? `${account.account_id.slice(0, 8)}…${account.account_id.slice(-4)}`
      : account.account_id

  const expirySourceLabel =
    effectiveExpiry.source === 'manual'
      ? t('Custom expiry')
      : effectiveExpiry.source === 'subscription'
        ? t('Subscription expires')
        : effectiveExpiry.source === 'token'
          ? t('Token expires')
          : t('Expiry date')

  return (
    <article
      data-status-kind={visual.kind}
      data-plan={planStyle.key}
      className={cn(
        'bg-card relative flex flex-col overflow-hidden rounded-xl border shadow-2xs transition-all hover:shadow-sm',
        visual.cardClass,
        planStyle.cardRingClass
      )}
    >
      {/* Top status color stripe */}
      <div className={cn('h-1 w-full shrink-0', visual.stripeClass)} />

      {/* Header: title, provider & plan tier badges */}
      <div className='space-y-1.5 px-3.5 pt-3 pb-2'>
        <div className='flex items-start justify-between gap-2'>
          <div className='min-w-0 flex-1'>
            <div className='flex items-center gap-1.5'>
              <span
                className={cn(
                  'size-2 shrink-0 rounded-full',
                  visual.dotClass
                )}
                aria-hidden='true'
              />
              <h3
                className='truncate text-sm font-semibold leading-tight'
                title={account.label || account.account_id}
              >
                {account.label || account.account_id}
              </h3>
            </div>
            <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[11px]'>
              <button
                type='button'
                onClick={() => void copy(account.account_id)}
                title={account.account_id}
                className='hover:text-foreground inline-flex items-center gap-1 font-mono transition-colors'
              >
                <span>{shortId}</span>
                {copied ? (
                  <Check className='size-3 text-emerald-500' />
                ) : (
                  <Copy className='size-3 opacity-60' />
                )}
              </button>
              {props.showChannelBadge && account.channels.length > 0 && (
                <span className='bg-background/80 text-foreground/80 rounded border px-1.5 py-px text-[10px] font-medium'>
                  {account.channels
                    .map((ch) =>
                      ch.groups.length
                        ? `${ch.name} (${ch.groups.join(', ')})`
                        : ch.name
                    )
                    .join(' · ')}
                </span>
              )}
            </div>
          </div>

          <div className='flex shrink-0 flex-col items-end gap-1'>
            <span
              className={cn(
                'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-bold tracking-wide uppercase',
                planStyle.badgeClass
              )}
            >
              {planStyle.key === 'pro' ? (
                <Crown className='size-3' />
              ) : planStyle.isPro ? (
                <Sparkles className='size-3' />
              ) : planStyle.key === 'plus' ? (
                <Zap className='size-3' />
              ) : null}
              {planStyle.label}
            </span>
            <span
              className={cn(
                'inline-flex items-center gap-1 rounded-md border px-1.5 py-px text-[10px] font-medium',
                providerVisual.badgeClass
              )}
              title={providerDisplayName}
            >
              <span
                className={cn('size-1.5 rounded-full', providerVisual.dotClass)}
              />
              {providerVisual.shortLabel}
            </span>
          </div>
        </div>

        {/* Status pills row */}
        <div className='flex flex-wrap items-center gap-1.5 pt-0.5'>
          <span
            className={cn(
              'inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-medium',
              visual.badgeClass
            )}
          >
            <span className={cn('size-1.5 rounded-full', visual.dotClass)} />
            {t(visual.labelKey)}
          </span>

          {account.state.disabled &&
            account.state.health &&
            account.state.health !== 'passed' && (
              <span className='inline-flex items-center gap-1 rounded-md border border-red-300/70 bg-red-500/10 px-1.5 py-0.5 text-[11px] font-medium text-red-700 dark:border-red-800 dark:text-red-300'>
                <AlertTriangle className='size-3' />
                {t(healthLabels[account.state.health] || 'Inconclusive')}
              </span>
            )}

          {account.state.disabled && account.state.degraded && (
            <span className='inline-flex items-center rounded-md border border-amber-300/70 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-800 dark:border-amber-800 dark:text-amber-300'>
              {t('Reduced quality')}
            </span>
          )}

          {!account.profile.plan_verified && (
            <span className='inline-flex items-center rounded-md border border-amber-300/70 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-800 dark:border-amber-800 dark:text-amber-300'>
              {t('Subscription unverified')}
            </span>
          )}

          {account.state.health === 'passed' && (
            <span className='text-muted-foreground text-[11px]'>
              {t('Passed')}
              {account.state.checked_at
                ? ` · ${formatTimestampToDate(account.state.checked_at)}`
                : ''}
            </span>
          )}
        </div>
      </div>

      {/* Expiry date & countdown box */}
      <div className='bg-background/70 mx-3.5 mb-2 flex items-center justify-between gap-2 rounded-lg border px-2.5 py-1.5 text-xs'>
        <div className='flex min-w-0 flex-1 flex-wrap items-center gap-x-1.5 gap-y-0.5'>
          <CalendarClock className='text-muted-foreground size-3.5 shrink-0' />
          <span className='text-muted-foreground text-[11px]'>
            {expirySourceLabel}:
          </span>
          {effectiveExpiry.timestamp > 0 ? (
            <>
              <span
                className='font-mono text-[11px] font-medium'
                title={formatTimestampToDate(effectiveExpiry.timestamp)}
              >
                {formatShortDate(effectiveExpiry.timestamp, locale)}
              </span>
              {expiryCountdown && (
                <span
                  className={cn(
                    'rounded px-1.5 py-px text-[10px] font-semibold',
                    expiryCountdown.expired
                      ? 'bg-red-500/15 text-red-700 dark:text-red-300'
                      : expiryCountdown.urgent
                        ? 'bg-amber-500/20 text-amber-800 dark:text-amber-200'
                        : expiryCountdown.soon
                          ? 'bg-amber-500/10 text-amber-700 dark:text-amber-300'
                          : 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                  )}
                >
                  {expiryCountdown.expired
                    ? t('Expired {{duration}} ago', {
                        duration: expiryDurationText,
                      })
                    : t('Expires in {{duration}}', {
                        duration: expiryDurationText,
                      })}
                </span>
              )}
              {effectiveExpiry.source === 'subscription' &&
                account.profile.will_renew === false && (
                  <span className='text-muted-foreground text-[10px]'>
                    · {t('No auto-renew')}
                  </span>
                )}
            </>
          ) : (
            <span className='text-muted-foreground text-[11px]'>
              {t('Not set')}
            </span>
          )}
        </div>
        {props.onEditExpiry && (
          <Button
            type='button'
            variant='ghost'
            size='icon-xs'
            disabled={props.disabled}
            title={t('Set expiry time')}
            aria-label={t('Set expiry time')}
            onClick={() => props.onEditExpiry?.(account)}
          >
            <Pencil className='size-3' />
          </Button>
        )}
      </div>

      {/* Compact CPA-style quota windows */}
      <div className='space-y-1.5 px-3.5 pb-2'>
        {windows.length > 0 ? (
          windows.map((w, idx) => {
            const pct = Math.min(
              100,
              Math.max(0, w.limit > 0 ? (w.used / w.limit) * 100 : 0)
            )
            const remaining = Math.max(0, w.limit - w.used)
            const resetCd =
              w.resetAt && w.resetAt * 1000 > nowMs
                ? computeExpiryCountdown(w.resetAt, nowMs)
                : null
            const resetText = resetCd
              ? formatDurationCompact(
                  resetCd.days,
                  resetCd.hours,
                  resetCd.minutes,
                  locale
                )
              : ''
            const label =
              resolveLocalizedText(w.name, i18n.language) || t('Limit')
            return (
              <div key={`${label}-${idx}`} className='space-y-1'>
                <div className='flex items-center justify-between gap-2 text-[11px]'>
                  <span className='font-medium'>{label}</span>
                  <div className='text-muted-foreground flex items-center gap-1.5 font-mono tabular-nums'>
                    <span className='text-foreground font-medium'>
                      {w.unit === '%'
                        ? `${formatNumber(Math.round(remaining), locale)}% ${t('left')}`
                        : `${formatNumber(w.used, locale)} / ${formatNumber(w.limit, locale)}${w.unit ? ` ${w.unit}` : ''}`}
                    </span>
                    {resetText && (
                      <span
                        className='text-[10px]'
                        title={formatTimestampToDate(w.resetAt)}
                      >
                        · {resetText}
                      </span>
                    )}
                  </div>
                </div>
                <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
                  <div
                    className={cn(
                      'h-full rounded-full transition-all',
                      getQuotaBarColorClass(pct)
                    )}
                    style={{ width: `${Math.max(pct > 0 ? 2 : 0, pct)}%` }}
                  />
                </div>
              </div>
            )
          })
        ) : account.profile.quota_error ? (
          <p className='text-destructive text-[11px]'>
            {t('Quota query unavailable')}
          </p>
        ) : (
          <p className='text-muted-foreground text-[11px]'>
            {t('Quota has not been queried')}
          </p>
        )}
        {account.profile.quota_checked_at > 0 && (
          <p className='text-muted-foreground text-[10px]'>
            {t('Last updated:')}{' '}
            {formatTimestampToDate(account.profile.quota_checked_at)}
          </p>
        )}
      </div>

      {/* Reset card bar (only for providers that support reset cards, e.g. GPT) */}
      {supportsReset && (
        <div className='bg-background/60 mx-3.5 mb-2 flex items-center justify-between gap-2 rounded-lg border border-dashed px-2.5 py-1.5 text-xs'>
          <div className='flex min-w-0 flex-wrap items-center gap-1.5'>
            <Ticket className='size-3.5 shrink-0 text-violet-500' />
            <span className='text-muted-foreground text-[11px]'>
              {t('Reset cards')}:
            </span>
            {availableResetCount !== null ? (
              <span
                className={cn(
                  'rounded px-1.5 py-px text-[11px] font-semibold',
                  availableResetCount > 0
                    ? 'bg-violet-500/15 text-violet-700 dark:text-violet-300'
                    : 'bg-muted text-muted-foreground'
                )}
              >
                {availableResetCount}
              </span>
            ) : (
              <span className='text-muted-foreground text-[11px]'>
                {t('Refresh to check')}
              </span>
            )}
            {resetExpiryCountdown && !resetExpiryCountdown.expired && (
              <span
                className='text-muted-foreground text-[10px]'
                title={formatTimestampToDate(earliestResetExpiry)}
              >
                ·{' '}
                {t('Expires in {{duration}}', {
                  duration: formatDurationCompact(
                    resetExpiryCountdown.days,
                    resetExpiryCountdown.hours,
                    resetExpiryCountdown.minutes,
                    locale
                  ),
                })}
              </span>
            )}
          </div>
          {props.onResetQuota && (
            <Button
              type='button'
              variant={
                availableResetCount && availableResetCount > 0
                  ? 'secondary'
                  : 'outline'
              }
              size='xs'
              disabled={
                props.disabled ||
                !!props.resetting ||
                !(availableResetCount && availableResetCount > 0)
              }
              isLoading={!!props.resetting}
              onClick={() => props.onResetQuota?.(account)}
            >
              <RotateCcw className='size-3' />
              {t('Use reset card')}
            </Button>
          )}
        </div>
      )}

      {/* Failure reason callout */}
      {account.state.last_error && (
        <div
          className='border-destructive/25 bg-destructive/5 text-destructive/90 mx-3.5 mb-2 rounded-md border px-2.5 py-1.5 text-[11px] leading-snug'
          title={account.state.last_error}
        >
          <span className='font-medium'>{t('Failure reason')}: </span>
          <span className='line-clamp-2 break-words'>
            {account.state.last_error}
          </span>
        </div>
      )}

      {/* Compact footer action bar */}
      <div className='bg-background/50 mt-auto flex items-center justify-between gap-1.5 border-t px-3 py-2'>
        <div className='flex flex-wrap items-center gap-1'>
          {props.onInspect && (
            <Button
              type='button'
              variant='outline'
              size='xs'
              disabled={props.disabled || !!props.inspecting}
              isLoading={!!props.inspecting}
              onClick={() => props.onInspect?.(account)}
            >
              <Activity className='size-3' />
              {t('Inspect')}
            </Button>
          )}
          <Button
            type='button'
            variant='outline'
            size='xs'
            disabled={props.disabled}
            onClick={() => props.onAction(account, 'refresh')}
          >
            <RefreshCw className='size-3' />
            {t('Refresh')}
          </Button>
          {account.state.degraded ? (
            <Button
              type='button'
              variant='outline'
              size='xs'
              disabled={props.disabled}
              onClick={() => props.onAction(account, 'restore')}
            >
              <RotateCcw className='size-3' />
              {t('Restore')}
            </Button>
          ) : (
            <Button
              type='button'
              variant='outline'
              size='xs'
              disabled={props.disabled}
              onClick={() =>
                props.onAction(
                  account,
                  account.state.disabled ? 'enable' : 'disable'
                )
              }
            >
              <Power className='size-3' />
              {t(account.state.disabled ? 'Enable' : 'Disable')}
            </Button>
          )}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon-xs'
                disabled={props.disabled}
                aria-label={t('More actions')}
              />
            }
          >
            <MoreHorizontal className='size-3.5' />
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end' className='w-44'>
            {props.onEditExpiry && (
              <DropdownMenuItem onSelect={() => props.onEditExpiry?.(account)}>
                <CalendarClock className='size-3.5' />
                {t('Set expiry time')}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={() => props.onLogin(account)}>
              <KeyRound className='size-3.5' />
              {t('Log in again')}
            </DropdownMenuItem>
            {props.onTransfer && (
              <DropdownMenuItem onSelect={() => props.onTransfer?.(account)}>
                <ArrowRightLeft className='size-3.5' />
                {t('Move to channel')}
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant='destructive'
              onSelect={() => props.onAction(account, 'remove')}
            >
              <Trash2 className='size-3.5' />
              {t('Remove')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </article>
  )
}
