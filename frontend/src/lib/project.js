// The editor's project model. Everything here is plain data so it can be saved as JSON.

import { resolveText } from './template.js'

export const W = 1280
export const H = 720
export const MIN_CLIP = 0.1

export const DEFAULT_LIMITS = {
  maxSeconds: 60,
  maxBytes: 24 << 20,
  motionKbps: 5520,
  stillKbps: 1500,
  headroom: 0.85,
  renderFps: 30,
}

let counter = 0

export function uid(prefix) {
  counter = (counter + 1) % 1e6
  return prefix + Date.now().toString(36) + counter.toString(36) + Math.random().toString(36).slice(2, 6)
}

export function newProject(name = 'Untitled') {
  return { version: 1, name, tracks: [newTrack()], sources: [], live: false }
}

export function newTrack() {
  return { id: uid('t'), hidden: false, clips: [] }
}

// Color grades for media clips. filterCSS turns these into canvas/CSS filters.
export const FILTER_GROUPS = [
  { id: 'look', label: 'Look' },
  { id: 'tone', label: 'Tone' },
  { id: 'tint', label: 'Tint' },
  { id: 'film', label: 'Film' },
  { id: 'mono', label: 'Mono' },
  { id: 'invert', label: 'Invert' },
]

export const FILTERS = [
  { id: 'none', label: 'None', group: 'look', css: '' },
  { id: 'cinematic', label: 'Cinematic', group: 'look', css: 'contrast(1.12) saturate(0.82) sepia(0.18) hue-rotate(-10deg)' },
  { id: 'vivid', label: 'Vivid', group: 'look', css: 'saturate(1.5) contrast(1.08)' },
  { id: 'soft', label: 'Soft', group: 'look', css: 'contrast(0.86) brightness(1.06) saturate(0.88)' },
  { id: 'punch', label: 'Punch', group: 'look', css: 'contrast(1.38) saturate(1.18) brightness(1.02)' },
  { id: 'fade', label: 'Fade', group: 'look', css: 'contrast(0.8) brightness(1.1) saturate(0.72)' },
  { id: 'matte', label: 'Matte', group: 'look', css: 'contrast(0.88) saturate(0.78) brightness(1.04)' },
  { id: 'drama', label: 'Drama', group: 'look', css: 'contrast(1.42) saturate(0.68) brightness(0.9)' },
  { id: 'pop', label: 'Pop', group: 'look', css: 'saturate(1.85) contrast(1.18) brightness(1.06)' },
  { id: 'pastel', label: 'Pastel', group: 'look', css: 'saturate(0.62) contrast(0.88) brightness(1.16)' },
  { id: 'dream', label: 'Dream', group: 'look', css: 'contrast(0.84) brightness(1.14) saturate(0.78) sepia(0.1)' },
  { id: 'crisp', label: 'Crisp', group: 'look', css: 'contrast(1.22) saturate(1.08) brightness(1.04)' },

  { id: 'warm', label: 'Warm', group: 'tone', css: 'sepia(0.28) saturate(1.28) hue-rotate(-8deg) brightness(1.04)' },
  { id: 'cool', label: 'Cool', group: 'tone', css: 'saturate(0.92) hue-rotate(16deg) brightness(1.03) contrast(1.05)' },
  { id: 'sunset', label: 'Sunset', group: 'tone', css: 'sepia(0.38) saturate(1.45) hue-rotate(-18deg) contrast(1.08) brightness(1.06)' },
  { id: 'golden', label: 'Golden', group: 'tone', css: 'sepia(0.32) saturate(1.5) brightness(1.1) hue-rotate(-12deg)' },
  { id: 'amber', label: 'Amber', group: 'tone', css: 'sepia(0.48) saturate(1.65) hue-rotate(-16deg) contrast(1.1)' },
  { id: 'rose', label: 'Rose', group: 'tone', css: 'hue-rotate(-22deg) saturate(1.4) sepia(0.14) brightness(1.05)' },
  { id: 'arctic', label: 'Arctic', group: 'tone', css: 'saturate(0.68) hue-rotate(26deg) brightness(1.1) contrast(0.96)' },
  { id: 'moody', label: 'Moody', group: 'tone', css: 'contrast(1.2) saturate(0.52) brightness(0.86) hue-rotate(10deg)' },
  { id: 'night', label: 'Night', group: 'tone', css: 'brightness(0.6) contrast(1.28) saturate(0.72) hue-rotate(14deg)' },

  { id: 'emerald', label: 'Emerald', group: 'tint', css: 'hue-rotate(78deg) saturate(1.35) contrast(1.06)' },
  { id: 'ocean', label: 'Ocean', group: 'tint', css: 'hue-rotate(168deg) saturate(1.25) contrast(1.06) brightness(0.98)' },
  { id: 'teal', label: 'Teal', group: 'tint', css: 'hue-rotate(145deg) saturate(1.15) contrast(1.08) brightness(1.02)' },
  { id: 'violet', label: 'Violet', group: 'tint', css: 'hue-rotate(255deg) saturate(1.2) contrast(1.1)' },
  { id: 'candy', label: 'Candy', group: 'tint', css: 'saturate(1.75) hue-rotate(-28deg) contrast(1.06) brightness(1.08)' },
  { id: 'neon', label: 'Neon', group: 'tint', css: 'saturate(2.1) contrast(1.32) hue-rotate(18deg) brightness(1.08)' },
  { id: 'infrared', label: 'Infrared', group: 'tint', css: 'hue-rotate(180deg) saturate(1.5) contrast(1.16) sepia(0.12)' },
  { id: 'aurora', label: 'Aurora', group: 'tint', css: 'hue-rotate(110deg) saturate(1.6) contrast(1.12) brightness(1.04)' },

  { id: 'sepia', label: 'Sepia', group: 'film', css: 'sepia(0.9) contrast(1.05) brightness(1.02)' },
  { id: 'vintage', label: 'Vintage', group: 'film', css: 'sepia(0.42) contrast(0.88) brightness(1.1) saturate(0.68)' },
  { id: 'faded', label: 'Faded', group: 'film', css: 'contrast(0.76) saturate(0.6) brightness(1.14) sepia(0.16)' },
  { id: 'bleach', label: 'Bleach', group: 'film', css: 'contrast(1.28) saturate(0.48) brightness(1.12)' },
  { id: 'lomo', label: 'Lomo', group: 'film', css: 'contrast(1.32) saturate(1.45) brightness(0.94) sepia(0.16)' },
  { id: 'slide', label: 'Slide', group: 'film', css: 'contrast(1.18) saturate(1.4) sepia(0.16) hue-rotate(-6deg) brightness(1.02)' },
  { id: 'cross', label: 'Cross', group: 'film', css: 'contrast(1.12) saturate(0.82) sepia(0.22) hue-rotate(18deg) brightness(1.04)' },
  { id: 'retro', label: 'Retro', group: 'film', css: 'sepia(0.55) contrast(1.15) saturate(1.2) hue-rotate(-8deg) brightness(0.98)' },

  { id: 'bw', label: 'B&W', group: 'mono', css: 'grayscale(1) contrast(1.12)' },
  { id: 'noir', label: 'Noir', group: 'mono', css: 'grayscale(1) contrast(1.45) brightness(0.88)' },
  { id: 'silver', label: 'Silver', group: 'mono', css: 'grayscale(1) contrast(0.9) brightness(1.14)' },
  { id: 'ink', label: 'Ink', group: 'mono', css: 'grayscale(1) contrast(1.7) brightness(0.82)' },
  { id: 'ash', label: 'Ash', group: 'mono', css: 'grayscale(0.85) contrast(0.95) saturate(0.3) brightness(1.05)' },
  { id: 'graphite', label: 'Graphite', group: 'mono', css: 'grayscale(1) contrast(1.2) brightness(0.78)' },

  { id: 'invert', label: 'Invert', group: 'invert', css: 'invert(1)' },
  { id: 'negative', label: 'Negative', group: 'invert', css: 'invert(1) contrast(1.15) saturate(1.2)' },
  { id: 'solar', label: 'Solar', group: 'invert', css: 'invert(0.72) hue-rotate(180deg) saturate(1.55) contrast(1.18)' },
  { id: 'xray', label: 'X-ray', group: 'invert', css: 'invert(0.92) grayscale(0.35) contrast(1.35) brightness(1.18)' },
  { id: 'ghost', label: 'Ghost', group: 'invert', css: 'invert(0.85) brightness(1.2) contrast(0.9) saturate(0.4)' },
  { id: 'blueprint', label: 'Blueprint', group: 'invert', css: 'grayscale(1) sepia(1) hue-rotate(180deg) saturate(3) contrast(1.12) brightness(0.95)' },
]

export const FILTER_LIBRARY = FILTER_GROUPS.map((group) => ({
  ...group,
  filters: FILTERS.filter((filter) => filter.group === group.id),
}))

export const FONTS = [
  'Segoe UI',
  'Segoe UI Variable Display',
  'Bahnschrift',
  'Arial',
  'Verdana',
  'Trebuchet MS',
  'Georgia',
  'Times New Roman',
  'Consolas',
  'Impact',
]

export const WEIGHTS = [
  { value: 300, label: 'Light' },
  { value: 400, label: 'Regular' },
  { value: 600, label: 'Semibold' },
  { value: 700, label: 'Bold' },
]

export const SWATCHES = ['#ffffff', '#1c1e22', '#c9ccd3', '#cdd5e4', '#f7a8ad', '#fcd59a']

export const CLOCK_FIELDS = [
  { id: 'time12', label: '12-hour time' },
  { id: 'time24', label: '24-hour time' },
  { id: 'date', label: 'Date' },
]

export const SOURCE_KINDS = {
  countdown: {
    label: 'Countdown',
    interval: 60,
    min: 60,
    fields: [
      { id: 'remaining', label: 'Remaining' },
      { id: 'label', label: 'Label' },
    ],
  },
  system: {
    label: 'System',
    interval: 5,
    min: 2,
    fields: [
      { id: 'cpu', label: 'CPU' },
      { id: 'ramUsed', label: 'RAM used' },
      { id: 'ramTotal', label: 'RAM total' },
      { id: 'ramPercent', label: 'RAM percent' },
      { id: 'diskFree', label: 'Disk free' },
      { id: 'diskTotal', label: 'Disk total' },
      { id: 'netDown', label: 'Download' },
      { id: 'netUp', label: 'Upload' },
    ],
  },
  temps: {
    label: 'Temperatures',
    interval: 5,
    min: 2,
    fields: [
      { id: 'cpu', label: 'CPU' },
      { id: 'gpu', label: 'GPU' },
    ],
  },
  media: {
    label: 'Now playing',
    interval: 2,
    min: 2,
    fields: [
      { id: 'title', label: 'Title' },
      { id: 'artist', label: 'Artist' },
      { id: 'album', label: 'Album' },
      { id: 'state', label: 'State' },
    ],
  },
  file: {
    label: 'File',
    interval: 5,
    min: 2,
    fields: [{ id: 'text', label: 'Text' }],
  },
}

function defaultSourceConfig(kind) {
  if (kind === 'countdown') {
    const when = new Date(Date.now() + 60 * 60 * 1000)
    const target = `${when.getFullYear()}-${pad(when.getMonth() + 1)}-${pad(when.getDate())}T${pad(when.getHours())}:${pad(when.getMinutes())}`
    return { target, label: 'Countdown' }
  }
  if (kind === 'file') return { path: '', mode: 'line', field: '' }
  return {}
}

export function clampInterval(kind, seconds) {
  const min = SOURCE_KINDS[kind]?.min ?? 2
  const n = Math.round(Number(seconds) || min)
  return Math.max(min, n)
}

export function newSource(kind, config = {}) {
  const spec = SOURCE_KINDS[kind]
  if (!spec) throw new Error('That source is not available.')
  return {
    id: uid('s'),
    name: spec.label,
    kind,
    intervalSec: spec.interval,
    config: { ...defaultSourceConfig(kind), ...config },
  }
}

export const TEXT_PRESETS = [
  { id: 'heading', label: 'Add a heading', text: 'Heading', size: 120, weight: 600 },
  { id: 'subheading', label: 'Add a subheading', text: 'Subheading', size: 64, weight: 400 },
  { id: 'body', label: 'Add body text', text: 'Body text', size: 40, weight: 400 },
]

// Built-in overlay packs. The painter in effects.js maps each id to Pixi filters.
// Slider defaults recreate the old Amount 70 look. `legacy` controls follow a saved
// Amount value until the clip stores its own settings. Speed, size, length, angle,
// and opacity stay put, because Amount never drove them.
const control = (id, label, def, legacy = false) => ({ id, label, def, legacy })

export const EFFECT_PACKS = [
  {
    id: 'weather',
    label: 'Weather',
    effects: [
      { id: 'snow', label: 'Snow' },
      { id: 'rain', label: 'Rain' },
      { id: 'storm', label: 'Storm' },
      { id: 'fog', label: 'Fog' },
      { id: 'sun', label: 'Sun' },
      { id: 'blizzard', label: 'Blizzard' },
    ],
  },
  {
    id: 'glitchpop',
    label: 'Glitchpop',
    effects: [
      { id: 'glitch', label: 'Glitch' },
      { id: 'duotone', label: 'Duotone wave' },
      { id: 'vhs', label: 'VHS' },
      { id: 'signal', label: 'Signal' },
      { id: 'prism', label: 'Prism' },
    ],
  },
]

export const EFFECT_CONTROLS = {
  snow: [
    control('density', 'Density', 70, true),
    control('speed', 'Speed', 50),
    control('size', 'Size', 50),
    control('opacity', 'Opacity', 100),
  ],
  rain: [
    control('density', 'Density', 70, true),
    control('speed', 'Speed', 50),
    control('length', 'Length', 50),
    control('angle', 'Angle', 50),
    control('opacity', 'Opacity', 100),
  ],
  storm: [
    control('density', 'Density', 70, true),
    control('speed', 'Speed', 50),
    control('angle', 'Angle', 50),
    control('darkness', 'Darkness', 60, true),
    control('lightning', 'Lightning', 80, true),
    control('flash', 'Bolt rate', 50),
  ],
  fog: [
    control('density', 'Density', 70, true),
    control('speed', 'Speed', 50),
    control('blur', 'Blur', 70, true),
    control('haze', 'Haze', 75, true),
  ],
  sun: [
    control('rays', 'Rays', 76, true),
    control('speed', 'Speed', 50),
    control('warmth', 'Warmth', 70, true),
  ],
  blizzard: [
    control('density', 'Density', 70, true),
    control('speed', 'Speed', 50),
    control('size', 'Size', 50),
    control('whiteout', 'Whiteout', 70, true),
  ],
  glitch: [
    control('offset', 'Offset', 66, true),
    control('slices', 'Slices', 60, true),
    control('split', 'Color split', 64, true),
    control('speed', 'Speed', 50),
  ],
  duotone: [
    control('color', 'Color', 90, true),
    control('wave', 'Wave', 72, true),
    control('speed', 'Speed', 50),
  ],
  vhs: [
    control('scanlines', 'Scanlines', 63, true),
    control('noise', 'Noise', 70, true),
    control('scratches', 'Scratches', 67, true),
    control('curve', 'Curve', 70, true),
    control('speed', 'Speed', 50),
  ],
  signal: [
    control('blocks', 'Blocks', 67, true),
    control('noise', 'Noise', 67, true),
    control('breakup', 'Breakup', 64, true),
    control('speed', 'Speed', 50),
  ],
  prism: [
    control('split', 'Split', 65, true),
    control('color', 'Color', 61, true),
    control('speed', 'Speed', 50),
  ],
}

export function effectMeta(id) {
  for (const pack of EFFECT_PACKS) {
    const effect = pack.effects.find((item) => item.id === id)
    if (effect) return { ...effect, packId: pack.id, packLabel: pack.label }
  }
  return null
}

export function effectControls(id) {
  return EFFECT_CONTROLS[id] || []
}

export function effectDefaults(id) {
  const settings = {}
  for (const item of effectControls(id)) settings[item.id] = item.def
  return settings
}

function clampUnit(value) {
  return Math.min(100, Math.max(0, value))
}

// effectSettings resolves every slider for a clip. Saved settings win. Older clips
// that only have Amount scale the controls Amount used to drive, and leave the rest
// at their defaults so a dimmed effect does not also change speed or size.
export function effectSettings(clip) {
  const controls = effectControls(clip?.effect)
  const stored = clip?.settings && typeof clip.settings === 'object' ? clip.settings : null
  const amount = Number.isFinite(clip?.amount) ? clip.amount : null
  const settings = {}
  for (const item of controls) {
    const raw = stored?.[item.id]
    if (Number.isFinite(raw)) settings[item.id] = clampUnit(raw)
    else if (amount != null && item.legacy) settings[item.id] = clampUnit(Math.round((item.def * amount) / 70))
    else settings[item.id] = item.def
  }
  return settings
}

export const TIME_FORMATS = [
  { id: '12h', label: '12-hour' },
  { id: '24h', label: '24-hour' },
]

export const DATE_FORMATS = [
  { id: 'short', label: 'Thu, Oct 02' },
  { id: 'long', label: 'Thursday, October 2' },
  { id: 'numeric', label: '02/10/2026' },
]

export function filterCSS(clip, blurScale = 1) {
  const a = clip.adjust || {}
  const parts = []
  if (a.brightness) parts.push(`brightness(${1 + a.brightness / 100})`)
  if (a.contrast) parts.push(`contrast(${1 + a.contrast / 100})`)
  if (a.saturation) parts.push(`saturate(${1 + a.saturation / 100})`)
  const preset = FILTERS.find((f) => f.id === clip.filter)
  if (preset?.css) parts.push(preset.css)
  if (a.blur) parts.push(`blur(${(a.blur * blurScale).toFixed(2)}px)`)
  return parts.length ? parts.join(' ') : 'none'
}

export function mediaClip(asset, start, duration) {
  return {
    id: uid('c'),
    type: 'media',
    assetId: asset.id,
    name: asset.name,
    start,
    duration,
    in: 0,
    fit: 'cover',
    scale: 1,
    x: 0,
    y: 0,
    adjust: { brightness: 0, contrast: 0, saturation: 0, blur: 0 },
    filter: 'none',
  }
}

export function textClip(preset, start, duration) {
  return {
    id: uid('c'),
    type: 'text',
    start,
    duration,
    text: preset.text,
    font: 'Segoe UI',
    weight: preset.weight,
    size: preset.size,
    color: '#ffffff',
    spacing: 0,
    upper: false,
    align: 'center',
    x: W / 2,
    y: H / 2,
  }
}

export function effectClip(effectId, start, duration) {
  const meta = effectMeta(effectId)
  return {
    id: uid('c'),
    type: 'effect',
    effect: effectId,
    name: meta?.label || 'Effect',
    start,
    duration,
    settings: effectDefaults(effectId),
  }
}

export function widgetClip(kind, start, duration) {
  if (kind === 'time') {
    return {
      id: uid('c'),
      type: 'time',
      start,
      duration,
      format: '12h',
      font: 'Segoe UI',
      weight: 300,
      size: 230,
      color: '#ffffff',
      spacing: -6,
      upper: false,
      align: 'left',
      x: 100,
      y: 380,
    }
  }
  return {
    id: uid('c'),
    type: 'date',
    start,
    duration,
    format: 'short',
    font: 'Segoe UI',
    weight: 400,
    size: 34,
    color: '#ffffff',
    spacing: 8,
    upper: true,
    align: 'left',
    x: 106,
    y: 196,
  }
}

export function isTextual(clip) {
  return clip?.type === 'text' || clip?.type === 'time' || clip?.type === 'date'
}

export function clipEnd(clip) {
  return clip.start + clip.duration
}

export function projectDuration(project) {
  let end = 0
  for (const track of project.tracks) {
    for (const clip of track.clips) end = Math.max(end, clipEnd(clip))
  }
  return end
}

// activeClips lists what is on screen at t, bottom layer first.
// The top timeline row is in front, so the last track is painted first.
export function activeClips(project, t) {
  const out = []
  for (let trackIndex = project.tracks.length - 1; trackIndex >= 0; trackIndex--) {
    const track = project.tracks[trackIndex]
    if (track.hidden) continue
    for (const clip of track.clips) {
      if (t >= clip.start && t < clipEnd(clip)) out.push({ clip, track, trackIndex })
    }
  }
  return out
}

export function findClip(project, id) {
  if (!id) return null
  for (let trackIndex = 0; trackIndex < project.tracks.length; trackIndex++) {
    const track = project.tracks[trackIndex]
    const index = track.clips.findIndex((c) => c.id === id)
    if (index >= 0) return { clip: track.clips[index], track, trackIndex, index }
  }
  return null
}

export function allClips(project) {
  return project.tracks.flatMap((t) => t.clips)
}

// projectKind is the label the home screen filters on.
export function projectKind(project, kindOfAsset) {
  const clips = allClips(project)
  const kinds = new Set(clips.filter((c) => c.type === 'media').map((c) => kindOfAsset(c.assetId)))
  if (kinds.has('video')) return 'video'
  if (kinds.has('gif')) return 'gif'
  if ((project.sources && project.sources.length) || clips.some((c) => c.type === 'time' || c.type === 'date')) return 'widget'
  return 'image'
}

export function hasMotion(project, kindOfAsset) {
  return allClips(project).some((c) => c.type === 'effect' || (c.type === 'media' && ['video', 'gif'].includes(kindOfAsset(c.assetId))))
}

// estimateBytes mirrors the encoder's bitrate choice. It is an upper bound: still
// pictures usually come out smaller than the target.
export function estimateBytes(seconds, motion, limits) {
  if (seconds <= 0) return 0
  let kbps = motion ? limits.motionKbps : limits.stillKbps
  const budget = (limits.maxBytes * 8 * limits.headroom) / seconds / 1000
  if (budget < kbps) kbps = budget
  return (kbps * 1000 * seconds) / 8
}

const pad = (n) => String(n).padStart(2, '0')

export function widgetText(clip, now) {
  if (clip.type === 'time') {
    const h = now.getHours()
    const m = pad(now.getMinutes())
    if (clip.format === '24h') return { main: `${pad(h)}:${m}`, suffix: '' }
    return { main: `${h % 12 || 12}:${m}`, suffix: h < 12 ? 'AM' : 'PM' }
  }
  if (clip.format === 'long') {
    const weekday = now.toLocaleDateString('en-US', { weekday: 'long' })
    const month = now.toLocaleDateString('en-US', { month: 'long' })
    return { main: `${weekday}, ${month} ${now.getDate()}`, suffix: '' }
  }
  if (clip.format === 'numeric') return { main: now.toLocaleDateString(), suffix: '' }
  const weekday = now.toLocaleDateString('en-US', { weekday: 'short' })
  const month = now.toLocaleDateString('en-US', { month: 'short' })
  return { main: `${weekday}, ${month} ${pad(now.getDate())}`, suffix: '' }
}

export function clipLabel(clip, now = new Date(), env = null) {
  if (clip.type === 'text') {
    const text = env ? resolveText(clip.text || '', env.sources || [], env.values || {}, now) : clip.text || ''
    return text.split('\n')[0] || 'Text'
  }
  if (clip.type === 'effect') return effectMeta(clip.effect)?.label || clip.name || 'Effect'
  if (clip.type === 'time' || clip.type === 'date') {
    const { main, suffix } = widgetText(clip, now)
    return suffix ? `${main} ${suffix}` : main
  }
  return clip.name || 'Media'
}

export function fmtClock(seconds) {
  const s = Math.max(0, Math.floor(seconds + 1e-6))
  return `${pad(Math.floor(s / 60))}:${pad(s % 60)}`
}

// fmtSourceTime shows a point in a source video as m:ss.s, so short sections read exactly.
export function fmtSourceTime(seconds) {
  const tenths = Math.max(0, Math.round(seconds * 10))
  const m = Math.floor(tenths / 600)
  const s = (tenths % 600) / 10
  return `${m}:${s < 10 ? '0' : ''}${s.toFixed(1)}`
}

// parseTime reads 90, 1:30, 1:30.5, or 1:02:03 as seconds. Empty text is null and
// anything else is NaN.
export function parseTime(text) {
  const s = String(text ?? '').trim()
  if (!s) return null
  if (!/^\d+(:\d{1,2}){0,2}(\.\d+)?$/.test(s)) return NaN
  return s.split(':').reduce((total, part) => total * 60 + Number(part), 0)
}

export function fmtBytes(bytes) {
  if (!bytes) return '0 KB'
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
