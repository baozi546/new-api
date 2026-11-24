import { describe, expect, test } from 'vitest'

import { parseAccountImport } from '../provider-account-import'

describe('pasted provider account import', () => {
  test('parses a credential object, an accounts export, and JSONL', () => {
    expect(parseAccountImport('{"access_token":"token-a"}')[0].account).toEqual(
      { access_token: 'token-a' }
    )
    expect(
      parseAccountImport(
        '{"accounts":[{"access_token":"a"},{"access_token":"b"}]}'
      )
    ).toHaveLength(2)
    const jsonl = `{"access_token":"a"}${String.fromCharCode(10)}{"access_token":"b"}`
    expect(parseAccountImport(jsonl)).toHaveLength(2)
  })

  test('parses a bare JWT and reports malformed lines without losing valid rows', () => {
    const jwt = 'eyJhbGciOiJub25l.eyJlbWFpbCI6ImFAZXhhbXBsZS50ZXN0In0.signature'
    const result = parseAccountImport(
      `${jwt}${String.fromCharCode(10)}not-json`
    )
    expect(result[0].account?.access_token).toBe(jwt)
    expect(result[1].error).toBe('Invalid account JSON')
  })

  test('rejects oversized pasted account data', () => {
    expect(parseAccountImport('x'.repeat(8 * 1024 * 1024 + 1))[0].error).toBe(
      'Account import exceeds 8 MiB'
    )
  })
})
