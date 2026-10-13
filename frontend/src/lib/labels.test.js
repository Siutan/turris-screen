import assert from 'node:assert/strict'
import test from 'node:test'
import { colorInt, isDeviceLabel, labelGroups, labelTexts } from './labels.js'
import { coolerFont } from './project.js'

test('a clock and a template are device labels, plain text is not', () => {
  assert.equal(isDeviceLabel({ type: 'time' }), true)
  assert.equal(isDeviceLabel({ type: 'date' }), true)
  assert.equal(isDeviceLabel({ type: 'text', text: 'CPU {{sys.cpu}}%' }), true)
  assert.equal(isDeviceLabel({ type: 'text', text: 'Heading' }), false)
})

test('label color matches the integer KANALI sends', () => {
  assert.equal(colorInt('#dcdcdc'), 14474460)
  assert.equal(colorInt('#ffffff'), 0xffffff)
})

test('a clock group keeps a stable id and the current time', () => {
  const project = {
    sources: [],
    tracks: [
      {
        clips: [
          {
            id: 'clock',
            type: 'time',
            start: 0,
            duration: 10,
            format: '24h',
            size: 120,
            color: '#dcdcdc',
            align: 'center',
            x: 640,
            y: 360,
          },
        ],
      },
    ],
  }
  const now = new Date(2026, 9, 9, 12, 31)
  const first = labelGroups(project, now)
  const again = labelGroups(project, now)
  assert.equal(first.length, 1)
  assert.equal(first[0].id, again[0].id)
  assert.equal(first[0].labels[0].text, '12:31')
  assert.equal(first[0].labels[0].font, 'roboto-regular')
  assert.equal(first[0].labels[0].color, 14474460)
  assert.equal(first[0].align, 2)
  const later = labelGroups(project, new Date(2026, 9, 9, 12, 32))
  assert.equal(later[0].id, first[0].id)
  assert.equal(later[0].labels[0].id, first[0].labels[0].id)
  assert.equal(labelTexts(later)[0].text, '12:32')
})

test('a template reads the sampled value', () => {
  const project = {
    sources: [{ id: 'sys', name: 'System', kind: 'system' }],
    tracks: [
      {
        clips: [
          {
            id: 'cpu',
            type: 'text',
            start: 0,
            duration: 10,
            text: '{{sys.cpu}}%',
            size: 48,
            color: '#ffffff',
            align: 'left',
            x: 80,
            y: 80,
          },
        ],
      },
    ],
  }
  const groups = labelGroups(project, new Date(), { sys: { fields: { cpu: '42' } } })
  assert.equal(groups[0].labels[0].text, '42%')
  assert.equal(groups[0].labels[0].font, 'roboto-regular')
})

test('only faces on the cooler are sent, and older names map across', () => {
  assert.equal(coolerFont({ font: 'BitcountSingle-Regular', weight: 400 }), 'BitcountSingle-Regular')
  assert.equal(coolerFont({ font: 'Orbitron-Regular', weight: 700 }), 'Orbitron-Regular')
  assert.equal(coolerFont({ font: 'Segoe UI', weight: 400 }), 'roboto-regular')
  assert.equal(coolerFont({ font: 'Impact', weight: 700 }), 'Orbitron-Regular')
  assert.equal(coolerFont({ font: 'roboto-regular', weight: 300 }), 'roboto-thin')
  assert.equal(coolerFont({ font: 'roboto-thin', weight: 400 }), 'roboto-thin')
})
