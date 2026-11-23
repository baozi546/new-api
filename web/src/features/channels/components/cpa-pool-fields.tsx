import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { Binding } from '../lib/cpa-channel-pools'

type Props = {
  pools: Binding[]
  pool: string
  category: string
  disabled?: boolean
  onPoolChange: (value: string) => void
  onCategoryChange: (value: string) => void
}

export function CPAPoolFields(props: Props) {
  const { t } = useTranslation()
  const pools = [
    ...new Map(
      props.pools
        .filter((channel) => channel.pool)
        .map((channel) => [channel.pool, channel])
    ).values(),
  ]
  return (
    <div className='grid gap-3 rounded-lg border p-4 sm:grid-cols-2'>
      <div className='space-y-2'>
        <label id='cpa-pool-label' className='text-sm font-medium'>
          {t('CPA channel pool')}
        </label>
        <Select
          value={props.pool || '__new__'}
          disabled={props.disabled}
          onValueChange={(value) => {
            if (value) props.onPoolChange(value === '__new__' ? '' : value)
          }}
        >
          <SelectTrigger aria-labelledby='cpa-pool-label'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='__new__'>
              {t('Create a dedicated pool')}
            </SelectItem>
            {pools.map((channel) => (
              <SelectItem key={channel.pool} value={channel.pool}>
                {t('Share pool with {{channel}}', { channel: channel.name })} (#
                {channel.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className='space-y-2'>
        <label id='cpa-category-label' className='text-sm font-medium'>
          {t('Route accounts by category')}
        </label>
        <Select
          value={props.category || '__all__'}
          disabled={props.disabled}
          onValueChange={(value) => {
            if (value) props.onCategoryChange(value === '__all__' ? '' : value)
          }}
        >
          <SelectTrigger aria-labelledby='cpa-category-label'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='__all__'>
              {t('Use all account categories')}
            </SelectItem>
            <SelectItem value='plus'>Plus</SelectItem>
            <SelectItem value='pro'>Pro</SelectItem>
            <SelectItem value='degraded'>{t('Reduced quality')}</SelectItem>
            <SelectItem value='unassigned'>{t('Unassigned')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <p className='text-muted-foreground text-xs sm:col-span-2'>
        {t(
          'Channels sharing a pool use the same accounts. Each channel calls only its selected account category.'
        )}
      </p>
    </div>
  )
}
