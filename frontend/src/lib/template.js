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

export function resolveText(text, sources = [], values = {}, now = new Date()) {
  const index = new Map()
  index.set('clock', clockFields(now))
  for (const source of sources || []) {
    const sampled = values?.[source.id]?.fields || {}
    const fields = source.kind === 'countdown' ? { ...sampled, ...countdownFields(source, now) } : { ...sampled }
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
