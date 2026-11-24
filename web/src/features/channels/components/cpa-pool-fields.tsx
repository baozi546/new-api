import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type Props = {
  category: string
  disabled?: boolean
  onCategoryChange: (value: string) => void
}

export function CPAPoolFields(props: Props) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-3 rounded-lg border p-4 sm:grid-cols-2'>
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
        {t('Accounts at this CPA address are shared automatically.')}
      </p>
    </div>
  )
}
