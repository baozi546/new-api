import { useMutation } from '@tanstack/react-query'
import { useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { parseAccountImport } from '../lib/provider-account-import'
import { importPoolAccount } from '../lib/provider-pool-api'

export function ProviderAccountImport(props: {
  channelId: number
  disabled?: boolean
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [fileError, setFileError] = useState('')
  const [results, setResults] = useState<Record<number, string>>({})
  const fileInput = useRef<HTMLInputElement>(null)
  const inputId = useId()
  const entries = useMemo(() => parseAccountImport(text), [text])
  const validCount = entries.filter((entry) => entry.account).length
  const mutation = useMutation({
    mutationFn: async () => {
      setResults({})
      for (const [index, entry] of entries.entries()) {
        if (!entry.account) continue
        let result: string
        try {
          const response = await importPoolAccount(
            props.channelId,
            entry.account
          )
          result =
            response.status === 'exists' ? t('Already imported') : t('Imported')
          if (response.quota_error)
            {result += ` · ${t('Quota query unavailable')}`}
        } catch (error) {
          result = getServerErrorMessage(error, t('Import failed'))
        }
        setResults((previous) => ({ ...previous, [index]: result }))
      }
    },
    onSettled: props.onDone,
  })

  async function readFiles(files: FileList | null): Promise<void> {
    if (!files) return
    setFileError('')
    const values: string[] = []
    let size = 0
    for (const file of files) {
      size += file.size
      if (size > 8 * 1024 * 1024) {
        setFileError(t('Account import exceeds 8 MiB'))
        return
      }
      values.push(await file.text())
    }
    const parsed = values.flatMap(parseAccountImport)
    if (parsed.some((entry) => entry.error)) {
      setFileError(t('Invalid account JSON'))
      return
    }
    setText(
      JSON.stringify(
        parsed.map((entry) => entry.account),
        null,
        2
      )
    )
    setResults({})
  }

  return (
    <>
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={props.disabled}
        onClick={() => setOpen(true)}
      >
        {t('Import accounts')}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (mutation.isPending) return
          setOpen(value)
          if (!value) {
            setText('')
            setResults({})
            setFileError('')
          }
        }}
        title={t('Import accounts')}
        footer={
          <Button
            type='button'
            disabled={mutation.isPending || !validCount}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending
              ? t('Importing...')
              : t('Import {{count}} accounts', { count: validCount })}
          </Button>
        }
      >
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor={inputId}>
              {t('Paste account credentials')}
            </FieldLabel>
            <Textarea
              id={inputId}
              className='min-h-48 font-mono'
              value={text}
              disabled={mutation.isPending}
              autoComplete='off'
              spellCheck={false}
              onChange={(event) => {
                setText(event.target.value)
                setResults({})
              }}
            />
            <FieldDescription>
              {t(
                'Paste a JSON object, JSON array, JSONL, Codex auth.json or access token. Credentials stay in server storage.'
              )}
            </FieldDescription>
          </Field>
          <input
            ref={fileInput}
            type='file'
            accept='.json,.jsonl,application/json'
            multiple
            hidden
            aria-label={t('Account JSON files')}
            onChange={(event) => {
              void readFiles(event.target.files).catch(() =>
                setFileError(t('Could not read account files'))
              )
              event.target.value = ''
            }}
          />
          <Button
            type='button'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => fileInput.current?.click()}
          >
            {t('Choose JSON files')}
          </Button>
          {fileError && (
            <p role='alert' className='text-destructive text-sm'>
              {fileError}
            </p>
          )}
          <div
            aria-live='polite'
            className='max-h-56 space-y-1 overflow-y-auto text-sm'
          >
            {entries.map((entry, index) => (
              <p key={entry.key}>
                {t('Account')} {index + 1} ·{' '}
                {entry.error
                  ? t(entry.error)
                  : results[index] || t('Ready to import')}
              </p>
            ))}
          </div>
        </FieldGroup>
      </Dialog>
    </>
  )
}
