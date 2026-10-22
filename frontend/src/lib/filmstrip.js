// Filmstrip sampling for timeline clips. Frames are taken across the clip's source
// range, so moving the in point picks up the frames that actually play.

const STEP = 0.25

export function quantizeTime(t) {
  const n = Number(t)
  if (!Number.isFinite(n) || n <= 0) return 0
  return Math.round(n / STEP) * STEP
}

export function filmstripSlots(widthPx) {
  if (!Number.isFinite(widthPx) || widthPx <= 0) return 1
  return Math.max(1, Math.min(20, Math.round(widthPx / 72)))
}

export function filmstripKey(assetId, from, to, slots) {
  return `${assetId}:${quantizeTime(from).toFixed(2)}:${quantizeTime(to).toFixed(2)}:${Math.max(1, slots | 0)}`
}

// filmstripTimes is one sample per slot, at the middle of that slot's share of [from, to].
export function filmstripTimes(from, to, slots) {
  const start = Math.max(0, Number(from) || 0)
  const end = Math.max(start, Number(to) || start)
  const n = Math.max(1, slots | 0)
  const span = end - start
  if (span < 0.05) return Array.from({ length: n }, () => quantizeTime(start))
  const times = []
  for (let i = 0; i < n; i++) times.push(quantizeTime(start + (span * (i + 0.5)) / n))
  return times
}
