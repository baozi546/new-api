import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

async function requestInspection<T>(
  method: 'GET' | 'POST',
  path = '',
  data?: unknown
): Promise<T> {
  const response = await api.request({
    method,
    url: `/api/channel-inspections/manage${path}`,
    data,
  })
  if (!response.data?.success) {
    throw new Error(
      typeof response.data?.message === 'string'
        ? response.data.message
        : 'Request failed'
    )
  }
  return response.data.data as T
}

type Settings = {
  reasoning_effort?: string
  degrade_after?: number
  title: string
  published: boolean
  scheduled: boolean
  minutes: number
  limit: number
  model: string
  questions: { prompt: string; answer: string }[]
}
type Card = {
  id?: number
  groups?: string[]
  name?: string
  title: string
  time: number
  failed: boolean
  running: boolean
  scheduled: boolean
  minutes: number
  settings?: Settings
  question?: string
  model?: string
  accounts: {
    code: string
    label?: string
    result: string
    number: number
    time: number
  }[]
}
const outcomes: Record<string, string> = {
  passed: 'Passed',
  wrong_answer: 'Answer mismatch',
  rate_limited: 'Rate limited',
  login_required: 'Login required',
  account_banned: 'Account banned',
  disabled: 'Disabled',
  inconclusive: 'Inconclusive',
}
function Editor({
  row,
  refresh,
}: {
  row: Card & { settings: Settings }
  refresh: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(row.settings)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const dirty = JSON.stringify(row.settings) !== JSON.stringify(draft)
  async function action(run: boolean) {
    setBusy(true)
    setMessage('')
    try {
      await requestInspection(
        'POST',
        `/inspections/${row.id}${run ? '/run' : ''}`,
        run ? {} : draft
      )
      setMessage(t(run ? 'Inspection started' : 'Saved'))
      refresh()
    } catch (e) {
      setMessage(t(e instanceof Error ? e.message : 'Request failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <details className='mt-4 border-t pt-3'>
      <summary className='cursor-pointer font-medium'>
        {t('Inspection settings')}
      </summary>
      <form
        className='mt-3 space-y-3'
        onSubmit={(e) => {
          e.preventDefault()
          void action(false)
        }}
      >
        <div className='grid gap-3 sm:grid-cols-2'>
          <label>
            {t('Customer display name')}
            <Input
              value={draft.title}
              required={draft.published}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </label>
          <label>
            {t('Test model')}
            <Input
              value={draft.model}
              required={draft.scheduled}
              onChange={(e) => setDraft({ ...draft, model: e.target.value })}
            />
          </label>
          <label>
            {t('Interval (minutes)')}
            <Input
              type='number'
              required
              min={1}
              max={10080}
              value={draft.minutes || ''}
              onChange={(e) =>
                setDraft({ ...draft, minutes: e.target.valueAsNumber })
              }
            />
          </label>
          <label>
            {t('Reasoning effort')}
            <Select value={draft.reasoning_effort || ''} onValueChange={(value) => setDraft({ ...draft, reasoning_effort: value || '' })} items={['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh'].map((value) => ({ value, label: value || t('Model default') }))}>
              <SelectTrigger className='w-full' aria-label={t('Reasoning effort')}><SelectValue /></SelectTrigger>
              <SelectContent>{['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh'].map((value) => <SelectItem key={value} value={value}>{value || t('Model default')}</SelectItem>)}</SelectContent>
            </Select>
          </label>
          <label>
            {t('Consecutive answer failures before isolation')}
            <Input type='number' min={1} max={20} value={draft.degrade_after || 3} onChange={(event) => setDraft({ ...draft, degrade_after: event.target.valueAsNumber })} />
          </label>
          <label>
            {t('Channel concurrency (0 = unlimited)')}
            <Input
              type='number'
              required
              min={0}
              max={1000000}
              value={Number.isFinite(draft.limit) ? draft.limit : ''}
              onChange={(e) =>
                setDraft({ ...draft, limit: e.target.valueAsNumber })
              }
            />
          </label>
        </div>
        <div className='flex flex-wrap gap-4'>
          <label>
            <input
              type='checkbox'
              checked={draft.published}
              onChange={(e) =>
                setDraft({ ...draft, published: e.target.checked })
              }
            />{' '}
            {t('Show to customers')}
          </label>
          <label>
            <input
              type='checkbox'
              checked={draft.scheduled}
              onChange={(e) =>
                setDraft({ ...draft, scheduled: e.target.checked })
              }
            />{' '}
            {t('Scheduled checks')}
          </label>
        </div>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Each round sends one question to each enabled account. Questions rotate in order. Answers must match after trimming spaces.'
          )}
        </p>
        {draft.questions.map((q, i) => (
          // These controlled inputs have no row state to retain when the ordered question list changes.
          // eslint-disable-next-line react/no-array-index-key
          <fieldset key={i} className='grid gap-2 rounded-md border p-3'>
            <legend className='px-1 text-sm'>
              {t('Question')} {i + 1}
            </legend>
            <label>
              {t('Prompt')}
              <Textarea
                required
                className='w-full rounded-md border p-2'
                value={q.prompt}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    questions: draft.questions.map((item, n) =>
                      n === i ? { ...item, prompt: e.target.value } : item
                    ),
                  })
                }
              />
            </label>
            <label>
              {t('Expected answer')}
              <Input
                required
                value={q.answer}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    questions: draft.questions.map((item, n) =>
                      n === i ? { ...item, answer: e.target.value } : item
                    ),
                  })
                }
              />
            </label>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={draft.questions.length === 1}
              onClick={() =>
                setDraft({
                  ...draft,
                  questions: draft.questions.filter((_, n) => i !== n),
                })
              }
            >
              {t('Remove')}
            </Button>
          </fieldset>
        ))}
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={draft.questions.length >= 32}
            onClick={() =>
              setDraft({
                ...draft,
                questions: [...draft.questions, { prompt: '', answer: '' }],
              })
            }
          >
            {t('Add question')}
          </Button>
          <Button type='submit' disabled={busy}>
            {t('Save')}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={busy || dirty || row.running || !draft.model.trim()}
            onClick={() => void action(true)}
          >
            {t('Run inspection now')}
          </Button>
        </div>
        {message && <p role='status'>{message}</p>}
      </form>
    </details>
  )
}
export function ChannelInspections() {
  const { t } = useTranslation()
  const admin = useAuthStore((s) => (s.auth.user?.role ?? 0) >= ROLE.ADMIN)
  const [preview, setPreview] = useState(false)
  const manage = admin && !preview
  const query = useQuery({
    queryKey: ['channel-inspections', manage ? 'manage' : 'customer'],
    queryFn: async () => {
      if (manage) return requestInspection<Card[]>('GET')
      const { data } = await api.get('/api/channel-inspections')
      if (!data?.success) throw new Error('Request failed')
      return data.data as Card[]
    },
    refetchInterval: 10000,
    retry: false,
  })
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Channel inspections')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {admin && (
          <>
            <Button variant='outline' onClick={() => setPreview(!preview)}>
              {t(preview ? 'Inspection settings' : 'Customer view')}
            </Button>
            <Link
              className={buttonVariants({ variant: 'outline' })}
              to='/channels'
            >
              {t('Channels')}
            </Link>
          </>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {query.isPending && <p>{t('Loading...')}</p>}
        {query.isError && <p role='alert'>{t('Request failed')}</p>}
        {query.data?.length === 0 && (
          <p>
            {t(
              manage
                ? 'No account pool channels'
                : 'No channel inspections published'
            )}
          </p>
        )}
        <div className='grid items-start gap-4 lg:grid-cols-2'>
          {query.data?.map((r, i) => (
            <article key={r.id ?? i} className='bg-card rounded-xl border p-5'>
              <h2 className='text-lg font-semibold'>{r.title || r.name}</h2>
              {(manage || (r.name && r.name !== r.title)) && (
                <p className='text-muted-foreground text-sm'>
                  {t('Channel')} {manage && `#${r.id} `}
                  {r.name}
                </p>
              )}
              <p className='mb-2 text-sm'>
                {t('Bound groups')}{' '}
                {r.groups?.filter(Boolean).join(' · ') || '—'}
              </p>
              <p className='text-muted-foreground mb-3 text-sm'>
                {r.running
                  ? t('Inspection in progress')
                  : t(r.scheduled ? 'Scheduled checks' : 'Manual checks')}
                {r.scheduled && ` · ${r.minutes} ${t('minutes')}`}
              </p>
              {r.failed && (
                <p role='status'>
                  {t('Account list unavailable; showing previous results')}
                </p>
              )}
              {r.time > 0 ? (
                <>
                  <p className='text-sm'>
                    {t('Last check')} · {formatTimestampToDate(r.time)}
                  </p>
                  {r.model && (
                    <p className='text-sm'>
                      {r.model} · {r.question}
                    </p>
                  )}
                  <p className='my-2 font-medium'>
                    {t('Passed')}{' '}
                    {r.accounts.filter((a) => a.result === 'passed').length} ·{' '}
                    {t('Answer mismatch')}{' '}
                    {
                      r.accounts.filter((a) => a.result === 'wrong_answer')
                        .length
                    }{' '}
                    · {t('Accounts')} {r.accounts.length}
                  </p>
                  <details>
                    <summary className='cursor-pointer'>
                      {t('Account results')}
                    </summary>
                    <ul className='divide-y'>
                      {r.accounts.map((a) => (
                        <li
                          key={a.code}
                          className='flex flex-wrap justify-between gap-2 py-2 text-sm'
                        >
                          <span>{a.label || `#${a.code}`}</span>
                          <span>
                            {t(outcomes[a.result] || 'Inconclusive')}
                            {a.time > 0 &&
                              ` · ${formatTimestampToDate(a.time)}`}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </details>
                </>
              ) : (
                <p className='text-muted-foreground'>{t('Not checked')}</p>
              )}
              {manage && r.settings && (
                <Editor
                  key={r.id}
                  row={{ ...r, settings: r.settings }}
                  refresh={() => void query.refetch()}
                />
              )}
            </article>
          ))}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
