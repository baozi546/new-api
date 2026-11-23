import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RotateCw, Save, UserPlus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'

type Account = { id: string; label: string; disabled: boolean; requires_sign_in: boolean; account_concurrency: number | null; account_pool: string; account_check: { time: number; result: string; consecutive: number; question: string; model: string } }
type Login = { flow: string; url: string }
const route = '/api/cpa/accounts'
const pools: Record<string, string> = { unassigned: 'Unassigned', plus: 'Plus', pro: 'Pro', degraded: 'Reduced quality' }
const results: Record<string, string> = { passed: 'Passed', wrong_answer: 'Answer mismatch', inconclusive: 'Inconclusive', rate_limited: 'Rate limited', login_required: 'Login required' }
const problem = (a: Account) => a.disabled || a.requires_sign_in || Boolean(a.account_check.result && a.account_check.result !== 'passed')

async function request<T>(method: 'GET' | 'PATCH' | 'POST', path = '', data?: unknown): Promise<T> {
  const r = await api.request({ method, url: route + path, data })
  if (!r.data?.success) throw new Error(typeof r.data?.message === 'string' ? r.data.message : 'Request failed')
  return r.data.data as T
}

function AccountRow({ account, onChange }: { account: Account; onChange: () => Promise<void> }) {
  const { t } = useTranslation()
  const [limit, setLimit] = useState(String(account.account_concurrency ?? 0))
  const [pool, setPool] = useState(account.account_pool)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function change(data: object) {
    setBusy(true); setError('')
    try { await request('PATCH', '', { id: account.id, ...data }); await onChange() }
    catch (e) { setError((e as Error).message) } finally { setBusy(false) }
  }
  const n = Number(limit)
  const check = account.account_check
  return <tr className='border-b align-top'>
    <td className='max-w-64 break-all p-2'>{account.label || account.id}{account.label && <small className='block text-muted-foreground'>{account.id}</small>}</td>
    <td className='p-2'><select aria-label={t('Account pool')} value={pool} onChange={e => setPool(e.target.value)} className='rounded-md border bg-background p-1'>{Object.entries(pools).map(([key, label]) => <option key={key} value={key}>{t(label)}</option>)}</select></td>
    <td className='p-2'><div className='flex items-center gap-1'><Input className='w-20' aria-label={t('Concurrent request limit')} type='number' min={0} max={1000000} value={limit} onChange={e => setLimit(e.target.value)} /><Button size='sm' variant='outline' aria-label={t('Save')} disabled={busy || account.account_concurrency === null || !limit || !Number.isInteger(n) || n < 0 || n > 1000000} onClick={() => change({ limit: n, pool })}><Save /></Button></div></td>
    <td className='p-2'>{t(results[check.result] || (account.requires_sign_in ? 'Login required' : 'Not checked'))}{check.time > 0 && <small className='block text-muted-foreground'>{formatTimestampToDate(check.time)} · {check.model}</small>}{check.question && <small className='block'>{check.question}</small>}</td>
    <td className='p-2'><Button size='sm' variant='outline' disabled={busy} onClick={() => change({ disabled: !account.disabled })}>{t(account.disabled ? 'Enable' : 'Disable')}</Button>{error && <p role='alert'>{error}</p>}</td>
  </tr>
}

export function CPAAccountsDialog() {
  const { t } = useTranslation(); const cache = useQueryClient()
  const [open, setOpen] = useState(false); const [issues, setIssues] = useState(false)
  const [pool, setPool] = useState(''); const [login, setLogin] = useState<Login | null>(null)
  const [callback, setCallback] = useState(''); const [busy, setBusy] = useState(false); const [message, setMessage] = useState('')
  const query = useQuery({queryKey:['cpa-accounts'], queryFn:()=>request<Account[]>('GET'), enabled:open, retry:false, refetchInterval: open ? 30000 : false})
  const reload = async () => { await cache.invalidateQueries({queryKey:['cpa-accounts']}) }
  async function authorize() {
    setBusy(true); setMessage('')
    try {
      if (!login) {setLogin(await request<Login>('POST','/login',{})); return}
      const status = await request<{status:string}>('POST','/login',{flow:login.flow, callback})
      setCallback(''); if (status.status === 'wait') {setMessage(t('Authorization pending'));return}
      setLogin(null); setMessage(t(status.status === 'ok' ? 'Account authorized' : 'Authorization failed'));await reload()
    } catch(e) {setMessage((e as Error).message)} finally {setBusy(false)}
  }
  const rows = (query.data ?? []).filter(a => (!issues || problem(a)) && (!pool || a.account_pool === pool))
  return <><Button variant='outline' onClick={() => setOpen(true)}><KeyRound />{t('CPA accounts')}</Button>
    <Dialog open={open} onOpenChange={v=>{setOpen(v); if(!v){setCallback('');setLogin(null);setMessage('')}}} title={t('CPA accounts')} contentClassName='sm:max-w-5xl' bodyClassName='space-y-4'>
      <div className='flex flex-wrap items-center gap-3'><select aria-label={t('Account pool')} className='rounded-md border bg-background p-1' value={pool} onChange={e=>setPool(e.target.value)}><option value=''>{t('All pools')}</option>{Object.entries(pools).map(([k,v])=><option key={k} value={k}>{t(v)}</option>)}</select>
        <label className='flex items-center gap-2'><input type='checkbox' checked={issues} onChange={e=>setIssues(e.target.checked)} />{t('Problem accounts')}</label>
        <Button variant='outline' onClick={()=>void reload()} aria-label={t('Refresh')}><RotateCw /></Button><Button disabled={busy || !!login} onClick={authorize}><UserPlus />{t('Authorize account')}</Button></div>
      {login && <div className='flex flex-wrap items-center gap-2'><Button nativeButton={false} render={<a href={login.url} target='_blank' rel='noopener noreferrer' />}>{t('Open sign-in page')}</Button><Input className='min-w-48 flex-1' aria-label={t('Callback URL')} placeholder={t('Callback URL')} value={callback} autoComplete='off' onChange={e=>setCallback(e.target.value)} /><Button variant='outline' disabled={busy} onClick={authorize}>{t('Continue')}</Button></div>}
      {message && <p role='status'>{message}</p>}
      {query.isError ? <p role='alert'>{query.error.message}</p> : query.isLoading ? <p>{t('Loading...')}</p> : <div className='overflow-x-auto'><table className='w-full text-left text-sm'><thead><tr>{['Account','Account pool','Concurrent request limit','Last check','Actions'].map(v=><th key={v} className='p-2'>{t(v)}</th>)}</tr></thead><tbody>{rows.map(a=><AccountRow key={[a.id,a.account_pool,a.account_concurrency].join(':')} account={a} onChange={reload} />)}</tbody></table>{rows.length===0 && <p className='p-3'>{t('No matching accounts')}</p>}</div>}
    </Dialog></>
}
