import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button, buttonVariants } from '@/components/ui/button'
import { ItemGroup } from '@/components/ui/item'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getProviderPool,
  operatePoolAccount,
  poolQueryKeys,
  type PoolAccount,
} from '../lib/provider-pool-api'
import { providerAccountHasIssue } from '../lib/provider-pool-issues'
import { PluginLoginDialog } from './dialogs/plugin-login-dialog'
import { ProviderAccountCard } from './provider-account-card'
import { ProviderAccountImport } from './provider-account-import'
import { ProviderPoolSettings } from './provider-pool-settings'

export function ProviderPoolPanel(props: {
  channelId: number | null
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
  const id = props.channelId || 0
  const query = useQuery({
    queryKey: poolQueryKeys.accounts(id),
    queryFn: () => getProviderPool(id),
    enabled: id > 0,
    refetchInterval: 15000,
  })
  const refresh = async (): Promise<void> => {
    // A mutation can affect any channel sharing this provider pool.
    await queryClient.invalidateQueries({ queryKey: ['provider-account-pool'] })
  }
  const action = useMutation({
    mutationFn: (value: { account: PoolAccount; operation: string }) =>
      operatePoolAccount(id, value.account.account_id, value.operation),
    onSuccess: async () => {
      setRemoving(null)
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
              onLogin={(value) =>
                setLogin({ owner: value.owner_id, index: value.index })
              }
              onAction={(value, operation) =>
                operation === 'remove'
                  ? setRemoving(value)
                  : action.mutate({ account: value, operation })
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
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null)
        }}
        title={t('Remove account')}
        desc={t(
          'This account will be removed from every channel sharing this pool.'
        )}
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
