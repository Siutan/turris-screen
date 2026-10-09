import { TEXT_PRESETS } from './project.js'

const COPY_PREFIX = 'turris-clips:'

// encodeClips and decodeClips carry copied clips through the system clipboard as text,
// so a copy made after a YouTube link wins over the link.
export function encodeClips(data) {
  return COPY_PREFIX + JSON.stringify(data)
}

export function decodeClips(text) {
  if (typeof text !== 'string' || !text.startsWith(COPY_PREFIX)) return null
  try {
    return JSON.parse(text.slice(COPY_PREFIX.length))
  } catch {
    return null
  }
}

function guard(fn, onError) {
  return () => {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }
}

// clipMenu lists what can be done to the selected clips from a right-click. Items are
// plain { label, keys, disabled, danger, run, title } objects, with 'sep' between groups.
export function clipMenu(studio, actions, onError) {
  const found = studio.selectedClips
  const ids = found.map((f) => f.clip.id)
  const many = ids.length > 1
  const n = (word) => (many ? `${word} ${ids.length} clips` : word)
  const allLocked = found.every((f) => f.clip.locked)
  const anyUnlocked = found.some((f) => !f.clip.locked)
  const merge = studio.canMerge(ids)
  const splittable = found.some((f) => studio.canSplit(f.clip))
  return [
    { label: 'Bring to Front', keys: 'Ctrl+Shift+]', disabled: !studio.canArrange(ids, 'front'), run: () => studio.arrange(ids, 'front') },
    { label: 'Bring Forward', keys: 'Ctrl+]', disabled: !studio.canArrange(ids, 'forward'), run: () => studio.arrange(ids, 'forward') },
    { label: 'Send Backward', keys: 'Ctrl+[', disabled: !studio.canArrange(ids, 'backward'), run: () => studio.arrange(ids, 'backward') },
    { label: 'Send to Back', keys: 'Ctrl+Shift+[', disabled: !studio.canArrange(ids, 'back'), run: () => studio.arrange(ids, 'back') },
    'sep',
    { label: allLocked ? 'Unlock' : 'Lock', keys: 'Ctrl+L', run: () => studio.toggleLock(ids) },
    { label: 'Center on screen', disabled: !anyUnlocked, run: () => studio.center(ids) },
    'sep',
    { label: 'Cut', keys: 'Ctrl+X', run: guard(() => actions.cut(), onError) },
    { label: 'Copy', keys: 'Ctrl+C', run: guard(() => actions.copy(), onError) },
    { label: 'Paste', keys: 'Ctrl+V', disabled: !actions.canPaste(), run: guard(() => actions.paste(), onError) },
    { label: 'Duplicate', keys: 'Ctrl+D', run: guard(() => studio.duplicate(ids), onError) },
    'sep',
    { label: 'Split at playhead', keys: 'S', disabled: !splittable, run: () => studio.split(ids) },
    ...(many ? [{ label: 'Merge clips', keys: 'Ctrl+J', disabled: !merge.ok, title: merge.reason, run: guard(() => studio.merge(ids), onError) }] : []),
    { label: n('Delete'), keys: 'Del', danger: true, run: () => studio.removeClips(ids) },
    { label: 'Ripple delete', keys: 'Shift+Del', danger: true, run: () => studio.removeClips(ids, { ripple: true }) },
  ]
}

// canvasMenu is shown when the right-click lands on empty canvas.
export function canvasMenu(studio, point, actions, onError) {
  const locked = studio.lockedCount()
  return [
    { label: 'Add text here', run: guard(() => studio.addText(TEXT_PRESETS[1], point), onError) },
    { label: 'Paste', keys: 'Ctrl+V', disabled: !actions.canPaste(), run: guard(() => actions.paste(), onError) },
    { label: 'Select all', keys: 'Ctrl+A', disabled: studio.duration <= 0, run: () => studio.selectAll() },
    'sep',
    { label: locked > 1 ? `Unlock all (${locked})` : 'Unlock all', disabled: !locked, run: () => studio.unlockAll() },
  ]
}
