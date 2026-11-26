import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout'
import { Button, buttonVariants } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'
import {
  resolveLocalizedText,
  type LocalizedTextValue,
} from '@/lib/localized-text'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { inspectPoolAccount } from './lib/provider-pool-api'

async function requestInspection<T>(
  method: 'GET' | 'POST' | 'DELETE',
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

type CandidateChannel = {
  id: number
  name: string
  type: number
  type_name: string
  groups: string[]
  models: string[]
  status: number
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
  is_pool?: boolean
  channel_type?: number
  channel_type_name?: string
  plugin_key?: string
  plugin_name?: LocalizedTextValue
  settings?: Settings
  question?: string
  model?: string
  accounts: {
    code: string
    account_id?: string
    label?: string
    reason?: string
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
        `/${row.id}${run ? '/run' : ''}`,
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
  const { t, i18n } = useTranslation()
  const admin = useAuthStore((s) => (s.auth.user?.role ?? 0) >= ROLE.ADMIN)
  const [preview, setPreview] = useState(false)
  const [inspectingKey, setInspectingKey] = useState<string | null>(null)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [selectedCandidateId, setSelectedCandidateId] = useState<string>('')
  const [unenrollTarget, setUnenrollTarget] = useState<Card | null>(null)
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
  const candidatesQuery = useQuery({
    queryKey: ['channel-inspections', 'candidates'],
    queryFn: () => requestInspection<CandidateChannel[]>('GET', '/candidates'),
    enabled: manage && addDialogOpen,
    retry: false,
  })
  const enrollMutation = useMutation({
    mutationFn: (channelId: number) =>
      requestInspection('POST', `/${channelId}/enroll`, {}),
    onSuccess: async () => {
      toast.success(t('Channel added to inspections'))
      setAddDialogOpen(false)
      setSelectedCandidateId('')
      await Promise.all([query.refetch(), candidatesQuery.refetch()])
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
  })
  const unenrollMutation = useMutation({
    mutationFn: (channelId: number) =>
      requestInspection('DELETE', `/${channelId}/enroll`),
    onSuccess: async () => {
      toast.success(t('Channel removed from inspections'))
      setUnenrollTarget(null)
      await Promise.all([query.refetch(), candidatesQuery.refetch()])
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
  })
  const inspectOne = useMutation({
    mutationFn: async (input: {
      channelId: number
      accountId: string
      label: string
    }) => {
      setInspectingKey(`${input.channelId}:${input.accountId}`)
      return inspectPoolAccount(input.channelId, input.accountId)
    },
    onSuccess: (res, input) => {
      const outcome = t(outcomes[res.result] || 'Inconclusive')
      if (res.result === 'passed') {
        toast.success(`${input.label}: ${outcome}`)
      } else {
        toast.error(
          res.reason
            ? `${input.label}: ${outcome} (${res.reason})`
            : `${input.label}: ${outcome}`
        )
      }
    },
    onError: (err) => {
      toast.error(getServerErrorMessage(err))
    },
    onSettled: async () => {
      setInspectingKey(null)
      await query.refetch()
    },
  })
  const candidates = candidatesQuery.data || []
  const effectiveCandidateId =
    selectedCandidateId || (candidates[0] ? String(candidates[0].id) : '')
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Channel inspections')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {admin && (
          <>
            {manage && (
              <Button
                type='button'
                onClick={() => {
                  setSelectedCandidateId('')
                  setAddDialogOpen(true)
                }}
              >
                <Plus className='size-4' />
                {t('Add channel to inspect')}
              </Button>
            )}
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
                ? 'No channels configured for inspection'
                : 'No channel inspections published'
            )}
          </p>
        )}
        <div className='grid items-start gap-4 lg:grid-cols-2'>
          {query.data?.map((r, i) => {
            const typeBadge = r.is_pool
              ? resolveLocalizedText(r.plugin_name, i18n.language) ||
                t('Account pool')
              : r.channel_type_name || t('Standard channel')
            return (
              <article key={r.id ?? i} className='bg-card rounded-xl border p-5'>
                <div className='flex items-start justify-between gap-2'>
                  <div className='min-w-0'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <h2 className='text-lg font-semibold'>
                        {r.title || r.name}
                      </h2>
                      {manage && (
                        <span className='bg-secondary text-secondary-foreground rounded-md border px-2 py-0.5 text-xs font-medium'>
                          {typeBadge}
                        </span>
                      )}
                    </div>
                    {(manage || (r.name && r.name !== r.title)) && (
                      <p className='text-muted-foreground text-sm'>
                        {t('Channel')} {manage && `#${r.id} `}
                        {r.name}
                      </p>
                    )}
                  </div>
                  {manage && r.is_pool === false && r.id && (
                    <Button
                      type='button'
                      variant='ghost'
                      size='xs'
                      onClick={() => setUnenrollTarget(r)}
                    >
                      <Trash2 className='size-3.5' />
                      {t('Remove')}
                    </Button>
                  )}
                </div>
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
                      · {t(r.is_pool === false ? 'Targets' : 'Accounts')}{' '}
                      {r.accounts.length}
                    </p>
                    <details>
                      <summary className='cursor-pointer'>
                        {t(
                          r.is_pool === false
                            ? 'Inspection results'
                            : 'Account results'
                        )}
                      </summary>
                      <ul className='divide-y'>
                        {r.accounts.map((a) => (
                          <li
                            key={a.code}
                            className='flex flex-wrap justify-between gap-2 py-2 text-sm'
                          >
                            <div className='min-w-0'>
                              <p>{a.label || `#${a.code}`}</p>
                              {manage && a.reason && (
                                <p className='text-muted-foreground mt-1 break-words'>
                                  {t('Failure reason')}: {a.reason}
                                </p>
                              )}
                            </div>
                            <div className='flex shrink-0 items-center gap-2'>
                              <span>
                                {t(outcomes[a.result] || 'Inconclusive')}
                                {a.time > 0 &&
                                  ` · ${formatTimestampToDate(a.time)}`}
                              </span>
                              {manage &&
                                r.is_pool !== false &&
                                r.id &&
                                a.account_id && (
                                  <Button
                                    type='button'
                                    variant='outline'
                                    size='sm'
                                    disabled={
                                      inspectOne.isPending &&
                                      inspectingKey ===
                                        `${r.id}:${a.account_id}`
                                    }
                                    isLoading={
                                      inspectOne.isPending &&
                                      inspectingKey ===
                                        `${r.id}:${a.account_id}`
                                    }
                                    onClick={() =>
                                      inspectOne.mutate({
                                        channelId: r.id!,
                                        accountId: a.account_id!,
                                        label: a.label || `#${a.code}`,
                                      })
                                    }
                                  >
                                    {t('Inspect account')}
                                  </Button>
                                )}
                            </div>
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
            )
          })}
        </div>

        <Dialog
          open={addDialogOpen}
          onOpenChange={setAddDialogOpen}
          title={t('Add channel to inspect')}
          description={t(
            'Enroll any standard channel (OpenAI, Claude, Gemini, New API, custom, etc.) for scheduled or manual quality inspections.'
          )}
          footer={
            <Button
              type='button'
              disabled={
                enrollMutation.isPending ||
                !effectiveCandidateId ||
                candidates.length === 0
              }
              isLoading={enrollMutation.isPending}
              onClick={() => {
                const cid = Number(effectiveCandidateId)
                if (cid > 0) enrollMutation.mutate(cid)
              }}
            >
              {t('Add')}
            </Button>
          }
        >
          {candidatesQuery.isPending ? (
            <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
          ) : candidates.length === 0 ? (
            <p className='text-muted-foreground text-sm'>
              {t('All testable channels are already enrolled in inspections.')}
            </p>
          ) : (
            <Field>
              <FieldLabel htmlFor='inspect-candidate-select'>
                {t('Channel')}
              </FieldLabel>
              <Select
                value={effectiveCandidateId}
                items={candidates.map((c) => ({
                  value: String(c.id),
                  label: `#${c.id} ${c.name} [${c.type_name}] (${c.groups.join(', ')})`,
                }))}
                onValueChange={(value) => setSelectedCandidateId(value || '')}
              >
                <SelectTrigger id='inspect-candidate-select' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {candidates.map((c) => (
                    <SelectItem key={c.id} value={String(c.id)}>
                      #{c.id} {c.name} [{c.type_name}] ({c.groups.join(', ')})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
        </Dialog>

        <ConfirmDialog
          open={!!unenrollTarget}
          onOpenChange={(open) => {
            if (!open) setUnenrollTarget(null)
          }}
          title={t('Remove channel from inspections')}
          desc={t(
            'This removes the channel from the inspection list and stops its scheduled checks without deleting the channel itself.'
          )}
          destructive
          isLoading={unenrollMutation.isPending}
          handleConfirm={() => {
            if (unenrollTarget?.id) {
              unenrollMutation.mutate(unenrollTarget.id)
            }
          }}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
