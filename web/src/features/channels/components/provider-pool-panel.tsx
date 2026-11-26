import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button, buttonVariants } from '@/components/ui/button'
import { ItemGroup } from '@/components/ui/item'
import { Field, FieldLabel } from '@/components/ui/field'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getProviderPool,
  inspectPoolAccount,
  operatePoolAccount,
  poolQueryKeys,
  type PoolAccount,
  type PoolChannel,
} from '../lib/provider-pool-api'
import { providerAccountHasIssue } from '../lib/provider-pool-issues'
import { PluginLoginDialog } from './dialogs/plugin-login-dialog'
import { ProviderAccountCard } from './provider-account-card'
import { ProviderAccountImport } from './provider-account-import'
import { ProviderPoolSettings } from './provider-pool-settings'

export function ProviderPoolPanel(props: {
  channelId: number | null
  channelName: string
  channels: PoolChannel[]
  plugin: { key: string; name: string }
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [problemsOnly, setProblemsOnly] = useState(false)
  const [login, setLogin] = useState<{
    owner: number
    index: number | null
  } | null>(null)
  const [removing, setRemoving] = useState<PoolAccount | null>(null)
  const [transferring, setTransferring] = useState<PoolAccount | null>(null)
  const [transferTargetId, setTransferTargetId] = useState<number | null>(null)
  const [inspectingId, setInspectingId] = useState<string | null>(null)
  const id = props.channelId || 0
  const query = useQuery({
    queryKey: poolQueryKeys.accounts(id),
    queryFn: () => getProviderPool(id),
    enabled: id > 0,
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
      setInspectingId(account.account_id)
      return inspectPoolAccount(id, account.account_id)
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
      setInspectingId(null)
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
        id,
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
  })
  const refreshAll = useMutation({
    mutationFn: async () => {
      for (const account of query.data?.items || [])
        {await operatePoolAccount(id, account.account_id, 'refresh')}
    },
    onSettled: refresh,
  })
  if (!id)
    {return (
      <p className='text-muted-foreground text-sm'>
        {t('Save the channel to manage its account pool.')}
      </p>
    )}
  if (query.isPending) return <LoadingState inline />
  if (query.isError)
    {return (
      <ErrorState
        title={t('Failed to load accounts')}
        description={getServerErrorMessage(query.error)}
        onRetry={() => void query.refetch()}
      />
    )}
  const accounts = query.data.items || []
  const problems = accounts.filter(providerAccountHasIssue)
  const visible = problemsOnly ? problems : accounts
  const transferTargets = props.channels.filter(
    (channel) => channel.id !== id && channel.plugin.key === props.plugin.key
  )
  const eligibleTransferTargets = transferring
    ? transferTargets.filter((channel) =>
        !transferring.profile.plan_verified ||
        !transferring.profile.category ||
        channel.groups.some(
          (group) =>
            group.trim().toLowerCase() ===
            transferring.profile.category.trim().toLowerCase()
        )
      )
    : []
  const busy = !!props.disabled || action.isPending || refreshAll.isPending
  return (
    <div className='space-y-4'>
      <ProviderPoolSettings
        key={JSON.stringify(query.data.settings)}
        channelId={id}
        settings={query.data.settings}
        disabled={busy}
      />
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          size='sm'
          disabled={busy}
          onClick={() => setLogin({ owner: id, index: null })}
        >
          {t('Add account')}
        </Button>
        <ProviderAccountImport
          channelId={id}
          channelName={props.channelName}
          disabled={busy}
          onDone={() => void refresh()}
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={busy || !accounts.length}
          onClick={() => refreshAll.mutate()}
        >
          {t('Refresh all quotas')}
        </Button>
        <Button
          type='button'
          variant={problemsOnly ? 'default' : 'outline'}
          size='sm'
          aria-pressed={problemsOnly}
          onClick={() => setProblemsOnly(!problemsOnly)}
        >
          {t('Problem accounts')} ({problems.length})
        </Button>
        <Link
          to='/channel-inspections'
          className={buttonVariants({ variant: 'outline', size: 'sm' })}
        >
          {t('Channel inspections')}
        </Link>
      </div>
      {visible.length === 0 ? (
        <EmptyState
          title={t(
            problemsOnly ? 'No problem accounts' : 'No accounts imported'
          )}
        />
      ) : (
        <ItemGroup className='gap-3'>
          {visible.map((account) => (
            <ProviderAccountCard
              key={account.account_id}
              account={account}
              disabled={busy}
              inspecting={
                inspectMutation.isPending && inspectingId === account.account_id
              }
              onInspect={(value) => inspectMutation.mutate(value)}
              onLogin={(value) =>
                setLogin({ owner: value.owner_id, index: value.index })
              }
              onAction={(value, operation) =>
                operation === 'remove'
                  ? setRemoving(value)
                  : action.mutate({ account: value, operation })
              }
              onTransfer={
                transferTargets.some(
                  (channel) =>
                    !account.profile.plan_verified ||
                    !account.profile.category ||
                    channel.groups.some(
                      (group) =>
                        group.trim().toLowerCase() ===
                        account.profile.category.trim().toLowerCase()
                    )
                )
                  ? (value) => {
                      const targets = transferTargets.filter(
                        (channel) =>
                          !value.profile.plan_verified ||
                          !value.profile.category ||
                          channel.groups.some(
                            (group) =>
                              group.trim().toLowerCase() ===
                              value.profile.category.trim().toLowerCase()
                          )
                      )
                      setTransferring(value)
                      setTransferTargetId(targets[0]?.id || null)
                    }
                  : undefined
              }
            />
          ))}
        </ItemGroup>
      )}
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
          plugin={props.plugin}
          onDone={() => {
            void refresh()
            setLogin(null)
          }}
        />
      )}
      <Dialog
        open={!!transferring}
        onOpenChange={(open) => {
          if (!open) {
            setTransferring(null)
            setTransferTargetId(null)
          }
        }}
        title={t('Move account to another channel')}
        description={t('Moving transfers the account, quota, and inspection status to the selected channel.')}
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
            <FieldLabel htmlFor={`transfer-account-${transferring.account_id}`}>
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
          if (removing)
            {action.mutate({ account: removing, operation: 'remove' })}
        }}
      />
    </div>
  )
}
