<script>
  import { onMount } from 'svelte'

  let { x, y, title = '', items, onClose } = $props()

  let el = $state(null)
  let left = $state(0)
  let top = $state(0)
  let placed = $state(false)

  function choose(item) {
    onClose()
    item.run()
  }

  onMount(() => {
    const r = el.getBoundingClientRect()
    left = Math.max(8, Math.min(x, window.innerWidth - r.width - 8))
    top = Math.max(8, Math.min(y, window.innerHeight - r.height - 8))
    placed = true
    el.querySelector('button:not(:disabled)')?.focus()

    const outside = (e) => {
      if (!el.contains(e.target)) onClose()
    }
    const key = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopImmediatePropagation()
        onClose()
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        e.stopImmediatePropagation()
        const buttons = [...el.querySelectorAll('button:not(:disabled)')]
        const i = buttons.indexOf(document.activeElement)
        const next = e.key === 'ArrowDown' ? i + 1 : i - 1
        buttons[(next + buttons.length) % buttons.length]?.focus()
      }
    }
    window.addEventListener('pointerdown', outside, true)
    window.addEventListener('keydown', key, true)
    window.addEventListener('blur', onClose)
    window.addEventListener('resize', onClose)
    return () => {
      window.removeEventListener('pointerdown', outside, true)
      window.removeEventListener('keydown', key, true)
      window.removeEventListener('blur', onClose)
      window.removeEventListener('resize', onClose)
    }
  })
</script>

<div
  class="menu ctx-menu"
  bind:this={el}
  style:left="{left}px"
  style:top="{top}px"
  style:visibility={placed ? null : 'hidden'}
  role="menu"
  tabindex="-1"
  aria-label={title || 'Options'}
  oncontextmenu={(e) => e.preventDefault()}
>
  {#if title}<p class="hint">{title}</p>{/if}
  {#each items as item, i (i)}
    {#if item === 'sep'}
      <hr />
    {:else}
      <button role="menuitem" class:danger={item.danger} disabled={item.disabled} title={item.title || null} onclick={() => choose(item)}>
        <span>{item.label}</span>
        {#if item.keys}<kbd>{item.keys}</kbd>{/if}
      </button>
    {/if}
  {/each}
</div>
