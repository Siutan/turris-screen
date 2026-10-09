<script>
  import { onMount } from 'svelte'
  import { GenerateKlipyEmoji, ImportKlipy, SearchKlipy } from '../../wailsjs/go/main/App.js'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime.js'

  let { onClose, onImported } = $props()

  const kinds = [
    { id: 'gif', label: 'GIFs' },
    { id: 'clip', label: 'Clips' },
    { id: 'sticker', label: 'Stickers' },
    { id: 'meme', label: 'Memes' },
    { id: 'emoji', label: 'AI emoji' },
  ]

  let type = $state('gif')
  let query = $state('')
  let prompt = $state('')
  let items = $state([])
  let page = $state(1)
  let hasNext = $state(false)
  let loading = $state(false)
  let error = $state('')
  let adding = $state('')
  let added = $state({})
  let generating = $state(false)
  let notice = $state('')
  let searchBox = $state(null)
  let seq = 0
  let alive = true
  let shownType = ''

  function message(err) {
    if (!err) return 'Something went wrong.'
    const text = typeof err === 'string' ? err : err.message || String(err)
    return text.charAt(0).toUpperCase() + text.slice(1).replace(/([^.])$/, '$1.')
  }

  async function load(kind, q, nextPage, append) {
    const id = append ? seq : ++seq
    loading = true
    if (!append) error = ''
    try {
      const result = await SearchKlipy({ type: kind, query: q.trim(), page: nextPage })
      if (!alive || id !== seq) return
      const list = result?.items ?? []
      items = append ? [...items, ...list] : list
      page = result?.page || nextPage
      hasNext = !!result?.hasNext
    } catch (err) {
      if (!alive || id !== seq) return
      if (!append) items = []
      hasNext = false
      error = message(err)
    } finally {
      if (alive && id === seq) loading = false
    }
  }

  function more() {
    if (loading || !hasNext || adding || generating) return
    load(type, query, page + 1, true)
  }

  async function add(item) {
    if (adding || generating || !item?.fileUrl) return
    adding = item.slug || item.fileUrl
    error = ''
    try {
      const asset = await ImportKlipy(item)
      if (!alive) return
      added = { ...added, [adding]: true }
      notice = ''
      onImported?.(asset)
    } catch (err) {
      if (alive) error = message(err)
    } finally {
      if (alive) adding = ''
    }
  }

  async function generate(event) {
    event?.preventDefault()
    const text = prompt.trim()
    if (!text || generating || adding) return
    generating = true
    error = ''
    try {
      const asset = await GenerateKlipyEmoji(text)
      if (!alive) return
      prompt = ''
      const bin = asset.kind === 'video' ? 'Videos' : asset.kind === 'gif' ? 'GIFs' : 'Images'
      notice = `Added “${asset.name}” to ${bin}.`
      onImported?.(asset)
    } catch (err) {
      if (alive) error = message(err)
    } finally {
      if (alive) generating = false
    }
  }

  function onKey(event) {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      onClose?.()
    }
  }

  $effect(() => {
    const kind = type
    const q = query
    if (kind !== shownType) {
      items = []
      hasNext = false
      notice = ''
      shownType = kind
    }
    const wait = q.trim() ? 280 : 0
    const handle = setTimeout(() => load(kind, q, 1, false), wait)
    return () => clearTimeout(handle)
  })

  onMount(() => {
    searchBox?.focus()
    return () => {
      alive = false
    }
  })
</script>

<div class="st-modal">
  <div class="st-modal-card klipy-card" role="dialog" aria-modal="true" aria-labelledby="klipy-title" tabindex="-1" onkeydown={onKey}>
    <div class="klipy-head">
      <h3 id="klipy-title">KLIPY</h3>
      <button class="klipy-x" type="button" aria-label="Close" onclick={() => onClose?.()}>×</button>
    </div>

    <div class="klipy-tabs" role="tablist">
      {#each kinds as kind (kind.id)}
        <button type="button" role="tab" class:on={type === kind.id} aria-selected={type === kind.id} onclick={() => (type = kind.id)}>{kind.label}</button>
      {/each}
    </div>

    {#if type === 'emoji'}
      <form class="klipy-generate" onsubmit={generate}>
        <input bind:value={prompt} maxlength="300" placeholder="Describe an emoji to generate" disabled={generating} aria-label="Emoji description" />
        <button class="st-btn primary" type="submit" disabled={generating || adding || !prompt.trim()}>{generating ? 'Generating…' : 'Generate'}</button>
      </form>
    {/if}

    <form class="klipy-search" onsubmit={(e) => { e.preventDefault(); load(type, query, 1, false) }}>
      <input bind:this={searchBox} bind:value={query} type="search" placeholder="Search KLIPY" aria-label="Search KLIPY" spellcheck="false" autocomplete="off" />
    </form>

    <div class="klipy-grid" aria-busy={loading}>
      {#if items.length === 0 && !loading && !error}
        <p class="klipy-empty">{query.trim() ? 'Nothing matched that search.' : 'Nothing trending right now.'}</p>
      {/if}
      {#each items as item (`${item.type}:${item.slug}:${item.fileUrl}`)}
        <button
          type="button"
          class="klipy-item"
          class:alpha={item.type === 'sticker' || item.type === 'emoji'}
          class:added={added[item.slug || item.fileUrl]}
          aria-label={item.title}
          title={item.title}
          disabled={!!adding || generating}
          onclick={() => add(item)}
        >
          {#if item.preview}<img src={item.preview} alt="" draggable="false" />{/if}
          {#if adding === (item.slug || item.fileUrl)}<span class="klipy-badge">Adding…</span>{:else if added[item.slug || item.fileUrl]}<span class="klipy-badge">Added</span>{/if}
        </button>
      {/each}
      {#if loading && items.length === 0}
        <p class="klipy-empty">Loading…</p>
      {/if}
      {#if hasNext}
        <button type="button" class="klipy-more" onclick={more} disabled={loading || !!adding || generating}>{loading ? 'Loading…' : 'More'}</button>
      {/if}
    </div>

    {#if error}<p class="yt-error" role="alert">{error}</p>{/if}
    {#if generating}<p class="klipy-wait">Generating your emoji. This can take a little while.</p>{:else if notice}<p class="klipy-wait">{notice}</p>{/if}

    <p class="klipy-credit">
      <button type="button" onclick={() => BrowserOpenURL('https://klipy.com')}>Powered by KLIPY</button>
    </p>
  </div>
</div>
