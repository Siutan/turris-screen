<script>
  import { MIN_CLIP, clipEnd, fmtClock, fmtSourceTime, parseTime } from '../lib/project.js'

  let { studio, clip, revision } = $props()

  let bar = $state(null)
  let fromText = $state('')
  let toText = $state('')
  let gesture = 0

  let range = $derived.by(() => {
    revision
    clip.in
    clip.duration
    clip.start
    return studio.sourceRange(clip.id)
  })

  let pct = (s) => `${(s / range.total) * 100}%`
  let playhead = $derived(range && studio.time >= clip.start && studio.time <= clipEnd(clip) ? (clip.in || 0) + studio.time - clip.start : null)

  $effect(() => {
    if (!range) return
    fromText = fmtSourceTime(range.from)
    toText = fmtSourceTime(range.to)
  })

  function show(edge) {
    studio.playing = false
    const fps = studio.limits.renderFps || 30
    studio.seek(edge === 'to' ? Math.max(clip.start, clipEnd(clip) - 1 / fps) : clip.start)
  }

  function secondsAt(clientX) {
    const r = bar.getBoundingClientRect()
    return (Math.max(0, Math.min(clientX - r.left, r.width)) / r.width) * range.total
  }

  function down(event) {
    if (event.button !== 0 || clip.locked || !range) return
    event.preventDefault()
    const edge = event.target.closest('[data-edge]')?.dataset.edge ?? 'move'
    const key = `${clip.id}:range:${++gesture}`
    const from0 = range.from
    const to0 = range.to
    const length = to0 - from0
    const t0 = secondsAt(event.clientX)
    const inside = t0 >= from0 && t0 <= to0
    if (edge === 'move' && !inside) {
      const from = Math.max(0, Math.min(t0 - length / 2, range.total - length))
      studio.setSourceRange(clip.id, from, from + length, key)
    }
    const start = edge === 'move' && !inside ? (clip.in || 0) : from0
    const offset = t0 - start
    bar.setPointerCapture(event.pointerId)
    show(edge)
    const move = (e) => {
      const t = secondsAt(e.clientX)
      if (edge === 'from') {
        const from = Math.max(0, to0 - range.longest, Math.min(t, to0 - MIN_CLIP))
        studio.setSourceRange(clip.id, from, to0, key)
      } else if (edge === 'to') {
        studio.setSourceRange(clip.id, from0, Math.max(t, from0 + MIN_CLIP), key)
      } else {
        const from = Math.max(0, Math.min(t - offset, range.total - length))
        studio.setSourceRange(clip.id, from, from + length, key)
      }
      show(edge)
    }
    const up = () => {
      bar.removeEventListener('pointermove', move)
      bar.removeEventListener('pointerup', up)
      bar.removeEventListener('pointercancel', up)
    }
    bar.addEventListener('pointermove', move)
    bar.addEventListener('pointerup', up)
    bar.addEventListener('pointercancel', up)
  }

  function commitFrom() {
    const t = parseTime(fromText)
    if (t === null || Number.isNaN(t) || t >= range.total) {
      fromText = fmtSourceTime(range.from)
      return
    }
    const length = range.to - range.from
    studio.setSourceRange(clip.id, t, t + length)
    show('from')
  }

  function commitTo() {
    const t = parseTime(toText)
    if (t === null || Number.isNaN(t) || t <= range.from) {
      toText = fmtSourceTime(range.to)
      return
    }
    studio.setSourceRange(clip.id, range.from, t)
    show('to')
  }

  function onKey(event, commit) {
    if (event.key === 'Enter') {
      event.preventDefault()
      commit()
      event.currentTarget.blur()
    } else if (event.key === 'Escape') {
      event.currentTarget.blur()
    }
  }
</script>

{#if range}
  <div class="src">
    <div
      class="src-bar"
      class:locked={clip.locked}
      bind:this={bar}
      onpointerdown={down}
      role="slider"
      tabindex="-1"
      aria-label="Part of the video this clip plays"
      aria-valuemin="0"
      aria-valuemax={range.total}
      aria-valuenow={range.from}
      title={clip.locked ? 'Unlock the clip to change its section.' : 'Drag the highlighted part to pick which section plays. Drag its ends to change the length.'}
    >
      <div class="src-win" style:left={pct(range.from)} style:width={pct(range.to - range.from)}>
        <span class="src-edge start" data-edge="from"></span>
        <span class="src-edge end" data-edge="to"></span>
      </div>
      {#if playhead !== null}
        <span class="src-now" style:left={pct(playhead)}></span>
      {/if}
    </div>
    <div class="src-scale">
      <span>0:00</span>
      <span>{fmtClock(range.total)}</span>
    </div>
    <div class="src-fields">
      <label>
        <span>From</span>
        <input class="ins-select" bind:value={fromText} disabled={clip.locked} onchange={commitFrom} onkeydown={(e) => onKey(e, commitFrom)} spellcheck="false" />
      </label>
      <label>
        <span>To</span>
        <input class="ins-select" bind:value={toText} disabled={clip.locked} onchange={commitTo} onkeydown={(e) => onKey(e, commitTo)} spellcheck="false" />
      </label>
    </div>
    <p class="ins-note">
      Plays {(range.to - range.from).toFixed(1)}s of the {fmtClock(range.total)} video. You can also Alt-drag the clip on the timeline.
    </p>
  </div>
{/if}
