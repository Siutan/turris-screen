// Loaded media the renderer draws from. One entry per bin asset, plus one <video>
// element per timeline clip so two clips of the same video can show different frames.

import { filmstripKey, filmstripTimes } from './filmstrip.js'

const POSTER_W = 320
const POSTER_H = 180
const FRAME_W = 96
const FRAME_H = 54
const MAX_GIF_FRAMES = 900

export class Sources {
  constructor(onChange) {
    this.entries = new Map()
    this.videos = new Map()
    this.strips = new Map()
    this.stripJobs = new Map()
    this.frameCache = new Map()
    this.thumbs = new Map()
    this.thumbChain = new Map()
    this.onChange = onChange || (() => {})
  }

  get(assetId) {
    const entry = this.entries.get(assetId)
    return entry?.status === 'ready' ? entry : null
  }

  status(assetId) {
    return this.entries.get(assetId)?.status || 'missing'
  }

  load(asset) {
    const existing = this.entries.get(asset.id)
    if (existing) return existing.ready
    const entry = { asset, kind: asset.kind, status: 'loading', width: 0, height: 0, duration: 0, poster: '' }
    this.entries.set(asset.id, entry)
    entry.ready = loadEntry(entry)
      .then(() => {
        entry.status = 'ready'
      })
      .catch((err) => {
        entry.status = 'error'
        entry.error = err?.message || String(err)
      })
      .finally(() => this.onChange())
    return entry.ready
  }

  forget(assetId) {
    const entry = this.entries.get(assetId)
    if (!entry) return
    entry.frames?.forEach((f) => f.bitmap.close?.())
    this.entries.delete(assetId)
    const thumb = this.thumbs.get(assetId)
    if (thumb) {
      thumb.pause()
      thumb.removeAttribute('src')
      thumb.load()
      this.thumbs.delete(assetId)
    }
    for (const key of this.frameCache.keys()) {
      if (!key.startsWith(`${assetId}@`)) continue
      this.frameCache.get(key)?.close?.()
      this.frameCache.delete(key)
    }
    for (const key of this.strips.keys()) if (key.startsWith(`${assetId}:`)) this.strips.delete(key)
  }

  // filmstrip is a horizontal strip of the frames between from and to.
  filmstrip(assetId, from, to, slots) {
    const entry = this.get(assetId)
    if (!entry?.duration) return Promise.resolve('')
    const key = filmstripKey(assetId, from, to, slots)
    const cached = this.strips.get(key)
    if (cached) return Promise.resolve(cached)
    const pending = this.stripJobs.get(key)
    if (pending) return pending
    const build = entry.kind === 'gif'
      ? Promise.resolve(this.#gifStrip(entry, from, to, slots))
      : entry.kind === 'video'
        ? this.#enqueue(assetId, () => this.#videoStrip(entry, from, to, slots))
        : Promise.resolve('')
    const job = build
      .then((url) => {
        if (url) this.strips.set(key, url)
        return url || ''
      })
      .catch(() => '')
      .finally(() => {
        if (this.stripJobs.get(key) === job) this.stripJobs.delete(key)
      })
    this.stripJobs.set(key, job)
    return job
  }

  #enqueue(assetId, task) {
    const prev = this.thumbChain.get(assetId) || Promise.resolve()
    const next = prev.catch(() => {}).then(task)
    this.thumbChain.set(assetId, next)
    return next
  }

  async #thumbVideo(entry) {
    let el = this.thumbs.get(entry.asset.id)
    if (!el) {
      el = makeVideo(entry.asset.url)
      this.thumbs.set(entry.asset.id, el)
    }
    if (el.readyState < 1) await waitFor(el, 'loadedmetadata')
    return el
  }

  async #frameAt(entry, el, time) {
    const t = Math.min(Math.max(0, time), Math.max(0, entry.duration - 0.05))
    const key = `${entry.asset.id}@${t.toFixed(2)}`
    const hit = this.frameCache.get(key)
    if (hit) return hit
    await seekTo(el, t)
    const slot = document.createElement('canvas')
    slot.width = FRAME_W
    slot.height = FRAME_H
    drawCover(slot.getContext('2d'), el, el.videoWidth, el.videoHeight, 0, 0, FRAME_W, FRAME_H)
    const bmp = await createImageBitmap(slot)
    this.frameCache.set(key, bmp)
    return bmp
  }

  async #videoStrip(entry, from, to, slots) {
    const el = await this.#thumbVideo(entry)
    const times = filmstripTimes(from, to, slots).map((t) => Math.min(t, Math.max(0, entry.duration - 0.05)))
    const canvas = document.createElement('canvas')
    canvas.width = FRAME_W * times.length
    canvas.height = FRAME_H
    const ctx = canvas.getContext('2d')
    let drawn = 0
    for (let i = 0; i < times.length; i++) {
      try {
        const bmp = await this.#frameAt(entry, el, times[i])
        if (!bmp) continue
        ctx.drawImage(bmp, i * FRAME_W, 0, FRAME_W, FRAME_H)
        drawn++
      } catch {
        /* skip a frame the decoder could not show */
      }
    }
    return drawn ? canvas.toDataURL('image/jpeg', 0.72) : ''
  }

  #gifStrip(entry, from, to, slots) {
    if (!entry.frames?.length) return ''
    const times = filmstripTimes(from, to, slots)
    const canvas = document.createElement('canvas')
    canvas.width = FRAME_W * times.length
    canvas.height = FRAME_H
    const ctx = canvas.getContext('2d')
    times.forEach((t, i) => {
      const local = entry.duration > 0 ? t % entry.duration : 0
      const bmp = gifFrameAt(entry, local)
      if (bmp) drawCover(ctx, bmp, bmp.width, bmp.height, i * FRAME_W, 0, FRAME_W, FRAME_H)
    })
    return canvas.toDataURL('image/jpeg', 0.72)
  }

  // video returns the element that plays this clip, creating it on first use.
  video(clip) {
    let el = this.videos.get(clip.id)
    const entry = this.get(clip.assetId)
    if (!entry || entry.kind !== 'video') return null
    if (!el || el.dataset.asset !== clip.assetId) {
      el?.pause()
      el = makeVideo(entry.asset.url)
      el.dataset.asset = clip.assetId
      el.addEventListener('loadeddata', () => this.onChange())
      this.videos.set(clip.id, el)
    }
    return el
  }

  // prune drops video elements for clips that are gone.
  prune(clipIds) {
    for (const [id, el] of this.videos) {
      if (clipIds.has(id)) continue
      el.pause()
      el.removeAttribute('src')
      el.load()
      this.videos.delete(id)
    }
  }

  // sourceTime is where in the asset a clip is at project time t.
  sourceTime(clip, t) {
    const entry = this.get(clip.assetId)
    const local = Math.max(0, t - clip.start) + (clip.in || 0)
    if (!entry) return local
    if (entry.kind === 'video') return Math.min(local, Math.max(0, entry.duration - 0.02))
    if (entry.kind === 'gif' && entry.duration > 0) return local % entry.duration
    return local
  }

  // frame is the drawable for a media clip at t, or null if it is not ready.
  frame(clip, t) {
    const entry = this.get(clip.assetId)
    if (!entry) return null
    if (entry.kind === 'video') {
      const el = this.video(clip)
      return el && el.readyState >= 2 ? el : null
    }
    if (entry.kind === 'gif' && entry.frames?.length) {
      const local = this.sourceTime(clip, t)
      const frames = entry.frames
      let lo = 0
      let hi = frames.length - 1
      while (lo < hi) {
        const mid = (lo + hi + 1) >> 1
        if (frames[mid].at <= local) lo = mid
        else hi = mid - 1
      }
      return frames[lo].bitmap
    }
    return entry.image
  }

  moving(clip) {
    if (clip.type !== 'media') return false
    const entry = this.get(clip.assetId)
    return entry?.kind === 'video' || (entry?.kind === 'gif' && entry.frames?.length > 1)
  }

  // sync keeps preview videos in step with the playhead. While paused it seeks and
  // redraws when the frame lands; while playing it lets the element run and corrects drift.
  sync(active, t, playing) {
    const live = new Set()
    for (const { clip } of active) {
      const el = this.video(clip)
      if (!el) continue
      live.add(clip.id)
      const target = this.sourceTime(clip, t)
      if (playing) {
        if (Math.abs(el.currentTime - target) > 0.25) el.currentTime = target
        if (el.paused) el.play().catch(() => {})
      } else {
        if (!el.paused) el.pause()
        if (Math.abs(el.currentTime - target) > 0.01) {
          el.currentTime = target
          el.addEventListener('seeked', () => this.onChange(), { once: true })
        }
      }
    }
    for (const [id, el] of this.videos) {
      if (!live.has(id) && !el.paused) el.pause()
    }
  }

  // prepare seeks every active video to its exact frame. Export uses this before each draw.
  async prepare(active, t) {
    const waits = []
    for (const { clip } of active) {
      const el = this.video(clip)
      if (!el) continue
      if (!el.paused) el.pause()
      waits.push(seekTo(el, this.sourceTime(clip, t)))
    }
    await Promise.all(waits)
  }

  pauseAll() {
    for (const el of this.videos.values()) el.pause()
  }
}

function makeVideo(url) {
  const el = document.createElement('video')
  el.muted = true
  el.playsInline = true
  el.preload = 'auto'
  el.crossOrigin = 'anonymous'
  el.src = url
  return el
}

export function seekTo(el, time) {
  return new Promise((resolve) => {
    const ready = () => el.readyState >= 2
    if (Math.abs(el.currentTime - time) < 0.0005 && ready() && !el.seeking) {
      resolve()
      return
    }
    let timer
    const done = () => {
      clearTimeout(timer)
      el.removeEventListener('seeked', done)
      if (ready()) resolve()
      else el.addEventListener('loadeddata', resolve, { once: true })
    }
    timer = setTimeout(done, 4000)
    el.addEventListener('seeked', done)
    el.currentTime = time
  })
}

function waitFor(el, event) {
  return new Promise((resolve, reject) => {
    el.addEventListener(event, resolve, { once: true })
    el.addEventListener('error', () => reject(new Error('This file could not be opened')), { once: true })
  })
}

async function loadEntry(entry) {
  const { asset } = entry
  if (asset.kind === 'video') {
    const el = makeVideo(asset.url)
    await waitFor(el, 'loadedmetadata')
    entry.width = el.videoWidth
    entry.height = el.videoHeight
    entry.duration = el.duration
    await seekTo(el, Math.min(0.5, el.duration / 2))
    entry.poster = poster(el, entry.width, entry.height)
    el.removeAttribute('src')
    el.load()
    return
  }
  if (asset.kind === 'gif' && 'ImageDecoder' in window) {
    try {
      await loadGif(entry)
      return
    } catch {
      /* fall back to a still of the first frame */
    }
  }
  const img = new Image()
  img.crossOrigin = 'anonymous'
  img.src = asset.url
  await img.decode()
  entry.image = img
  entry.width = img.naturalWidth
  entry.height = img.naturalHeight
  entry.kind = 'image'
  entry.poster = poster(img, entry.width, entry.height)
}

async function loadGif(entry) {
  const res = await fetch(entry.asset.url)
  const data = await res.arrayBuffer()
  const decoder = new window.ImageDecoder({ data, type: 'image/gif' })
  await decoder.tracks.ready
  const count = Math.min(decoder.tracks.selectedTrack.frameCount, MAX_GIF_FRAMES)
  const frames = []
  let at = 0
  for (let i = 0; i < count; i++) {
    const { image } = await decoder.decode({ frameIndex: i })
    const bitmap = await createImageBitmap(image)
    const seconds = image.duration ? image.duration / 1e6 : 0.1
    frames.push({ bitmap, at })
    at += seconds > 0.01 ? seconds : 0.1
    if (i === 0) {
      entry.width = image.displayWidth
      entry.height = image.displayHeight
    }
    image.close()
  }
  decoder.close()
  entry.frames = frames
  entry.duration = at
  entry.poster = poster(frames[0].bitmap, entry.width, entry.height)
}

function gifFrameAt(entry, local) {
  const frames = entry.frames || []
  if (!frames.length) return null
  let lo = 0
  let hi = frames.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (frames[mid].at <= local) lo = mid
    else hi = mid - 1
  }
  return frames[lo].bitmap
}

function drawCover(ctx, source, sw, sh, dx, dy, dw, dh) {
  if (!ctx || !source || !sw || !sh) return
  const scale = Math.max(dw / sw, dh / sh)
  const w = sw * scale
  const h = sh * scale
  ctx.save()
  ctx.beginPath()
  ctx.rect(dx, dy, dw, dh)
  ctx.clip()
  ctx.drawImage(source, dx + (dw - w) / 2, dy + (dh - h) / 2, w, h)
  ctx.restore()
}

function poster(source, w, h) {
  const canvas = document.createElement('canvas')
  canvas.width = POSTER_W
  canvas.height = POSTER_H
  const ctx = canvas.getContext('2d')
  const s = Math.max(POSTER_W / w, POSTER_H / h)
  ctx.drawImage(source, (POSTER_W - w * s) / 2, (POSTER_H - h * s) / 2, w * s, h * s)
  return canvas.toDataURL('image/jpeg', 0.8)
}
