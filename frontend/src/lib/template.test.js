import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveText, sourcePreview } from './template.js'

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

test('a world clock uses the zone on the source', () => {
  const sources = [{ id: 'lon', name: 'London', kind: 'zone', config: { zone: 'Europe/London' } }]
  const now = new Date(Date.UTC(2026, 9, 9, 10, 41))
  assert.equal(resolveText('{{lon.time24}}', sources, {}, now), '11:41')
})

test('calendar remaining ticks from the cached start', () => {
  const sources = [{ id: 'cal', name: 'Calendar', kind: 'calendar', config: { url: 'https://example.com/a.ics' } }]
  const values = { cal: { fields: { next: 'Design review', start: '2026-10-09T22:00:00' } } }
  const now = new Date(2026, 9, 9, 21, 46)
  assert.equal(resolveText('{{cal.remaining}} {{cal.next}}', sources, values, now), '14 min Design review')
})

test('a running focus timer counts down in seconds', () => {
  const end = new Date(2026, 9, 9, 21, 46, 30).toISOString()
  const sources = [{ id: 'f', name: 'Focus', kind: 'focus', config: { running: true, end } }]
  const now = new Date(2026, 9, 9, 21, 46, 0)
  assert.equal(resolveText('{{f.remaining}}', sources, {}, now), '0:30')
})

test('a live card shows the sampled value, with a unit for cpu', () => {
  const source = { id: 'sys', kind: 'system', config: {} }
  const now = new Date(2026, 9, 9, 21, 41)
  assert.equal(sourcePreview(source, { sys: { fields: { cpu: '42' } } }, now), '42%')
  assert.equal(sourcePreview({ id: 'lon', kind: 'zone', config: { zone: 'Europe/London' } }, {}, new Date(Date.UTC(2026, 9, 9, 10, 41))), '11:41')
  assert.equal(sourcePreview(source, {}, now), '')
})

test('countdown remaining is computed from the stored target', () => {
  const sources = [{ id: 'c', name: 'Countdown', kind: 'countdown', config: { target: '2026-10-09T22:00', label: 'Show' } }]
  const now = new Date(2026, 9, 9, 21, 46)
  assert.equal(resolveText('{{c.remaining}} {{c.label}}', sources, {}, now), '14 min Show')
})
