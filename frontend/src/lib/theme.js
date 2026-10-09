const KEY = 'turris-theme'

export function storedTheme() {
  try {
    const value = localStorage.getItem(KEY)
    if (value === 'light' || value === 'dark' || value === 'auto') return value
  } catch {
    /* storage can be unavailable in private previews */
  }
  return 'auto'
}

export function systemTheme() {
  if (typeof window === 'undefined' || !window.matchMedia) return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveTheme(preference) {
  return preference === 'auto' ? systemTheme() : preference
}

function syncWindow() {
  const raw = getComputedStyle(document.documentElement).getPropertyValue('--canvas').trim()
  const match = /^#([0-9a-f]{6})$/i.exec(raw)
  const set = window.runtime?.WindowSetBackgroundColour
  if (!match || typeof set !== 'function') return
  const value = Number.parseInt(match[1], 16)
  try {
    set((value >> 16) & 255, (value >> 8) & 255, value & 255, 255)
  } catch {
    /* browser preview has no desktop window */
  }
}

export function applyTheme(preference) {
  const theme = resolveTheme(preference)
  const root = document.documentElement
  root.dataset.theme = theme
  root.style.colorScheme = theme
  syncWindow()
  return theme
}

export function saveTheme(preference) {
  try {
    localStorage.setItem(KEY, preference)
  } catch {
    /* ignore */
  }
  return applyTheme(preference)
}
