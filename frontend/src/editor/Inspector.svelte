<script>
  import { DATE_FORMATS, FILTERS, FONTS, SWATCHES, TIME_FORMATS, WEIGHTS, filterCSS, fmtBytes, isTextual } from '../lib/project.js'

  let { studio, sources, assets, revision, onError } = $props()

  let open = $state({ media: true, text: true, widgets: true, adjust: true })
  let mediaMenu = $state(false)

  let many = $derived(studio.selection.length > 1)
  let clip = $derived(many ? null : (studio.selected?.clip ?? null))
  let group = $derived.by(() => {
    if (!many) return null
    const clips = studio.selectedClips.map((f) => f.clip)
    return {
      count: clips.length,
      tracks: new Set(studio.selectedClips.map((f) => f.trackIndex)).size,
      locked: clips.every((c) => c.locked),
      merge: studio.canMerge(),
    }
  })

  function act(fn) {
    try {
      fn()
    } catch (err) {
      onError?.(err)
    }
  }
  let isMedia = $derived(clip?.type === 'media')
  let isText = $derived(isTextual(clip))
  let asset = $derived(isMedia ? assets.find((a) => a.id === clip.assetId) : null)
  let entry = $derived.by(() => {
    revision
    return isMedia ? sources.get(clip.assetId) : null
  })

  const adjustments = [
    { key: 'brightness', label: 'Brightness', min: -100, max: 100 },
    { key: 'contrast', label: 'Contrast', min: -100, max: 100 },
    { key: 'saturation', label: 'Saturation', min: -100, max: 100 },
    { key: 'blur', label: 'Blur', min: 0, max: 20 },
  ]

  function set(key, value) {
    const target = clip
    studio.edit(() => {
      target[key] = value
    }, `${target.id}:${key}`)
  }

  function adjust(key, value) {
    const target = clip
    studio.edit(() => {
      target.adjust = { ...target.adjust, [key]: value }
    }, `${target.id}:adjust:${key}`)
  }

  function resetFrame() {
    const target = clip
    mediaMenu = false
    studio.edit(() => {
      target.x = 0
      target.y = 0
      target.scale = 1
    })
  }

  function resetLook() {
    const target = clip
    mediaMenu = false
    studio.edit(() => {
      target.adjust = { brightness: 0, contrast: 0, saturation: 0, blur: 0 }
      target.filter = 'none'
    })
  }

  function fill(value, min, max) {
    return `${((value - min) / (max - min)) * 100}%`
  }

  function dims() {
    if (!entry) return ''
    const parts = [`${entry.width} × ${entry.height}`]
    if (entry.kind === 'video' || entry.kind === 'gif') parts.push(`${entry.duration.toFixed(1)}s`)
    if (asset?.size) parts.push(fmtBytes(asset.size))
    return parts.join(' · ')
  }
</script>

{#snippet head(key, title)}
  <button class="ins-head" onclick={() => (open[key] = !open[key])} aria-expanded={open[key]}>
    <span>{title}</span>
    <svg class:closed={!open[key]} viewBox="0 0 12 12" aria-hidden="true"><path d="M2.6 7.6L6 4.2l3.4 3.4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /></svg>
  </button>
{/snippet}

<aside class="ins">
  {#if group}
    <section class="ins-card ins-hint ins-group">
      <p><strong>{group.count} clips selected</strong></p>
      <p>{group.tracks > 1 ? `Across ${group.tracks} tracks.` : 'All on one track.'} Drag any of them to move the group. Shift-click to add or remove clips.</p>
      <div class="ins-actions">
        <button class="ins-action" onclick={() => act(() => studio.merge())} disabled={!group.merge.ok} title={group.merge.reason ?? 'Join into one clip'}>Merge</button>
        <button class="ins-action" onclick={() => act(() => studio.duplicate())}>Duplicate</button>
        <button class="ins-action" onclick={() => act(() => studio.center())} disabled={group.locked}>Center</button>
        <button class="ins-action" onclick={() => act(() => studio.toggleLock())}>{group.locked ? 'Unlock' : 'Lock'}</button>
        <button class="ins-action danger" onclick={() => act(() => studio.removeClips())}>Delete</button>
      </div>
      {#if !group.merge.ok}
        <p class="ins-note">{group.merge.reason}</p>
      {/if}
    </section>
  {:else if !clip}
    <section class="ins-card ins-hint">
      <p><strong>Nothing selected</strong></p>
      <p>Pick a clip on the timeline or click something in the preview to edit it.</p>
    </section>
  {/if}

  {#if isMedia}
    <section class="ins-card">
      {@render head('media', 'Media')}
      {#if open.media}
        <div class="ins-body">
          <div class="ins-file">
            {#if entry?.poster}
              <img src={entry.poster} alt="" />
            {:else}
              <span class="ins-file-ph"></span>
            {/if}
            <div class="ins-file-copy">
              <strong>{clip.name || asset?.name || 'Media'}</strong>
              <span>{dims()}</span>
            </div>
            <div class="ins-more-wrap">
              <button class="ins-more" onclick={() => (mediaMenu = !mediaMenu)} aria-label="Media options" aria-expanded={mediaMenu}>
                <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="3.2" cy="8" r="1.15" fill="currentColor" /><circle cx="8" cy="8" r="1.15" fill="currentColor" /><circle cx="12.8" cy="8" r="1.15" fill="currentColor" /></svg>
              </button>
              {#if mediaMenu}
                <div class="menu ins-menu">
                  <button onclick={resetFrame}>Reset position and zoom</button>
                  <button onclick={resetLook}>Reset adjustments</button>
                  <button class="danger" onclick={() => { mediaMenu = false; studio.removeClip() }}>Remove from timeline</button>
                </div>
              {/if}
            </div>
          </div>
          <div class="ins-row">
            <span>Frame</span>
            <div class="ins-seg">
              <button class:on={clip.fit !== 'contain'} onclick={() => set('fit', 'cover')}>Fill</button>
              <button class:on={clip.fit === 'contain'} onclick={() => set('fit', 'contain')}>Fit</button>
            </div>
          </div>
          <label class="ins-slider">
            <span>Zoom</span>
            <input type="range" min="0.1" max="4" step="0.01" value={clip.scale} style:--fill={fill(clip.scale, 0.1, 4)} oninput={(e) => set('scale', Number(e.currentTarget.value))} />
            <em>{Math.round(clip.scale * 100)}%</em>
          </label>
        </div>
      {/if}
    </section>
  {/if}

  {#if isText}
    <section class="ins-card">
      {@render head('text', clip.type === 'text' ? 'Text' : clip.type === 'time' ? 'Time' : 'Date')}
      {#if open.text}
        <div class="ins-body">
          {#if clip.type === 'text'}
            <textarea rows="2" value={clip.text} oninput={(e) => set('text', e.currentTarget.value)} aria-label="Text" spellcheck="false"></textarea>
          {:else}
            <select class="ins-select wide" value={clip.format} onchange={(e) => set('format', e.currentTarget.value)} aria-label="Format">
              {#each clip.type === 'time' ? TIME_FORMATS : DATE_FORMATS as f (f.id)}
                <option value={f.id}>{f.label}</option>
              {/each}
            </select>
            <p class="ins-note">The cooler shows the {clip.type} from when you save. It does not tick on its own.</p>
          {/if}
          <div class="ins-row">
            <span>Font</span>
            <div class="ins-pair">
              <select class="ins-select" value={clip.font} onchange={(e) => set('font', e.currentTarget.value)} aria-label="Font">
                {#each FONTS as f (f)}
                  <option value={f}>{f}</option>
                {/each}
              </select>
              <select class="ins-select" value={clip.weight} onchange={(e) => set('weight', Number(e.currentTarget.value))} aria-label="Weight">
                {#each WEIGHTS as w (w.value)}
                  <option value={w.value}>{w.label}</option>
                {/each}
              </select>
            </div>
          </div>
          <label class="ins-slider">
            <span>Size</span>
            <input type="range" min="12" max="400" step="1" value={clip.size} style:--fill={fill(clip.size, 12, 400)} oninput={(e) => set('size', Number(e.currentTarget.value))} />
            <em>{clip.size}</em>
          </label>
          <label class="ins-slider">
            <span>Spacing</span>
            <input type="range" min="-20" max="40" step="1" value={clip.spacing} style:--fill={fill(clip.spacing, -20, 40)} oninput={(e) => set('spacing', Number(e.currentTarget.value))} />
            <em>{clip.spacing}</em>
          </label>
          <div class="ins-row">
            <span>Align</span>
            <div class="ins-seg">
              {#each ['left', 'center', 'right'] as a (a)}
                <button class:on={clip.align === a} onclick={() => set('align', a)} aria-label="Align {a}">
                  <svg viewBox="0 0 16 16" aria-hidden="true">
                    {#if a === 'left'}<path d="M3 4h10M3 8h6M3 12h8" />{:else if a === 'center'}<path d="M3 4h10M5 8h6M4 12h8" />{:else}<path d="M3 4h10M7 8h6M5 12h8" />{/if}
                  </svg>
                </button>
              {/each}
              <button class:on={clip.upper} onclick={() => set('upper', !clip.upper)} aria-label="All caps" title="All caps">AA</button>
            </div>
          </div>
          <p class="ins-label">Text Color</p>
          <div class="ins-swatches">
            {#each SWATCHES as c (c)}
              <button class="sw" class:on={clip.color?.toLowerCase() === c} style:background={c} onclick={() => set('color', c)} aria-label="Color {c}"></button>
            {/each}
            <label class="sw custom" class:on={!SWATCHES.includes(clip.color?.toLowerCase())} title="Custom color">
              <input type="color" value={clip.color} oninput={(e) => set('color', e.currentTarget.value)} aria-label="Custom color" />
            </label>
          </div>
        </div>
      {/if}
    </section>
  {/if}

  <section class="ins-card">
    {@render head('widgets', 'Widgets')}
    {#if open.widgets}
      <div class="ins-body">
        {#each [{ kind: 'time', label: 'Time Widget' }, { kind: 'date', label: 'Date Widget' }] as w (w.kind)}
          <div class="ins-toggle">
            <span class="ins-toggle-icon" aria-hidden="true">
              {#if w.kind === 'time'}
                <svg viewBox="0 0 16 16"><circle cx="8" cy="8" r="5.6" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M8 5v3.2l2.1 1.3" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
              {:else}
                <svg viewBox="0 0 16 16"><rect x="2.5" y="3.5" width="11" height="10" rx="2" fill="none" stroke="currentColor" stroke-width="1.3" /><path d="M2.5 6.6h11M5.5 2.3v2.4M10.5 2.3v2.4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" /></svg>
              {/if}
            </span>
            <span>{w.label}</span>
            <button
              class="switch"
              class:on={studio.hasWidget(w.kind)}
              role="switch"
              aria-checked={studio.hasWidget(w.kind)}
              aria-label={w.label}
              onclick={() => studio.toggleWidget(w.kind, !studio.hasWidget(w.kind))}
            ><i></i></button>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  {#if isMedia}
    <section class="ins-card">
      {@render head('adjust', 'Adjustments')}
      {#if open.adjust}
        <div class="ins-body">
          {#each adjustments as a (a.key)}
            <label class="ins-slider">
              <span>{a.label}</span>
              <input
                type="range"
                min={a.min}
                max={a.max}
                step="1"
                value={clip.adjust?.[a.key] ?? 0}
                style:--fill={fill(clip.adjust?.[a.key] ?? 0, a.min, a.max)}
                oninput={(e) => adjust(a.key, Number(e.currentTarget.value))}
                ondblclick={() => adjust(a.key, 0)}
              />
              <em>{clip.adjust?.[a.key] ?? 0}</em>
            </label>
          {/each}
          <p class="ins-label">Filters</p>
          <div class="ins-filters">
            {#each FILTERS as f (f.id)}
              <button class="flt" class:on={(clip.filter || 'none') === f.id} onclick={() => set('filter', f.id)}>
                <span class="flt-shot">
                  {#if entry?.poster}
                    <img src={entry.poster} alt="" style:filter={filterCSS({ ...clip, filter: f.id }, 0.25)} />
                  {/if}
                </span>
                <em>{f.label}</em>
              </button>
            {/each}
          </div>
        </div>
      {/if}
    </section>
  {/if}
</aside>
