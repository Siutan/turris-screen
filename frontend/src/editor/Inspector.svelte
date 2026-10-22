<script>
  import LivePanel from './LivePanel.svelte'
  import SourceRange from './SourceRange.svelte'
  import { CLOCK_FIELDS, DATE_FORMATS, FILTER_LIBRARY, FONTS, SWATCHES, TIME_FORMATS, WEIGHTS, coolerFont, effectControls, effectDefaults, effectMeta, effectSettings, filterCSS, fmtBytes, isTextual, sourceFields } from '../lib/project.js'

  let { studio, sources, assets, revision, onError } = $props()

  let open = $state({ media: true, source: true, text: true, adjust: true, effect: true })
  let mediaMenu = $state(false)
  let fieldMenu = $state(false)
  let textEl = $state(null)

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
  let isEffect = $derived(clip?.type === 'effect')
  let effectInfo = $derived(isEffect ? effectMeta(clip.effect) : null)
  let effectOpts = $derived(isEffect ? effectControls(clip.effect) : [])
  let effectValues = $derived(isEffect ? effectSettings(clip) : null)
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

  function insertField(token) {
    const target = clip
    if (!target || target.type !== 'text') return
    fieldMenu = false
    const value = target.text || ''
    const start = textEl?.selectionStart ?? value.length
    const end = textEl?.selectionEnd ?? value.length
    const next = `${value.slice(0, start)}{{${token}}}${value.slice(end)}`
    studio.edit(() => {
      target.text = next
    })
  }

  function set(key, value) {
    const target = clip
    studio.edit(() => {
      target[key] = value
    }, `${target.id}:${key}`)
  }

  function setEffect(key, value) {
    const target = clip
    const next = { ...effectSettings(target), [key]: value }
    studio.edit(() => {
      target.settings = next
      delete target.amount
    }, `${target.id}:fx:${key}`)
  }

  function resetEffect(key) {
    const target = clip
    const defaults = effectDefaults(target.effect)
    const next = key ? { ...effectSettings(target), [key]: defaults[key] } : defaults
    studio.edit(() => {
      target.settings = next
      delete target.amount
    }, key ? `${target.id}:fx:${key}` : '')
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
  {:else if studio.liveItem}
    <LivePanel {studio} {onError} />
  {:else if !clip}
    <section class="ins-card ins-hint">
      <p><strong>Nothing selected</strong></p>
      <p>Select something on the canvas or timeline. Its options show up here.</p>
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

  {#if isMedia && entry?.kind === 'video'}
    <section class="ins-card">
      {@render head('source', 'Video section')}
      {#if open.source}
        <div class="ins-body">
          <SourceRange {studio} {sources} {clip} {revision} />
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
            <textarea rows="2" bind:this={textEl} value={clip.text} oninput={(e) => set('text', e.currentTarget.value)} aria-label="Text" spellcheck="false"></textarea>
            <div class="ins-more-wrap">
              <button class="ins-action" onclick={() => (fieldMenu = !fieldMenu)} aria-expanded={fieldMenu}>Insert field</button>
              {#if fieldMenu}
                <div class="menu ins-menu field-menu">
                  {#each CLOCK_FIELDS as field (field.id)}
                    <button onclick={() => insertField(`clock.${field.id}`)}>{field.label}</button>
                  {/each}
                  {#each studio.project.sources || [] as source (source.id)}
                    {#each sourceFields(source) as field (field.id)}
                      <button onclick={() => insertField(`${source.id}.${field.id}`)}>{source.name} · {field.label}</button>
                    {/each}
                  {/each}
                </div>
              {/if}
            </div>
          {:else}
            <select class="ins-select wide" value={clip.format} onchange={(e) => set('format', e.currentTarget.value)} aria-label="Format">
              {#each clip.type === 'time' ? TIME_FORMATS : DATE_FORMATS as f (f.id)}
                <option value={f.id}>{f.label}</option>
              {/each}
            </select>
            <p class="ins-note">This updates on the cooler while the app is open. Save again so the picture is not also printing the old text.</p>
          {/if}
          <div class="ins-row">
            <span>Font</span>
            <div class="ins-pair">
              <select class="ins-select" value={coolerFont(clip)} onchange={(e) => set('font', e.currentTarget.value)} aria-label="Font">
                {#each FONTS as f (f.id)}
                  <option value={f.id}>{f.label}</option>
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

  {#if isEffect}
    <section class="ins-card">
      {@render head('effect', effectInfo?.packLabel || 'Effect')}
      {#if open.effect}
        <div class="ins-body">
          <div class="ins-file">
            <div class="ins-file-copy">
              <strong>{effectInfo?.label || clip.name || 'Effect'}</strong>
              <span>Covers the whole frame</span>
            </div>
          </div>
          {#each effectOpts as opt (opt.id)}
            <label class="ins-slider fx">
              <span>{opt.label}</span>
              <input
                type="range"
                min="0"
                max="100"
                step="1"
                value={effectValues?.[opt.id] ?? 0}
                style:--fill={fill(effectValues?.[opt.id] ?? 0, 0, 100)}
                aria-label={opt.label}
                oninput={(e) => setEffect(opt.id, Number(e.currentTarget.value))}
                ondblclick={() => resetEffect(opt.id)}
              />
              <em>{effectValues?.[opt.id] ?? 0}</em>
            </label>
          {/each}
          <button class="ins-reset" type="button" onclick={() => resetEffect()}>Reset to defaults</button>
          <p class="ins-note">Zero turns that part off. Speed 50 is the original pace, and 100 is twice as fast. Double-click a slider to reset it. Clicks on the canvas pass through.</p>
        </div>
      {/if}
    </section>
  {/if}

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
          <div class="flt-library">
            {#each FILTER_LIBRARY as group (group.id)}
              <div class="flt-group">
                <p class="ins-label">{group.label}</p>
                <div class="ins-filters">
                  {#each group.filters as f (f.id)}
                    <button class="flt" class:on={(clip.filter || 'none') === f.id} title={f.label} onclick={() => set('filter', f.id)}>
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
            {/each}
          </div>
        </div>
      {/if}
    </section>
  {/if}
</aside>
