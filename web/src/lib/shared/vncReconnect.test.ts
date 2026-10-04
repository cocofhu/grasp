import { describe, expect, it } from 'vitest'
import { closeReasonKey, reconnectDelayMs, shouldAutoReconnect } from './vncReconnect'

describe('vncReconnect', () => {
  it('auto-reconnects idle, desktop-closed and drops, not superseded or evicted', () => {
    expect(shouldAutoReconnect('idle')).toBe(true)
    expect(shouldAutoReconnect('desktop-closed')).toBe(true)
    expect(shouldAutoReconnect('disconnect')).toBe(true)
    expect(shouldAutoReconnect('')).toBe(true)
    expect(shouldAutoReconnect('superseded')).toBe(false)
    expect(shouldAutoReconnect('evicted')).toBe(false)
  })

  it('backs off exponentially and caps at 30s', () => {
    expect([0, 1, 2, 3, 4, 5, 6].map(reconnectDelayMs)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000])
    expect(reconnectDelayMs(-1)).toBe(1000)
  })

  it('maps unknown reasons to the generic disconnect copy', () => {
    expect(closeReasonKey('idle')).toBe('idle')
    expect(closeReasonKey('superseded')).toBe('superseded')
    expect(closeReasonKey('weird')).toBe('disconnect')
    expect(closeReasonKey('')).toBe('disconnect')
  })
})
