import { activeClips, coolerFont, widgetText } from './project.js'
import { resolveText } from './template.js'

// A clock, a date, or a text clip with {{fields}} is drawn by the cooler as a label.
// The picture underneath keeps looping. KANALI updates that layer with CMD_Update_UIItemText.
export function isDeviceLabel(clip) {
  if (!clip) return false
  if (clip.type === 'time' || clip.type === 'date') return true
  return clip.type === 'text' && /\{\{/.test(clip.text || '')
}

const ALIGN = { left: 1, center: 2, right: 3 }

function hashId(text) {
  let h = 2166136261
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 16777619)
  return ((h >>> 0) % 900000) + 1000
}

export function colorInt(hex) {
  const match = /^#?([0-9a-f]{6})$/i.exec(String(hex || '').trim())
  if (!match) return 0xffffff
  return parseInt(match[1], 16)
}

function deviceFont(clip) {
  return coolerFont(clip)
}

function clipLines(clip, now, sources, values) {
  const shape = (s) => (clip.upper ? String(s || '').toUpperCase() : String(s || ''))
  if (clip.type === 'text') {
    const text = resolveText(clip.text || '', sources, values, now)
    return shape(text).split('\n')
  }
  const widget = widgetText(clip, now)
  const main = shape(widget.main)
  const suffix = shape(widget.suffix)
  return [suffix ? `${main} ${suffix}` : main]
}

function fallbackBox(clip, lines) {
  const size = Math.max(8, Number(clip.size) || 40)
  const width = Math.max(1, ...lines.map((line) => line.length)) * size * 0.62
  const height = Math.max(1, lines.length) * size * 1.12
  let left = Number(clip.x) || 0
  if (clip.align === 'center') left -= width / 2
  if (clip.align === 'right') left -= width
  return { left, top: (Number(clip.y) || 0) - height / 2, blockW: width, blockH: height }
}

function clamp(n, lo, hi) {
  return Math.min(hi, Math.max(lo, Math.round(n)))
}

// labelGroups is the run-config payload for the live text on screen at t = 0.
// measure(clip, now, sources, values) may return { left, top, blockW, blockH }.
export function labelGroups(project, now = new Date(), values = {}, measure = null) {
  const sources = project?.sources || []
  const groups = []
  for (const { clip } of activeClips(project, 0)) {
    if (!isDeviceLabel(clip)) continue
    const lines = clipLines(clip, now, sources, values).map((line) => (line === '' ? ' ' : line))
    const box = measure?.(clip, now, sources, values) || fallbackBox(clip, lines)
    const pad = Math.round((Number(clip.size) || 40) * 0.35)
    const width = clamp((box.blockW || 1) + pad * 2, 1, 1280)
    const height = clamp((box.blockH || 1) + pad, 1, 720)
    const x = clamp((box.left || 0) - pad, 0, 1279)
    const y = clamp(box.top || 0, 0, 719)
    groups.push({
      id: hashId(`g:${clip.id}`),
      x,
      y,
      w: width,
      h: height,
      align: ALIGN[clip.align] || ALIGN.left,
      labels: lines.map((text, line) => ({
        id: hashId(`l:${clip.id}:${line}`),
        line,
        text,
        size: clamp(clip.size || 40, 8, 400),
        color: colorInt(clip.color),
        font: deviceFont(clip),
      })),
    })
  }
  return groups
}

// labelTexts is the batch KANALI sends when a value changes and the layout does not.
export function labelTexts(groups) {
  const out = []
  for (const group of groups || []) {
    for (const label of group.labels || []) {
      out.push({ groupId: group.id, labelId: label.id, text: label.text })
    }
  }
  return out
}

export function labelLayoutKey(groups) {
  return (groups || [])
    .map((group) =>
      [
        group.id,
        group.x,
        group.y,
        group.w,
        group.h,
        group.align,
        ...(group.labels || []).map((label) => `${label.id}:${label.line}:${label.size}:${label.color}:${label.font}`),
      ].join(','),
    )
    .join('|')
}

export function labelTextKey(items) {
  return (items || []).map((item) => `${item.labelId}=${item.text}`).join('\n')
}
