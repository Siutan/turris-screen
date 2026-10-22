<script>
  import { GetSourceSecret, PickSourceFile, PickSourceProgram, SearchPlaces, SetSourceSecret } from '../../wailsjs/go/main/App.js'
  import { SOURCE_KINDS, liveItem } from '../lib/project.js'
  import { ageLabel } from '../lib/live.js'
  import { session } from '../lib/session.svelte.js'

  let { studio, onError } = $props()

  let item = $derived(liveItem(studio.liveItem))
  let copies = $derived(item ? studio.liveSources(item.id) : [])
  let taken = $derived(!!item?.unique && copies.length > 0)

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
    for (const source of copies) {
      if (!usesSecret(source) || secretLoads.has(source.id)) continue
      secretLoads.add(source.id)
      GetSourceSecret(source.id)
        .then((secret) => {
          secrets = { ...secrets, [source.id]: secret || {} }
        })
        .catch(() => {})
    }
  })

  function act(fn) {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }

  function sourceState(source) {
    session.revision
    const value = session.values[source.id]
    if (!value) return { label: 'Waiting', bad: false }
    if (value.error) return { label: value.error, bad: true }
    const age = ageLabel(value.at, Date.now())
    return { label: age ? `OK · ${age}` : 'OK', bad: false }
  }

  function instanceBase(source) {
    const config = source.config || {}
    const path = String(config.path || '')
    const file = path.split(/[\\/]/).pop()
    if (source.kind === 'weather') return config.place?.trim() || 'This PC'
    if (source.kind === 'zone') return config.zone?.trim() || 'Clock'
    if (source.kind === 'countdown') return config.label?.trim() || 'Countdown'
    if (source.kind === 'file' || source.kind === 'command') return file || source.name
    if (source.kind === 'mail') return config.host?.trim() || source.name
    if (source.kind === 'focus') return `${config.minutes || 25} min`
    if (config.preset === 'ha') return config.entity?.trim() || source.name
    if (config.preset === 'github') return config.repo?.trim() || source.name
    if (config.url) {
      try {
        return new URL(config.url).host || config.url
      } catch {
        return config.url
      }
    }
    return source.name
  }

  function instanceName(source, index) {
    const base = instanceBase(source)
    const shared = copies.filter((entry) => instanceBase(entry) === base).length > 1
    return shared ? `${base} ${index + 1}` : base
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

  function reveal(id) {
    queueMicrotask(() => document.getElementById(`live-${id}`)?.scrollIntoView({ block: 'nearest' }))
  }

  async function addCopy() {
    if (!item || taken) return
    if (item.kind === 'file') {
      if (!desktop()) {
        onError?.(new Error('Start the desktop app to choose a file.'))
        return
      }
      try {
        const path = await PickSourceFile()
        if (!path) return
        const mode = path.toLowerCase().endsWith('.json') ? 'json' : 'line'
        let source
        act(() => {
          source = studio.addSource('file', { path, mode, field: '' })
        })
        if (source) {
          studio.focusSource(source)
          reveal(source.id)
        }
      } catch (err) {
        onError?.(err)
      }
      return
    }
    const config = item.preset ? { preset: item.preset } : {}
    let source
    act(() => {
      source = studio.addSource(item.kind, config, item.extra || {})
    })
    if (source) {
      studio.focusSource(source)
      reveal(source.id)
    }
  }
</script>

{#if item}
  {@const spec = SOURCE_KINDS[item.kind]}
  <section class="ins-card">
    <div class="ins-head">
      <span>{item.label}</span>
      <button class="src-action" type="button" onclick={addCopy} disabled={taken}>{taken ? 'Added' : 'Add'}</button>
    </div>
    <div class="ins-body">
      {#if copies.length === 0}
        <p class="ins-note">Nothing here yet. Add one, then use its fields in a text clip.</p>
      {/if}
      {#each copies as source, index (source.id)}
        {@const state = sourceState(source)}
        {@const preset = source.config?.preset || ''}
        <div class="live-card" id="live-{source.id}" class:on={studio.liveSourceId === source.id}>
          <div class="src-copy">
            <strong>{instanceName(source, index)}</strong>
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
                  <button type="button" role="option" aria-selected="false" onmousedown={(e) => e.preventDefault()} onclick={() => pickPlace(source, hit)}>{hit.label}</button>
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
              <input type="number" min={spec.min} step="1" value={source.intervalSec} aria-label="{instanceName(source, index)} interval" onchange={(e) => act(() => studio.updateSource(source.id, { intervalSec: Number(e.currentTarget.value) }))} />
              <span>s</span>
            </label>
          {/if}
          <button class="src-remove" onclick={() => act(() => studio.removeSource(source.id))}>Remove</button>
        </div>
      {/each}
      {#if session.lastError}<p class="bin-note bad">{session.lastError}</p>{/if}
    </div>
  </section>
{/if}
