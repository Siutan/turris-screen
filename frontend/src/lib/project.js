// The editor's project model. Everything here is plain data so it can be saved as JSON.

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
  return { version: 1, name, tracks: [newTrack()] }
}

export function newTrack() {
  return { id: uid('t'), hidden: false, clips: [] }
}

export const FILTERS = [
  { id: 'none', label: 'None', css: '' },
  { id: 'cinematic', label: 'Cinematic', css: 'contrast(1.12) saturate(0.82) sepia(0.18) hue-rotate(-10deg)' },
  { id: 'vivid', label: 'Vivid', css: 'saturate(1.5) contrast(1.08)' },
  { id: 'soft', label: 'Soft', css: 'contrast(0.86) brightness(1.06) saturate(0.88)' },
  { id: 'bw', label: 'B&W', css: 'grayscale(1) contrast(1.12)' },
]

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

export const TEXT_PRESETS = [
  { id: 'heading', label: 'Add a heading', text: 'Heading', size: 120, weight: 600 },
  { id: 'subheading', label: 'Add a subheading', text: 'Subheading', size: 64, weight: 400 },
  { id: 'body', label: 'Add body text', text: 'Body text', size: 40, weight: 400 },
]

// Built-in overlay packs. The painter in effects.js maps each id to Pixi filters.
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

export function effectMeta(id) {
  for (const pack of EFFECT_PACKS) {
    const effect = pack.effects.find((item) => item.id === id)
    if (effect) return { ...effect, packId: pack.id, packLabel: pack.label }
  }
  return null
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
    amount: 70,
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
export function activeClips(project, t) {
  const out = []
  project.tracks.forEach((track, trackIndex) => {
    if (track.hidden) return
    for (const clip of track.clips) {
      if (t >= clip.start && t < clipEnd(clip)) out.push({ clip, track, trackIndex })
    }
  })
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
  if (clips.some((c) => c.type === 'time' || c.type === 'date')) return 'widget'
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

export function clipLabel(clip, now = new Date()) {
  if (clip.type === 'text') return clip.text.split('\n')[0] || 'Text'
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
