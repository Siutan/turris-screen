<script>
  let { interactive = false, title = '', disabled = false, onclick } = $props()
</script>

{#snippet mark()}
  <svg class="mark" viewBox="0 0 32 32" aria-hidden="true">
    <g fill="currentColor">
      <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(0 16 16)" />
      <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(120 16 16)" />
      <rect x="13.35" y="3.1" width="5.3" height="10.4" rx="2.65" transform="rotate(240 16 16)" />
      <circle cx="16" cy="16" r="2.5" />
    </g>
  </svg>
{/snippet}

{#if interactive}
  <button class="brand chrome-brand nodrag" {title} {disabled} {onclick}>
    {@render mark()}
    Turris Studio
  </button>
{:else}
  <div class="brand chrome-brand">
    {@render mark()}
    Turris Studio
  </div>
{/if}
