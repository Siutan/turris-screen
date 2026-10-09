import { resetEffects } from './effects.js'
import { H, W, activeClips, projectDuration } from './project.js'
import { drawFrame } from './render.js'

const BATCH = 8
const QUALITY = 0.92

// renderFrames draws every frame at fps and streams them to the Go side as JPEGs.
// It returns the export id to finish. Frames where nothing moves reuse the last image.
export async function renderFrames({ project, sources, fps, api, signal, onProgress, now = new Date() }) {
  const duration = projectDuration(project)
  const total = Math.max(1, Math.round(duration * fps))
  const canvas = document.createElement('canvas')
  canvas.width = W
  canvas.height = H
  const ctx = canvas.getContext('2d')
  sources.pauseAll()
  resetEffects()
  const id = await api.BeginExport()
  try {
    let batch = []
    let lastKey = ''
    let lastFrame = ''
    for (let i = 0; i < total; i++) {
      if (signal?.aborted) throw new DOMException('Export cancelled', 'AbortError')
      const t = i / fps
      const active = activeClips(project, t)
      const moving = active.some(({ clip }) => clip.type === 'effect' || sources.moving(clip))
      const key = active.map(({ clip }) => clip.id).join('|')
      let frame = lastFrame
      if (moving || key !== lastKey || !frame) {
        await sources.prepare(active, t)
        drawFrame(ctx, project, sources, t, now)
        frame = canvas.toDataURL('image/jpeg', QUALITY)
      }
      lastKey = moving ? '' : key
      lastFrame = frame
      batch.push(frame)
      if (batch.length >= BATCH) {
        await api.AppendFrames(id, batch)
        batch = []
      }
      onProgress?.((i + 1) / total)
    }
    if (batch.length) await api.AppendFrames(id, batch)
    return id
  } catch (err) {
    api.CancelExport(id).catch(() => {})
    throw err
  }
}

// thumbnail renders a small JPEG of the project for the library card.
export async function thumbnail(project, sources, now = new Date()) {
  const duration = projectDuration(project)
  const t = Math.min(1, duration / 2)
  const full = document.createElement('canvas')
  full.width = W
  full.height = H
  const ctx = full.getContext('2d')
  await sources.prepare(activeClips(project, t), t)
  drawFrame(ctx, project, sources, t, now)
  const small = document.createElement('canvas')
  small.width = 480
  small.height = 270
  small.getContext('2d').drawImage(full, 0, 0, 480, 270)
  return small.toDataURL('image/jpeg', 0.85)
}
