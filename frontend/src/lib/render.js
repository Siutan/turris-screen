import { paintEffect, syncEffects } from './effects.js'
import { H, W, activeClips, filterCSS, isTextual, widgetText } from './project.js'
import { resolveText } from './template.js'

let measureCtx

function measurer() {
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')
  return measureCtx
}

const PREVIEW_FONTS = {
  'roboto-regular': ['Roboto', 400],
  'roboto-thin': ['Roboto', 100],
  'BitcountSingle-Regular': ['Bitcount Single', 400],
  'Orbitron-Regular': ['Orbitron', 400],
  'RubikGlitch-Regular': ['Rubik Glitch', 400],
}

export function fontString(weight, size, family) {
  const mapped = PREVIEW_FONTS[family]
  const name = mapped ? mapped[0] : family
  const faceWeight = mapped ? mapped[1] : weight
  return `${faceWeight} ${Math.max(1, size)}px "${name}", "Segoe UI", sans-serif`
}

// drawFrame paints the project at time t onto a 1280×720 context.
// skip drops clips that the cooler draws itself, so a clock is not baked into the loop.
export function drawFrame(ctx, project, sources, t, now = new Date(), values = null, skip = null) {
  const env = { sources: project.sources || [], values: values || {} }
  ctx.save()
  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.filter = 'none'
  ctx.globalAlpha = 1
  ctx.fillStyle = '#000'
  ctx.fillRect(0, 0, W, H)
  syncEffects(project)
  for (const { clip } of activeClips(project, t)) {
    if (skip?.(clip)) continue
    if (clip.type === 'media') drawMedia(ctx, clip, sources, t)
    else if (isTextual(clip)) drawText(ctx, clip, now, env)
    else if (clip.type === 'effect') paintEffect(ctx, clip, t)
  }
  ctx.restore()
}

export function mediaRect(clip, width, height) {
  if (!width || !height) return { x: 0, y: 0, w: W, h: H }
  const base = clip.fit === 'contain' ? Math.min(W / width, H / height) : Math.max(W / width, H / height)
  const s = base * (clip.scale || 1)
  const w = width * s
  const h = height * s
  return { x: (W - w) / 2 + (clip.x || 0), y: (H - h) / 2 + (clip.y || 0), w, h }
}

function drawMedia(ctx, clip, sources, t) {
  const entry = sources.get(clip.assetId)
  const frame = sources.frame(clip, t)
  if (!entry || !frame) return
  const r = mediaRect(clip, entry.width, entry.height)
  ctx.save()
  ctx.filter = filterCSS(clip)
  ctx.drawImage(frame, r.x, r.y, r.w, r.h)
  ctx.restore()
}

// textLayout measures a text or widget clip. Widgets draw a smaller suffix (AM/PM) after the time.
export function textLayout(clip, now = new Date(), ctx = measurer(), env = null) {
  const content = clip.type === 'text'
    ? { main: env ? resolveText(clip.text || '', env.sources || [], env.values || {}, now) : (clip.text || ''), suffix: '' }
    : widgetText(clip, now)
  const shape = (s) => (clip.upper ? s.toUpperCase() : s)
  const lines = shape(content.main).split('\n')
  const suffix = shape(content.suffix)
  ctx.save()
  ctx.font = fontString(clip.weight, clip.size, clip.font)
  ctx.letterSpacing = `${clip.spacing || 0}px`
  const widths = lines.map((line) => ctx.measureText(line).width)
  const suffixSize = clip.size * 0.3
  let suffixW = 0
  if (suffix) {
    ctx.font = fontString(400, suffixSize, clip.font)
    ctx.letterSpacing = '0px'
    suffixW = ctx.measureText(suffix).width
  }
  ctx.restore()
  const gap = suffix ? clip.size * 0.12 : 0
  const lineH = clip.size * 1.12
  const blockW = Math.max(1, ...widths) + gap + suffixW
  const blockH = lines.length * lineH
  let left = clip.x - blockW / 2
  if (clip.align === 'left') left = clip.x
  if (clip.align === 'right') left = clip.x - blockW
  return { lines, widths, suffix, suffixSize, suffixW, gap, lineH, left, top: clip.y - blockH / 2, blockW, blockH }
}

function drawText(ctx, clip, now, env) {
  const L = textLayout(clip, now, ctx, env)
  ctx.save()
  ctx.fillStyle = clip.color || '#fff'
  ctx.textBaseline = 'middle'
  ctx.textAlign = 'left'
  ctx.font = fontString(clip.weight, clip.size, clip.font)
  ctx.letterSpacing = `${clip.spacing || 0}px`
  const textW = L.blockW - L.gap - L.suffixW
  L.lines.forEach((line, i) => {
    let x = L.left
    if (clip.align === 'center') x = L.left + (textW - L.widths[i]) / 2
    if (clip.align === 'right') x = L.left + textW - L.widths[i]
    ctx.fillText(line, x, L.top + L.lineH * (i + 0.5))
  })
  if (L.suffix) {
    ctx.font = fontString(400, L.suffixSize, clip.font)
    ctx.letterSpacing = '0px'
    const last = L.lines.length - 1
    ctx.fillText(L.suffix, L.left + L.widths[last] + L.gap, L.top + L.lineH * (last + 0.5) + clip.size * 0.08)
  }
  ctx.restore()
}

// clipBounds is the on-canvas box of a clip, in 1280×720 units.
export function clipBounds(clip, sources, now = new Date(), env = null) {
  if (clip.type === 'effect') return { x: 0, y: 0, w: W, h: H }
  if (clip.type === 'media') {
    const entry = sources.get(clip.assetId)
    return mediaRect(clip, entry?.width, entry?.height)
  }
  const L = textLayout(clip, now, measurer(), env)
  return { x: L.left, y: L.top, w: L.blockW, h: L.blockH }
}

// hitTest returns the top-most visible clip under (x, y) at time t. Locked clips are
// skipped unless asked for, so clicks reach whatever is under them.
export function hitTest(project, sources, t, x, y, { locked = false, now = new Date(), values = null } = {}) {
  const env = { sources: project.sources || [], values: values || {} }
  const active = activeClips(project, t)
  for (let i = active.length - 1; i >= 0; i--) {
    const { clip } = active[i]
    if (clip.type === 'effect') continue
    if (clip.locked && !locked) continue
    const b = clipBounds(clip, sources, now, env)
    const pad = isTextual(clip) ? 12 : 0
    if (x >= b.x - pad && x <= b.x + b.w + pad && y >= b.y - pad && y <= b.y + b.h + pad) return clip
  }
  return null
}
