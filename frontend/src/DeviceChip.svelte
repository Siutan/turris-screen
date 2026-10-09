<script>
  import { onMount } from 'svelte'

  let { status, working = false, onConnect, onRestart } = $props()
  let open = $state(false)
  let root = $state(null)

  function toggle(event) {
    event.stopPropagation()
    if (working) return
    if (!status.connected) {
      open = false
      onConnect?.()
      return
    }
    open = !open
  }

  function restart() {
    open = false
    onRestart?.()
  }

  onMount(() => {
    const close = (event) => {
      if (root && event.target instanceof Node && root.contains(event.target)) return
      open = false
    }
    const onKey = (event) => {
      if (event.key === 'Escape') open = false
    }
    document.addEventListener('click', close)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('click', close)
      document.removeEventListener('keydown', onKey)
    }
  })
</script>

<div class="device-wrap nodrag" bind:this={root}>
  <button
    class="device"
    onclick={toggle}
    disabled={working && !status.connected}
    aria-expanded={open}
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
        <span class="state-text">{status.connected ? 'Connected' : 'Not connected'}</span>
      </span>
    </span>
  </button>
  {#if open}
    <div class="menu device-menu">
      {#if status.serial}
        <p class="hint">{status.serial}</p>
      {/if}
      <button onclick={restart} disabled={working} title="Restarts the cooler’s screen if the background stops changing.">
        Restart cooler
      </button>
    </div>
  {/if}
</div>
