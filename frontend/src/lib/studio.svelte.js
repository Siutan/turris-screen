import { clipBounds } from './render.js'
import {
  DEFAULT_LIMITS,
  H,
  MIN_CLIP,
  W,
  clipEnd,
  findClip,
  effectClip,
  mediaClip,
  newProject,
  newTrack,
  projectDuration,
  textClip,
  uid,
  widgetClip,
} from './project.js'

const HISTORY = 120
const MERGE_MS = 800

// Studio is the editor state: the project, the playhead, selection, and undo history.
export class Studio {
  project = $state(newProject())
  projectId = $state('')
  time = $state(0)
  playing = $state(false)
  // selection lists the selected clip ids. The last one is the primary clip the inspector edits.
  selection = $state([])
  dirty = $state(false)
  canUndo = $state(false)
  canRedo = $state(false)
  limits = $state({ ...DEFAULT_LIMITS })
  pps = $state(60)
  // fitKey changes whenever a project is opened, so the timeline can fit itself once.
  fitKey = $state(0)

  duration = $derived(projectDuration(this.project))
  selected = $derived(findClip(this.project, this.selection.at(-1)))
  selectedClips = $derived(this.selection.map((id) => findClip(this.project, id)).filter(Boolean))

  get selectedId() {
    return this.selection.at(-1) ?? null
  }

  set selectedId(id) {
    this.selection = id ? [id] : []
  }

  #past = []
  #future = []
  #mergeKey = ''
  #mergeAt = 0

  constructor(sources) {
    this.sources = sources
  }

  load(project, id = '') {
    this.project = project
    this.projectId = id
    this.time = 0
    this.playing = false
    this.selectedId = null
    this.dirty = false
    this.#past = []
    this.#future = []
    this.#flags()
    this.fitKey++
  }

  serialize() {
    return JSON.stringify($state.snapshot(this.project))
  }

  // edit records an undo step and applies fn. Edits with the same mergeKey in quick
  // succession (a slider drag) share one step.
  edit(fn, mergeKey = '') {
    const now = Date.now()
    if (!mergeKey || mergeKey !== this.#mergeKey || now - this.#mergeAt > MERGE_MS) this.#push(this.serialize())
    this.#mergeKey = mergeKey
    this.#mergeAt = now
    fn(this.project)
    this.dirty = true
  }

  // remember records a state captured before a pointer gesture, if the gesture changed anything.
  remember(before) {
    if (before === this.serialize()) return
    this.#push(before)
    this.#mergeKey = ''
    this.dirty = true
  }

  undo() {
    if (!this.#past.length) return
    this.#future.push(this.serialize())
    this.#restore(this.#past.pop())
  }

  redo() {
    if (!this.#future.length) return
    this.#past.push(this.serialize())
    this.#restore(this.#future.pop())
  }

  #push(state) {
    this.#past.push(state)
    if (this.#past.length > HISTORY) this.#past.shift()
    this.#future = []
    this.#flags()
  }

  #restore(state) {
    this.project = JSON.parse(state)
    this.#mergeKey = ''
    this.#prune()
    this.time = Math.min(this.time, this.duration)
    this.dirty = true
    this.#flags()
  }

  #flags() {
    this.canUndo = this.#past.length > 0
    this.canRedo = this.#future.length > 0
  }

  select(id) {
    this.selectedId = id
  }

  isSelected(id) {
    return this.selection.includes(id)
  }

  // toggleSelect adds a clip to the selection, or takes it out if it is already in.
  toggleSelect(id) {
    if (this.selection.includes(id)) this.selection = this.selection.filter((s) => s !== id)
    else this.selection = [...this.selection, id]
  }

  selectMany(ids, additive = false) {
    const next = additive ? [...this.selection] : []
    for (const id of ids) if (!next.includes(id)) next.push(id)
    this.selection = next
  }

  selectAll() {
    this.selection = this.project.tracks.flatMap((t) => t.clips.map((c) => c.id))
  }

  #prune() {
    const kept = this.selection.filter((id) => findClip(this.project, id))
    if (kept.length !== this.selection.length) this.selection = kept
  }

  // targets is what an action applies to: the given clips, or else the selection.
  #targets(ids) {
    const list = ids === undefined ? this.selection : Array.isArray(ids) ? ids : [ids]
    return list.map((id) => findClip(this.project, id)).filter(Boolean)
  }

  seek(t) {
    this.time = Math.max(0, Math.min(t, Math.max(this.duration, 0)))
  }

  room(start) {
    return this.limits.maxSeconds - start
  }

  // naturalLength is how long a new clip of this asset should be.
  naturalLength(asset) {
    const entry = this.sources.get(asset.id)
    if (asset.kind === 'video' && entry?.duration) return entry.duration
    if (asset.kind === 'gif' && entry?.duration) {
      const d = entry.duration
      return d >= 3 ? d : Math.ceil(3 / d) * d
    }
    return 5
  }

  // addMedia puts an asset on the timeline. Without a track it goes at the end of the main track.
  addMedia(asset, at = {}) {
    let trackIndex = at.trackIndex
    let start = at.start
    if (trackIndex === undefined) {
      trackIndex = 0
      const main = this.project.tracks[0]
      start = main ? main.clips.reduce((end, c) => Math.max(end, clipEnd(c)), 0) : 0
    }
    start = Math.max(0, start ?? this.time)
    const room = this.room(start)
    if (room < MIN_CLIP) throw new Error(`Projects are limited to ${this.limits.maxSeconds} seconds.`)
    const clip = mediaClip(asset, round(start), round(Math.min(this.naturalLength(asset), room)))
    this.edit((p) => {
      if (!p.tracks[trackIndex]) {
        p.tracks.push(newTrack())
        trackIndex = p.tracks.length - 1
      }
      p.tracks[trackIndex].clips.push(clip)
    })
    this.#settle(clip.id)
    this.selectedId = clip.id
    this.time = clip.start
    return clip
  }

  // addOverlay puts an asset on a new top track at the playhead.
  addOverlay(asset) {
    const start = this.time >= this.duration ? 0 : this.time
    const room = this.room(start)
    if (room < MIN_CLIP) throw new Error(`Projects are limited to ${this.limits.maxSeconds} seconds.`)
    const span = this.duration > start ? this.duration - start : this.naturalLength(asset)
    const clip = mediaClip(asset, round(start), round(Math.min(span, this.naturalLength(asset), room)))
    clip.scale = 0.5
    this.edit((p) => {
      const track = newTrack()
      track.clips.push(clip)
      p.tracks.unshift(track)
    })
    this.selectedId = clip.id
    return clip
  }

  // addEffect puts a full-frame overlay on a new top track at the playhead.
  addEffect(effectId) {
    const start = this.time >= this.duration ? 0 : this.time
    const span = this.duration > start ? this.duration - start : 5
    const room = this.room(start)
    if (room < MIN_CLIP) throw new Error(`Projects are limited to ${this.limits.maxSeconds} seconds.`)
    const clip = effectClip(effectId, round(start), round(Math.min(Math.max(span, 2), room)))
    this.edit((p) => {
      const track = newTrack()
      track.clips.push(clip)
      p.tracks.unshift(track)
    })
    this.selectedId = clip.id
    return clip
  }

  addText(preset, at = null) {
    const start = this.time >= this.duration ? 0 : this.time
    const span = this.duration > start ? this.duration - start : 5
    const clip = textClip(preset, round(start), round(Math.min(Math.max(span, 2), this.room(start))))
    if (at) {
      clip.x = Math.round(at.x)
      clip.y = Math.round(at.y)
    }
    this.edit((p) => {
      const track = newTrack()
      track.clips.push(clip)
      p.tracks.unshift(track)
    })
    this.selectedId = clip.id
    return clip
  }

  hasWidget(kind) {
    return this.project.tracks.some((t) => t.clips.some((c) => c.type === kind))
  }

  toggleWidget(kind, on) {
    if (on === this.hasWidget(kind)) return
    if (on) {
      const clip = widgetClip(kind, 0, round(Math.min(Math.max(this.duration, 5), this.limits.maxSeconds)))
      this.edit((p) => {
        const track = newTrack()
        track.clips.push(clip)
        p.tracks.unshift(track)
      })
      this.selectedId = clip.id
      return
    }
    this.edit((p) => {
      for (const track of p.tracks) track.clips = track.clips.filter((c) => c.type !== kind)
      p.tracks = p.tracks.filter((t) => t.clips.length)
      if (!p.tracks.length) p.tracks.push(newTrack())
    })
    this.#prune()
  }

  removeClip(id) {
    this.removeClips(id)
  }

  // removeClips deletes clips. With ripple, later clips on the same track slide left
  // to close each gap.
  removeClips(ids, { ripple = false } = {}) {
    const found = this.#targets(ids)
    if (!found.length) return
    const gone = new Set(found.map((f) => f.clip.id))
    this.edit((p) => {
      for (const track of p.tracks) {
        const removed = track.clips.filter((c) => gone.has(c.id)).sort((a, b) => b.start - a.start)
        track.clips = track.clips.filter((c) => !gone.has(c.id))
        if (!ripple) continue
        for (const r of removed) {
          const end = clipEnd(r)
          for (const c of track.clips) if (c.start >= end - 1e-3) c.start = round(c.start - r.duration)
        }
      }
    })
    this.#prune()
    this.time = Math.min(this.time, this.duration)
  }

  // canSplit says whether the playhead is far enough inside the clip to cut it.
  canSplit(clip, t = this.time) {
    return t > clip.start + MIN_CLIP / 2 && t < clipEnd(clip) - MIN_CLIP / 2
  }

  // split cuts clips at the playhead: the given ones, the selection, or with nothing
  // selected every clip under the playhead. Right halves keep playing from the same
  // source time and become the selection.
  split(ids) {
    const t = this.time
    let found = this.#targets(ids)
    if (ids === undefined && !found.length) {
      found = this.project.tracks.flatMap((track) => track.clips.map((clip) => findClip(this.project, clip.id)))
    }
    found = found.filter((f) => this.canSplit(f.clip, t))
    if (!found.length) return false
    const rights = []
    this.edit(() => {
      for (const { clip } of found) {
        const right = { ...$state.snapshot(clip), id: uid('c'), start: round(t), duration: round(clipEnd(clip) - t) }
        if (clip.type === 'media') right.in = round((clip.in || 0) + (t - clip.start))
        clip.duration = round(t - clip.start)
        findClip(this.project, clip.id).track.clips.push(right)
        rights.push(right.id)
      }
    })
    this.selection = rights
    return true
  }

  // duplicate copies clips to just after the last of them, keeping their spacing and tracks.
  duplicate(ids) {
    const found = this.#targets(ids)
    if (!found.length) return
    const from = Math.min(...found.map((f) => f.clip.start))
    const end = Math.max(...found.map((f) => clipEnd(f.clip)))
    if (end + MIN_CLIP > this.limits.maxSeconds) throw new Error(`Projects are limited to ${this.limits.maxSeconds} seconds.`)
    const copies = found.map(({ clip, trackIndex }) => {
      const copy = { ...$state.snapshot(clip), id: uid('c'), start: round(end + clip.start - from) }
      copy.duration = round(Math.min(copy.duration, this.limits.maxSeconds - copy.start))
      return { copy, trackIndex }
    }).filter(({ copy }) => copy.duration >= MIN_CLIP)
    this.edit((p) => {
      for (const { copy, trackIndex } of copies) p.tracks[trackIndex].clips.push(copy)
      for (const { copy } of copies) this.#settle(copy.id)
    })
    this.selection = copies.map(({ copy }) => copy.id)
  }

  // canMerge says whether the clips can become one clip, and if not, why.
  canMerge(ids) {
    const found = this.#targets(ids)
    if (found.length < 2) return { ok: false, reason: 'Select two or more clips on one track.' }
    if (new Set(found.map((f) => f.trackIndex)).size > 1) return { ok: false, reason: 'Clips have to be on the same track to merge.' }
    const sorted = found.map((f) => f.clip).sort((a, b) => a.start - b.start)
    const first = sorted[0]
    const same = sorted.every((c) => c.type === first.type && (c.type !== 'media' || c.assetId === first.assetId) && (c.type !== 'text' || c.text === first.text) && (c.type !== 'effect' || c.effect === first.effect))
    if (!same) return { ok: false, reason: 'Only pieces of the same picture, video, GIF, text, or effect can merge.' }
    const end = clipEnd(sorted.at(-1))
    const members = new Set(sorted.map((c) => c.id))
    const blocked = found[0].track.clips.some((c) => !members.has(c.id) && c.start < end - 1e-3 && clipEnd(c) > first.start + 1e-3)
    if (blocked) return { ok: false, reason: 'Another clip is in the way.' }
    if (end - first.start > this.trimBounds(first).maxEnd - first.start + 1e-3) {
      return { ok: false, reason: 'The video is not long enough to cover the gap.' }
    }
    return { ok: true, reason: '' }
  }

  // merge joins clips on one track into the first of them, filling any gaps between.
  merge(ids) {
    const check = this.canMerge(ids)
    if (!check.ok) throw new Error(check.reason)
    const sorted = this.#targets(ids).map((f) => f.clip).sort((a, b) => a.start - b.start)
    const keep = sorted[0]
    const end = clipEnd(sorted.at(-1))
    const gone = new Set(sorted.slice(1).map((c) => c.id))
    this.edit((p) => {
      keep.duration = round(end - keep.start)
      for (const track of p.tracks) track.clips = track.clips.filter((c) => !gone.has(c.id))
    })
    this.selection = [keep.id]
  }

  // copy describes clips for the clipboard: their settings, start offsets, and tracks.
  copy(ids) {
    const found = this.#targets(ids)
    if (!found.length) return null
    const from = Math.min(...found.map((f) => f.clip.start))
    const lowest = Math.min(...found.map((f) => f.trackIndex))
    return {
      version: 1,
      clips: found.map(({ clip, trackIndex }) => ({ ...$state.snapshot(clip), start: round(clip.start - from), track: trackIndex - lowest, base: trackIndex })),
    }
  }

  // paste puts copied clips at the playhead, on the tracks they came from.
  paste(data) {
    const clips = Array.isArray(data?.clips) ? data.clips.filter((c) => c && typeof c.type === 'string' && c.duration > 0) : []
    if (!clips.length) return false
    const at = this.time >= this.duration ? this.duration : this.time
    const span = Math.max(...clips.map((c) => c.start + c.duration))
    if (at + MIN_CLIP > this.limits.maxSeconds) throw new Error(`Projects are limited to ${this.limits.maxSeconds} seconds.`)
    const base = Math.min(...clips.map((c) => (Number.isInteger(c.base) ? c.base : 0)))
    const placed = []
    this.edit((p) => {
      for (const c of clips) {
        const { track, base: _base, ...rest } = c
        const clip = { ...rest, id: uid('c'), start: round(at + c.start), locked: false }
        clip.duration = round(Math.min(clip.duration, this.limits.maxSeconds - clip.start))
        if (clip.duration < MIN_CLIP) continue
        const index = Math.max(0, base + (Number.isInteger(track) ? track : 0))
        while (p.tracks.length <= index) p.tracks.push(newTrack())
        p.tracks[index].clips.push(clip)
        placed.push(clip.id)
      }
      for (const id of placed) this.#settle(id)
    })
    this.selection = placed
    if (span > 0) this.time = at
    return placed.length > 0
  }

  addTrack() {
    this.edit((p) => {
      p.tracks.push(newTrack())
    })
  }

  removeTrack(index) {
    if (this.project.tracks.length <= 1) return
    this.edit((p) => {
      p.tracks.splice(index, 1)
    })
    this.#prune()
  }

  // reorderTrack moves a whole track. Dragging calls this directly and records one undo step.
  reorderTrack(from, to) {
    if (from === to || from < 0 || to < 0 || to >= this.project.tracks.length) return
    const [track] = this.project.tracks.splice(from, 1)
    this.project.tracks.splice(to, 0, track)
  }

  toggleTrack(index) {
    this.edit((p) => {
      p.tracks[index].hidden = !p.tracks[index].hidden
    })
  }

  // moveClip places a clip on a track at a start time. Used while dragging; call settle on drop.
  moveClip(id, trackIndex, start) {
    const found = findClip(this.project, id)
    if (!found) return
    const { clip } = found
    clip.start = round(Math.max(0, Math.min(start, this.limits.maxSeconds - clip.duration)))
    if (trackIndex !== found.trackIndex && this.project.tracks[trackIndex]) {
      found.track.clips.splice(found.index, 1)
      this.project.tracks[trackIndex].clips.push(clip)
    }
  }

  // trimBounds says how far a clip's edges can move given its source.
  trimBounds(clip) {
    const entry = clip.type === 'media' ? this.sources.get(clip.assetId) : null
    const max = this.limits.maxSeconds
    if (entry?.kind === 'video') {
      return { minStart: Math.max(0, clip.start - (clip.in || 0)), maxEnd: Math.min(max, clip.start + entry.duration - (clip.in || 0)) }
    }
    return { minStart: 0, maxEnd: max }
  }

  // sourceRange describes which part of a video clip's source plays: from and to are
  // seconds into the source, and longest is how much the clip can grow to without
  // passing the project limit or the next clip on its track.
  sourceRange(id) {
    const found = findClip(this.project, id)
    if (!found || found.clip.type !== 'media') return null
    const entry = this.sources.get(found.clip.assetId)
    if (entry?.kind !== 'video' || !entry.duration) return null
    const { clip, track } = found
    const next = track.clips.reduce((min, c) => (c.id !== clip.id && c.start >= clip.start ? Math.min(min, c.start) : min), Infinity)
    const from = clip.in || 0
    return {
      from,
      to: from + clip.duration,
      total: entry.duration,
      longest: Math.max(MIN_CLIP, Math.min(this.limits.maxSeconds, next) - clip.start),
    }
  }

  // setSourceRange picks the part of the source video a clip plays. The clip keeps its
  // place on the timeline; its length follows the range, within sourceRange's limits.
  setSourceRange(id, from, to, mergeKey = '') {
    const range = this.sourceRange(id)
    if (!range) return
    let length = Math.max(MIN_CLIP, Math.min(to - from, range.longest))
    from = Math.max(0, Math.min(from, range.total - MIN_CLIP))
    length = Math.min(length, range.total - from)
    if (Math.abs(from - range.from) < 1e-4 && Math.abs(from + length - range.to) < 1e-4) return
    const { clip } = findClip(this.project, id)
    this.edit(() => {
      clip.in = round(from)
      clip.duration = round(length)
    }, mergeKey)
  }

  // arrangeTarget finds the track a clip is stacked against when moving it in the given
  // direction ('front', 'forward', 'backward', 'back'). Only clips that share screen time count.
  // The top timeline row is in front, so lower indexes sit above higher ones.
  arrangeTarget(id, where) {
    const found = findClip(this.project, id)
    if (!found) return null
    const { clip, trackIndex } = found
    const stacked = []
    this.project.tracks.forEach((track, j) => {
      if (j !== trackIndex && collides(track, clip)) stacked.push(j)
    })
    const above = stacked.filter((j) => j < trackIndex)
    const below = stacked.filter((j) => j > trackIndex)
    if (where === 'front' && above.length) return { anchor: Math.min(...above), dir: -1 }
    if (where === 'forward' && above.length) return { anchor: Math.max(...above), dir: -1 }
    if (where === 'backward' && below.length) return { anchor: Math.min(...below), dir: 1 }
    if (where === 'back' && below.length) return { anchor: Math.max(...below), dir: 1 }
    return null
  }

  canArrange(ids, where) {
    return this.#targets(ids).some((f) => this.arrangeTarget(f.clip.id, where))
  }

  // arrange restacks clips by moving each to the track just past its anchor, adding one
  // if needed. Clips nearest the destination go first so a group keeps its order.
  arrange(ids, where) {
    const towardFront = where === 'front' || where === 'forward'
    const order = this.#targets(ids)
      .sort((a, b) => (towardFront ? a.trackIndex - b.trackIndex : b.trackIndex - a.trackIndex))
      .map((f) => f.clip.id)
    if (!order.some((id) => this.arrangeTarget(id, where))) return false
    this.edit((p) => {
      for (const id of order) {
        const target = this.arrangeTarget(id, where)
        if (!target) continue
        const { clip, track, index } = findClip(p, id)
        track.clips.splice(index, 1)
        const neighbour = p.tracks[target.anchor + target.dir]
        if (neighbour && !collides(neighbour, clip)) {
          neighbour.clips.push(clip)
        } else {
          const fresh = newTrack()
          fresh.clips.push(clip)
          p.tracks.splice(target.dir > 0 ? target.anchor + 1 : target.anchor, 0, fresh)
        }
        if (!track.clips.length && p.tracks.length > 1) p.tracks.splice(p.tracks.indexOf(track), 1)
      }
    })
    return true
  }

  // toggleLock locks every clip if any is unlocked, otherwise unlocks them all.
  toggleLock(ids) {
    const found = this.#targets(ids)
    if (!found.length) return
    const lock = found.some((f) => !f.clip.locked)
    this.edit(() => {
      for (const f of found) f.clip.locked = lock
    })
  }

  // moveGroup shifts dragged clips together. Each entry has the id and where the clip
  // started (start0, track0); dt and dTrack are already clamped by the caller.
  moveGroup(group, dt, dTrack) {
    for (const g of group) this.moveClip(g.id, g.track0 + dTrack, g.start0 + dt)
  }

  // groupFits says whether every clip in the group would land on a track without
  // overlapping a clip outside the group.
  groupFits(group, dt, dTrack) {
    const members = new Set(group.map((g) => g.id))
    return group.every((g) => {
      const track = this.project.tracks[g.track0 + dTrack]
      if (!track) return false
      const start = g.start0 + dt
      const end = start + g.duration
      return !track.clips.some((c) => !members.has(c.id) && c.start < end - 1e-3 && clipEnd(c) > start + 1e-3)
    })
  }

  // nudge moves unlocked clips on the screen by dx, dy pixels.
  nudge(ids, dx, dy, mergeKey = '') {
    const found = this.#targets(ids).filter((f) => !f.clip.locked)
    if (!found.length) return
    this.edit(() => {
      for (const { clip } of found) {
        clip.x = Math.round((clip.x || 0) + dx)
        clip.y = Math.round((clip.y || 0) + dy)
      }
    }, mergeKey)
  }

  lockedCount() {
    return this.project.tracks.reduce((n, t) => n + t.clips.filter((c) => c.locked).length, 0)
  }

  unlockAll() {
    if (!this.lockedCount()) return
    this.edit((p) => {
      for (const track of p.tracks) for (const c of track.clips) c.locked = false
    })
  }

  // center moves clips so the box around all of them sits in the middle of the screen.
  center(ids) {
    const found = this.#targets(ids).filter((f) => !f.clip.locked)
    if (!found.length) return
    const boxes = found.map((f) => clipBounds(f.clip, this.sources))
    const left = Math.min(...boxes.map((b) => b.x))
    const top = Math.min(...boxes.map((b) => b.y))
    const right = Math.max(...boxes.map((b) => b.x + b.w))
    const bottom = Math.max(...boxes.map((b) => b.y + b.h))
    const dx = Math.round(W / 2 - (left + right) / 2)
    const dy = Math.round(H / 2 - (top + bottom) / 2)
    if (!dx && !dy) return
    this.nudge(found.map((f) => f.clip.id), dx, dy)
  }

  settle(id) {
    this.#settle(id)
  }

  // #settle moves a clip that overlaps another on its track onto the track just above it,
  // so the clip stays in front. A new row is inserted when the row above is full.
  #settle(id) {
    const found = findClip(this.project, id)
    if (!found) return
    const { clip, track, trackIndex, index } = found
    if (!collides(track, clip)) return
    track.clips.splice(index, 1)
    const above = trackIndex > 0 ? this.project.tracks[trackIndex - 1] : null
    if (above && !collides(above, clip)) {
      above.clips.push(clip)
      return
    }
    const track2 = newTrack()
    track2.clips.push(clip)
    this.project.tracks.splice(trackIndex, 0, track2)
  }
}

// collides says whether any other clip on the track shares time with clip.
function collides(track, clip) {
  return track.clips.some((c) => c.id !== clip.id && c.start < clipEnd(clip) - 1e-3 && clipEnd(c) > clip.start + 1e-3)
}

export function round(n) {
  return Math.round(n * 1000) / 1000
}
