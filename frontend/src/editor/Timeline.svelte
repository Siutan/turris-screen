<script>
  import { onMount, tick, untrack } from 'svelte'
  import { MIN_CLIP, clipEnd, clipLabel, fmtSourceTime, isTextual } from '../lib/project.js'
  import { round } from '../lib/studio.svelte.js'
  import { clipMenu } from '../lib/menus.js'
  import ContextMenu from './ContextMenu.svelte'

  let { studio, sources, revision, clipboard, onError } = $props()

  let menu = $state(null)
  let marquee = $state(null)
  let slipping = $state(null)

  let mergeCheck = $derived(studio.canMerge())
  let canSplitNow = $derived.by(() => {
    const pool = studio.selection.length ? studio.selectedClips.map((f) => f.clip) : studio.project.tracks.flatMap((t) => t.clips)
    return pool.some((c) => studio.canSplit(c))
  })

  function clipContext(event, clip) {
    event.preventDefault()
    event.stopPropagation()
    if (!studio.isSelected(clip.id)) studio.select(clip.id)
    const count = studio.selection.length
    menu = { x: event.clientX, y: event.clientY, title: count > 1 ? `${count} clips` : clipLabel(clip), items: clipMenu(studio, clipboard, onError) }
  }

  const GUTTER = 100
  const LANE_MARGIN = 16
  const MIN_SPAN = 2

  let ruler = $state(null)
  let scroller = $state(null)
  let laneW = $state(600)
  // span is how many seconds fit across the view. It only changes when the user zooms
  // or fits, so dragging clips never rescales the timeline under the pointer.
  let span = $state(10)
  let scrollX = $state(0)

  let maxSpan = $derived(studio.limits.maxSeconds + 2)
  let pps = $derived(laneW / span)
  let total = $derived(Math.max(span, maxSpan))

  $effect(() => {
    studio.pps = pps
  })

  // fit shows the whole project. Automatic fits keep at least ten seconds in view so a
  // first short clip leaves room for the next one.
  function fit(least = MIN_SPAN + 1) {
    const d = studio.duration
    span = d > 0 ? Math.min(maxSpan, Math.max(least, d * 1.04 + 0.5)) : 10
    if (scroller) scroller.scrollLeft = 0
  }

  // Fit when a project opens, and when the first clip lands on an empty timeline.
  $effect(() => {
    studio.fitKey
    untrack(() => fit(10))
  })

  let hasContent = $derived(studio.duration > 0)
  $effect(() => {
    if (hasContent) untrack(() => fit(10))
  })

  // zoomAt scales the view around a point so the time under the cursor stays put.
  async function zoomAt(factor, clientX) {
    const next = Math.min(maxSpan, Math.max(MIN_SPAN, span * factor))
    if (next === span || !scroller) return
    const r = scroller.getBoundingClientRect()
    const x = clientX - r.left - GUTTER
    const at = (scroller.scrollLeft + x) / pps
    span = next
    await tick()
    scroller.scrollLeft = Math.max(0, at * pps - x)
  }

  function onWheel(event) {
    if (!event.ctrlKey && !event.metaKey) return
    event.preventDefault()
    zoomAt(event.deltaY < 0 ? 1 / 1.15 : 1.15, event.clientX)
  }

  let ticks = $derived.by(() => {
    const major = [1, 2, 5, 10, 15, 30].find((s) => s * pps >= 64) ?? 30
    const sub = major * pps >= 120 ? 10 : major * pps >= 60 ? 5 : 2
    const out = []
    const step = major / sub
    for (let i = 0; i * step <= total + 1e-6; i++) {
      const t = round(i * step)
      out.push({ t, major: i % sub === 0 })
    }
    return out
  })

  function trackIcon(track) {
    const first = track.clips[0]
    if (!first) return 'empty'
    if (first.type === 'media') return 'media'
    if (first.type === 'text') return 'text'
    if (first.type === 'effect') return 'effect'
    return 'widget'
  }

  function poster(clip) {
    revision
    return sources.get(clip.assetId)?.poster || ''
  }

  function missing(clip) {
    revision
    return clip.type === 'media' && sources.status(clip.assetId) === 'error'
  }

  function timeAt(clientX) {
    const r = ruler.getBoundingClientRect()
    return Math.max(0, (clientX - r.left) / pps)
  }

  function scrub(event) {
    if (event.button !== 0) return
    studio.playing = false
    studio.seek(timeAt(event.clientX))
    const move = (e) => studio.seek(timeAt(e.clientX))
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }

  // laneDown on empty space: a click moves the playhead and clears the selection; a drag
  // draws a box that selects every clip it touches. Shift or Ctrl adds to the selection.
  function laneDown(event) {
    if (event.button !== 0 || event.target !== event.currentTarget) return
    studio.playing = false
    const additive = event.shiftKey || event.ctrlKey || event.metaKey
    const kept = additive ? [...studio.selection] : []
    const x0 = event.clientX
    const y0 = event.clientY
    let dragging = false
    track(window, {
      move: (e) => {
        if (!dragging && Math.abs(e.clientX - x0) < 4 && Math.abs(e.clientY - y0) < 4) return
        dragging = true
        const box = { left: Math.min(x0, e.clientX), top: Math.min(y0, e.clientY), right: Math.max(x0, e.clientX), bottom: Math.max(y0, e.clientY) }
        marquee = box
        const hits = [...scroller.querySelectorAll('.tl-clip[data-clip]')]
          .filter((el) => {
            const r = el.getBoundingClientRect()
            return r.left < box.right && r.right > box.left && r.top < box.bottom && r.bottom > box.top
          })
          .map((el) => el.dataset.clip)
        studio.selectMany([...kept, ...hits])
      },
      up: () => {
        marquee = null
        if (dragging) return
        if (!additive) studio.select(null)
        studio.seek(timeAt(x0))
      },
    })
  }

  function snapPoints(skip) {
    const pts = [0, studio.time]
    for (const track of studio.project.tracks) {
      for (const c of track.clips) {
        if (skip.has(c.id)) continue
        pts.push(c.start, clipEnd(c))
      }
    }
    return pts
  }

  function snap(t, points) {
    const limit = 7 / pps
    let best = t
    let dist = limit
    for (const p of points) {
      const d = Math.abs(p - t)
      if (d < dist) {
        dist = d
        best = p
      }
    }
    return best
  }

  function trackUnder(x, y) {
    const el = document.elementFromPoint(x, y)?.closest('[data-track]')
    if (!el) return null
    const i = Number(el.dataset.track)
    return Number.isInteger(i) && i < studio.project.tracks.length ? i : null
  }

  function track(window_, handlers) {
    const { move, up } = handlers
    const done = () => {
      window_.removeEventListener('pointermove', move)
      window_.removeEventListener('pointerup', done)
      window_.removeEventListener('pointercancel', done)
      up()
    }
    window_.addEventListener('pointermove', move)
    window_.addEventListener('pointerup', done)
    window_.addEventListener('pointercancel', done)
  }

  // clipDown selects and drags. Shift or Ctrl toggles the clip in the selection. Dragging
  // any selected clip moves every unlocked selected clip, keeping their spacing and tracks.
  function clipDown(event, clip) {
    if (event.button !== 0) return
    event.stopPropagation()
    studio.playing = false
    const additive = event.shiftKey || event.ctrlKey || event.metaKey
    const wasSelected = studio.isSelected(clip.id)
    if (additive && !wasSelected) studio.toggleSelect(clip.id)
    else if (!additive && !wasSelected) studio.select(clip.id)
    else if (!additive) studio.selection = [...studio.selection.filter((id) => id !== clip.id), clip.id]

    const edge = event.target.closest('[data-edge]')?.dataset.edge
    if (edge && !clip.locked && studio.selection.length === 1) return trim(event, clip, edge)
    if (event.altKey && !clip.locked && studio.sourceRange(clip.id)) return slip(event, clip)

    const group = studio.selectedClips
      .filter((f) => !f.clip.locked)
      .map((f) => ({ id: f.clip.id, start0: f.clip.start, track0: f.trackIndex, duration: f.clip.duration }))
    const lead = group.find((g) => g.id === clip.id)
    const before = studio.serialize()
    const x0 = event.clientX
    const y0 = event.clientY
    const points = snapPoints(new Set(group.map((g) => g.id)))
    const minDt = -Math.min(...group.map((g) => g.start0))
    const maxDt = studio.limits.maxSeconds - Math.max(...group.map((g) => g.start0 + g.duration))
    const minTrack = Math.min(...group.map((g) => g.track0))
    const maxTrack = Math.max(...group.map((g) => g.track0))
    let moved = false
    let lastTrack = 0
    track(window, {
      move: (e) => {
        if (!lead) return
        if (!moved && Math.abs(e.clientX - x0) < 3 && Math.abs(e.clientY - y0) < 6) return
        moved = true
        let s = lead.start0 + (e.clientX - x0) / pps
        const byStart = snap(s, points)
        const byEnd = snap(s + lead.duration, points) - lead.duration
        s = Math.abs(byStart - s) <= Math.abs(byEnd - s) ? byStart : byEnd
        const dt = Math.max(minDt, Math.min(maxDt, s - lead.start0))
        const target = trackUnder(e.clientX, e.clientY)
        let dTrack = target === null ? 0 : target - lead.track0
        dTrack = Math.max(-minTrack, Math.min(studio.project.tracks.length - 1 - maxTrack, dTrack))
        if (group.length > 1 && dTrack !== lastTrack && !studio.groupFits(group, dt, dTrack)) dTrack = lastTrack
        lastTrack = dTrack
        studio.moveGroup(group, dt, dTrack)
      },
      up: () => {
        if (!moved) {
          if (additive && wasSelected) studio.toggleSelect(clip.id)
          else if (!additive) studio.select(clip.id)
          studio.seek(timeAt(x0))
          return
        }
        for (const g of [...group].sort((a, b) => a.start0 - b.start0)) studio.settle(g.id)
        studio.remember(before)
      },
    })
  }

  function trim(event, clip, edge) {
    const before = studio.serialize()
    const x0 = event.clientX
    const start0 = clip.start
    const end0 = clipEnd(clip)
    const in0 = clip.in || 0
    const bounds = studio.trimBounds(clip)
    const points = snapPoints(new Set([clip.id]))
    const entry = clip.type === 'media' ? sources.get(clip.assetId) : null
    track(window, {
      move: (e) => {
        const dt = (e.clientX - x0) / pps
        if (edge === 'start') {
          let s = snap(start0 + dt, points)
          s = Math.max(bounds.minStart, Math.min(s, end0 - MIN_CLIP))
          clip.start = round(s)
          clip.duration = round(end0 - s)
          if (clip.type === 'media') {
            let next = in0 + (s - start0)
            if (entry?.kind === 'gif' && entry.duration) next = ((next % entry.duration) + entry.duration) % entry.duration
            clip.in = round(Math.max(0, next))
          }
        } else {
          let end = snap(end0 + dt, points)
          end = Math.min(bounds.maxEnd, Math.max(end, start0 + MIN_CLIP))
          clip.duration = round(end - start0)
        }
      },
      up: () => studio.remember(before),
    })
  }

  function clipTitle(clip) {
    const base = `${clipLabel(clip)} · ${clip.duration.toFixed(1)}s`
    if (clip.type !== 'media' || sources.get(clip.assetId)?.kind !== 'video') return base
    const from = clip.in || 0
    return `${base} · plays ${fmtSourceTime(from)}–${fmtSourceTime(from + clip.duration)} of the video. Alt-drag to pick a different part.`
  }

  // slip keeps a video clip in place and slides which part of the source it plays.
  // Dragging right shows earlier footage, like pulling the film through a window.
  function slip(event, clip) {
    const range = studio.sourceRange(clip.id)
    const length = range.to - range.from
    const x0 = event.clientX
    const key = `${clip.id}:slip:${x0}:${event.timeStamp}`
    studio.playing = false
    slipping = clip.id
    track(window, {
      move: (e) => {
        const from = Math.max(0, Math.min(range.from - (e.clientX - x0) / pps, range.total - length))
        studio.setSourceRange(clip.id, from, from + length, key)
        if (studio.time < clip.start || studio.time > clipEnd(clip)) studio.seek(clip.start)
      },
      up: () => (slipping = null),
    })
  }

  function act(fn) {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }

  onMount(() => {
    const ro = new ResizeObserver(() => {
      laneW = Math.max(200, scroller.clientWidth - GUTTER - LANE_MARGIN)
    })
    ro.observe(scroller)
    scroller.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      ro.disconnect()
      scroller.removeEventListener('wheel', onWheel)
    }
  })
</script>

<div class="tl-wrap">
<button class="tl-fit" onclick={() => fit()} title="Fit the timeline to the project" aria-label="Fit timeline">
  <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="4.6" fill="none" stroke="currentColor" stroke-width="1.3" /><circle cx="8" cy="8" r="1.4" fill="currentColor" /><path d="M8 1.2v2.4M8 12.4v2.4M1.2 8h2.4M12.4 8h2.4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
</button>
<div class="tl" bind:this={scroller} onscroll={() => (scrollX = scroller.scrollLeft)}>
  <div class="tl-inner" style:width="{GUTTER + total * pps + LANE_MARGIN}px">
    <div class="tl-head">
      <div class="tl-tools">
        <button class="tl-tool" onclick={() => act(() => studio.split())} disabled={!canSplitNow} title="Split at the playhead (S). With nothing selected, cuts every clip under it." aria-label="Split">
          <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="4.5" cy="4.5" r="2" fill="none" stroke="currentColor" stroke-width="1.3" /><circle cx="4.5" cy="11.5" r="2" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M6.2 5.6L13.5 12M6.2 10.4L13.5 4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
        </button>
        <button class="tl-tool" onclick={() => act(() => studio.duplicate())} disabled={!studio.selection.length} title="Duplicate (Ctrl+D)" aria-label="Duplicate">
          <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="5" y="5" width="8.5" height="8.5" rx="2" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M11 3.2V3a.5.5 0 00-.5-.5H4A1.5 1.5 0 002.5 4v6.5a.5.5 0 00.5.5h.2" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
        </button>
        <button class="tl-tool" onclick={() => act(() => studio.merge())} disabled={!mergeCheck.ok} title={mergeCheck.ok ? 'Merge clips (Ctrl+J)' : `Merge clips (Ctrl+J). ${mergeCheck.reason}`} aria-label="Merge clips">
          <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="1.8" y="4.5" width="5" height="7" rx="1.4" fill="none" stroke="currentColor" stroke-width="1.3" /><rect x="9.2" y="4.5" width="5" height="7" rx="1.4" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M6.8 8h2.4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
        </button>
        <button class="tl-tool" onclick={() => studio.removeClips()} disabled={!studio.selection.length} title="Delete (Del). Shift+Del closes the gap." aria-label="Delete">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.2c0 .5.5.8 1 .8h3.8c.5 0 .9-.3 1-.8l.6-8.2" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
      </div>
      <div class="tl-ruler" bind:this={ruler} role="slider" tabindex="-1" aria-label="Timeline" aria-valuenow={studio.time} onpointerdown={scrub}>
        {#each ticks as tick (tick.t)}
          <span class="tick" class:major={tick.major} style:left="{tick.t * pps}px">
            {#if tick.major}<em>{tick.t}s</em>{/if}
          </span>
        {/each}
        {#if studio.duration > 0}
          <span class="tl-end" style:left="{studio.duration * pps}px" title="End of the loop"></span>
        {/if}
      </div>
    </div>

    <div class="tl-rows">
      {#each studio.project.tracks as track, i (track.id)}
        <div class="tl-row" class:hidden={track.hidden}>
          <div class="tl-gutter">
            <button class="tl-eye" onclick={() => studio.toggleTrack(i)} aria-label={track.hidden ? 'Show track' : 'Hide track'} title={track.hidden ? 'Show track' : 'Hide track'}>
              {#if track.hidden}
                <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 8s2.2-4.5 6-4.5S14 8 14 8s-2.2 4.5-6 4.5S2 8 2 8z" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M3 13L13 3" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
              {:else}
                <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 8s2.2-4.5 6-4.5S14 8 14 8s-2.2 4.5-6 4.5S2 8 2 8z" fill="none" stroke="currentColor" stroke-width="1.3" /><circle cx="8" cy="8" r="1.9" fill="currentColor" /></svg>
              {/if}
            </button>
            <span class="tl-kind" aria-hidden="true">
              {#if trackIcon(track) === 'media'}
                <svg viewBox="0 0 16 16"><rect x="2.5" y="3" width="11" height="10" rx="2" fill="none" stroke="currentColor" stroke-width="1.2" /><path d="M4.5 11l2.6-2.8 1.8 1.8 1.4-1.4 1.7 2.4" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" /><circle cx="10.4" cy="5.8" r="0.9" fill="currentColor" /></svg>
              {:else if trackIcon(track) === 'text'}
                <svg viewBox="0 0 16 16"><path d="M3.5 4h9M8 4v8.5" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
              {:else if trackIcon(track) === 'widget'}
                <svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-width="1.2" /><path d="M8 5v3.2l2 1.3" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" /></svg>
              {:else if trackIcon(track) === 'effect'}
                <svg viewBox="0 0 16 16"><path d="M8 1.8l1.2 3.6 3.8.2-3 2.3 1.1 3.6L8 9.4 4.9 11.5l1.1-3.6-3-2.3 3.8-.2z" fill="none" stroke="currentColor" stroke-width="1.1" stroke-linejoin="round" /></svg>
              {:else}
                <svg viewBox="0 0 16 16"><path d="M8 2.8l5.5 3L8 8.8l-5.5-3z M2.5 10.2L8 13.2l5.5-3" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" /></svg>
              {/if}
            </span>
            {#if studio.project.tracks.length > 1}
              <button class="tl-drop" onclick={() => studio.removeTrack(i)} aria-label="Remove track" title="Remove track">×</button>
            {/if}
          </div>
          <div class="tl-lane" data-track={i} role="presentation" onpointerdown={laneDown}>
            {#each track.clips as clip (clip.id)}
              <div
                class="tl-clip"
                class:media={clip.type === 'media'}
                class:text={isTextual(clip)}
                class:effect={clip.type === 'effect'}
                class:missing={missing(clip)}
                class:selected={studio.isSelected(clip.id)}
                class:primary={studio.selection.length > 1 && studio.selectedId === clip.id}
                data-clip={clip.id}
                class:locked={clip.locked}
                style:left="{clip.start * pps}px"
                style:width="{Math.max(6, clip.duration * pps)}px"
                style:background-image={clip.type === 'media' && poster(clip) ? `url(${poster(clip)})` : null}
                title={clipTitle(clip)}
                role="button"
                tabindex="-1"
                onpointerdown={(e) => clipDown(e, clip)}
                oncontextmenu={(e) => clipContext(e, clip)}
              >
                <span class="edge start" data-edge="start"></span>
                {#if clip.locked}
                  <span class="tl-lock" title="Locked">
                    <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5" fill="currentColor" /><path d="M5.5 7V5.2a2.5 2.5 0 015 0V7" fill="none" stroke="currentColor" stroke-width="1.4" /></svg>
                  </span>
                {/if}
                {#if isTextual(clip)}
                  <span class="tl-label"><b>T<small>T</small></b>{clipLabel(clip)}</span>
                  <span class="grip" aria-hidden="true">⋮</span>
                {:else if clip.type === 'effect'}
                  <span class="tl-label">{clipLabel(clip)}</span>
                {:else if missing(clip)}
                  <span class="tl-label">Missing media</span>
                {/if}
                {#if slipping === clip.id}
                  <span class="tl-slip">{fmtSourceTime(clip.in || 0)} – {fmtSourceTime((clip.in || 0) + clip.duration)}</span>
                {/if}
                <span class="edge end" data-edge="end"></span>
              </div>
            {/each}
          </div>
        </div>
      {/each}
      <div class="tl-row adder">
        <div class="tl-gutter">
          <span class="tl-kind" aria-hidden="true">
            <svg viewBox="0 0 16 16"><path d="M8 2.8l5.5 3L8 8.8l-5.5-3z M2.5 10.2L8 13.2l5.5-3" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round" /></svg>
          </span>
        </div>
        <div class="tl-lane" data-track={studio.project.tracks.length}>
          <button class="tl-add" onclick={() => studio.addTrack()}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M6 1.5v9M1.5 6h9" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
            Add Track
          </button>
        </div>
      </div>
    </div>

    {#if studio.time * pps >= scrollX - 1}
      <div class="tl-playhead" style:left="calc(var(--gutter) + {studio.time * pps}px)" aria-hidden="true">
        <span></span>
      </div>
    {/if}
  </div>
</div>
</div>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} title={menu.title} items={menu.items} onClose={() => (menu = null)} />
{/if}

{#if marquee}
  <div
    class="tl-marquee"
    style:left="{marquee.left}px"
    style:top="{marquee.top}px"
    style:width="{marquee.right - marquee.left}px"
    style:height="{marquee.bottom - marquee.top}px"
    aria-hidden="true"
  ></div>
{/if}
