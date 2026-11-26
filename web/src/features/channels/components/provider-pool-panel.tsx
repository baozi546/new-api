import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Plus,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button, buttonVariants } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatTimestampToDate } from '@/lib/format'
import { resolveLocalizedText } from '@/lib/localized-text'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  getAllProviderPoolAccounts,
  getProviderPool,
  inspectPoolAccount,
  operatePoolAccount,
  poolQueryKeys,
  resetPoolAccountQuota,
  setPoolAccountExpiry,
  type PoolAccount,
  type PoolChannel,
  type PoolPlugin,
} from '../lib/provider-pool-api'
import { providerAccountHasActiveProblem } from '../lib/provider-pool-issues'
import {
  getEffectiveExpiry,
  getPlanStyle,
  isAccountExpiringOrExpired,
} from '../lib/provider-pool-meta'
import { PluginLoginDialog } from './dialogs/plugin-login-dialog'
import { ProviderAccountCard } from './provider-account-card'
import { ProviderAccountImport } from './provider-account-import'
import { ProviderPoolSettings } from './provider-pool-settings'

type StatusFilter = 'all' | 'active' | 'problem' | 'disabled' | 'expiring'

function toDatetimeLocalValue(unixSeconds: number): string {
  if (!unixSeconds || unixSeconds <= 0) return ''
  const date = new Date(unixSeconds * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function ProviderPoolPanel(props: {
  channelId: number | null
  channelName: string
  channels: PoolChannel[]
  plugin?: PoolPlugin
  pluginFilter?: string
  viewAll?: boolean
  disabled?: boolean
}) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [planFilter, setPlanFilter] = useState<string>('all')
  const [searchText, setSearchText] = useState('')
  const [showSettings, setShowSettings] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())

  const [login, setLogin] = useState<{
    owner: number
    index: number | null
    plugin: PoolPlugin
  } | null>(null)
  const [removing, setRemoving] = useState<PoolAccount | null>(null)
  const [transferring, setTransferring] = useState<PoolAccount | null>(null)
  const [transferTargetId, setTransferTargetId] = useState<number | null>(null)
  const [inspectingKey, setInspectingKey] = useState<string | null>(null)
  const [resetTarget, setResetTarget] = useState<PoolAccount | null>(null)
  const [resettingKey, setResettingKey] = useState<string | null>(null)
  const [expiryTarget, setExpiryTarget] = useState<PoolAccount | null>(null)
  const [expiryInput, setExpiryInput] = useState('')
  const [addChannelPickId, setAddChannelPickId] = useState<number>(
    props.channelId || props.channels[0]?.id || 0
  )

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 30000)
    return () => window.clearInterval(timer)
  }, [])

  const isAllMode = !!props.viewAll && !props.channelId
  const id = props.channelId || 0

  const effectiveTargetChannel = useMemo(() => {
    if (id > 0) {
      return props.channels.find((ch) => ch.id === id) || props.channels[0]
    }
    const filtered = props.pluginFilter
      ? props.channels.filter((ch) => ch.plugin.key === props.pluginFilter)
      : props.channels
    return (
      filtered.find((ch) => ch.id === addChannelPickId) ||
      filtered[0] ||
      props.channels[0]
    )
  }, [id, props.channels, props.pluginFilter, addChannelPickId])

  const singleQuery = useQuery({
    queryKey: poolQueryKeys.accounts(id),
    queryFn: () => getProviderPool(id),
    enabled: !isAllMode && id > 0,
    refetchInterval: 15000,
  })

  const allQuery = useQuery({
    queryKey: poolQueryKeys.allAccounts(props.pluginFilter),
    queryFn: () => getAllProviderPoolAccounts(props.pluginFilter),
    enabled: isAllMode,
    refetchInterval: 15000,
  })

  const refresh = async (): Promise<void> => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['provider-account-pool'] }),
      queryClient.invalidateQueries({ queryKey: poolQueryKeys.channels }),
      queryClient.invalidateQueries({ queryKey: ['channel-inspections'] }),
    ])
  }

  const inspectOutcomeLabels: Record<string, string> = {
    passed: 'Passed',
    wrong_answer: 'Answer mismatch',
    rate_limited: 'Rate limited',
    login_required: 'Login required',
    account_banned: 'Account banned',
    disabled: 'Disabled',
    inconclusive: 'Inconclusive',
  }

  const inspectMutation = useMutation({
    mutationFn: async (account: PoolAccount) => {
      setInspectingKey(`${account.owner_id}:${account.account_id}`)
      return inspectPoolAccount(account.owner_id, account.account_id)
    },
    onSuccess: (res, account) => {
      const label = account.label || account.account_id
      const outcome = t(inspectOutcomeLabels[res.result] || 'Inconclusive')
      if (res.result === 'passed') {
        toast.success(`${label}: ${outcome}`)
      } else {
        toast.error(
          res.reason
            ? `${label}: ${outcome} (${res.reason})`
            : `${label}: ${outcome}`
        )
      }
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
    onSettled: async () => {
      setInspectingKey(null)
      await refresh()
    },
  })

  const resetQuotaMutation = useMutation({
    mutationFn: async (account: PoolAccount) => {
      setResettingKey(`${account.owner_id}:${account.account_id}`)
      return resetPoolAccountQuota(account.owner_id, account.account_id)
    },
    onSuccess: (_res, account) => {
      toast.success(
        t('Reset card applied for {{name}}', {
          name: account.label || account.account_id,
        })
      )
      setResetTarget(null)
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
    onSettled: async () => {
      setResettingKey(null)
      await refresh()
    },
  })

  const setExpiryMutation = useMutation({
    mutationFn: async (input: {
      account: PoolAccount
      customExpiresAt: number
    }) =>
      setPoolAccountExpiry(
        input.account.owner_id,
        input.account.account_id,
        input.customExpiresAt
      ),
    onSuccess: () => {
      toast.success(t('Expiry time updated'))
      setExpiryTarget(null)
      setExpiryInput('')
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
    onSettled: async () => {
      await refresh()
    },
  })

  const action = useMutation({
    mutationFn: (value: {
      account: PoolAccount
      operation: string
      targetChannelId?: number
    }) =>
      operatePoolAccount(
        value.account.owner_id,
        value.account.account_id,
        value.operation,
        value.targetChannelId
      ),
    onSuccess: async () => {
      setRemoving(null)
      setTransferring(null)
      setTransferTargetId(null)
      await refresh()
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
  })

  const accounts = useMemo(
    () =>
      isAllMode
        ? allQuery.data?.items || []
        : singleQuery.data?.items || [],
    [isAllMode, allQuery.data?.items, singleQuery.data?.items]
  )

  const refreshAll = useMutation({
    mutationFn: async () => {
      for (const account of accounts) {
        await operatePoolAccount(
          account.owner_id,
          account.account_id,
          'refresh'
        )
      }
    },
    onSuccess: () => {
      toast.success(t('Quotas refreshed'))
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
    onSettled: refresh,
  })

  const planOptions = useMemo(() => {
    const map = new Map<string, { key: string; label: string; count: number }>()
    for (const account of accounts) {
      const style = getPlanStyle(
        account.profile.plan,
        account.profile.category
      )
      const prev = map.get(style.key)
      if (prev) {
        prev.count += 1
      } else {
        map.set(style.key, { key: style.key, label: style.label, count: 1 })
      }
    }
    return Array.from(map.values()).sort((a, b) => b.count - a.count)
  }, [accounts])

  const counts = useMemo(() => {
    let active = 0
    let problem = 0
    let disabled = 0
    let expiring = 0
    for (const account of accounts) {
      const hasProblem = providerAccountHasActiveProblem(account)
      const isExpiring = isAccountExpiringOrExpired(account, nowMs)
      if (account.state.disabled) {
        disabled += 1
      } else if (!hasProblem) {
        active += 1
      }
      if (hasProblem) {
        problem += 1
      }
      if (isExpiring) {
        expiring += 1
      }
    }
    return {
      total: accounts.length,
      active,
      problem,
      disabled,
      expiring,
    }
  }, [accounts, nowMs])

  const visible = useMemo(() => {
    const q = searchText.trim().toLowerCase()
    return accounts.filter((account) => {
      if (statusFilter === 'active') {
        if (
          account.state.disabled ||
          providerAccountHasActiveProblem(account)
        ) {
          return false
        }
      } else if (statusFilter === 'problem') {
        if (!providerAccountHasActiveProblem(account)) return false
      } else if (statusFilter === 'disabled') {
        if (!account.state.disabled) return false
      } else if (statusFilter === 'expiring') {
        if (!isAccountExpiringOrExpired(account, nowMs)) return false
      }

      if (planFilter !== 'all') {
        const style = getPlanStyle(
          account.profile.plan,
          account.profile.category
        )
        if (style.key !== planFilter) return false
      }

      if (q) {
        const hay = [
          account.label,
          account.account_id,
          account.profile.plan,
          account.plugin_key || '',
          ...account.channels.map((c) => `${c.name} ${c.groups.join(' ')}`),
        ]
          .join(' ')
          .toLowerCase()
        if (!hay.includes(q)) return false
      }

      return true
    })
  }, [accounts, statusFilter, planFilter, searchText, nowMs])

  if (!isAllMode && !id) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('Save the channel to manage its account pool.')}
      </p>
    )
  }

  const activeQuery = isAllMode ? allQuery : singleQuery
  if (activeQuery.isPending) return <LoadingState inline />
  if (activeQuery.isError) {
    return (
      <ErrorState
        title={t('Failed to load accounts')}
        description={getServerErrorMessage(activeQuery.error)}
        onRetry={() => void activeQuery.refetch()}
      />
    )
  }

  const getTransferTargetsFor = (account: PoolAccount): PoolChannel[] => {
    const accountPluginKey = account.plugin_key || props.plugin?.key || ''
    return props.channels.filter(
      (channel) =>
        channel.id !== account.owner_id &&
        channel.plugin.key === accountPluginKey &&
        (!account.profile.plan_verified ||
          !account.profile.category ||
          account.profile.category === 'unknown' ||
          channel.groups.some(
            (group) =>
              group.trim().toLowerCase() ===
              account.profile.category.trim().toLowerCase()
          ))
    )
  }

  const eligibleTransferTargets = transferring
    ? getTransferTargetsFor(transferring)
    : []

  const busy =
    !!props.disabled ||
    action.isPending ||
    refreshAll.isPending ||
    resetQuotaMutation.isPending ||
    setExpiryMutation.isPending

  const statItems: {
    key: StatusFilter
    label: string
    count: number
    toneClass: string
    dotClass: string
  }[] = [
    {
      key: 'all',
      label: t('All accounts'),
      count: counts.total,
      toneClass: 'border-border',
      dotClass: 'bg-foreground/70',
    },
    {
      key: 'active',
      label: t('Enabled'),
      count: counts.active,
      toneClass: 'border-emerald-300/70 dark:border-emerald-900/70',
      dotClass: 'bg-emerald-500',
    },
    {
      key: 'problem',
      label: t('Issues'),
      count: counts.problem,
      toneClass: 'border-red-300/70 dark:border-red-900/70',
      dotClass: 'bg-red-500',
    },
    {
      key: 'disabled',
      label: t('Disabled'),
      count: counts.disabled,
      toneClass: 'border-zinc-300/70 dark:border-zinc-700/70',
      dotClass: 'bg-zinc-400',
    },
    {
      key: 'expiring',
      label: t('Expiring soon'),
      count: counts.expiring,
      toneClass: 'border-amber-300/70 dark:border-amber-900/70',
      dotClass: 'bg-amber-500',
    },
  ]

  return (
    <div className='space-y-3.5'>
      {/* CPA-style clickable status summary strip */}
      <div className='grid grid-cols-2 gap-2 sm:grid-cols-5'>
        {statItems.map((item) => {
          const active = statusFilter === item.key
          return (
            <button
              key={item.key}
              type='button'
              onClick={() => setStatusFilter(item.key)}
              aria-pressed={active}
              className={cn(
                'bg-card flex items-center justify-between rounded-lg border px-3 py-2 text-left transition-all hover:shadow-2xs',
                item.toneClass,
                active && 'ring-primary bg-primary/5 ring-2'
              )}
            >
              <div className='flex min-w-0 items-center gap-2'>
                <span
                  className={cn('size-2 shrink-0 rounded-full', item.dotClass)}
                />
                <span className='text-muted-foreground truncate text-xs font-medium'>
                  {item.label}
                </span>
              </div>
              <span className='font-mono text-base font-bold tabular-nums'>
                {item.count}
              </span>
            </button>
          )
        })}
      </div>

      {/* Action & filter toolbar */}
      <div className='flex flex-wrap items-center justify-between gap-2 rounded-xl border bg-card/60 p-2.5'>
        <div className='flex flex-wrap items-center gap-2'>
          {isAllMode && props.channels.length > 1 && effectiveTargetChannel && (
            <Select
              value={String(effectiveTargetChannel.id)}
              items={props.channels
                .filter(
                  (ch) =>
                    !props.pluginFilter || ch.plugin.key === props.pluginFilter
                )
                .map((ch) => ({
                  value: String(ch.id),
                  label: ch.name,
                }))}
              onValueChange={(value) => setAddChannelPickId(Number(value))}
            >
              <SelectTrigger
                className='h-8 w-36 text-xs'
                aria-label={t('Target channel')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {props.channels
                  .filter(
                    (ch) =>
                      !props.pluginFilter ||
                      ch.plugin.key === props.pluginFilter
                  )
                  .map((ch) => (
                    <SelectItem key={ch.id} value={String(ch.id)}>
                      {ch.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          )}

          {effectiveTargetChannel && (
            <>
              <Button
                type='button'
                size='sm'
                disabled={busy}
                onClick={() =>
                  setLogin({
                    owner: effectiveTargetChannel.id,
                    index: null,
                    plugin: effectiveTargetChannel.plugin,
                  })
                }
              >
                <Plus className='size-3.5' />
                {t('Add account')}
              </Button>
              <ProviderAccountImport
                channelId={effectiveTargetChannel.id}
                channelName={effectiveTargetChannel.name}
                disabled={busy}
                onDone={() => void refresh()}
              />
            </>
          )}

          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={busy || !accounts.length}
            isLoading={refreshAll.isPending}
            onClick={() => refreshAll.mutate()}
          >
            <RefreshCw className='size-3.5' />
            {t('Refresh all quotas')}
          </Button>

          {!isAllMode && singleQuery.data?.settings && (
            <Button
              type='button'
              variant={showSettings ? 'secondary' : 'outline'}
              size='sm'
              onClick={() => setShowSettings((prev) => !prev)}
            >
              <Settings2 className='size-3.5' />
              {t('Concurrency')}: {singleQuery.data.settings.concurrency}
            </Button>
          )}

          <Link
            to='/channel-inspections'
            className={buttonVariants({ variant: 'outline', size: 'sm' })}
          >
            <ShieldCheck className='size-3.5' />
            {t('Channel inspections')}
          </Link>
        </div>

        {/* Search + Plan tier filters */}
        <div className='flex flex-wrap items-center gap-2'>
          <div className='relative w-52'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
            <Input
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
              placeholder={t('Search email, ID, channel...')}
              className='h-8 pl-8 text-xs'
            />
          </div>
          {planOptions.length > 1 && (
            <div className='flex flex-wrap items-center gap-1'>
              <Button
                type='button'
                variant={planFilter === 'all' ? 'secondary' : 'ghost'}
                size='xs'
                onClick={() => setPlanFilter('all')}
              >
                {t('All plans')}
              </Button>
              {planOptions.map((opt) => (
                <Button
                  key={opt.key}
                  type='button'
                  variant={planFilter === opt.key ? 'secondary' : 'ghost'}
                  size='xs'
                  onClick={() => setPlanFilter(opt.key)}
                >
                  {opt.label} ({opt.count})
                </Button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Collapsible per-channel concurrency settings */}
      {!isAllMode && showSettings && singleQuery.data?.settings && (
        <ProviderPoolSettings
          key={JSON.stringify(singleQuery.data.settings)}
          channelId={id}
          settings={singleQuery.data.settings}
          disabled={busy}
        />
      )}

      {/* Compact multi-column CPA-style card grid */}
      {visible.length === 0 ? (
        <EmptyState
          title={t(
            accounts.length === 0
              ? 'No accounts imported'
              : 'No matching accounts'
          )}
        />
      ) : (
        <div className='grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3'>
          {visible.map((account) => {
            const cardKey = `${account.owner_id}:${account.account_id}`
            const targets = getTransferTargetsFor(account)
            const ownerChannel = props.channels.find(
              (ch) => ch.id === account.owner_id
            )
            return (
              <ProviderAccountCard
                key={cardKey}
                account={account}
                disabled={busy}
                showChannelBadge={isAllMode}
                inspecting={
                  inspectMutation.isPending && inspectingKey === cardKey
                }
                resetting={
                  resetQuotaMutation.isPending && resettingKey === cardKey
                }
                onInspect={(value) => inspectMutation.mutate(value)}
                onResetQuota={(value) => setResetTarget(value)}
                onEditExpiry={(value) => {
                  setExpiryTarget(value)
                  const eff = getEffectiveExpiry(value)
                  setExpiryInput(
                    eff.source === 'manual' || eff.source === 'subscription'
                      ? toDatetimeLocalValue(eff.timestamp)
                      : ''
                  )
                }}
                onLogin={(value) => {
                  const pluginObj: PoolPlugin = ownerChannel?.plugin ||
                    props.plugin || {
                      key: value.plugin_key || '',
                      name:
                        value.plugin_name || value.plugin_key || 'Provider',
                    }
                  setLogin({
                    owner: value.owner_id,
                    index: value.index,
                    plugin: pluginObj,
                  })
                }}
                onAction={(value, operation) =>
                  operation === 'remove'
                    ? setRemoving(value)
                    : action.mutate({ account: value, operation })
                }
                onTransfer={
                  targets.length > 0
                    ? (value) => {
                        setTransferring(value)
                        setTransferTargetId(targets[0]?.id || null)
                      }
                    : undefined
                }
              />
            )
          })}
        </div>
      )}

      {/* OAuth Login / Re-login dialog */}
      {login && (
        <PluginLoginDialog
          open
          onOpenChange={(open) => {
            if (!open) setLogin(null)
          }}
          channelId={login.owner}
          keyIndex={login.index}
          add
          pool
          plugin={{
            key: login.plugin.key,
            name:
              resolveLocalizedText(login.plugin.name, i18n.language) ||
              login.plugin.key,
          }}
          onDone={() => {
            void refresh()
            setLogin(null)
          }}
        />
      )}

      {/* Reset Card confirmation dialog */}
      <ConfirmDialog
        open={!!resetTarget}
        onOpenChange={(open) => {
          if (!open) setResetTarget(null)
        }}
        title={t('Use quota reset card')}
        desc={
          resetTarget ? (
            <div className='space-y-2 text-sm'>
              <p>
                {t(
                  'This will consume 1 rate-limit reset card to reset the quota windows for "{{name}}". Continue?',
                  { name: resetTarget.label || resetTarget.account_id }
                )}
              </p>
              {resetTarget.profile.reset_credits &&
                resetTarget.profile.reset_credits.applicable_available_count ===
                  0 && (
                  <p className='text-amber-600 dark:text-amber-400 text-xs'>
                    {t(
                      'Note: This account has not currently hit its rate limit ceiling. Upstream may reject or still consume the reset card.'
                    )}
                  </p>
                )}
            </div>
          ) : (
            ''
          )
        }
        confirmText={t('Confirm reset')}
        isLoading={resetQuotaMutation.isPending}
        handleConfirm={() => {
          if (resetTarget) {
            resetQuotaMutation.mutate(resetTarget)
          }
        }}
      />

      {/* Manual expiry time editor dialog */}
      <Dialog
        open={!!expiryTarget}
        onOpenChange={(open) => {
          if (!open) {
            setExpiryTarget(null)
            setExpiryInput('')
          }
        }}
        title={t('Set account expiry time')}
        description={
          expiryTarget
            ? t(
                'Set or override the subscription expiry time for "{{name}}". Leave empty or clear to use the auto-detected subscription date.',
                { name: expiryTarget.label || expiryTarget.account_id }
              )
            : ''
        }
        footer={
          <div className='flex flex-wrap items-center gap-2'>
            {expiryTarget && (expiryTarget.state.custom_expires_at || 0) > 0 && (
              <Button
                type='button'
                variant='outline'
                disabled={setExpiryMutation.isPending}
                onClick={() => {
                  if (!expiryTarget) return
                  setExpiryMutation.mutate({
                    account: expiryTarget,
                    customExpiresAt: 0,
                  })
                }}
              >
                {t('Reset to auto-detected')}
              </Button>
            )}
            <Button
              type='button'
              disabled={setExpiryMutation.isPending || !expiryInput}
              isLoading={setExpiryMutation.isPending}
              onClick={() => {
                if (!expiryTarget || !expiryInput) return
                const ts = Math.floor(new Date(expiryInput).getTime() / 1000)
                if (!Number.isFinite(ts) || ts <= 0) {
                  toast.error(t('Invalid date'))
                  return
                }
                setExpiryMutation.mutate({
                  account: expiryTarget,
                  customExpiresAt: ts,
                })
              }}
            >
              {t('Save')}
            </Button>
          </div>
        }
      >
        {expiryTarget && (
          <div className='space-y-3'>
            <Field>
              <FieldLabel htmlFor='account-custom-expiry'>
                {t('Expiry date and time')}
              </FieldLabel>
              <Input
                id='account-custom-expiry'
                type='datetime-local'
                value={expiryInput}
                onChange={(e) => setExpiryInput(e.target.value)}
              />
              {(expiryTarget.profile.subscription_expires_at || 0) > 0 && (
                <FieldDescription>
                  {t('Auto-detected subscription expiry:')}{' '}
                  {formatTimestampToDate(
                    expiryTarget.profile.subscription_expires_at!
                  )}
                </FieldDescription>
              )}
            </Field>
          </div>
        )}
      </Dialog>

      {/* Transfer account dialog */}
      <Dialog
        open={!!transferring}
        onOpenChange={(open) => {
          if (!open) {
            setTransferring(null)
            setTransferTargetId(null)
          }
        }}
        title={t('Move account to another channel')}
        description={t(
          'Moving transfers the account, quota, and inspection status to the selected channel.'
        )}
        footer={
          <Button
            type='button'
            disabled={busy || !transferTargetId || !transferring}
            isLoading={action.isPending}
            onClick={() => {
              if (!transferring || !transferTargetId) return
              action.mutate({
                account: transferring,
                operation: 'transfer',
                targetChannelId: transferTargetId,
              })
            }}
          >
            {t('Move account')}
          </Button>
        }
      >
        {transferring && (
          <Field>
            <FieldLabel
              htmlFor={`transfer-account-${transferring.account_id}`}
            >
              {t('Target channel')}
            </FieldLabel>
            <Select
              value={transferTargetId ? String(transferTargetId) : ''}
              items={eligibleTransferTargets.map((channel) => ({
                value: String(channel.id),
                label: `${channel.name} (${channel.groups.join(', ')})`,
              }))}
              onValueChange={(value) => setTransferTargetId(Number(value))}
            >
              <SelectTrigger
                id={`transfer-account-${transferring.account_id}`}
                className='w-full'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {eligibleTransferTargets.map((channel) => (
                  <SelectItem key={channel.id} value={String(channel.id)}>
                    {channel.name} ({channel.groups.join(', ')})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        )}
      </Dialog>

      {/* Remove account confirmation */}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null)
        }}
        title={t('Remove account')}
        desc={t('This account will be removed from this channel.')}
        destructive
        isLoading={action.isPending}
        handleConfirm={() => {
          if (removing) {
            action.mutate({ account: removing, operation: 'remove' })
          }
        }}
      />
    </div>
  )
}
