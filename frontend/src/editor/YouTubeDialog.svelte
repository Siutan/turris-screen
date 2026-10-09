<script>
  import { onMount, untrack } from 'svelte'
  import { CancelYouTube, GetYouTubeTool, ImportYouTube } from '../../wailsjs/go/main/App.js'
  import { EventsOff, EventsOn } from '../../wailsjs/runtime/runtime.js'

  let { url: initialUrl = '', onClose, onImported } = $props()

  const firstUrl = untrack(() => initialUrl)
  let url = $state(firstUrl)
  let start = $state(startFromLink(firstUrl))
  let end = $state('')
  let busy = $state(false)
  let cancelling = $state(false)
  let error = $state('')
  let tool = $state(null)
  let input = $state(null)

  let progress = $state({ label: 'Starting', percent: 0 })

  // parseTime reads "90", "1:30", or "1:02:03" as seconds. Blank is null.
  function parseTime(text) {
    const s = text.trim()
    if (!s) return null
    if (!/^\d+(:\d{1,2}){0,2}(\.\d+)?$/.test(s)) return NaN
    return s.split(':').reduce((total, part) => total * 60 + Number(part), 0)
  }

  // startFromLink picks up ?t=75 or ?t=1m15s from a shared link.
  function startFromLink(link) {
    const m = /[?&#]t=(\d+h)?(\d+m)?(\d+)s?\b/.exec(link || '')
    if (!m) return ''
    const secs = (parseInt(m[1]) || 0) * 3600 + (parseInt(m[2]) || 0) * 60 + Number(m[3])
    return secs ? `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}` : ''
  }

  function message(err) {
    if (!err) return 'Something went wrong'
    const text = typeof err === 'string' ? err : err.message || String(err)
    return text.charAt(0).toUpperCase() + text.slice(1).replace(/([^.])$/, '$1.')
  }

  async function submit(event) {
    event?.preventDefault()
    if (busy) return
    error = ''
    const from = parseTime(start)
    const to = parseTime(end)
    if (!url.trim()) return (error = 'Paste a YouTube link first.')
    if (Number.isNaN(from) || Number.isNaN(to)) return (error = 'Write times like 1:30 or 90.')
    if (from !== null && to !== null && to <= from) return (error = 'The end has to be after the start.')
    busy = true
    progress = { label: 'Starting', percent: 0 }
    try {
      const asset = await ImportYouTube({ url: url.trim(), start: from ?? 0, end: to ?? 0 })
      onImported?.(asset)
    } catch (err) {
      if (cancelling) onClose?.()
      else error = message(err)
    } finally {
      busy = false
      cancelling = false
    }
  }

  function cancel() {
    if (!busy) return onClose?.()
    cancelling = true
    CancelYouTube()
  }

  function onKey(event) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      if (!busy) onClose?.()
    }
  }

  onMount(() => {
    input?.focus()
    input?.select()
    GetYouTubeTool()
      .then((t) => (tool = t))
      .catch(() => (tool = null))
    EventsOn('youtube', (p) => {
      if (busy) progress = p
    })
    return () => EventsOff('youtube')
  })
</script>

<div class="st-modal">
  <div class="st-modal-card yt-card" role="dialog" aria-modal="true" aria-labelledby="yt-title" tabindex="-1" onkeydown={onKey}>
  <form class="yt-form" onsubmit={submit}>
    <div class="yt-head">
      <span class="yt-logo" aria-hidden="true">
        <svg viewBox="0 0 24 24"><rect x="1.5" y="4.5" width="21" height="15" rx="4.5" fill="#ff0033" /><path d="M10 8.8v6.4l5.6-3.2z" fill="white" /></svg>
      </span>
      <h3 id="yt-title">Import from YouTube</h3>
    </div>

    <label class="yt-field">
      <span>Link</span>
      <input
        bind:this={input}
        bind:value={url}
        oninput={() => {
          error = ''
          if (!start) start = startFromLink(url)
        }}
        type="url"
        placeholder="https://www.youtube.com/watch?v=…"
        spellcheck="false"
        autocomplete="off"
        disabled={busy}
      />
    </label>

    <div class="yt-times">
      <label class="yt-field">
        <span>Start <em>optional</em></span>
        <input bind:value={start} placeholder="0:00" inputmode="numeric" disabled={busy} />
      </label>
      <label class="yt-field">
        <span>End <em>optional</em></span>
        <input bind:value={end} placeholder="End of video" inputmode="numeric" disabled={busy} />
      </label>
    </div>

    <p class="st-modal-note">
      Only the picture is downloaded, up to 10 minutes from the start time. Projects play up to a minute, so pick the part you want.
      {#if tool && !tool.installed}The first import also downloads yt-dlp (about 18 MB) from GitHub.{/if}
    </p>

    {#if busy}
      <div class="yt-progress" aria-live="polite">
        <p>{cancelling ? 'Cancelling' : progress.label}… <em>{Math.round(progress.percent)}%</em></p>
        <div class="st-meter"><span style:width="{progress.percent}%"></span></div>
      </div>
    {/if}

    {#if error}<p class="yt-error" role="alert">{error}</p>{/if}

    <div class="st-modal-actions">
      <button type="button" class="st-btn" onclick={cancel} disabled={cancelling}>Cancel</button>
      <button type="submit" class="st-btn primary" disabled={busy}>{busy ? 'Importing…' : 'Import'}</button>
    </div>
  </form>
  </div>
</div>
