import assert from 'node:assert/strict'
import test from 'node:test'
import { filmstripKey, filmstripSlots, filmstripTimes } from './filmstrip.js'

test('a later range samples that part of the video', () => {
  const early = filmstripTimes(0, 4, 4)
  const later = filmstripTimes(10, 14, 4)
  assert.ok(later.every((t) => t >= 10 && t <= 14))
  assert.notDeepEqual(early, later)
  assert.equal(filmstripKey('a', 0, 4, 4) === filmstripKey('a', 10, 14, 4), false)
})

test('a small range nudge keeps the same filmstrip', () => {
  assert.equal(filmstripKey('a', 10, 20, 6), filmstripKey('a', 10.05, 20.04, 6))
})

test('wider clips ask for more frames', () => {
  assert.equal(filmstripSlots(40), 1)
  assert.ok(filmstripSlots(800) > filmstripSlots(80))
  assert.equal(filmstripSlots(4000), 20)
})
