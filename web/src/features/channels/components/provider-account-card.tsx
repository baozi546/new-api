import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemTitle,
} from '@/components/ui/item'
import { formatTimestampToDate } from '@/lib/format'

import type { PoolAccount } from '../lib/provider-pool-api'
import { ChannelBalanceDetails } from './channel-balance-details'

const healthLabels: Record<string, string> = {
  passed: 'Passed',
  wrong_answer: 'Answer mismatch',
  rate_limited: 'Rate limited',
  login_required: 'Login required',
  account_banned: 'Account banned',
  inconclusive: 'Inconclusive',
}

export function ProviderAccountCard(props: {
  account: PoolAccount
  disabled: boolean
  onAction: (account: PoolAccount, action: string) => void
  onLogin: (account: PoolAccount) => void
  onTransfer?: (account: PoolAccount) => void
}) {
  const { t } = useTranslation()
  const account = props.account
  return (
    <Item variant='outline' className='items-start'>
      <ItemContent className='min-w-0 gap-2'>
        <ItemTitle className='flex-wrap break-all'>
          {account.label || account.account_id}
          <StatusBadge
            label={account.profile.plan}
            variant='neutral'
            copyable={false}
          />
          {!account.profile.plan_verified && (
            <StatusBadge
              label={t('Subscription unverified')}
              variant='warning'
              copyable={false}
            />
          )}
          {account.state.disabled && (
            <StatusBadge
              label={t('Disabled')}
              variant='warning'
              copyable={false}
            />
          )}
          {account.needs_login && (
            <StatusBadge
              label={t('Login required')}
              variant='warning'
              copyable={false}
            />
          )}
          {account.state.degraded && (
            <StatusBadge
              label={t('Reduced quality')}
              variant='warning'
              copyable={false}
            />
          )}
        </ItemTitle>
        <ItemDescription className='break-all'>
          {account.account_id}
        </ItemDescription>
        <ChannelBalanceDetails
          result={{
            windows: account.profile.windows || [],
            error: account.profile.quota_error
              ? t('Quota query unavailable')
              : undefined,
          }}
          updatedTime={account.profile.quota_checked_at}
          showProgress
        />
        {!account.profile.quota_checked_at && (
          <p className='text-muted-foreground text-xs'>
            {t('Quota has not been queried')}
          </p>
        )}
        {account.state.health && (
          <p className='text-sm'>
            {t(healthLabels[account.state.health] || 'Inconclusive')}
            {account.state.checked_at
              ? ` · ${formatTimestampToDate(account.state.checked_at)}`
              : ''}
          </p>
        )}
        <ItemDescription>
          {t('Channels and groups')} ·{' '}
          {account.channels
            .map((channel) => `${channel.name} (${channel.groups.join(', ')})`)
            .join(' · ')}
        </ItemDescription>
      </ItemContent>
      <ItemActions className='flex-wrap justify-start'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled}
          onClick={() => props.onAction(account, 'refresh')}
        >
          {t('Refresh quota')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled}
          onClick={() => props.onLogin(account)}
        >
          {t('Log in again')}
        </Button>
        {!account.state.degraded && (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() =>
              props.onAction(
                account,
                account.state.disabled ? 'enable' : 'disable'
              )
            }
          >
            {t(account.state.disabled ? 'Enable' : 'Disable')}
          </Button>
        )}
        {account.state.degraded && (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => props.onAction(account, 'restore')}
          >
            {t('Restore account')}
          </Button>
        )}
        {props.onTransfer && (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => props.onTransfer?.(account)}
          >
            {t('Move to channel')}
          </Button>
        )}
        <Button
          type='button'
          variant='ghost'
          size='sm'
          disabled={props.disabled}
          onClick={() => props.onAction(account, 'remove')}
        >
          {t('Remove')}
        </Button>
      </ItemActions>
    </Item>
  )
}
