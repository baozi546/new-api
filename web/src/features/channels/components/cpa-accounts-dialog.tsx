import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RotateCw, Save, UserPlus } from 'lucide-react'
import {
  createContext,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { formatTimestampToDate } from '@/lib/format'

import { request, useCPABindings, type Binding } from '../lib/cpa-channel-pools'

type Account = {
  channel_pool_id?: string
  id: string
  label: string
  disabled: boolean
  requires_sign_in: boolean
  unavailable: boolean
  quota_blocked: boolean
  status: string
  account_concurrency: number | null
  account_pool: string
  account_check: {
    time: number
    result: string
    consecutive: number
    question: string
    model: string
  }
}
type Login = { flow: string; url: string }
const pools: Record<string, string> = {
  unassigned: 'Unassigned',
  plus: 'Plus',
  pro: 'Pro',
  degraded: 'Reduced quality',
}
const results: Record<string, string> = {
  passed: 'Passed',
  wrong_answer: 'Answer mismatch',
  inconclusive: 'Inconclusive',
  rate_limited: 'Rate limited',
  login_required: 'Login required',
}
function issuesOf(a: Account): string[] {
  const issues: string[] = []
  if (a.disabled) issues.push('Disabled')
  if (a.requires_sign_in) issues.push('Login required')
  if (a.quota_blocked) issues.push('Quota blocked')
  if (!issues.length && (a.unavailable || a.status === 'error')) {
    issues.push('Temporarily unavailable')
  }
  const last = a.account_check.result
  if (
    last &&
    last !== 'passed' &&
    results[last] &&
    !issues.includes(results[last])
  ) {
    issues.push(results[last])
  }
  return issues
}
const problem = (a: Account) => issuesOf(a).length > 0

const PoolContext = createContext<{
  channels: Binding[]
  accounts?: Account[]
  failed: boolean
  show: (binding: Binding) => void
} | null>(null)
export function CPAChannelPoolProvider({
  enabled,
  children,
}: {
  enabled: boolean
  children: ReactNode
}) {
  const [selected, setSelected] = useState<Binding | null>(null)
  const channels = useCPABindings(enabled)
  const accounts = useQuery({
    queryKey: ['cpa-accounts'],
    queryFn: () => request<Account[]>('GET'),
    enabled: enabled && !!channels.data?.channels.length,
    staleTime: 30000,
    refetchInterval: 30000,
    retry: false,
  })
  const value = useMemo(
    () =>
      enabled && channels.data
        ? {
            channels: channels.data.channels,
            accounts: accounts.data,
            failed: accounts.isError || channels.isError,
            show: setSelected,
          }
        : null,
    [enabled, channels.data, channels.isError, accounts.data, accounts.isError]
  )
  return (
    <PoolContext.Provider value={value}>
      {children}
      {enabled && selected && (
        <CPAAccountsDialog
          binding={selected}
          problemsOnly={!selected.pool}
          onClose={() => setSelected(null)}
        />
      )}
    </PoolContext.Provider>
  )
}
export function CPAChannelPoolStatus({ id }: { id: number }) {
  const pool = useContext(PoolContext)
  const { t } = useTranslation()
  const binding = pool?.channels.find((ch) => ch.id === id)
  if (!pool || !binding) return null
  const members = pool.accounts?.filter(
    (a) => (a.channel_pool_id || '') === binding.pool
  )
  const shared = pool.channels.some(
    (ch) => ch.id !== id && ch.pool === binding.pool
  )
  let counts = t('Loading...')
  if (pool.failed) {
    counts = t('Account status unavailable')
  } else if (members && binding.pool && members.length === 0) {
    counts = t('No accounts · Add account')
  } else if (members) {
    counts = t('{{issues}} of {{total}} accounts need attention', {
      issues: members.filter(problem).length,
      total: members.length,
    })
  }
  return (
    <Button
      size='sm'
      variant='link'
      className='h-auto self-start px-0 text-xs whitespace-normal'
      onClick={() => pool.show(binding)}
    >
      {shared && `${t('Shared CPA pool')} · `}
      {counts}
    </Button>
  )
}
function AccountRow({
  account,
  onChange,
}: {
  account: Account
  onChange: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [limit, setLimit] = useState(String(account.account_concurrency ?? 0))
  const [pool, setPool] = useState(account.account_pool)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function change(data: object) {
    setBusy(true)
    setError('')
    try {
      await request('PATCH', '', { id: account.id, ...data })
      await onChange()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const n = Number(limit)
  const check = account.account_check
  return (
    <tr className='border-b align-top'>
      <td className='max-w-64 p-2 break-all'>
        {account.label || account.id}
        {account.label && (
          <small className='text-muted-foreground block'>{account.id}</small>
        )}
      </td>
      <td className='p-2'>
        <select
          aria-label={t('Account pool')}
          value={pool}
          onChange={(e) => setPool(e.target.value)}
          className='bg-background rounded-md border p-1'
        >
          {Object.entries(pools).map(([key, label]) => (
            <option key={key} value={key}>
              {t(label)}
            </option>
          ))}
        </select>
      </td>
      <td className='p-2'>
        <div className='flex items-center gap-1'>
          <Input
            className='w-20'
            aria-label={t('Concurrent request limit')}
            type='number'
            min={0}
            max={1000000}
            value={limit}
            onChange={(e) => setLimit(e.target.value)}
          />
          <Button
            size='sm'
            variant='outline'
            aria-label={t('Save')}
            disabled={
              busy ||
              account.account_concurrency === null ||
              !limit ||
              !Number.isInteger(n) ||
              n < 0 ||
              n > 1000000
            }
            onClick={() => change({ limit: n, pool })}
          >
            <Save />
          </Button>
        </div>
      </td>
      <td className='p-2'>
        {(issuesOf(account).length
          ? issuesOf(account)
          : [results[check.result] || 'Not checked']
        )
          .map((s) => t(s))
          .join(' · ')}
        {check.time > 0 && (
          <small className='text-muted-foreground block'>
            {t('Last check')} · {formatTimestampToDate(check.time)} ·{' '}
            {check.model} · {t(results[check.result] || 'Not checked')}
          </small>
        )}
        {check.question && <small className='block'>{check.question}</small>}
      </td>
      <td className='p-2'>
        <Button
          size='sm'
          variant='outline'
          disabled={busy}
          onClick={() => change({ disabled: !account.disabled })}
        >
          {t(account.disabled ? 'Enable' : 'Disable')}
        </Button>
        {error && <p role='alert'>{error}</p>}
      </td>
    </tr>
  )
}

export function CPAAccountsDialog({
  binding,
  problemsOnly = false,
  onClose,
}: { binding?: Binding; problemsOnly?: boolean; onClose?: () => void } = {}) {
  const { t } = useTranslation()
  const cache = useQueryClient()
  const [open, setOpen] = useState(!!binding || problemsOnly)
  const [issues, setIssues] = useState(problemsOnly)
  const [pool, setPool] = useState('')
  const [login, setLogin] = useState<Login | null>(null)
  const [targetPool, setTargetPool] = useState('')
  const [callback, setCallback] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const query = useQuery({
    queryKey: ['cpa-accounts'],
    queryFn: () => request<Account[]>('GET'),
    enabled: open,
    retry: false,
    refetchInterval: open ? 30000 : false,
  })
  const reload = async () => {
    await cache.invalidateQueries({ queryKey: ['cpa-accounts'] })
  }
  async function authorize() {
    setBusy(true)
    setMessage('')
    try {
      if (!login) {
        setLogin(
          await request<Login>('POST', '/login', {
            ...(targetPool ? { pool: targetPool } : {}),
            ...(binding?.pool ? { channel_id: binding.id } : {}),
          })
        )
        return
      }
      const status = await request<{ status: string }>('POST', '/login', {
        flow: login.flow,
        callback,
      })
      setCallback('')
      if (status.status === 'wait') {
        setMessage(t('Authorization pending'))
        return
      }
      setLogin(null)
      if (status.status === 'pool_conflict') {
        setMessage(t('Account already belongs to another channel pool'))
        return
      }
      setMessage(
        t(
          status.status === 'ok' ? 'Account authorized' : 'Authorization failed'
        )
      )
      await reload()
    } catch (e) {
      setMessage((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const rows = (query.data ?? []).filter(
    (a) =>
      (!binding || (a.channel_pool_id || '') === binding.pool) &&
      (!issues || problem(a)) &&
      (!pool || a.account_pool === pool)
  )
  return (
    <>
      {!binding && !problemsOnly && (
        <Button variant='outline' onClick={() => setOpen(true)}>
          <KeyRound />
          {t('CPA accounts')}
        </Button>
      )}
      <Dialog
        open={open}
        onOpenChange={(v) => {
          setOpen(v)
          if (!v) {
            setCallback('')
            setLogin(null)
            setMessage('')
            onClose?.()
          }
        }}
        title={
          binding
            ? `${binding.name} · ${t('CPA accounts')}`
            : t(problemsOnly ? 'Problem accounts' : 'CPA accounts')
        }
        contentClassName='sm:max-w-5xl'
        bodyClassName='space-y-4'
      >
        <div className='flex flex-wrap items-center gap-3'>
          <select
            aria-label={t('Account pool')}
            className='bg-background rounded-md border p-1'
            value={pool}
            onChange={(e) => setPool(e.target.value)}
          >
            <option value=''>{t('All pools')}</option>
            {Object.entries(pools).map(([k, v]) => (
              <option key={k} value={k}>
                {t(v)}
              </option>
            ))}
          </select>
          <label className='flex items-center gap-2'>
            <input
              type='checkbox'
              checked={issues}
              onChange={(e) => setIssues(e.target.checked)}
            />
            {t('Problem accounts')}
          </label>
          <Button
            variant='outline'
            onClick={() => void reload()}
            aria-label={t('Refresh')}
          >
            <RotateCw />
          </Button>
        </div>
        {!problemsOnly && (
          <div className='flex flex-wrap items-center gap-2'>
            <label className='flex items-center gap-2'>
              {t(binding ? 'Subscription label' : 'Add account to pool')}
              <select
                className='bg-background rounded-md border p-1'
                value={targetPool}
                disabled={busy || !!login}
                onChange={(e) => setTargetPool(e.target.value)}
              >
                <option value=''>{t('Detect from subscription')}</option>
                {Object.entries(pools).map(([k, v]) => (
                  <option key={k} value={k}>
                    {t(v)}
                  </option>
                ))}
              </select>
            </label>
            <Button disabled={busy || !!login} onClick={authorize}>
              <UserPlus />
              {t('Authorize account')}
            </Button>
          </div>
        )}
        {login && (
          <div className='flex flex-wrap items-center gap-2'>
            <a
              className={buttonVariants()}
              href={login.url}
              target='_blank'
              rel='noopener noreferrer'
            >
              {t('Open sign-in page')}
            </a>
            <Input
              className='min-w-48 flex-1'
              aria-label={t('Callback URL')}
              placeholder={t('Callback URL')}
              value={callback}
              autoComplete='off'
              onChange={(e) => setCallback(e.target.value)}
            />
            <Button variant='outline' disabled={busy} onClick={authorize}>
              {t('Continue')}
            </Button>
          </div>
        )}
        {message && <p role='status'>{message}</p>}
        {query.isError && <p role='alert'>{query.error.message}</p>}
        {query.isLoading && <p>{t('Loading...')}</p>}
        {query.isSuccess && (
          <div className='overflow-x-auto'>
            <table className='w-full text-left text-sm'>
              <thead>
                <tr>
                  {[
                    'Account',
                    'Account pool',
                    'Concurrent request limit',
                    'Status',
                    'Actions',
                  ].map((v) => (
                    <th key={v} className='p-2'>
                      {t(v)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((a) => (
                  <AccountRow
                    key={[a.id, a.account_pool, a.account_concurrency].join(
                      ':'
                    )}
                    account={a}
                    onChange={reload}
                  />
                ))}
              </tbody>
            </table>
            {rows.length === 0 && (
              <p className='p-3'>{t('No matching accounts')}</p>
            )}
          </div>
        )}
      </Dialog>
    </>
  )
}
