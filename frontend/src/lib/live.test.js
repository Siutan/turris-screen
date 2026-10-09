import assert from 'node:assert/strict'
import test from 'node:test'
import { MOTION_MIN_MS, createSendGate, sourcesDue } from './live.js'

test('a source is due only after its interval', () => {
  const source = { id: 's', intervalSec: 5 }
  assert.equal(sourcesDue([source], {}, 1000).length, 1)
  assert.equal(sourcesDue([source], { s: 1000 }, 5999).length, 0)
  assert.equal(sourcesDue([source], { s: 1000 }, 6000).length, 1)
})

test('a signature change is required before send', () => {
  const gate = createSendGate()
  assert.equal(gate.want('a'), true)
  gate.start('a')
  gate.done(true)
  assert.equal(gate.want('a'), false)
  assert.equal(gate.want('b'), true)
})

test('a moving clip waits out the minimum gap before it is sent again', () => {
  const gate = createSendGate()
  gate.arm(1000)
  assert.equal(gate.want('a', 1000, MOTION_MIN_MS), false)
  assert.equal(gate.want('a', 1000 + MOTION_MIN_MS - 1, MOTION_MIN_MS), false)
  assert.equal(gate.want('a', 1000 + MOTION_MIN_MS, MOTION_MIN_MS), true)
  gate.start('a', 1000 + MOTION_MIN_MS)
  gate.done(true)
  assert.equal(gate.want('b', 1000 + MOTION_MIN_MS + 1000, MOTION_MIN_MS), false)
  assert.equal(gate.want('b', 1000 + MOTION_MIN_MS * 2, MOTION_MIN_MS), true)
})

test('an in-flight send stores one pending send', () => {
  const gate = createSendGate()
  assert.equal(gate.want('a'), true)
  gate.start('a')
  assert.equal(gate.want('b'), false)
  assert.equal(gate.want('c'), false)
  assert.equal(gate.done(true), true)
  assert.equal(gate.want('c'), true)
})
