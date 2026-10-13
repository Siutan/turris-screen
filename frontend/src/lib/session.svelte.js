import { hasMotion } from './project.js'
import { renderFrames, renderStill } from './exporter.js'
import { labelGroups, labelLayoutKey, labelTextKey, labelTexts } from './labels.js'
import { textLayout } from './render.js'
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
  lastSecond = -1
  labelFor = ''
  labelLayout = ''
  labelText = ''
  labelAt = 0
  labelsOn = false
  gate = createSendGate()
  abort = null

  follow(project, id, media) {
    if ((id || '') !== this.projectId) this.resetLabels()
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
    if ((id || '') !== this.projectId) this.resetLabels()
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
    this.resetLabels()
    this.rendering = false
  }

  resetLabels() {
    this.gate = createSendGate()
    this.labelFor = ''
    this.labelLayout = ''
    this.labelText = ''
    this.labelAt = 0
    this.labelsOn = false
  }

  // forgetLabels makes the next tick send the layout again. A show clears the layer.
  forgetLabels() {
    this.labelLayout = ''
    this.labelAt = 0
    this.labelsOn = false
  }
}

export const session = new LiveSession()

export { ageLabel }

const LABEL_REFRESH_MS = 20000

function measureLabel(clip, now, sources, values) {
  return textLayout(clip, now, undefined, { sources, values })
}

function onScreen(ctx) {
  const item = ctx?.item
  return !!(item?.remoteName && !item.draft && ctx.connected && item.remoteName === ctx.activeName)
}

// syncLabels installs the text layer, then pushes new strings when a value changes.
// It returns true when this project has live text, so the picture is not encoded again.
export async function syncLabels(ctx, project = session.project, now = new Date(), force = false) {
  if (!project || !ctx?.setLabels || !onScreen(ctx)) return false
  const groups = labelGroups(project, now, session.values, measureLabel)
  const texts = labelTexts(groups)
  const layout = labelLayoutKey(groups)
  const text = labelTextKey(texts)
  const mine = !session.projectId || session.projectId === ctx.item.id
  const nowMs = now.getTime()
  if (!groups.length) {
    if (mine && session.labelsOn && session.labelFor === ctx.item.remoteName) {
      await ctx.setLabels([])
      session.labelsOn = false
      session.labelLayout = ''
      session.labelText = ''
      session.labelFor = ctx.item.remoteName
    }
    return false
  }
  const stale = force || !mine || session.labelFor !== ctx.item.remoteName || nowMs - session.labelAt >= LABEL_REFRESH_MS
  if (stale || layout !== session.labelLayout) {
    await ctx.setLabels(groups)
    if (mine) {
      session.labelFor = ctx.item.remoteName
      session.labelLayout = layout
      session.labelText = text
      session.labelAt = nowMs
      session.labelsOn = true
    }
    return true
  }
  if (text !== session.labelText && ctx.updateLabels) {
    await ctx.updateLabels(texts)
    if (mine) session.labelText = text
  }
  return true
}

// runLiveTick samples sources that are due. Clock, date, and {{fields}} go out as
// labels in a face the cooler already has. A project with no labels still re-encodes
// a still when Keep live is on and the baked text changes.
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
  const second = Math.floor(nowMs / 1000)
  if (second !== session.lastSecond) {
    session.lastSecond = second
    changed = true
  }
  const localKind = new Set(['countdown', 'zone', 'focus'])
  if ((project.sources || []).some((source) => source.kind === 'focus' && source.config?.running)) changed = true
  const due = sourcesDue(project.sources || [], session.fetched, nowMs, session.values).filter((source) => !localKind.has(source.kind))
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
  if (ctx.busy || session.busy || session.rendering) return
  const watching = session.attached || project.live
  if (watching && labelGroups(project, now, session.values).length) {
    if (onScreen(ctx) && ctx.setLabels) {
      try {
        await syncLabels(ctx, project, now)
        session.lastError = ''
      } catch (err) {
        session.lastError = message(err)
      }
    }
    return
  }
  if (!project.live) return
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
        omitLabels: true,
        onProgress: (portion) => {
          session.renderPercent = portion * 100
        },
      })
      session.renderLabel = 'Encoding'
      await ctx.refreshClip({ projectId: item.id, exportId })
    } else {
      const jpeg = await renderStill(project, session.media, now, values, true)
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
