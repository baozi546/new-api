type AccountImportCandidate = {
  account?: Record<string, unknown>
  error?: string
}

export type AccountImportEntry = AccountImportCandidate & { key: string }

const MAX_IMPORT_BYTES = 8 * 1024 * 1024
const MAX_ACCOUNT_BYTES = 64 * 1024
const MAX_ACCOUNTS = 1000

function importEntry(value: unknown): AccountImportCandidate {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return { error: 'Expected an account JSON object' }
  }
  if (
    new TextEncoder().encode(JSON.stringify(value)).length > MAX_ACCOUNT_BYTES
  ) {
    return { error: 'Account credential is too large' }
  }
  return { account: value as Record<string, unknown> }
}

function accountImportKeys(
  entries: AccountImportCandidate[]
): AccountImportEntry[] {
  const occurrences = new Map<string, number>()
  return entries.map((entry) => {
    const identity = entry.account
      ? JSON.stringify(entry.account)
      : entry.error || 'invalid'
    const occurrence = occurrences.get(identity) || 0
    occurrences.set(identity, occurrence + 1)
    return { ...entry, key: `${identity}:${occurrence}` }
  })
}

/** Parse without executing pasted text or retaining secrets in browser storage. */
export function parseAccountImport(text: string): AccountImportEntry[] {
  const input = text.replace(/^\uFEFF/, '').trim()
  if (!input) return []
  if (new TextEncoder().encode(input).length > MAX_IMPORT_BYTES) {
    return accountImportKeys([{ error: 'Account import exceeds 8 MiB' }])
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(input)
    if (typeof parsed === 'string') parsed = JSON.parse(parsed)
  } catch {
    const lines = input.split(/\r?\n/).filter((line) => line.trim())
    if (lines.length > MAX_ACCOUNTS)
      {return accountImportKeys([
        { error: 'Import up to 1000 accounts at a time' },
      ])}
    return accountImportKeys(
      lines.map((line) => {
        try {
          return importEntry(JSON.parse(line))
        } catch {
          // A bare access JWT can identify its account through provider claims.
          if (
            /^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(
              line.trim()
            )
          ) {
            return importEntry({ type: 'codex', access_token: line.trim() })
          }
          return { error: 'Invalid account JSON' }
        }
      })
    )
  }
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    const container = parsed as { accounts?: unknown; credentials?: unknown }
    if (Array.isArray(container.accounts)) parsed = container.accounts
    else if (Array.isArray(container.credentials))
      {parsed = container.credentials}
  }
  const items = Array.isArray(parsed) ? parsed : [parsed]
  if (items.length > MAX_ACCOUNTS)
    {return accountImportKeys([
      { error: 'Import up to 1000 accounts at a time' },
    ])}
  return accountImportKeys(items.map(importEntry))
}
