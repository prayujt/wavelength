<script lang="ts">
  let { playing = false, size = 18 }: { playing?: boolean; size?: number } =
    $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.8"
  stroke-linecap="round"
  aria-hidden="true"
  data-testid="playback-wave"
  class:playing
>
  <path d="M2 10v4" />
  <path d="M6 7v10" />
  <path d="M10 4v16" />
  <path d="M14 8v8" />
  <path d="M18 6v12" />
  <path d="M22 10v4" />
</svg>

<style>
  svg {
    flex: 0 0 auto;
    vertical-align: middle;
  }
  @media (prefers-reduced-motion: no-preference) {
    .playing path {
      transform-origin: center;
      animation: wave 920ms ease-in-out infinite;
    }
    .playing path:nth-child(2n) {
      animation-delay: -320ms;
      animation-duration: 780ms;
    }
    .playing path:nth-child(3n) {
      animation-delay: -510ms;
      animation-duration: 1040ms;
    }
    @keyframes wave {
      0%,
      100% {
        transform: scaleY(0.66);
      }
      45% {
        transform: scaleY(1.22);
      }
      70% {
        transform: scaleY(0.82);
      }
    }
  }
</style>
