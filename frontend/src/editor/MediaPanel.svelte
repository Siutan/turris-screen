<script>
  import { GetSourceSecret, PickSourceFile, PickSourceProgram, SearchPlaces, SetSourceSecret } from '../../wailsjs/go/main/App.js'
  import { EFFECT_PACKS, SOURCE_KINDS, TEXT_PRESETS, hasMotion, widgetText } from '../lib/project.js'
  import { isDeviceLabel } from '../lib/labels.js'
  import { MOTION_MIN_MS, ageLabel } from '../lib/live.js'
  import { session } from '../lib/session.svelte.js'
  import { tooltip } from '../lib/tooltip.js'
  import ContextMenu from './ContextMenu.svelte'

  let {
    studio,
    sources,
    assets,
    revision,
    importing = false,
    exported = false,
    tab = $bindable('image'),
    onImport,
    onYouTube,
    onKlipy,
    onRemoveAsset,
    onAssetDown,
    onError,
  } = $props()

  let view = $state('media')
  let addMenu = $state(null)
  let addMenuClosed = 0

  function openAddMenu(event) {
    // The menu closes on the press that lands on "+", so a second click should not reopen it.
    if (performance.now() - addMenuClosed < 300) return
    const r = event.currentTarget.getBoundingClientRect()
    addMenu = {
      x: r.left,
      y: r.bottom + 6,
      items: [
        { label: 'From this PC…', run: () => onImport?.() },
        { label: 'From KLIPY…', run: () => onKlipy?.() },
        { label: 'From YouTube…', run: () => onYouTube?.() },
      ],
    }
  }

  const tabs = [
    { id: 'image', label: 'Images' },
    { id: 'video', label: 'Videos' },
    { id: 'gif', label: 'GIFs' },
  ]

  let shown = $derived(assets.filter((a) => a.kind === tab))
  let used = $derived(new Set(studio.project.tracks.flatMap((t) => t.clips.filter((c) => c.type === 'media').map((c) => c.assetId))))
  let now = $state(new Date())

  $effect(() => {
    const timer = setInterval(() => (now = new Date()), 1000)
    return () => clearInterval(timer)
  })

  function thumb(asset) {
    revision
    const entry = sources.get(asset.id)
    if (entry?.poster) return entry.poster
    return asset.kind === 'video' ? '' : asset.url
  }

  function loadState(asset) {
    revision
    return sources.status(asset.id)
  }

  function length(asset) {
    revision
    const d = sources.get(asset.id)?.duration
    if (!d || asset.kind === 'image') return ''
    return d < 10 ? `${d.toFixed(1)}s` : `${Math.round(d)}s`
  }

  function tracksHaveLabels(project) {
    return (project?.tracks || []).some((track) => (track.clips || []).some(isDeviceLabel))
  }

  let kindOf = (id) => assets.find((asset) => asset.id === id)?.kind || 'image'
  let motion = $derived(hasMotion(studio.project, kindOf))
  let liveReason = $derived(!exported ? 'Save to the library and show it on the cooler before keeping this live.' : '')
  let liveDetail = $derived.by(() => {
    const parts = []
    if (liveReason) parts.push(liveReason)
    if (motion && !tracksHaveLabels(studio.project)) parts.push(`Video, GIF, and effect clips restart from the beginning. A new copy goes out at least ${MOTION_MIN_MS / 60000} minutes apart.`)
    if (!parts.length) parts.push('Time, date, and fields update on the cooler while this app is open. Keep live leaves that running after you close the project.')
    return parts.join(' ')
  })
  let projectSources = $derived(studio.project.sources || [])

  function sourceState(source) {
    session.revision
    const value = session.values[source.id]
    if (!value) return { label: 'Waiting', bad: false }
    if (value.error) return { label: value.error, bad: true }
    const age = ageLabel(value.at, Date.now())
    return { label: age ? `OK · ${age}` : 'OK', bad: false }
  }

  const uniqueKinds = new Set(['system', 'temps', 'media'])
  const sourceButtons = ['countdown', 'zone', 'system', 'temps', 'media', 'weather', 'calendar', 'http', 'rss', 'mail', 'command', 'focus']
  const presets = [
    { preset: 'sports', name: 'Sports', intervalSec: 900 },
    { preset: 'price', name: 'Price', intervalSec: 180 },
    { preset: 'live', name: 'Live status', intervalSec: 180 },
    { preset: 'ha', name: 'Home Assistant', intervalSec: 60 },
    { preset: 'github', name: 'GitHub', intervalSec: 60 },
  ]
  let secrets = $state({})
  let secretLoads = new Set()
  let placeDraft = $state({})
  let placeHits = $state({})
  let placeOpen = $state(null)
  let placeTimer = {}

  function desktop() {
    return typeof window !== 'undefined' && window.go?.main?.App
  }

  function usesSecret(source) {
    return source.kind === 'mail' || source.kind === 'http'
  }

  $effect(() => {
    if (!desktop()) return
    for (const source of projectSources) {
      if (!usesSecret(source) || secretLoads.has(source.id)) continue
      secretLoads.add(source.id)
      GetSourceSecret(source.id)
        .then((secret) => {
          secrets = { ...secrets, [source.id]: secret || {} }
        })
        .catch(() => {})
    }
  })

  function addSource(kind) {
    act(() => studio.addSource(kind))
  }

  function addPreset(preset) {
    act(() => studio.addSource('http', { preset: preset.preset }, { name: preset.name, intervalSec: preset.intervalSec }))
  }

  function secretOf(id) {
    return secrets[id] || {}
  }

  function headerName(id) {
    return Object.keys(secretOf(id).headers || {})[0] || ''
  }

  async function saveSecret(id, patch) {
    if (!desktop()) {
      onError?.(new Error('Start the desktop app to store this.'))
      return
    }
    const current = secretOf(id)
    const next = {
      headers: patch.headers ?? current.headers ?? {},
      token: patch.token ?? current.token ?? '',
      user: patch.user ?? current.user ?? '',
      pass: patch.pass ?? current.pass ?? '',
    }
    secrets = { ...secrets, [id]: next }
    try {
      await SetSourceSecret(id, next)
    } catch (err) {
      onError?.(err)
    }
  }

  function saveHeader(id, name, value) {
    const headers = {}
    if (name.trim() && value.trim()) headers[name.trim()] = value.trim()
    saveSecret(id, { headers })
  }

  function setFocus(source, running) {
    const minutes = Math.max(1, Number(source.config.minutes) || 25)
    const end = running ? new Date(Date.now() + minutes * 60000).toISOString() : source.config.end
    act(() => studio.updateSource(source.id, { config: { minutes, running, end } }))
  }

  function placeValue(source) {
    return placeDraft[source.id] ?? source.config.place ?? ''
  }

  function onPlaceInput(source, value) {
    placeDraft = { ...placeDraft, [source.id]: value }
    placeOpen = source.id
    clearTimeout(placeTimer[source.id])
    placeTimer[source.id] = setTimeout(() => searchPlace(source.id, value), 250)
  }

  async function searchPlace(id, value) {
    const query = value.trim()
    if (query.length < 2 || !desktop() || placeOpen !== id) {
      placeHits = { ...placeHits, [id]: [] }
      return
    }
    try {
      const hits = await SearchPlaces(query)
      if ((placeDraft[id] ?? '').trim() !== query || placeOpen !== id) return
      placeHits = { ...placeHits, [id]: hits || [] }
    } catch {
      placeHits = { ...placeHits, [id]: [] }
    }
  }

  function pickPlace(source, hit) {
    clearTimeout(placeTimer[source.id])
    const next = { ...placeDraft }
    delete next[source.id]
    placeDraft = next
    placeHits = { ...placeHits, [source.id]: [] }
    placeOpen = null
    act(() => studio.updateSource(source.id, { config: { place: hit.label, lat: hit.lat, lon: hit.lon } }))
  }

  function commitPlace(source) {
    const draft = placeDraft[source.id]
    if (draft == null) return
    const typed = draft.trim()
    const current = (source.config.place || '').trim()
    clearTimeout(placeTimer[source.id])
    const next = { ...placeDraft }
    delete next[source.id]
    placeDraft = next
    placeHits = { ...placeHits, [source.id]: [] }
    if (placeOpen === source.id) placeOpen = null
    if (typed === current) return
    act(() => studio.updateSource(source.id, { config: { place: typed, lat: '', lon: '' } }))
  }

  function onPlaceKey(source, event) {
    if (event.key === 'Escape') {
      placeHits = { ...placeHits, [source.id]: [] }
      return
    }
    const hit = (placeHits[source.id] || [])[0]
    if (event.key === 'Enter' && hit) {
      event.preventDefault()
      pickPlace(source, hit)
    }
  }

  async function chooseProgram(source) {
    if (!desktop()) {
      onError?.(new Error('Start the desktop app to choose a program.'))
      return
    }
    try {
      const path = await PickSourceProgram()
      if (!path) return
      act(() => studio.updateSource(source.id, { config: { path } }))
    } catch (err) {
      onError?.(err)
    }
  }

  async function addFile() {
    if (typeof window === 'undefined' || !window.go?.main?.App) {
      onError?.(new Error('Start the desktop app to choose a file.'))
      return
    }
    try {
      const path = await PickSourceFile()
      if (!path) return
      const mode = path.toLowerCase().endsWith('.json') ? 'json' : 'line'
      act(() => studio.addSource('file', { path, mode, field: '' }))
    } catch (err) {
      onError?.(err)
    }
  }

  function act(fn) {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }

  const widgets = [
    { kind: 'time', label: 'Time', format: '12h', upper: false },
    { kind: 'date', label: 'Date', format: 'short', upper: true },
  ]

  function preview(kind, format, upper) {
    const { main, suffix } = widgetText({ type: kind, format }, now)
    const text = suffix ? `${main} ${suffix}` : main
    return upper ? text.toUpperCase() : text
  }

  function widgetClipOf(kind) {
    for (const track of studio.project.tracks) {
      const clip = track.clips.find((c) => c.type === kind)
      if (clip) return clip
    }
    return null
  }

  // Opening a widget turns it on if needed, then selects it so the inspector can edit it.
  function focusWidget(kind) {
    const clip = widgetClipOf(kind)
    if (!clip) {
      act(() => studio.toggleWidget(kind, true))
      return
    }
    studio.select(clip.id)
    if (studio.time < clip.start || studio.time >= clip.start + clip.duration) studio.seek(clip.start)
  }
</script>

<aside class="st-library">
<nav class="rail" aria-label="Editor tools">
  {#each [{ id: 'media', label: 'Media' }, { id: 'text', label: 'Text' }, { id: 'widget', label: 'Widget' }, { id: 'effect', label: 'Effects' }] as item (item.id)}
    <button class="rail-btn" class:on={view === item.id} use:tooltip={item.label} onclick={() => (view = item.id)} aria-pressed={view === item.id} aria-label={item.label}>
      <span class="rail-icon" aria-hidden="true">
        {#if item.id === 'media'}
          <svg viewBox="0 0 20 20"><rect x="3" y="3.5" width="14" height="13" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M5.5 14l3.4-3.6 2.3 2.3 1.8-1.8 2.2 3.1" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><circle cx="13" cy="7.3" r="1.2" fill="currentColor" /></svg>
        {:else if item.id === 'text'}
          <svg viewBox="0 0 20 20"><path d="M4.5 5h11M10 5v11" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
        {:else if item.id === 'widget'}
          <svg viewBox="0 0 20 20"><rect x="3" y="3" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="11" y="3" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="3" y="11" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="11" y="11" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /></svg>
        {:else}
          <svg viewBox="0 0 20 20"><path d="M10 2.2l1.5 4.6 4.8.2-3.8 2.9 1.4 4.6L10 11.8 6.1 14.5l1.4-4.6L3.7 7l4.8-.2z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" /></svg>
        {/if}
      </span>
    </button>
  {/each}
</nav>

<section class="bin" aria-label={view === 'media' ? 'Media' : view === 'text' ? 'Text' : view === 'effect' ? 'Effects' : 'Widgets'}>
  {#if view === 'media'}
    <div class="bin-head">
      <h3>Media</h3>
      <button class="bin-add" onclick={openAddMenu} disabled={importing} aria-label="Add media" aria-haspopup="menu" aria-expanded={!!addMenu} title="Add pictures, GIFs, or videos">
        <svg viewBox="0 0 14 14" aria-hidden="true"><path d="M7 2v10M2 7h10" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
      </button>
    </div>
    <div class="bin-tabs" role="tablist">
      {#each tabs as t (t.id)}
        <button class:on={tab === t.id} role="tab" aria-selected={tab === t.id} onclick={() => (tab = t.id)}>{t.label}</button>
      {/each}
    </div>
    {#if shown.length === 0}
      <button class="bin-empty" onclick={onImport} disabled={importing}>
        <strong>{importing ? 'Importing…' : `No ${tabs.find((t) => t.id === tab).label.toLowerCase()} yet`}</strong>
        <span>Press + or drop files anywhere in the window.</span>
      </button>
      {#if tab === 'video'}
        <button class="bin-yt" onclick={() => onYouTube?.()} disabled={importing}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="1.5" y="4.5" width="21" height="15" rx="4.5" fill="#ff0033" /><path d="M10 8.8v6.4l5.6-3.2z" fill="white" /></svg>
          Import from YouTube
        </button>
      {/if}
      <button class="bin-yt" onclick={() => onKlipy?.()} disabled={importing}>Browse KLIPY</button>
    {:else}
      <div class="bin-grid">
        {#each shown as asset (asset.id)}
          <div
            class="bin-item"
            class:used={used.has(asset.id)}
            class:broken={loadState(asset) === 'error'}
            role="button"
            tabindex="0"
            title="{asset.name} — click to add, or drag onto the timeline"
            onpointerdown={(e) => onAssetDown?.(asset, e)}
            onkeydown={(e) => e.key === 'Enter' && act(() => studio.addMedia(asset))}
          >
            {#if thumb(asset)}
              <img src={thumb(asset)} alt="" draggable="false" />
            {:else}
              <span class="bin-ph"></span>
            {/if}
            {#if length(asset)}<em class="bin-len">{length(asset)}</em>{/if}
            <button
              class="bin-x"
              aria-label="Remove {asset.name} from media"
              title="Remove from media"
              onpointerdown={(e) => e.stopPropagation()}
              onclick={(e) => {
                e.stopPropagation()
                onRemoveAsset?.(asset)
              }}>×</button
            >
          </div>
        {/each}
      </div>
    {/if}
  {:else if view === 'text'}
    <div class="bin-head"><h3>Text</h3></div>
    <div class="bin-list">
      {#each TEXT_PRESETS as preset (preset.id)}
        <button class="bin-preset" style:font-weight={preset.weight} style:font-size="{Math.min(22, preset.size / 5)}px" onclick={() => act(() => studio.addText(preset))}>
          {preset.label}
        </button>
      {/each}
    </div>
    <p class="bin-note">Text is added at the playhead. Select it on the canvas to edit it on the right.</p>
  {:else if view === 'effect'}
    <div class="bin-head"><h3>Effects</h3></div>
    <div class="bin-effects">
      {#each EFFECT_PACKS as pack (pack.id)}
        <h4 class="bin-pack">{pack.label}</h4>
        {#each pack.effects as effect (effect.id)}
          <button class="bin-preset" onclick={() => act(() => studio.addEffect(effect.id))}>{effect.label}</button>
        {/each}
      {/each}
    </div>
    <p class="bin-note">An effect covers the frame and changes whatever is on the tracks below it. Titles on a higher track stay sharp.</p>
  {:else}
    <div class="bin-head"><h3>Widgets</h3></div>
    <div class="bin-list">
      {#each widgets as w (w.kind)}
        {@const on = studio.hasWidget(w.kind)}
        <div class="bin-widget" class:on class:active={on && studio.selected?.clip?.type === w.kind}>
          <button class="bin-widget-open" onclick={() => focusWidget(w.kind)}>
            <span class="bin-widget-shot {w.kind}">{preview(w.kind, w.format, w.upper)}</span>
            <span class="bin-widget-copy"><strong>{w.label}</strong><em>{on ? 'On the canvas' : 'Off'}</em></span>
          </button>
          <button
            class="switch"
            class:on
            role="switch"
            aria-checked={on}
            aria-label="{on ? 'Turn off' : 'Turn on'} {w.label}"
            onclick={() => act(() => studio.toggleWidget(w.kind, !on))}
          ><i></i></button>
        </div>
      {/each}
    <div class="src-live">
      <div>
        <strong>Keep live</strong>
        <p>{liveDetail}</p>
      </div>
      <button
        class="switch"
        class:on={studio.project.live}
        role="switch"
        aria-checked={!!studio.project.live}
        aria-label="Keep live"
        disabled={!!liveReason && !studio.project.live}
        onclick={() => {
          if (liveReason && !studio.project.live) return
          act(() => studio.setLive(!studio.project.live))
        }}
      ><i></i></button>
    </div>
    <div class="src-add">
      {#each sourceButtons as kind (kind)}
        {@const spec = SOURCE_KINDS[kind]}
        {@const taken = uniqueKinds.has(kind) && projectSources.some((source) => source.kind === kind)}
        <button onclick={() => addSource(kind)} disabled={taken}>{taken ? spec.label : `Add ${spec.label.toLowerCase()}`}</button>
      {/each}
      {#each presets as preset (preset.preset)}
        <button onclick={() => addPreset(preset)}>Add {preset.name.toLowerCase()}</button>
      {/each}
      <button onclick={addFile}>Add file</button>
    </div>
    {#each projectSources as source (source.id)}
      {@const spec = SOURCE_KINDS[source.kind]}
      {@const state = sourceState(source)}
      {@const preset = source.config?.preset || ''}
      <div class="src-row">
        <div class="src-copy">
          <strong>{source.name}</strong>
          <em class:bad={state.bad}>{state.label}</em>
        </div>
        {#if source.kind === 'countdown'}
          <input class="src-input" value={source.config.label} aria-label="Countdown label" onchange={(e) => act(() => studio.updateSource(source.id, { config: { label: e.currentTarget.value } }))} />
          <input class="src-input" type="datetime-local" value={source.config.target} aria-label="Countdown target" onchange={(e) => act(() => studio.updateSource(source.id, { config: { target: e.currentTarget.value } }))} />
        {:else if source.kind === 'zone'}
          <input class="src-input" value={source.config.zone} placeholder="Europe/London" aria-label="Time zone" onchange={(e) => act(() => studio.updateSource(source.id, { config: { zone: e.currentTarget.value } }))} />
        {:else if source.kind === 'file'}
          <p class="src-path" title={source.config.path}>{source.config.path || 'No file'}</p>
          <div class="src-file">
            <select class="src-input" value={source.config.mode} aria-label="File mode" onchange={(e) => act(() => studio.updateSource(source.id, { config: { mode: e.currentTarget.value } }))}>
              <option value="line">First line</option>
              <option value="json">JSON field</option>
            </select>
            {#if source.config.mode === 'json'}
              <input class="src-input" value={source.config.field} placeholder="score.home" aria-label="JSON field" onchange={(e) => act(() => studio.updateSource(source.id, { config: { field: e.currentTarget.value } }))} />
            {/if}
          </div>
        {:else if source.kind === 'weather'}
          <input
            class="src-input"
            value={placeValue(source)}
            placeholder="City, or leave blank for this PC"
            aria-label="City"
            aria-expanded={placeOpen === source.id && (placeHits[source.id] || []).length > 0}
            autocomplete="off"
            spellcheck="false"
            oninput={(e) => onPlaceInput(source, e.currentTarget.value)}
            onfocus={() => (placeOpen = source.id)}
            onkeydown={(e) => onPlaceKey(source, e)}
            onblur={() => commitPlace(source)}
          />
          {#if placeOpen === source.id && (placeHits[source.id] || []).length}
            <div class="src-suggest" role="listbox">
              {#each placeHits[source.id] as hit (hit.lat + hit.lon)}
                <button type="button" role="option" onmousedown={(e) => e.preventDefault()} onclick={() => pickPlace(source, hit)}>{hit.label}</button>
              {/each}
            </div>
          {/if}
          <p class="src-path">Leave this blank to use this PC. Choose a suggestion so the right city is used.</p>
        {:else if source.kind === 'calendar' || source.kind === 'rss'}
          <input class="src-input" value={source.config.url} placeholder="https://" aria-label={source.kind === 'calendar' ? 'Calendar address' : 'Feed address'} onchange={(e) => act(() => studio.updateSource(source.id, { config: { url: e.currentTarget.value } }))} />
        {:else if source.kind === 'http' && preset === 'ha'}
          <input class="src-input" value={source.config.base} placeholder="https://home.local" aria-label="Home Assistant address" onchange={(e) => act(() => studio.updateSource(source.id, { config: { base: e.currentTarget.value } }))} />
          <input class="src-input" value={source.config.entity} placeholder="sensor.temperature" aria-label="Entity" onchange={(e) => act(() => studio.updateSource(source.id, { config: { entity: e.currentTarget.value } }))} />
          <input class="src-input" type="password" value={secretOf(source.id).token || ''} placeholder="Token" aria-label="Home Assistant token" onchange={(e) => saveSecret(source.id, { token: e.currentTarget.value })} />
        {:else if source.kind === 'http' && preset === 'github'}
          <input class="src-input" value={source.config.repo} placeholder="owner/name" aria-label="Repository" onchange={(e) => act(() => studio.updateSource(source.id, { config: { repo: e.currentTarget.value } }))} />
          <input class="src-input" type="password" value={secretOf(source.id).token || ''} placeholder="Token, optional" aria-label="GitHub token" onchange={(e) => saveSecret(source.id, { token: e.currentTarget.value })} />
        {:else if source.kind === 'http'}
          <input class="src-input" value={source.config.url} placeholder="https://" aria-label="JSON address" onchange={(e) => act(() => studio.updateSource(source.id, { config: { url: e.currentTarget.value } }))} />
          <input class="src-input" value={source.config.field} placeholder={preset === 'price' ? 'price' : preset === 'live' ? 'title' : 'score.home'} aria-label="JSON field" onchange={(e) => act(() => studio.updateSource(source.id, { config: { field: e.currentTarget.value } }))} />
          {#if preset === 'sports' || preset === 'live'}
            <input class="src-input" value={source.config.status} placeholder="status" aria-label="Status field" onchange={(e) => act(() => studio.updateSource(source.id, { config: { status: e.currentTarget.value } }))} />
          {/if}
          {#if preset === 'sports'}
            <p class="src-path">Checks every 30 seconds while the status says live, and this interval otherwise.</p>
          {/if}
          {#if preset === 'price'}
            <input class="src-input" value={source.config.change} placeholder="change" aria-label="Change field" onchange={(e) => act(() => studio.updateSource(source.id, { config: { change: e.currentTarget.value } }))} />
          {/if}
          <div class="src-file">
            <input class="src-input" value={headerName(source.id)} placeholder="Header" aria-label="Header name" onchange={(e) => saveHeader(source.id, e.currentTarget.value, secretOf(source.id).headers?.[headerName(source.id)] || '')} />
            <input class="src-input" type="password" value={secretOf(source.id).headers?.[headerName(source.id)] || ''} placeholder="Value" aria-label="Header value" onchange={(e) => saveHeader(source.id, headerName(source.id), e.currentTarget.value)} />
          </div>
        {:else if source.kind === 'mail'}
          <input class="src-input" value={source.config.host} placeholder="imap.example.com" aria-label="Mail server" onchange={(e) => act(() => studio.updateSource(source.id, { config: { host: e.currentTarget.value } }))} />
          <input class="src-input" value={secretOf(source.id).user || ''} placeholder="Account" aria-label="Mail account" onchange={(e) => saveSecret(source.id, { user: e.currentTarget.value })} />
          <input class="src-input" type="password" value={secretOf(source.id).pass || ''} placeholder="Password" aria-label="Mail password" onchange={(e) => saveSecret(source.id, { pass: e.currentTarget.value })} />
          <p class="src-path">Unread count and the latest subject. Stays off until the account is saved.</p>
        {:else if source.kind === 'command'}
          <p class="src-path">This program runs on this PC.</p>
          <p class="src-path" title={source.config.path}>{source.config.path || 'No program'}</p>
          <button class="src-action" type="button" onclick={() => chooseProgram(source)}>Choose program</button>
          <label class="src-check">
            <input type="checkbox" checked={!!source.config.confirmed} onchange={(e) => act(() => studio.updateSource(source.id, { config: { confirmed: e.currentTarget.checked } }))} />
            I understand this runs on this PC
          </label>
        {:else if source.kind === 'focus'}
          <label class="src-interval">
            <span>Minutes</span>
            <input type="number" min="1" step="1" value={source.config.minutes} aria-label="Focus minutes" onchange={(e) => act(() => studio.updateSource(source.id, { config: { minutes: Number(e.currentTarget.value) } }))} />
          </label>
          <button class="src-action" type="button" onclick={() => setFocus(source, !source.config.running)}>{source.config.running ? 'Stop' : 'Start'}</button>
        {/if}
        {#if source.kind !== 'focus'}
          <label class="src-interval">
            <span>Every</span>
            <input type="number" min={spec.min} step="1" value={source.intervalSec} aria-label="{source.name} interval" onchange={(e) => act(() => studio.updateSource(source.id, { intervalSec: Number(e.currentTarget.value) }))} />
            <span>s</span>
          </label>
        {/if}
        <button class="src-remove" onclick={() => act(() => studio.removeSource(source.id))}>Remove</button>
      </div>
    {/each}
    {#if session.lastError}<p class="bin-note bad">{session.lastError}</p>{/if}
    <p class="bin-note">Time, date, and fields update on the cooler while this app is open. Keep live leaves that running after you close the project.</p>
    </div>
  {/if}
</section>

{#if addMenu}
  <ContextMenu x={addMenu.x} y={addMenu.y} title="" items={addMenu.items} onClose={() => {
      addMenu = null
      addMenuClosed = performance.now()
    }} />
{/if}
</aside>
