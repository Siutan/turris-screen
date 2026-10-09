import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveText } from './template.js'

test('known fields replace and unknown tokens remain', () => {
  const sources = [{ id: 'sys', name: 'System', kind: 'system', config: {} }]
  const values = { sys: { fields: { cpu: '42' }, error: '' } }
  const now = new Date(2026, 9, 9, 21, 41)
  const text = resolveText('{{sys.cpu}}% {{System.cpu}} {{missing.cpu}} {{clock.time24}}', sources, values, now)
  assert.equal(text, '42% 42 {{missing.cpu}} 21:41')
})

test('last good values survive an error flag', () => {
  const sources = [{ id: 'sys', name: 'System', kind: 'system', config: {} }]
  const values = { sys: { fields: { cpu: '42' }, error: 'offline' } }
  assert.equal(resolveText('CPU {{sys.cpu}}', sources, values, new Date()), 'CPU 42')
})

test('countdown remaining is computed from the stored target', () => {
  const sources = [{ id: 'c', name: 'Countdown', kind: 'countdown', config: { target: '2026-10-09T22:00', label: 'Show' } }]
  const now = new Date(2026, 9, 9, 21, 46)
  assert.equal(resolveText('{{c.remaining}} {{c.label}}', sources, {}, now), '14 min Show')
})
