<script>
  import { onMount } from 'svelte'
  import { ActiveName, Apply, Connect, GetLibrary, Job, Remove, RestartCooler } from '../wailsjs/go/main/App.js'
  import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime.js'
  import Brand from './Brand.svelte'
  import DeviceChip from './DeviceChip.svelte'
  import Editor from './editor/Editor.svelte'

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
  let chromeLead = $state(null)
  let chromeTools = $state(null)
  let editing = $state('')
  let editorKey = $state(0)
  let filter = $state('all')
  let query = $state('')
  let sort = $state('recent')
  let menu = $state(null)

  const filters = [
    { id: 'all', label: 'All' },
    { id: 'image', label: 'Images' },
    { id: 'video', label: 'Videos' },
    { id: 'gif', label: 'GIFs' },
    { id: 'widget', label: 'Widgets' },
  ]

  function message(err) {
    if (!err) return 'Something went wrong'
    if (typeof err === 'string') return err
    return err.message || String(err)
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

  function editedLabel(item) {
    const match = String(item.created || '').match(/^(\d{1,2}) ([A-Za-z]{3}) (\d{4})/)
    const when = match ? `${match[2]} ${Number(match[1])}, ${match[3]}` : item.created
    const prefix = item.draft ? 'Draft · edited' : 'Edited'
    const length = item.duration ? ` · ${Math.round(item.duration)}s` : ''
    return when ? `${prefix} ${when}${length}` : prefix
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

  function openEditor(id = '') {
    menu = null
    error = ''
    editing = id
    editorKey++
    view = 'edit'
  }

  async function closeEditor() {
    view = 'home'
    editing = ''
    await settle()
  }

  async function onSaved() {
    await refreshLibrary()
    await refreshActive()
  }

  function connectQuietly() {
    connect().catch((err) => {
      error = message(err)
    })
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

  let activeItem = $derived(library.find((item) => item.remoteName && item.remoteName === activeName) ?? null)

  let hero = $derived.by(() => {
    if (activeItem) {
      return {
        kicker: 'Now showing',
        title: activeItem.name,
        meta: `${kindLabel(kindOf(activeItem))} · ${editedLabel(activeItem)}`,
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
    }
  })
</script>

<div class="app">
  <header class="chrome" role="toolbar" aria-label="Window" tabindex="-1" ondblclick={onTitleDblClick}>
    <div class="chrome-lead" bind:this={chromeLead}>
      {#if view !== 'edit'}
        <Brand />
      {/if}
    </div>
    {#if job.active}
      <p class="job">{job.label} <em>{Math.round(job.percent)}%</em></p>
      <div class="meter" aria-hidden="true"><span style:width="{job.percent}%"></span></div>
    {/if}
    <div class="spacer"></div>
    <div class="chrome-tools nodrag" bind:this={chromeTools}></div>
    {#if view !== 'edit'}
      <DeviceChip compact {status} {working} onConnect={connectQuietly} onRestart={restartCooler} />
    {/if}
    <div class="wins nodrag">
      <button class="winbtn" aria-label="Minimise" onclick={() => windowAction(WindowMinimise)}>−</button>
      <button class="winbtn" aria-label="Maximise" onclick={() => windowAction(WindowToggleMaximise)}>□</button>
      <button class="winbtn close" aria-label="Close" onclick={() => windowAction(Quit)}>×</button>
    </div>
  </header>

  <div class="shell" class:editing={view === 'edit'}>
    {#if view === 'home'}
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
            <button class="add" onclick={() => openEditor()} disabled={working}>
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
              <article class="card" class:editable={item.editable}>
                <button class="shot" onclick={() => item.editable && openEditor(item.id)} disabled={!item.editable || working} aria-label="Edit {item.name}">
                  {#if item.thumb}
                    <img alt="" src={item.thumb} />
                  {:else}
                    <div class="shot-fallback"></div>
                  {/if}
                  <span class="badge">{kindLabel(kindOf(item))}</span>
                  {#if item.remoteName && item.remoteName === activeName}
                    <span class="badge live">On the cooler</span>
                  {/if}
                </button>
                <button class="more" aria-label="Actions for {item.name}" aria-expanded={menu?.type === 'card' && menu.id === item.id} onclick={(event) => toggleCardMenu(item.id, event)}>
                  <svg viewBox="0 0 16 16" aria-hidden="true">
                    <circle cx="3.2" cy="8" r="1.15" fill="currentColor" />
                    <circle cx="8" cy="8" r="1.15" fill="currentColor" />
                    <circle cx="12.8" cy="8" r="1.15" fill="currentColor" />
                  </svg>
                </button>
                {#if menu?.type === 'card' && menu.id === item.id}
                  <div class="menu card-menu">
                    {#if item.editable}
                      <button onclick={() => openEditor(item.id)} disabled={working}>Edit</button>
                    {/if}
                    {#if item.remoteName}
                      <button onclick={() => applyItem(item.id)} disabled={working || !status.connected}>
                        {item.draft ? 'Show last saved version' : 'Show on cooler'}
                      </button>
                    {/if}
                    <button class="danger" onclick={() => removeItem(item.id)} disabled={working}>Remove</button>
                  </div>
                {/if}
                <h3>{item.name}</h3>
                <p>{editedLabel(item)}</p>
              </article>
            {/each}
          </div>
        {/if}
      </section>
    {:else}
      {#key editorKey}
        <Editor
          projectId={editing}
          {status}
          {working}
          {job}
          {chromeLead}
          {chromeTools}
          onClose={closeEditor}
          {onSaved}
          onConnect={connectQuietly}
          onRestart={restartCooler}
        />
      {/key}
    {/if}
  </div>
</div>
