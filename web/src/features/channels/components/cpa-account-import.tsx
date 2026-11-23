import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { request } from '../lib/cpa-channel-pools'

export function CPAAccountImport({
  channelId,
  category,
  onChange,
}: {
  channelId: number
  category: string
  onChange: () => Promise<void>
}) {
  const { t } = useTranslation()
  const picker = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const [results, setResults] = useState<{ label: string; message: string }[]>(
    []
  )

  async function importFiles(files: File[]) {
    if (!files.length || busy) return
    setBusy(true)
    setResults([])
    const report = (label: string, message: string) =>
      setResults((prev) => [...prev, { label, message }])
    try {
      for (const file of files) {
        let entries: unknown[]
        try {
          if (file.size > 8 * 1024 * 1024) throw new Error()
          const content: unknown = JSON.parse(
            (await file.text()).replace(/^\uFEFF/, '')
          )
          entries = Array.isArray(content) ? content : [content]
          if (!entries.length) throw new Error()
        } catch {
          report(file.name, t('Invalid CPA backup'))
          continue
        }
        for (const [i, account] of entries.entries()) {
          const label =
            entries.length === 1 ? file.name : `${file.name} #${i + 1}`
          if (
            !account ||
            typeof account !== 'object' ||
            Array.isArray(account)
          ) {
            report(label, t('Invalid CPA backup'))
            continue
          }
          try {
            const { status } = await request<{ status: string }>(
              'POST',
              '/import',
              {
                channel_id: channelId,
                account: category
                  ? { ...account, account_pool: category }
                  : account,
              }
            )
            const messages: Record<string, string> = {
              imported: t('Imported (disabled)'),
              exists: t('Account already exists'),
              other_pool: t('Account already belongs to another channel pool'),
              invalid: t('Invalid CPA backup'),
            }
            report(label, messages[status] ?? t('Import failed'))
          } catch {
            report(label, t('Import failed'))
          }
        }
      }
    } finally {
      setBusy(false)
      await onChange()
    }
  }
  return (
    <div className='space-y-2'>
      <input
        type='file'
        aria-label={t('Batch import JSON')}
        accept='.json,application/json'
        multiple
        hidden
        ref={picker}
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? [])
          event.currentTarget.value = ''
          void importFiles(files)
        }}
      />
      <Button
        variant='outline'
        disabled={busy}
        onClick={() => picker.current?.click()}
      >
        {t('Batch import JSON')}
      </Button>
      {results.length > 0 && (
        <ul aria-live='polite' className='max-h-40 overflow-y-auto text-sm'>
          {results.map(({ label, message }, i) => (
            <li key={i}>
              {label} · {message}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
