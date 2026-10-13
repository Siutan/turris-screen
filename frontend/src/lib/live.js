import { activeClips, widgetText } from './project.js'
import { resolveText } from './template.js'

// MOTION_MIN_MS is the shortest gap between cooler updates for a video, GIF, or effect.
// Each update restarts the clip, so it does not follow the faster source intervals.
export const MOTION_MIN_MS = 2 * 60 * 1000

// sourcesDue lists sources whose interval has elapsed. A source with no sample yet is due.
function intervalMs(source, values) {
  let sec = Math.max(1, Number(source.intervalSec) || 1)
  if (source.config?.preset === 'sports') {
    const status = String(values?.[source.id]?.fields?.status || '')
    if (/live/i.test(status)) sec = 30
  }
  return sec * 1000
}

export function sourcesDue(sources, fetched, nowMs, values = null) {
  const out = []
  for (const source of sources || []) {
    const interval = intervalMs(source, values)
    const last = fetched?.[source.id]
    if (last == null || nowMs - last >= interval) out.push(source)
  }
  return out
}

export function minuteBucket(now) {
  return Math.floor(now.getTime() / 60000)
}

// mergeSample keeps the previous fields when a fetch fails without new ones.
export function mergeSample(prev, sample) {
  const incoming = sample?.fields || {}
  if (sample?.error && Object.keys(incoming).length === 0) {
    return { fields: { ...(prev?.fields || {}) }, error: sample.error, at: prev?.at || 0 }
  }
  return { fields: { ...incoming }, error: sample?.error || '', at: Date.now() }
}

export function ageLabel(at, nowMs) {
  if (!at) return ''
  const sec = Math.max(0, Math.round((nowMs - at) / 1000))
  if (sec < 5) return 'just now'
  if (sec < 60) return `${sec}s ago`
  const min = Math.round(sec / 60)
  return min === 1 ? '1 min ago' : `${min} min ago`
}

// textSignature is the text a send would bake in. A still uses the frame at time 0.
// A moving clip uses every text and widget clip, because the whole timeline is encoded.
export function textSignature(project, now, values, whole = false) {
  const clips = whole
    ? (project?.tracks || []).flatMap((track) => track.clips || [])
    : activeClips(project, 0).map((item) => item.clip)
  const lines = []
  for (const clip of clips) {
    if (clip.type === 'text') lines.push(resolveText(clip.text || '', project.sources || [], values || {}, now))
    else if (clip.type === 'time' || clip.type === 'date') {
      const widget = widgetText(clip, now)
      lines.push(widget.suffix ? `${widget.main} ${widget.suffix}` : widget.main)
    }
  }
  return lines.join('\n')
}

// createSendGate sends only when the signature changed, and remembers one follow-up
// while a send is already running.
export function createSendGate() {
  let inflight = false
  let pending = false
  let sending = null
  let last = null
  let sentAt = 0
  return {
    // arm starts the gap clock without sending, so a clip already on the cooler is left alone.
    arm(nowMs) {
      if (sentAt === 0) sentAt = nowMs
    },
    want(signature, nowMs = 0, minGap = 0) {
      if (inflight) {
        if (signature !== sending) pending = true
        return false
      }
      if (signature === last) return false
      if (minGap > 0 && sentAt !== 0 && nowMs - sentAt < minGap) return false
      return true
    },
    start(signature, nowMs = 0) {
      inflight = true
      sending = signature
      pending = false
      if (nowMs) sentAt = nowMs
    },
    done(ok) {
      inflight = false
      if (ok) last = sending
      const follow = pending
      pending = false
      sending = null
      return follow
    },
  }
}
