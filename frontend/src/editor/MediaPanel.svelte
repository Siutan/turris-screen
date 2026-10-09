<script>
  import { TEXT_PRESETS, widgetText } from '../lib/project.js'
  import ContextMenu from './ContextMenu.svelte'

  let {
    studio,
    sources,
    assets,
    revision,
    importing = false,
    tab = $bindable('image'),
    onImport,
    onYouTube,
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
    const timer = setInterval(() => (now = new Date()), 15000)
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

  function act(fn) {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }

  function preview(kind, format, upper) {
    const { main, suffix } = widgetText({ type: kind, format }, now)
    const text = suffix ? `${main} ${suffix}` : main
    return upper ? text.toUpperCase() : text
  }
</script>

<nav class="rail" aria-label="Editor tools">
  {#each [{ id: 'media', label: 'Media' }, { id: 'text', label: 'Text' }, { id: 'widget', label: 'Widget' }] as item (item.id)}
    <button class="rail-btn" class:on={view === item.id} onclick={() => (view = item.id)} aria-pressed={view === item.id}>
      <span class="rail-icon" aria-hidden="true">
        {#if item.id === 'media'}
          <svg viewBox="0 0 20 20"><rect x="3" y="3.5" width="14" height="13" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.4" /><path d="M5.5 14l3.4-3.6 2.3 2.3 1.8-1.8 2.2 3.1" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" /><circle cx="13" cy="7.3" r="1.2" fill="currentColor" /></svg>
        {:else if item.id === 'text'}
          <svg viewBox="0 0 20 20"><path d="M4.5 5h11M10 5v11" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
        {:else}
          <svg viewBox="0 0 20 20"><rect x="3" y="3" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="11" y="3" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="3" y="11" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /><rect x="11" y="11" width="6" height="6" rx="1.6" fill="none" stroke="currentColor" stroke-width="1.4" /></svg>
        {/if}
      </span>
      {item.label}
    </button>
  {/each}
</nav>

<section class="bin" aria-label={view === 'media' ? 'Media' : view === 'text' ? 'Text' : 'Widgets'}>
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
    <p class="bin-note">Text goes on its own track at the playhead. Drag it in the preview to place it.</p>
  {:else}
    <div class="bin-head"><h3>Widgets</h3></div>
    <div class="bin-list">
      <button class="bin-widget" class:on={studio.hasWidget('time')} onclick={() => studio.toggleWidget('time', !studio.hasWidget('time'))}>
        <span class="bin-widget-shot time">{preview('time', '12h')}</span>
        <span class="bin-widget-copy"><strong>Time</strong><em>{studio.hasWidget('time') ? 'On · click to remove' : 'Click to add'}</em></span>
      </button>
      <button class="bin-widget" class:on={studio.hasWidget('date')} onclick={() => studio.toggleWidget('date', !studio.hasWidget('date'))}>
        <span class="bin-widget-shot date">{preview('date', 'short', true)}</span>
        <span class="bin-widget-copy"><strong>Date</strong><em>{studio.hasWidget('date') ? 'On · click to remove' : 'Click to add'}</em></span>
      </button>
    </div>
    <p class="bin-note">Widgets are drawn into the clip when you save, so they show the time you saved it.</p>
  {/if}
</section>

{#if addMenu}
  <ContextMenu x={addMenu.x} y={addMenu.y} title="" items={addMenu.items} onClose={() => {
      addMenu = null
      addMenuClosed = performance.now()
    }} />
{/if}
