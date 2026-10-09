import { hasMotion } from './project.js'
import { renderFrames, renderStill } from './exporter.js'
import { MOTION_MIN_MS, ageLabel, createSendGate, mergeSample, minuteBucket, sourcesDue, textSignature } from './live.js'

// session is the live-text store. Sampled values stay out of the project, so polling
// does not mark a draft dirty or push an undo step.
class LiveSession {
  project = $state(null)
  projectId = $state('')
  revision = $state(0)
  values = $state({})
  busy = $state(false)
  rendering = $state(false)
  renderLabel = $state('')
  renderPercent = $state(0)
  lastError = $state('')
  attached = false
  media = null
  fetched = {}
  lastMinute = -1
  gate = createSendGate()
  abort = null

  follow(project, id, media) {
    if ((id || '') !== this.projectId) this.gate = createSendGate()
    this.project = project
    this.projectId = id || ''
    this.media = media
    this.attached = true
  }

  // unfollow keeps a live project running after the editor closes.
  unfollow() {
    this.attached = false
    this.busy = false
    if (!this.project?.live) this.clear()
  }

  park(project, id, media) {
    if ((id || '') !== this.projectId) this.gate = createSendGate()
    this.project = project
    this.projectId = id || ''
    this.media = media
    this.attached = false
  }

  clear() {
    this.project = null
    this.projectId = ''
    this.media = null
    this.attached = false
    this.gate = createSendGate()
    this.rendering = false
  }
}

export const session = new LiveSession()

export { ageLabel }

// runLiveTick samples sources that are due and, when Keep live is on, sends again
// if the baked text changed. A still is one frame. A video, GIF, or effect is the
// whole clip, and that send waits MOTION_MIN_MS because the cooler restarts it.
export async function runLiveTick(ctx) {
  const project = session.project
  if (!project) return
  const now = new Date()
  const nowMs = now.getTime()
  const bucket = minuteBucket(now)
  let changed = false
  if (bucket !== session.lastMinute) {
    session.lastMinute = bucket
    changed = true
  }
  const due = sourcesDue(project.sources || [], session.fetched, nowMs).filter((source) => source.kind !== 'countdown')
  if (due.length && ctx.sample) {
    try {
      const samples = await ctx.sample(due.map((source) => ({ id: source.id, kind: source.kind, config: source.config || {} })))
      let values = session.values
      for (const sample of samples || []) {
        values = { ...values, [sample.id]: mergeSample(values[sample.id], sample) }
        session.fetched[sample.id] = nowMs
      }
      session.values = values
      changed = true
    } catch (err) {
      session.lastError = message(err)
    }
  }
  if (changed) session.revision++
  if (!project.live || ctx.busy || session.busy || session.rendering) return
  const motion = hasMotion(project, ctx.kindOf || (() => 'image'))
  if (motion) session.gate.arm(nowMs)
  const item = ctx.item
  if (!item?.remoteName || item.draft || !ctx.connected || item.remoteName !== ctx.activeName) return
  const values = session.values
  const signature = textSignature(project, now, values, motion)
  const minGap = motion ? MOTION_MIN_MS : 0
  if (!session.gate.want(signature, nowMs, minGap) || !session.media) return
  session.gate.start(signature, motion ? nowMs : 0)
  const controller = new AbortController()
  session.abort = controller
  try {
    if (motion) {
      session.rendering = true
      session.renderLabel = 'Rendering the clip'
      session.renderPercent = 0
      await new Promise((resolve) => requestAnimationFrame(resolve))
      const exportId = await renderFrames({
        project,
        sources: session.media,
        fps: ctx.fps,
        api: ctx.api,
        signal: controller.signal,
        now,
        values,
        onProgress: (portion) => {
          session.renderPercent = portion * 100
        },
      })
      session.renderLabel = 'Encoding'
      await ctx.refreshClip({ projectId: item.id, exportId })
    } else {
      const jpeg = await renderStill(project, session.media, now, values)
      await ctx.refresh({ projectId: item.id, jpeg })
    }
    session.gate.done(true)
    session.lastError = ''
    await ctx.reload?.()
  } catch (err) {
    session.gate.done(false)
    if (err?.name !== 'AbortError') session.lastError = message(err)
  } finally {
    session.rendering = false
    session.abort = null
  }
}

function message(err) {
  if (!err) return 'Could not update the cooler'
  if (typeof err === 'string') return err
  return err.message || String(err)
}
