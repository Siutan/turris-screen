<script>
  import { onMount } from 'svelte'
  import { ActiveName, Apply, Connect, GetLibrary, Job, Remove, RestartCooler, ShowImage } from '../wailsjs/go/main/App.js'
  import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime.js'

  let status = $state({
    connected: false,
    serial: '',
    detail: 'Looking for the cooler…',
  })
  let library = $state([])
  let activeName = $state('')
  let error = $state('')
  let working = $state(false)
  let job = $state({ active: false, label: '', percent: 0 })
  let hideTimer
  let jobWatch = 0
  let view = $state('home')
  let filter = $state('all')
  let query = $state('')
  let sort = $state('recent')
  let menu = $state(null)
  let title = $state('')
  let zoom = $state(1)
  let source = $state(null)
  let objectURL = $state('')
  let offsetX = $state(0)
  let offsetY = $state(0)
  let frameW = $state(0)
  let frameH = $state(0)
  let dragging = false
  let dragX = 0
  let dragY = 0
  let originX = 0
  let originY = 0

  let frame = $state(null)
  let picker = $state(null)

  const filters = [
    { id: 'all', label: 'All' },
    { id: 'image', label: 'Images' },
    { id: 'video', label: 'Videos' },
    { id: 'gif', label: 'GIFs' },
    { id: 'widget', label: 'Widgets' },
  ]

  function cover() {
    if (!source || !frameW || !frameH) return 1
    return Math.max(frameW / source.naturalWidth, frameH / source.naturalHeight)
  }

  function message(err) {
    if (!err) return 'Something went wrong'
    if (typeof err === 'string') return err
    return err.message || String(err)
  }

  function scaled() {
    return cover() * Number(zoom)
  }

  function clamp() {
    if (!source) return
    const w = source.naturalWidth * scaled()
    const h = source.naturalHeight * scaled()
    const minX = Math.min(0, frameW - w)
    const minY = Math.min(0, frameH - h)
    offsetX = Math.min(0, Math.max(minX, offsetX))
    offsetY = Math.min(0, Math.max(minY, offsetY))
  }

  function centerImage() {
    if (!source) return
    offsetX = (frameW - source.naturalWidth * scaled()) / 2
    offsetY = (frameH - source.naturalHeight * scaled()) / 2
    clamp()
  }

  function measure() {
    if (!frame) return
    frameW = frame.clientWidth
    frameH = frame.clientHeight
  }

  function loadFile(file) {
    if (!file || !file.type.startsWith('image/')) {
      error = 'Choose a picture file.'
      return
    }
    error = ''
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      if (objectURL) URL.revokeObjectURL(objectURL)
      objectURL = url
      source = img
      title = file.name.replace(/\.[^.]+$/, '')
      zoom = 1
      requestAnimationFrame(() => {
        measure()
        centerImage()
      })
    }
    img.onerror = () => {
      URL.revokeObjectURL(url)
      error = 'That picture could not be opened.'
    }
    img.src = url
  }

  function exportPNG() {
    const canvas = document.createElement('canvas')
    canvas.width = 1280
    canvas.height = 720
    const ctx = canvas.getContext('2d')
    const s = scaled()
    ctx.drawImage(
      source,
      -offsetX / s,
      -offsetY / s,
      frameW / s,
      frameH / s,
      0,
      0,
      1280,
      720,
    )
    return canvas.toDataURL('image/png')
  }

  function hasBridge() {
    return typeof window !== 'undefined' && !!window.go?.main?.App
  }

  function kindOf(item) {
    return item.kind || 'image'
  }

  function kindLabel(kind) {
    if (kind === 'gif') return 'GIF'
    if (kind === 'video') return 'Video'
    if (kind === 'widget') return 'Widget'
    return 'Image'
  }

  async function settle() {
    if (!hasBridge()) return
    try {
      status = await Connect()
      const shown = activeName
      await refreshLibrary()
      await refreshActive()
      if (!activeName && shown) activeName = shown
    } catch (err) {
      if (!error) error = message(err)
    }
  }

  function editedLabel(created) {
    const match = String(created || '').match(/^(\d{1,2}) ([A-Za-z]{3}) (\d{4})/)
    if (!match) return created ? `Edited ${created}` : 'Edited'
    return `Edited ${match[2]} ${Number(match[1])}, ${match[3]}`
  }

  function clearDraft() {
    if (objectURL) URL.revokeObjectURL(objectURL)
    objectURL = ''
    source = null
    title = ''
    zoom = 1
    offsetX = 0
    offsetY = 0
  }

  async function refreshLibrary() {
    if (!hasBridge()) return
    library = (await GetLibrary()) ?? []
  }

  async function refreshActive() {
    if (!hasBridge() || !status.connected) {
      activeName = ''
      return
    }
    try {
      activeName = (await ActiveName()) || ''
    } catch {
      activeName = ''
    }
  }

  async function connect() {
    error = ''
    if (!hasBridge()) {
      status = {
        connected: false,
        serial: '',
        detail: 'Start the desktop app to reach the cooler.',
      }
      return
    }
    status = await Connect()
    if (status.connected) await refreshActive()
  }

  async function show() {
    if (!source || working) return
    if (!hasBridge()) {
      error = 'Start the desktop app to send this picture to the cooler.'
      return
    }
    working = true
    error = ''
    applyProgress({ active: true, label: 'Encoding image', percent: 4 })
    const stopWatch = watchJob()
    try {
      const item = await ShowImage(title, exportPNG())
      clearDraft()
      view = 'home'
      if (item?.remoteName) activeName = item.remoteName
    } catch (err) {
      error = message(err)
      applyProgress({ active: false })
    } finally {
      stopWatch()
      working = false
      await settle()
    }
  }

  async function applyItem(id) {
    menu = null
    if (working) return
    working = true
    error = ''
    applyProgress({ active: true, label: 'Sending to the cooler', percent: 8 })
    const stopWatch = watchJob()
    try {
      await Apply(id)
    } catch (err) {
      error = message(err)
      applyProgress({ active: false })
    } finally {
      stopWatch()
      working = false
      await settle()
    }
  }

  async function restartCooler() {
    menu = null
    if (working || !hasBridge()) return
    working = true
    error = ''
    applyProgress({ active: true, label: 'Restarting the cooler', percent: 30 })
    try {
      status = await RestartCooler()
      await refreshActive()
    } catch (err) {
      error = message(err)
      status = await Connect()
    } finally {
      applyProgress({ active: false })
      working = false
    }
  }

  async function removeItem(id) {
    menu = null
    if (working) return
    error = ''
    try {
      await Remove(id)
      await refreshLibrary()
      await refreshActive()
    } catch (err) {
      error = message(err)
    }
  }

  function openEditor() {
    menu = null
    error = ''
    view = 'edit'
  }

  function goHome() {
    error = ''
    view = 'home'
  }

  function onDevice(event) {
    event.stopPropagation()
    if (working) return
    if (!status.connected) {
      menu = null
      connect().catch((err) => {
        error = message(err)
      })
      return
    }
    menu = menu?.type === 'device' ? null : { type: 'device' }
  }

  function toggleCardMenu(id, event) {
    event.stopPropagation()
    menu = menu?.type === 'card' && menu.id === id ? null : { type: 'card', id }
  }

  function toggleSort(event) {
    event.stopPropagation()
    menu = menu?.type === 'sort' ? null : { type: 'sort' }
  }

  function chooseSort(next, event) {
    event.stopPropagation()
    sort = next
    menu = null
  }

  function pointerDown(event) {
    if (!source) return
    dragging = true
    dragX = event.clientX
    dragY = event.clientY
    originX = offsetX
    originY = offsetY
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  function pointerMove(event) {
    if (!dragging) return
    offsetX = originX + (event.clientX - dragX)
    offsetY = originY + (event.clientY - dragY)
    clamp()
  }

  function pointerUp() {
    dragging = false
  }

  function applyProgress(update) {
    clearTimeout(hideTimer)
    const payload = Array.isArray(update) ? update[0] : update
    if (!payload?.active) {
      job = { active: false, label: '', percent: 0 }
      return
    }
    const percent = Math.max(0, Math.min(100, Number(payload.percent) || 0))
    job = { active: true, label: payload.label || '', percent }
    if (percent >= 100) {
      hideTimer = setTimeout(() => {
        job = { active: false, label: '', percent: 0 }
      }, 1400)
    }
  }

  function watchJob() {
    const watch = ++jobWatch
    const timer = setInterval(async () => {
      if (!hasBridge() || watch !== jobWatch) return
      try {
        const update = await Job()
        if (watch !== jobWatch || !update?.active) return
        applyProgress(update)
      } catch {
        /* the desktop bridge reports the same state through events */
      }
    }, 200)
    return () => {
      if (jobWatch === watch) jobWatch++
      clearInterval(timer)
    }
  }

  function windowAction(fn) {
    try {
      fn()
    } catch {
      /* preview outside the desktop window */
    }
  }

  function onTitleDblClick(event) {
    if (event.target.closest('.nodrag')) return
    windowAction(WindowToggleMaximise)
  }

  function onWheel(event) {
    if (!source) return
    event.preventDefault()
    const next = zoom * (event.deltaY < 0 ? 1.08 : 0.92)
    zoom = Math.min(4, Math.max(1, next))
    clamp()
  }

  let activeItem = $derived(library.find((item) => item.remoteName && item.remoteName === activeName) ?? null)

  let hero = $derived.by(() => {
    if (activeItem) {
      return {
        kicker: 'Now showing',
        title: activeItem.name,
        meta: `${kindLabel(kindOf(activeItem))} · ${editedLabel(activeItem.created)}`,
        thumb: activeItem.thumb,
      }
    }
    if (!status.connected) {
      return {
        kicker: 'On the cooler',
        title: 'Not connected',
        meta: status.detail || 'Connect to see what the screen is showing.',
        thumb: '',
      }
    }
    if (activeName) {
      return {
        kicker: 'On the cooler',
        title: 'Something else is showing',
        meta: 'The screen is playing media from outside your projects.',
        thumb: '',
      }
    }
    return {
      kicker: 'On the cooler',
      title: 'Nothing showing',
      meta: 'Send a project and it will stay on the screen.',
      thumb: '',
    }
  })

  let visible = $derived.by(() => {
    const q = query.trim().toLowerCase()
    let items = library.filter((item) => {
      if (filter !== 'all' && kindOf(item) !== filter) return false
      if (q && !String(item.name || '').toLowerCase().includes(q)) return false
      return true
    })
    if (sort === 'name') {
      items = [...items].sort((a, b) => String(a.name).localeCompare(String(b.name)))
    }
    return items
  })

  let emptyCopy = $derived.by(() => {
    if (query.trim()) return 'No projects match that search.'
    if (filter === 'video') return 'No videos yet.'
    if (filter === 'gif') return 'No GIFs yet.'
    if (filter === 'widget') return 'No widgets yet.'
    if (filter === 'image') return 'No images yet.'
    return 'Projects you create will show up here.'
  })

  let sortLabel = $derived(sort === 'name' ? 'Name' : 'Recently Edited')

  onMount(() => {
    let stop = () => {}
    try {
      stop = EventsOn('progress', (update) => {
        applyProgress(update)
      })
    } catch {
      stop = () => {}
    }
    const closeMenu = (event) => {
      if (event.target instanceof Element && event.target.closest('.menu')) return
      menu = null
    }
    const onKey = (event) => {
      if (event.key === 'Escape') menu = null
    }
    document.addEventListener('click', closeMenu)
    document.addEventListener('keydown', onKey)
    connect()
      .then(refreshLibrary)
      .catch((err) => {
        error = message(err)
      })
    return () => {
      if (typeof stop === 'function') stop()
      document.removeEventListener('click', closeMenu)
      document.removeEventListener('keydown', onKey)
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  })

  $effect(() => {
    const node = frame
    if (!node) return
    const observer = new ResizeObserver(() => {
      const prevW = frameW
      const prevH = frameH
      measure()
      if (source && (prevW === 0 || prevH === 0)) centerImage()
      else clamp()
    })
    observer.observe(node)
    node.addEventListener('wheel', onWheel, { passive: false })
    const tick = requestAnimationFrame(() => {
      const prevW = frameW
      const prevH = frameH
      measure()
      if (source && (prevW === 0 || prevH === 0)) centerImage()
      else clamp()
    })
    return () => {
      cancelAnimationFrame(tick)
      observer.disconnect()
      node.removeEventListener('wheel', onWheel)
    }
  })
</script>

<div class="app">
  <header class="chrome" role="toolbar" aria-label="Window" tabindex="-1" ondblclick={onTitleDblClick}>
    {#if job.active}
      <p class="job">{job.label} <em>{Math.round(job.percent)}%</em></p>
      <div class="meter" aria-hidden="true"><span style:width="{job.percent}%"></span></div>
    {/if}
    <div class="spacer"></div>
    <div class="wins nodrag">
      <button class="winbtn" aria-label="Minimise" onclick={() => windowAction(WindowMinimise)}>−</button>
      <button class="winbtn" aria-label="Maximise" onclick={() => windowAction(WindowToggleMaximise)}>□</button>
      <button class="winbtn close" aria-label="Close" onclick={() => windowAction(Quit)}>×</button>
    </div>
  </header>

  <div class="shell">
    {#if view === 'home'}
      <div class="topbar" role="banner" tabindex="-1" ondblclick={onTitleDblClick}>
        <div class="brand">
          <svg class="mark" viewBox="0 0 32 32" aria-hidden="true">
            <g fill="currentColor">
              <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(0 16 16)" />
              <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(120 16 16)" />
              <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(240 16 16)" />
              <circle cx="16" cy="16" r="2.5" />
            </g>
          </svg>
          Turris Studio
        </div>
        <div class="device-wrap nodrag">
          <button
            class="device"
            onclick={onDevice}
            disabled={working && !status.connected}
            aria-expanded={menu?.type === 'device'}
            title={status.connected ? status.serial || 'Connected' : status.detail}
          >
            <span class="device-icon" aria-hidden="true">
              <svg viewBox="0 0 36 28">
                <rect x="1.2" y="2" width="33.6" height="20" rx="4" fill="#f4f5f7" stroke="#d5d8de" />
                <rect x="4.2" y="5" width="27.6" height="12.2" rx="2" fill="#d7dbe2" />
                <path d="M13 25.2h10" stroke="#c5c9d1" stroke-width="1.8" stroke-linecap="round" />
              </svg>
            </span>
            <span class="device-copy">
              <strong>TRYX TURRIS 620</strong>
              <span class="state">
                <i class="dot" class:on={status.connected}></i>
                <span class="state-text">
                  {status.connected ? 'Connected' : 'Not connected'}
                </span>
              </span>
            </span>
          </button>
          {#if menu?.type === 'device'}
            <div class="menu device-menu">
              {#if status.serial}
                <p class="hint">{status.serial}</p>
              {/if}
              <button onclick={restartCooler} disabled={working} title="Restarts the cooler’s screen if the background stops changing.">
                Restart cooler
              </button>
            </div>
          {/if}
        </div>
      </div>

      {#if error}
        <p class="banner">{error}</p>
      {/if}

      <section class="hero" aria-label="On the cooler">
        <div class="hero-copy">
          <p class="kicker">{hero.kicker}</p>
          <h2>{hero.title}</h2>
          <p class="meta">{hero.meta}</p>
        </div>
        <div class="hero-visual">
          {#if hero.thumb}
            <img class="preview" alt="" src={hero.thumb} />
          {:else}
            <div class="preview placeholder" aria-hidden="true">
              <svg viewBox="0 0 48 32">
                <rect x="1" y="1" width="46" height="30" rx="4" fill="none" stroke="currentColor" stroke-width="1.4" />
                <path d="M8 23l8-8 6 6 6-7 12 9" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" />
              </svg>
            </div>
          {/if}
        </div>
      </section>

      <section class="projects" aria-label="Your projects">
        <div class="projects-head">
          <div>
            <h2>Your Projects</h2>
            <p>Manage and edit your display content.</p>
          </div>
          <div class="projects-tools">
            <label class="search">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <circle cx="7" cy="7" r="4.25" fill="none" stroke="currentColor" stroke-width="1.4" />
                <path d="M10.4 10.4L13.5 13.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
              </svg>
              <input
                type="search"
                placeholder="Search projects..."
                bind:value={query}
                aria-label="Search projects"
                autocomplete="off"
                spellcheck="false"
              />
            </label>
            <button class="add" onclick={openEditor} disabled={working}>
              <svg viewBox="0 0 14 14" aria-hidden="true">
                <path d="M7 1.6v10.8M1.6 7h10.8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
              </svg>
              Add New
            </button>
          </div>
        </div>

        <div class="toolbar">
          <div class="chips" role="tablist" aria-label="Project type">
            {#each filters as item (item.id)}
              <button
                class="chip"
                class:on={filter === item.id}
                role="tab"
                aria-selected={filter === item.id}
                onclick={() => (filter = item.id)}
              >{item.label}</button>
            {/each}
          </div>
          <div class="sort">
            <button class="sort-btn" onclick={toggleSort} aria-expanded={menu?.type === 'sort'}>
              {sortLabel}
              <svg viewBox="0 0 12 12" aria-hidden="true">
                <path d="M2.4 4.4L6 8l3.6-3.6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </button>
            {#if menu?.type === 'sort'}
              <div class="menu sort-menu">
                <button class:on={sort === 'recent'} onclick={(event) => chooseSort('recent', event)}>Recently Edited</button>
                <button class:on={sort === 'name'} onclick={(event) => chooseSort('name', event)}>Name</button>
              </div>
            {/if}
          </div>
        </div>

        {#if visible.length === 0}
          <p class="empty-note">{emptyCopy}</p>
        {:else}
          <div class="grid">
            {#each visible as item (item.id)}
              <article class="card">
                <div class="shot">
                  {#if item.thumb}
                    <img alt="" src={item.thumb} />
                  {:else}
                    <div class="shot-fallback"></div>
                  {/if}
                  <span class="badge">{kindLabel(kindOf(item))}</span>
                  <button class="more" aria-label="Actions for {item.name}" aria-expanded={menu?.type === 'card' && menu.id === item.id} onclick={(event) => toggleCardMenu(item.id, event)}>
                    <svg viewBox="0 0 16 16" aria-hidden="true">
                      <circle cx="3.2" cy="8" r="1.15" fill="currentColor" />
                      <circle cx="8" cy="8" r="1.15" fill="currentColor" />
                      <circle cx="12.8" cy="8" r="1.15" fill="currentColor" />
                    </svg>
                  </button>
                </div>
                {#if menu?.type === 'card' && menu.id === item.id}
                  <div class="menu card-menu">
                    <button onclick={() => applyItem(item.id)} disabled={working || !status.connected}>Show on cooler</button>
                    <button class="danger" onclick={() => removeItem(item.id)} disabled={working}>Remove</button>
                  </div>
                {/if}
                <h3>{item.name}</h3>
                <p>{editedLabel(item.created)}</p>
              </article>
            {/each}
          </div>
        {/if}
      </section>
    {:else}
      <div class="editor">
        <div class="editor-head">
          <button class="back" onclick={goHome}>
            <svg viewBox="0 0 12 12" aria-hidden="true">
              <path d="M7.6 2.4L4 6l3.6 3.6" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            Projects
          </button>
          <div>
            <h2>New picture</h2>
            <p>Crop it to the cooler’s screen, then send it.</p>
          </div>
        </div>

        <div
          class="frame"
          class:ready={!!source}
          role="application"
          aria-label="Crop to the cooler screen"
          bind:this={frame}
          onpointerdown={pointerDown}
          onpointermove={pointerMove}
          onpointerup={pointerUp}
          onpointercancel={pointerUp}
          ondragover={(event) => event.preventDefault()}
          ondrop={(event) => {
            event.preventDefault()
            loadFile(event.dataTransfer?.files?.[0])
          }}
        >
          {#if source}
            <img
              alt=""
              src={objectURL}
              draggable="false"
              style:width="{source.naturalWidth * scaled()}px"
              style:height="{source.naturalHeight * scaled()}px"
              style:transform="translate({offsetX}px, {offsetY}px)"
            />
          {:else}
            <div class="drop">
              <strong>Drop a picture here</strong>
              <span>This frame is the cooler’s 1280×720 screen.</span>
            </div>
          {/if}
        </div>

        <div class="controls">
          <label class="zoom">
            Zoom
            <input
              type="range"
              min="1"
              max="4"
              step="0.01"
              value={zoom}
              disabled={!source}
              oninput={(event) => {
                zoom = Number(event.currentTarget.value)
                clamp()
              }}
            />
          </label>
          <div class="actions">
            <button class="btn" onclick={() => picker?.click()}>Choose picture</button>
            <button class="btn" onclick={centerImage} disabled={!source}>Reset crop</button>
          </div>
          <input
            type="text"
            placeholder="Name in the library"
            bind:value={title}
            disabled={!source}
          />
          <button class="add send" onclick={show} disabled={!source || working || !status.connected}>
            Show on cooler
          </button>
        </div>
        <input
          bind:this={picker}
          type="file"
          accept="image/*"
          hidden
          onchange={(event) => {
            loadFile(event.currentTarget.files?.[0])
            event.currentTarget.value = ''
          }}
        />
        {#if error}
          <p class="banner">{error}</p>
        {:else}
          <p class="note">Drag to move the picture. Scroll or use the slider to zoom. Quit KANALI before connecting; it uses the same bridge.</p>
        {/if}
      </div>
    {/if}
  </div>
</div>
