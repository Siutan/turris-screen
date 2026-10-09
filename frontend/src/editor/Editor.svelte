<script>
  import { onMount } from 'svelte'
  import {
    CancelExport,
    AppendFrames,
    BeginExport,
    FinishExport,
    GetAssets,
    GetExportLimits,
    GetProject,
    ImportMedia,
    ImportPaths,
    RemoveAsset,
    SaveDraft,
  } from '../../wailsjs/go/main/App.js'
  import { OnFileDrop, OnFileDropOff } from '../../wailsjs/runtime/runtime.js'
  import Brand from '../Brand.svelte'
  import DeviceChip from '../DeviceChip.svelte'
  import Inspector from './Inspector.svelte'
  import MediaPanel from './MediaPanel.svelte'
  import Preview from './Preview.svelte'
  import Timeline from './Timeline.svelte'
  import KlipyDialog from './KlipyDialog.svelte'
  import YouTubeDialog from './YouTubeDialog.svelte'
  import { estimateBytes, fmtBytes, hasMotion, newProject, projectKind } from '../lib/project.js'
  import { Sources } from '../lib/sources.js'
  import { Studio } from '../lib/studio.svelte.js'
  import { renderFrames, thumbnail } from '../lib/exporter.js'
  import { decodeClips, encodeClips } from '../lib/menus.js'

  let { projectId = '', status, working = false, job, chromeLead = null, chromeTools = null, onClose, onSaved, onConnect, onRestart } = $props()

  // Moves a node into the window title bar. The bar is outside this component.
  function portal(node, target) {
    const place = (el) => {
      if (el && node.parentNode !== el) el.appendChild(node)
    }
    place(target)
    return {
      update: place,
      destroy() {
        node.remove()
      },
    }
  }

  let revision = $state(0)
  const sources = new Sources(() => revision++)
  const studio = new Studio(sources)

  let assets = $state([])
  let importing = $state(false)
  let loading = $state(true)
  let saving = $state(null)
  let toast = $state(null)
  let leaving = $state(false)
  let naming = $state(false)
  let dragging = $state(null)
  let youtube = $state(null)
  let klipy = $state(false)
  let binTab = $state('image')
  let toastTimer
  let abort = null

  let kindOf = (id) => assets.find((a) => a.id === id)?.kind || 'image'
  let motion = $derived(hasMotion(studio.project, kindOf))
  let estimate = $derived(estimateBytes(studio.duration, motion, studio.limits))
  let busy = $derived(!!saving || working)

  function hasBridge() {
    return typeof window !== 'undefined' && !!window.go?.main?.App
  }

  function message(err) {
    if (!err) return 'Something went wrong'
    if (typeof err === 'string') return err
    return err.message || String(err)
  }

  function notify(text, tone = 'info') {
    clearTimeout(toastTimer)
    toast = { text, tone }
    toastTimer = setTimeout(() => (toast = null), tone === 'error' ? 6000 : 3500)
  }

  function fail(err) {
    notify(message(err), 'error')
  }

  function addAssets(list) {
    if (!list?.length) return
    const ids = new Set(list.map((a) => a.id))
    assets = [...list, ...assets.filter((a) => !ids.has(a.id))]
    list.forEach((a) => sources.load(a))
  }

  async function importFiles(paths) {
    if (!hasBridge()) return fail('Start the desktop app to add media.')
    importing = true
    try {
      const result = paths ? await ImportPaths(paths) : await ImportMedia()
      addAssets(result?.assets)
      if (result?.errors?.length) fail(result.errors.join(' '))
      return result?.assets ?? []
    } catch (err) {
      fail(err)
      return []
    } finally {
      importing = false
    }
  }

  function openYouTube(url = '') {
    if (!hasBridge()) return fail('Start the desktop app to import from YouTube.')
    youtube = { url }
  }

  function youtubeImported(asset) {
    youtube = null
    addAssets([asset])
    binTab = 'video'
    notify(`Added “${asset.name}” to Videos. Click it or drag it onto the timeline.`)
  }

  function openKlipy() {
    if (!hasBridge()) return fail('Start the desktop app to browse KLIPY.')
    klipy = true
  }

  function klipyImported(asset) {
    addAssets([asset])
    binTab = asset.kind === 'video' ? 'video' : asset.kind === 'gif' ? 'gif' : 'image'
  }

  let copied = $state(null)

  function copyClips({ cut = false } = {}) {
    const data = studio.copy()
    if (!data) return null
    copied = data
    if (cut) studio.removeClips()
    return data
  }

  function pasteClips(data = copied) {
    if (!data) return
    try {
      studio.paste(data)
    } catch (err) {
      fail(err)
    }
  }

  const clipboard = {
    copy: () => {
      const data = copyClips()
      if (data) navigator.clipboard?.writeText(encodeClips(data)).catch(() => {})
    },
    cut: () => {
      const data = copyClips({ cut: true })
      if (data) navigator.clipboard?.writeText(encodeClips(data)).catch(() => {})
    },
    canPaste: () => !!copied,
    paste: () => pasteClips(),
  }

  function onCopy(event) {
    if (youtube || klipy || saving || leaving || typing(event.target) || !studio.selection.length) return
    if (window.getSelection()?.toString()) return
    const data = copyClips({ cut: event.type === 'cut' })
    if (!data) return
    event.preventDefault()
    event.clipboardData?.setData('text/plain', encodeClips(data))
  }

  function onPaste(event) {
    if (youtube || klipy || saving || leaving || typing(event.target)) return
    const text = event.clipboardData?.getData('text/plain')?.trim() ?? ''
    const clips = decodeClips(text)
    if (clips) {
      event.preventDefault()
      copied = clips
      pasteClips(clips)
      return
    }
    if (/^(https?:\/\/)?((www|m|music)\.)?(youtube\.com|youtu\.be|youtube-nocookie\.com)\//i.test(text)) {
      event.preventDefault()
      openYouTube(text)
      return
    }
    if (!text && copied) {
      event.preventDefault()
      pasteClips()
    }
  }

  async function removeAsset(asset) {
    if (studio.project.tracks.some((t) => t.clips.some((c) => c.assetId === asset.id))) {
      return fail(`${asset.name} is on the timeline. Remove those clips first.`)
    }
    try {
      await RemoveAsset(asset.id)
      assets = assets.filter((a) => a.id !== asset.id)
      sources.forget(asset.id)
    } catch (err) {
      fail(err)
    }
  }

  // dropTarget finds where a drag from the media bin or the desktop landed.
  function dropTarget(x, y) {
    const el = document.elementFromPoint(x, y)
    const lane = el?.closest('[data-track]')
    if (lane) {
      const r = lane.getBoundingClientRect()
      return { trackIndex: Number(lane.dataset.track), start: Math.max(0, (x - r.left) / studio.pps) }
    }
    if (el?.closest('[data-drop="preview"]')) return { overlay: true }
    return null
  }

  function place(asset, target) {
    try {
      if (!target) studio.addMedia(asset)
      else if (target.overlay) studio.addOverlay(asset)
      else studio.addMedia(asset, target)
    } catch (err) {
      fail(err)
    }
  }

  async function placeWhenReady(asset, target) {
    await sources.load(asset)
    if (sources.status(asset.id) === 'error') return fail(`${asset.name} could not be opened.`)
    place(asset, target)
  }

  function assetDown(asset, event) {
    if (event.button !== 0) return
    const x0 = event.clientX
    const y0 = event.clientY
    let active = false
    const move = (e) => {
      if (!active && Math.hypot(e.clientX - x0, e.clientY - y0) < 5) return
      active = true
      dragging = { asset, x: e.clientX, y: e.clientY, thumb: sources.get(asset.id)?.poster || (asset.kind !== 'video' ? asset.url : '') }
    }
    const up = (e) => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      dragging = null
      if (!active) return placeWhenReady(asset, null)
      const target = dropTarget(e.clientX, e.clientY)
      if (target) placeWhenReady(asset, target)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }

  function payload(thumb) {
    return {
      id: studio.projectId,
      name: studio.project.name,
      kind: projectKind(studio.project, kindOf),
      duration: studio.duration,
      data: studio.serialize(),
      thumb,
    }
  }

  async function saveDraft() {
    if (busy) return false
    if (!hasBridge()) {
      fail('Start the desktop app to save projects.')
      return false
    }
    saving = { phase: 'draft', label: 'Saving draft', percent: 50 }
    studio.playing = false
    try {
      const item = await SaveDraft(payload(await thumbnail(studio.project, sources)))
      studio.projectId = item.id
      studio.dirty = false
      onSaved?.(item)
      notify('Draft saved.')
      return true
    } catch (err) {
      fail(err)
      return false
    } finally {
      saving = null
    }
  }

  async function saveToLibrary() {
    if (busy) return
    if (!hasBridge()) return fail('Start the desktop app to save projects.')
    if (studio.duration <= 0) return fail('Add something to the timeline first.')
    if (studio.duration > studio.limits.maxSeconds + 0.01) return fail(`Clips are limited to ${studio.limits.maxSeconds} seconds.`)
    const pending = assets.filter((a) => sources.status(a.id) === 'loading')
    if (pending.length) await Promise.all(pending.map((a) => sources.load(a)))
    studio.playing = false
    abort = new AbortController()
    saving = { phase: 'render', label: 'Rendering frames', percent: 0 }
    const now = new Date()
    let exportId = ''
    try {
      const thumb = await thumbnail(studio.project, sources, now)
      exportId = await renderFrames({
        project: studio.project,
        sources,
        fps: studio.limits.renderFps,
        api: { BeginExport, AppendFrames, CancelExport },
        signal: abort.signal,
        now,
        onProgress: (p) => (saving = { phase: 'render', label: 'Rendering frames', percent: p * 100 }),
      })
      saving = { phase: 'encode', label: 'Encoding', percent: 0 }
      const result = await FinishExport({ exportId, motion, show: status.connected, save: payload(thumb) })
      studio.projectId = result.item.id
      studio.dirty = false
      onSaved?.(result.item)
      if (result.shown) notify('Saved to your library and showing on the cooler.')
      else if (result.showError) notify(`Saved to your library, but the cooler did not take it: ${result.showError}`, 'error')
      else notify('Saved to your library. Connect the cooler to show it.')
    } catch (err) {
      if (err?.name === 'AbortError') notify('Export cancelled.')
      else fail(err)
    } finally {
      abort = null
      saving = null
    }
  }

  function cancelExport() {
    abort?.abort()
  }

  function requestClose() {
    if (busy) return
    if (studio.dirty) {
      leaving = true
      return
    }
    onClose?.()
  }

  async function saveAndLeave() {
    leaving = false
    if (await saveDraft()) onClose?.()
  }

  function rename(value) {
    naming = false
    const name = value.trim() || 'Untitled'
    if (name !== studio.project.name) studio.edit((p) => (p.name = name))
  }

  function typing(target) {
    return target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  }

  function onKey(event) {
    if (saving || leaving || youtube || klipy) return
    const mod = event.ctrlKey || event.metaKey
    const key = event.key.toLowerCase()
    if (mod && key === 's') {
      event.preventDefault()
      saveDraft()
      return
    }
    if (typing(event.target)) return
    if (mod && key === 'z') {
      event.preventDefault()
      if (event.shiftKey) studio.redo()
      else studio.undo()
    } else if (mod && key === 'y') {
      event.preventDefault()
      studio.redo()
    } else if (mod && key === 'd') {
      event.preventDefault()
      try {
        studio.duplicate()
      } catch (err) {
        fail(err)
      }
    } else if (mod && (event.code === 'BracketRight' || event.code === 'BracketLeft')) {
      event.preventDefault()
      const up = event.code === 'BracketRight'
      studio.arrange(undefined, up ? (event.shiftKey ? 'front' : 'forward') : event.shiftKey ? 'back' : 'backward')
    } else if (mod && key === 'l') {
      event.preventDefault()
      studio.toggleLock()
    } else if (mod && key === 'a') {
      event.preventDefault()
      studio.selectAll()
    } else if (mod && key === 'j') {
      event.preventDefault()
      try {
        studio.merge()
      } catch (err) {
        fail(err)
      }
    } else if (key === ' ') {
      event.preventDefault()
      if (studio.duration > 0) studio.playing = !studio.playing
    } else if (key === 'delete' || key === 'backspace') {
      if (studio.selection.length) {
        event.preventDefault()
        studio.removeClips(undefined, { ripple: event.shiftKey })
      }
    } else if (key === 's' && !mod) {
      studio.split()
    } else if (event.altKey && ['arrowleft', 'arrowright', 'arrowup', 'arrowdown'].includes(key) && studio.selection.length) {
      event.preventDefault()
      const step = event.shiftKey ? 10 : 1
      const dx = key === 'arrowleft' ? -step : key === 'arrowright' ? step : 0
      const dy = key === 'arrowup' ? -step : key === 'arrowdown' ? step : 0
      studio.nudge(undefined, dx, dy, 'nudge')
    } else if (key === 'arrowleft' || key === 'arrowright') {
      event.preventDefault()
      studio.playing = false
      const step = event.shiftKey ? 1 : 1 / studio.limits.renderFps
      studio.seek(studio.time + (key === 'arrowleft' ? -step : step))
    } else if (key === 'escape') {
      studio.select(null)
    } else if (key === 'home') {
      studio.seek(0)
    }
  }

  $effect(() => {
    const ids = new Set(studio.project.tracks.flatMap((t) => t.clips.map((c) => c.id)))
    sources.prune(ids)
  })

  onMount(() => {
    window.addEventListener('keydown', onKey)
    window.addEventListener('paste', onPaste)
    window.addEventListener('copy', onCopy)
    window.addEventListener('cut', onCopy)
    let dropOn = false
    ;(async () => {
      if (!hasBridge()) {
        studio.load(newProject(), '')
        loading = false
        return
      }
      try {
        studio.limits = { ...studio.limits, ...(await GetExportLimits()) }
        addAssets((await GetAssets()) ?? [])
        if (projectId) {
          const data = JSON.parse(await GetProject(projectId))
          await Promise.all(assets.map((a) => sources.load(a)))
          studio.load(data, projectId)
        } else {
          studio.load(newProject(), '')
        }
      } catch (err) {
        studio.load(newProject(), '')
        fail(err)
      }
      loading = false
      try {
        OnFileDrop(async (x, y, paths) => {
          const added = await importFiles(paths)
          const target = dropTarget(x, y)
          if (target && added.length) placeWhenReady(added[0], target)
        }, false)
        dropOn = true
      } catch {
        /* file drop needs the desktop window */
      }
    })()
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('paste', onPaste)
      window.removeEventListener('copy', onCopy)
      window.removeEventListener('cut', onCopy)
      if (dropOn) OnFileDropOff()
      abort?.abort()
      sources.pauseAll()
      sources.prune(new Set())
      clearTimeout(toastTimer)
    }
  })
</script>

<div class="studio">
  <div class="chrome-back" use:portal={chromeLead}>
    <Brand interactive title="Back to your projects" disabled={busy} onclick={requestClose} />
  </div>
  <div class="chrome-editor" use:portal={chromeTools}>
    <div class="st-history">
      <button class="st-round" onclick={() => studio.undo()} disabled={!studio.canUndo || busy} aria-label="Undo" title="Undo (Ctrl+Z)">
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5.5 4L2.5 7l3 3" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /><path d="M3 7h6.5a3.5 3.5 0 010 7H8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
      </button>
      <button class="st-round" onclick={() => studio.redo()} disabled={!studio.canRedo || busy} aria-label="Redo" title="Redo (Ctrl+Shift+Z)">
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M10.5 4l3 3-3 3" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /><path d="M13 7H6.5a3.5 3.5 0 000 7H8" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
      </button>
    </div>
    <DeviceChip compact {status} working={busy} {onConnect} {onRestart} />
  </div>

  <header class="st-top">
    <div class="st-title nodrag">
      {#if naming}
        <!-- svelte-ignore a11y_autofocus -->
        <input
          class="st-name-input"
          value={studio.project.name}
          autofocus
          spellcheck="false"
          maxlength="80"
          onblur={(e) => rename(e.currentTarget.value)}
          onkeydown={(e) => {
            if (e.key === 'Enter') rename(e.currentTarget.value)
            if (e.key === 'Escape') naming = false
          }}
          aria-label="Project name"
        />
      {:else}
        <button class="st-name" onclick={() => (naming = true)} title="Rename">
          <span>{studio.project.name}</span>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M10.6 2.9l2.5 2.5-7.4 7.4-3 .5.5-3z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" /></svg>
        </button>
      {/if}
      {#if studio.dirty}<span class="st-sub">Unsaved</span>{/if}
    </div>

    <div class="spacer"></div>

    <button class="st-btn nodrag" onclick={saveDraft} disabled={busy || loading}>Save</button>
    <button class="st-btn primary nodrag" onclick={saveToLibrary} disabled={busy || loading || studio.duration <= 0} title={status.connected ? 'Save and show on the cooler' : 'Save the project. The cooler is not connected.'}>
      Save &amp; Apply
    </button>
  </header>

  <div class="st-body">
    <MediaPanel
      {studio}
      {sources}
      {assets}
      {revision}
      {importing}
      bind:tab={binTab}
      onImport={() => importFiles()}
      onYouTube={() => openYouTube()}
      onKlipy={openKlipy}
      onRemoveAsset={removeAsset}
      onAssetDown={assetDown}
      onError={fail}
    />

    <main class="st-center">
      <Preview {studio} {sources} {revision} {estimate} suspended={saving?.phase === 'render'} {loading} {clipboard} onError={fail} />
    </main>

    <Inspector {studio} {sources} {assets} {revision} onError={fail} />

    <section class="st-timeline" aria-label="Timeline">
      <Timeline {studio} {sources} {revision} {clipboard} onError={fail} />
    </section>
  </div>

  {#if dragging}
    <div class="st-ghost" style:left="{dragging.x}px" style:top="{dragging.y}px" aria-hidden="true">
      {#if dragging.thumb}<img src={dragging.thumb} alt="" />{/if}
    </div>
  {/if}

  {#if toast}
    <p class="st-toast" class:error={toast.tone === 'error'} role="status">{toast.text}</p>
  {/if}

  {#if saving && saving.phase !== 'draft'}
    {@const label = saving.phase === 'render' ? saving.label : job?.active ? job.label : saving.label}
    {@const percent = saving.phase === 'render' ? saving.percent : job?.active ? job.percent : 0}
    <div class="st-modal" role="dialog" aria-modal="true" aria-label="Saving">
      <div class="st-modal-card">
        <h3>Saving to your library</h3>
        <p>{label}… <em>{Math.round(percent)}%</em></p>
        <div class="st-meter"><span style:width="{percent}%"></span></div>
        <p class="st-modal-note">{fmtBytes(estimate)} or less · {status.connected ? 'It will show on the cooler when it is done.' : 'The cooler is not connected, so it will only be saved.'}</p>
        {#if saving.phase === 'render'}
          <button class="st-btn" onclick={cancelExport}>Cancel</button>
        {/if}
      </div>
    </div>
  {/if}

  {#if youtube}
    <YouTubeDialog url={youtube.url} onClose={() => (youtube = null)} onImported={youtubeImported} />
  {/if}

  {#if klipy}
    <KlipyDialog onClose={() => (klipy = false)} onImported={klipyImported} />
  {/if}

  {#if leaving}
    <div class="st-modal" role="dialog" aria-modal="true" aria-label="Unsaved changes">
      <div class="st-modal-card">
        <h3>Keep your changes?</h3>
        <p>This project has edits that are not saved.</p>
        <div class="st-modal-actions">
          <button class="st-btn" onclick={() => (leaving = false)}>Cancel</button>
          <button class="st-btn danger" onclick={() => { leaving = false; onClose?.() }}>Discard</button>
          <button class="st-btn primary" onclick={saveAndLeave}>Save</button>
        </div>
      </div>
    </div>
  {/if}
</div>
