// resolveText fills {{id.field}} and {{name.field}} from sampled values.
// Clock fields come from the clock. Countdown fields are computed from the stored target.

const TOKEN = /\{\{\s*([^{}]+?)\s*\}\}/g

const pad = (n) => String(n).padStart(2, '0')

export function clockFields(now) {
  const h = now.getHours()
  const m = pad(now.getMinutes())
  const weekday = now.toLocaleDateString('en-US', { weekday: 'short' })
  const month = now.toLocaleDateString('en-US', { month: 'short' })
  return {
    time12: `${h % 12 || 12}:${m} ${h < 12 ? 'AM' : 'PM'}`,
    time24: `${pad(h)}:${m}`,
    date: `${weekday}, ${month} ${pad(now.getDate())}`,
  }
}

export function formatRemaining(target, now) {
  const end = new Date(target)
  if (!target || Number.isNaN(end.getTime())) return ''
  const past = end < now
  let minutes = Math.round(Math.abs(end - now) / 60000)
  let text
  if (minutes < 60) text = `${minutes} min`
  else if (minutes < 60 * 24) {
    const hours = Math.floor(minutes / 60)
    minutes %= 60
    text = minutes ? `${hours} hr ${minutes} min` : `${hours} hr`
  } else {
    const days = Math.round(minutes / (60 * 24))
    text = days === 1 ? '1 day' : `${days} days`
  }
  return past ? `${text} ago` : text
}

function countdownFields(source, now) {
  return {
    label: source.config?.label || '',
    remaining: formatRemaining(source.config?.target, now),
  }
}

function zoneFields(source, now) {
  const zone = String(source.config?.zone || '').trim()
  if (!zone) return null
  try {
    const time24 = new Intl.DateTimeFormat('en-GB', { timeZone: zone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(now)
    const time12 = new Intl.DateTimeFormat('en-US', { timeZone: zone, hour: 'numeric', minute: '2-digit', hour12: true }).format(now)
    const date = new Intl.DateTimeFormat('en-US', { timeZone: zone, weekday: 'short', month: 'short', day: '2-digit' }).format(now)
    return { time12, time24, date }
  } catch {
    return null
  }
}

export function formatFocus(end, now) {
  const target = new Date(end)
  if (!end || Number.isNaN(target.getTime())) return ''
  let sec = Math.round((target - now) / 1000)
  if (sec < 0) sec = 0
  return `${Math.floor(sec / 60)}:${pad(sec % 60)}`
}

function focusFields(source, now) {
  if (!source.config?.running) return { remaining: '' }
  return { remaining: formatFocus(source.config?.end, now) }
}

function localFields(source, sampled, now) {
  if (source.kind === 'countdown') return countdownFields(source, now)
  if (source.kind === 'zone') return zoneFields(source, now)
  if (source.kind === 'focus') return focusFields(source, now)
  if (source.kind === 'calendar') return { remaining: formatRemaining(sampled?.start, now) }
  return null
}

export function resolveText(text, sources = [], values = {}, now = new Date()) {
  const index = new Map()
  index.set('clock', clockFields(now))
  for (const source of sources || []) {
    const sampled = values?.[source.id]?.fields || {}
    const extra = localFields(source, sampled, now)
    const fields = extra ? { ...sampled, ...extra } : { ...sampled }
    index.set(source.id, fields)
    if (source.name && !index.has(source.name)) index.set(source.name, fields)
  }
  return String(text || '').replace(TOKEN, (full, key) => {
    const dot = key.indexOf('.')
    if (dot <= 0) return full
    const fields = index.get(key.slice(0, dot))
    const field = key.slice(dot + 1)
    if (!fields || fields[field] == null) return full
    return String(fields[field])
  })
}
