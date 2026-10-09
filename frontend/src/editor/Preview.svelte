<script>
  import { onMount, untrack } from 'svelte'
  import { H, W, activeClips, clipLabel, fmtBytes, fmtClock, isTextual } from '../lib/project.js'
  import { clipBounds, drawFrame, hitTest } from '../lib/render.js'
  import { canvasMenu, clipMenu } from '../lib/menus.js'
  import ContextMenu from './ContextMenu.svelte'

  let { studio, sources, revision, estimate, suspended = false, loading = false, clipboard, onError } = $props()

  let menu = $state(null)

  let canvas = $state(null)
  let stage = $state(null)
  let wrap = $state(null)
  let stageW = $state(0)
  let boxes = $state([])
  let full = $state(false)
  let raf = 0
  let loop = 0
  let ctx

  let scale = $derived(stageW / W || 0)

  // While an export renders it owns the video elements, so the preview stops seeking them.
  function schedule() {
    if (raf || !ctx || suspended) return
    raf = requestAnimationFrame(() => {
      raf = 0
      paint()
    })
  }

  function paint() {
    if (!ctx || suspended) return
    const active = activeClips(studio.project, studio.time)
    sources.sync(active, studio.time, studio.playing)
    drawFrame(ctx, studio.project, sources, studio.time)
    const primary = studio.selectedId
    boxes = active
      .filter((a) => studio.isSelected(a.clip.id))
      .map((a) => ({ ...clipBounds(a.clip, sources), id: a.clip.id, text: isTextual(a.clip), locked: !!a.clip.locked, primary: a.clip.id === primary }))
  }

  $effect(() => {
    $state.snapshot(studio.project)
    studio.time
    studio.selection
    revision
    if (!studio.playing && !suspended) schedule()
  })

  $effect(() => {
    if (!studio.playing) return
    let startWall = performance.now()
    let startT = untrack(() => (studio.time >= studio.duration - 0.02 ? 0 : studio.time))
    const tick = (now) => {
      const d = studio.duration
      if (d <= 0) {
        studio.playing = false
        return
      }
      let t = startT + (now - startWall) / 1000
      if (t >= d) {
        // The cooler loops the clip, so the preview does too.
        startT = 0
        startWall = now
        t = 0
      }
      studio.time = t
      paint()
      loop = requestAnimationFrame(tick)
    }
    loop = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(loop)
      sources.pauseAll()
    }
  })

  function toCanvas(event) {
    const r = stage.getBoundingClientRect()
    return { x: ((event.clientX - r.left) / r.width) * W, y: ((event.clientY - r.top) / r.height) * H }
  }

  function pointerDown(event) {
    if (event.button !== 0) return
    const p = toCanvas(event)
    const hit = hitTest(studio.project, sources, studio.time, p.x, p.y)
    const additive = event.shiftKey || event.ctrlKey || event.metaKey
    if (!hit) {
      if (!additive) studio.select(null)
      return
    }
    const wasSelected = studio.isSelected(hit.id)
    if (additive && !wasSelected) studio.toggleSelect(hit.id)
    else if (!wasSelected) studio.select(hit.id)
    else if (!additive) studio.selection = [...studio.selection.filter((id) => id !== hit.id), hit.id]
    const group = studio.selectedClips.filter((f) => !f.clip.locked).map((f) => ({ clip: f.clip, x0: f.clip.x || 0, y0: f.clip.y || 0 }))
    const before = studio.serialize()
    const sx = event.clientX
    const sy = event.clientY
    const per = W / stage.getBoundingClientRect().width
    let moved = false
    stage.setPointerCapture(event.pointerId)
    const move = (e) => {
      if (!moved && Math.abs(e.clientX - sx) < 2 && Math.abs(e.clientY - sy) < 2) return
      moved = true
      for (const g of group) {
        g.clip.x = Math.round(g.x0 + (e.clientX - sx) * per)
        g.clip.y = Math.round(g.y0 + (e.clientY - sy) * per)
      }
    }
    const up = () => {
      stage.removeEventListener('pointermove', move)
      stage.removeEventListener('pointerup', up)
      stage.removeEventListener('pointercancel', up)
      if (!moved) {
        if (additive && wasSelected) studio.toggleSelect(hit.id)
        else if (!additive) studio.select(hit.id)
        return
      }
      studio.remember(before)
    }
    stage.addEventListener('pointermove', move)
    stage.addEventListener('pointerup', up)
    stage.addEventListener('pointercancel', up)
  }

  function onContext(event) {
    event.preventDefault()
    const p = toCanvas(event)
    const hit = hitTest(studio.project, sources, studio.time, p.x, p.y, { locked: true })
    if (hit) {
      if (!studio.isSelected(hit.id)) studio.select(hit.id)
      const count = studio.selection.length
      menu = { x: event.clientX, y: event.clientY, title: count > 1 ? `${count} clips` : clipLabel(hit), items: clipMenu(studio, clipboard, onError) }
    } else {
      studio.select(null)
      menu = { x: event.clientX, y: event.clientY, title: '', items: canvasMenu(studio, p, clipboard, onError) }
    }
  }

  function onWheel(event) {
    const clip = studio.selected?.clip
    if (!clip || studio.selection.length > 1 || !boxes.length || clip.locked) return
    event.preventDefault()
    const f = event.deltaY < 0 ? 1.06 : 1 / 1.06
    studio.edit(() => {
      if (isTextual(clip)) clip.size = Math.round(Math.min(600, Math.max(8, clip.size * f)))
      else clip.scale = Math.min(6, Math.max(0.1, (clip.scale || 1) * f))
    }, `wheel:${clip.id}`)
  }

  function toggleFull() {
    if (document.fullscreenElement) document.exitFullscreen?.()
    else wrap?.requestFullscreen?.().catch(() => {})
  }

  function togglePlay() {
    if (studio.duration <= 0) return
    studio.playing = !studio.playing
  }

  onMount(() => {
    ctx = canvas.getContext('2d')
    const ro = new ResizeObserver(() => {
      stageW = stage.clientWidth
    })
    ro.observe(stage)
    stage.addEventListener('wheel', onWheel, { passive: false })
    const onFull = () => (full = document.fullscreenElement === wrap)
    document.addEventListener('fullscreenchange', onFull)
    const clock = setInterval(schedule, 10000)
    schedule()
    return () => {
      ro.disconnect()
      stage.removeEventListener('wheel', onWheel)
      document.removeEventListener('fullscreenchange', onFull)
      clearInterval(clock)
      cancelAnimationFrame(raf)
    }
  })
</script>

<div class="pv">
  <div class="pv-area" bind:this={wrap} class:full>
    <div class="pv-stage" bind:this={stage} data-drop="preview" role="application" aria-label="Preview" onpointerdown={pointerDown} oncontextmenu={onContext}>
      <canvas bind:this={canvas} width={W} height={H}></canvas>
      {#each boxes as box (box.id)}
        <div
          class="pv-box"
          class:text={box.text}
          class:locked={box.locked}
          class:secondary={boxes.length > 1 && !box.primary}
          style:left="{box.x * scale}px"
          style:top="{box.y * scale}px"
          style:width="{box.w * scale}px"
          style:height="{box.h * scale}px"
        >
          {#if box.locked}
            <span class="pv-lock" title="Locked. Right-click to unlock.">
              <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5" fill="currentColor" /><path d="M5.5 7V5.2a2.5 2.5 0 015 0V7" fill="none" stroke="currentColor" stroke-width="1.4" /></svg>
            </span>
          {/if}
        </div>
      {/each}
      {#if studio.duration <= 0 && !loading}
        <div class="pv-empty">
          <strong>Add media to start</strong>
          <span>Pick pictures, GIFs, or videos from the media panel, or drop files anywhere.</span>
        </div>
      {/if}
    </div>
    {#if menu}
      <ContextMenu x={menu.x} y={menu.y} title={menu.title} items={menu.items} onClose={() => (menu = null)} />
    {/if}
  </div>

  <div class="pv-bar">
    <button class="pv-play" onclick={togglePlay} disabled={studio.duration <= 0} aria-label={studio.playing ? 'Pause' : 'Play'}>
      {#if studio.playing}
        <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4" y="3" width="3" height="10" rx="1" fill="currentColor" /><rect x="9" y="3" width="3" height="10" rx="1" fill="currentColor" /></svg>
      {:else}
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5 3.2v9.6c0 .5.5.8.9.5l7.2-4.8c.4-.3.4-.8 0-1L5.9 2.7c-.4-.3-.9 0-.9.5z" fill="currentColor" /></svg>
      {/if}
    </button>
    <span class="pv-time"><strong>{fmtClock(studio.time)}</strong> / {fmtClock(studio.duration)}</span>
    <input
      class="pv-scrub"
      type="range"
      min="0"
      max={Math.max(studio.duration, 0.01)}
      step="0.01"
      value={studio.time}
      disabled={studio.duration <= 0}
      style:--fill="{studio.duration ? (studio.time / studio.duration) * 100 : 0}%"
      oninput={(e) => {
        studio.playing = false
        studio.seek(Number(e.currentTarget.value))
      }}
      aria-label="Playhead"
    />
    <span class="pv-size" title="Estimated size on the cooler. Clips are capped at {studio.limits.maxSeconds} seconds and {fmtBytes(studio.limits.maxBytes)}.">
      ≤ {fmtBytes(estimate)}
    </span>
    <button class="pv-icon" onclick={toggleFull} aria-label="Full screen preview">
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2.5 6V2.5H6M10 2.5h3.5V6M13.5 10v3.5H10M6 13.5H2.5V10" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /></svg>
    </button>
    <span class="pv-res" title="The cooler’s screen">1280 × 720</span>
  </div>
</div>
