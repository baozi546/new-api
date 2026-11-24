import type { PoolAccount } from './provider-pool-api'

export function providerAccountHasIssue(account: PoolAccount): boolean {
  return (
    account.state.disabled ||
    account.needs_login ||
    !!account.state.degraded ||
    !!account.profile.quota_error ||
    (!!account.state.health && account.state.health !== 'passed')
  )
}
