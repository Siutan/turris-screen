<script>
  import { onMount } from 'svelte'
  import { Apply, Connect, GetLibrary, Job, Remove, RestartCooler, ShowImage } from '../wailsjs/go/main/App.js'
  import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime.js'

  let status = $state({
    connected: false,
    serial: '',
    detail: 'Looking for the cooler…',
  })
  let library = $state([])
  let error = $state('')
  let working = $state(false)
  let job = $state({ active: false, label: '', percent: 0 })
  let hideTimer
  let jobWatch = 0
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

  async function refreshLibrary() {
    if (!hasBridge()) return
    library = (await GetLibrary()) ?? []
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
      await ShowImage(title, exportPNG())
      await refreshLibrary()
    } catch (err) {
      error = message(err)
      applyProgress({ active: false })
    } finally {
      stopWatch()
      working = false
      status = await Connect()
    }
  }

  async function applyItem(id) {
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
      status = await Connect()
    }
  }

  async function restartCooler() {
    if (working || !hasBridge()) return
    working = true
    error = ''
    applyProgress({ active: true, label: 'Restarting the cooler', percent: 30 })
    try {
      status = await RestartCooler()
    } catch (err) {
      error = message(err)
      status = await Connect()
    } finally {
      applyProgress({ active: false })
      working = false
    }
  }

  async function removeItem(id) {
    if (working) return
    error = ''
    try {
      await Remove(id)
      await refreshLibrary()
    } catch (err) {
      error = message(err)
    }
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

  onMount(() => {
    let stop = () => {}
    try {
      stop = EventsOn('progress', (update) => {
        applyProgress(update)
      })
    } catch {
      stop = () => {}
    }
    const observer = new ResizeObserver(() => {
      const prevW = frameW
      const prevH = frameH
      measure()
      if (source && (prevW === 0 || prevH === 0)) centerImage()
      else clamp()
    })
    if (frame) {
      observer.observe(frame)
      frame.addEventListener('wheel', onWheel, { passive: false })
    }
    connect()
      .then(refreshLibrary)
      .catch((err) => {
        error = message(err)
      })
    return () => {
      if (typeof stop === 'function') stop()
      observer.disconnect()
      frame?.removeEventListener('wheel', onWheel)
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  })
</script>

<div class="app">
  <header class="titlebar" role="toolbar" aria-label="Turris Screen" tabindex="-1" ondblclick={onTitleDblClick}>
    {#if job.active}
      <div class="wash" style:width="{job.percent}%"></div>
      <div class="meter" aria-hidden="true"><span style:width="{job.percent}%"></span></div>
    {/if}
    <h1>TURRIS SCREEN</h1>
    {#if job.active}
      <p class="job">{job.label} <em>{Math.round(job.percent)}%</em></p>
    {/if}
    <div class="spacer"></div>
    <div class="pill nodrag">
      <span class="dot" class:on={status.connected}></span>
      {#if status.connected}
        {status.serial}
      {:else}
        {status.detail || 'Not connected'}
      {/if}
    </div>
    {#if !status.connected}
      <button class="nodrag" onclick={connect} disabled={working}>Connect</button>
    {:else}
      <button
        class="nodrag"
        onclick={restartCooler}
        disabled={working}
        title="Restarts the cooler’s screen. Use this if the background stops changing."
      >Restart cooler</button>
    {/if}
    <div class="wins nodrag">
      <button class="winbtn" aria-label="Minimise" onclick={() => windowAction(WindowMinimise)}>−</button>
      <button class="winbtn" aria-label="Maximise" onclick={() => windowAction(WindowToggleMaximise)}>□</button>
      <button class="winbtn close" aria-label="Close" onclick={() => windowAction(Quit)}>×</button>
    </div>
  </header>

  <div class="workspace">
    <section class="stage">
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
          <div class="empty">
            <div>
              <strong>Drop a picture here</strong>
              <div>The frame is the cooler’s 1280×720 screen.</div>
            </div>
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
          <button onclick={() => picker?.click()}>Choose picture</button>
          <button onclick={centerImage} disabled={!source}>Reset crop</button>
        </div>
        <input
          type="text"
          placeholder="Name in the library"
          bind:value={title}
          disabled={!source}
        />
        <button class="primary" onclick={show} disabled={!source || working || !status.connected}>
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
        <p class="error">{error}</p>
      {:else}
        <p class="note">Drag to move the picture. Scroll or use the slider to zoom. Quit KANALI before connecting; it uses the same bridge.</p>
      {/if}
    </section>

    <aside class="library">
      <h2>Library</h2>
      {#if library.length === 0}
        <p class="blank">Pictures you show on the cooler stay here, so you can put one back on screen without cropping it again.</p>
      {:else}
        <div class="grid">
          {#each library as item (item.id)}
            <article class="card">
              <img alt="" src={item.thumb} />
              <div>
                <h3>{item.name}</h3>
                <p>{item.created}</p>
                <div class="row">
                  <button onclick={() => applyItem(item.id)} disabled={working || !status.connected}>Show</button>
                  <button class="danger" onclick={() => removeItem(item.id)} disabled={working}>Remove</button>
                </div>
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </aside>
  </div>
</div>
