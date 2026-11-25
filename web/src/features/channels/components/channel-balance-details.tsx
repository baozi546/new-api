/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { Progress } from '@/components/ui/progress'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { resolveLocalizedText } from '@/lib/localized-text'
import { cn } from '@/lib/utils'

import type { ChannelBalanceResult } from '../types'

type ChannelBalanceDetailsProps = {
  showProgress?: boolean
  result?: ChannelBalanceResult | null
  /** Unix seconds of the last query; 0 when never queried. */
  updatedTime?: number
  className?: string
}

function formatRemainingTime(resetAt: number, now: number, locale: string) {
  let remainingMinutes = Math.max(0, Math.ceil((resetAt * 1000 - now) / 60000))
  const parts: string[] = []
  const formatUnit = (value: number, unit: 'day' | 'hour' | 'minute') =>
    new Intl.NumberFormat(locale, {
      style: 'unit',
      unit,
      unitDisplay: 'short',
    }).format(value)

  const days = Math.floor(remainingMinutes / 1440)
  if (days > 0) {
    parts.push(formatUnit(days, 'day'))
    remainingMinutes -= days * 1440
  }
  const hours = Math.floor(remainingMinutes / 60)
  if (hours > 0) {
    parts.push(formatUnit(hours, 'hour'))
    remainingMinutes -= hours * 60
  }
  if (remainingMinutes > 0 && parts.length < 2) {
    parts.push(formatUnit(remainingMinutes, 'minute'))
  }
  return parts.join(' ')
}

/**
 * The limits of a balance with their reset times, when it was last queried
 * and the last query error, one line each.
 */
export function ChannelBalanceDetails(props: ChannelBalanceDetailsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30000)
    return () => window.clearInterval(timer)
  }, [])
  const windows = props.result?.windows ?? []
  const error = props.result?.error?.trim()
  if (windows.length === 0 && !props.updatedTime && !error) return null

  return (
    <ul className={cn('space-y-0.5 text-xs', props.className)}>
      {windows.map((window) => (
        <li key={JSON.stringify(window)}>
          {props.showProgress && (
            <Progress
              value={Math.min(
                100,
                Math.max(0, (window.used / window.limit) * 100)
              )}
              aria-label={
                resolveLocalizedText(window.name, i18n.language) || t('Limit')
              }
              className='my-1'
            />
          )}
          {resolveLocalizedText(window.name, i18n.language) || t('Limit')}:{' '}
          {formatNumber(window.used, locale)} /{' '}
          {formatNumber(window.limit, locale)}
          {window.unit ? ` ${window.unit}` : ''}
          {window.resetAt ? (
            <>
              {' · '}
              {t('Reset at:')} {formatTimestampToDate(window.resetAt)}
              {window.resetAt > now / 1000 && (
                <>
                  {' · '}
                  {t('Resets in:')}{' '}
                  {formatRemainingTime(window.resetAt, now, locale)}
                </>
              )}
            </>
          ) : null}
        </li>
      ))}
      {props.updatedTime ? (
        <li>
          {t('Last updated:')} {formatTimestampToDate(props.updatedTime)}
        </li>
      ) : null}
      {error && (
        <li className='wrap-anywhere'>
          {t('Last error:')} {error}
        </li>
      )}
    </ul>
  )
}
