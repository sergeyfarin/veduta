<script lang="ts">
  interface Props {
    height?: string;
    width?: string;
  }
  let { height = '20px', width = '100%' }: Props = $props();
</script>

<span class="skeleton" style="height: {height}; width: {width}" aria-hidden="true"></span>

<style>
  .skeleton {
    display: block;
    border-radius: var(--v-r-md);
    background: linear-gradient(
      90deg,
      var(--v-surface-2) 25%,
      var(--v-border) 37%,
      var(--v-surface-2) 63%
    );
    background-size: 400% 100%;
    animation: sheen 1.4s ease infinite;
  }
  @keyframes sheen {
    from { background-position: 100% 0; }
    to { background-position: -100% 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    .skeleton { animation: none; }
  }
</style>
