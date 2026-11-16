<script setup lang="ts">
defineProps<{ kind: string }>()
</script>

<template>
  <svg
    class="resource-illustration"
    :class="`resource-${kind}`"
    width="96"
    height="72"
    viewBox="0 0 96 72"
    fill="none"
    aria-hidden="true"
    focusable="false"
  >
    <rect class="resource-window" x="16" y="10" width="64" height="52" rx="7" />
    <path class="resource-header" d="M23 11h50a6 6 0 0 1 6 6v5H17v-5a6 6 0 0 1 6-6Z" />
    <path class="resource-divider" d="M17 22h62" />
    <g class="resource-window-dots">
      <circle cx="23" cy="16.5" r="1" />
      <circle cx="27.5" cy="16.5" r="1" />
      <circle cx="32" cy="16.5" r="1" />
    </g>
    <rect v-if="kind === 'browser'" class="resource-address" x="39" y="14" width="33" height="5" rx="2.5" />
    <template v-else-if="kind === 'app'">
      <path class="resource-caption" d="M43 16.5h20" />
      <circle class="resource-live" cx="72" cy="16.5" r="1.8" />
    </template>
    <path v-else class="resource-caption" d="M43 16.5h28" />

    <template v-if="kind === 'ide'">
      <path class="resource-sidebar" d="M17 23h16v38H23a6 6 0 0 1-6-6Z" />
      <path class="resource-divider" d="M33 23v38" />
      <path class="resource-detail" d="M22 31h6m-6 6h4m-4 6h6" />
      <path class="resource-symbol" d="m44 31-5 5 5 5m16-10 5 5-5 5m-6-12-4 14" />
      <path class="resource-detail" d="M40 49h25m-25 5h17" />
    </template>
    <template v-else-if="kind === 'terminal'">
      <path class="resource-symbol" d="m32 35 8 7-8 7m15 0h11" />
      <path class="resource-terminal-cursor" d="M64 37v12" />
    </template>
    <template v-else-if="kind === 'browser'">
      <rect class="resource-block" x="24" y="29" width="24" height="15" rx="2.5" />
      <path class="resource-block-mark" d="m28 41 4.5-5 3.5 3.5 2.5-2.5 5 4" />
      <path class="resource-detail" d="M53 31h19m-19 5h13" />
      <path class="resource-line" d="M24 50h48m-48 5h32" />
      <rect class="resource-select" x="21" y="26" width="30" height="21" rx="3.5" />
      <path class="resource-cursor" d="M47 42v11l3-2.7 2.1 4.5 2-.9-2.1-4.4h4Z" />
    </template>
    <template v-else-if="kind === 'app'">
      <rect class="resource-panel" x="23" y="27" width="50" height="29" rx="3" />
      <path class="resource-detail" d="M28 32.5h12" />
      <path class="resource-line" d="M60 32.5h8" />
      <path class="resource-bars" d="M31 51v-5m7 5v-9m7 9v-7m7 7v-13m7 13v-10" />
    </template>
    <template v-else-if="kind === 'computer'">
      <rect class="resource-symbol" x="34" y="30" width="28" height="19" rx="2.5" />
      <path class="resource-symbol" d="M44 55h8m-4-6v6" />
      <path class="resource-detail" d="M42 45h12" />
    </template>
    <template v-else-if="kind === 'log'">
      <g class="resource-log-dots">
        <circle cx="28" cy="32" r="1.4" />
        <circle cx="28" cy="41" r="1.4" />
        <circle cx="28" cy="50" r="1.4" />
      </g>
      <path class="resource-symbol" d="M35 32h28m-28 9h20" />
      <path class="resource-detail" d="M35 50h25" />
    </template>
  </svg>
</template>

<style scoped>
.resource-illustration {
  --resource-hover: var(--ra-card-hover, 0);
  --resource-edge: color-mix(in srgb, var(--ra-muted) 40%, var(--ra-line));
  --resource-symbol: color-mix(in srgb, var(--ra-accent) 75%, var(--ra-ink));
  width: 96px;
  height: 72px;
  flex-shrink: 0;
  stroke-width: 1.5;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.resource-window {
  fill: var(--ra-surface);
  stroke: color-mix(in srgb, var(--ra-accent) calc(var(--resource-hover) * 45%), var(--resource-edge));
  transition: stroke 200ms ease;
}
.resource-header,
.resource-sidebar {
  fill: color-mix(in srgb, var(--ra-accent) 5%, var(--ra-surface));
  stroke: none;
}
.resource-divider {
  stroke: color-mix(in srgb, var(--ra-muted) 15%, var(--ra-line));
  stroke-width: 1;
}
.resource-window-dots {
  fill: color-mix(in srgb, var(--ra-muted) 65%, var(--ra-line));
  stroke: none;
}
.resource-address {
  fill: color-mix(in srgb, var(--ra-muted) 10%, var(--ra-surface));
  stroke: color-mix(in srgb, var(--ra-muted) 15%, var(--ra-line));
  stroke-width: .7;
}
.resource-caption {
  stroke: color-mix(in srgb, var(--ra-muted) 25%, var(--ra-line));
  stroke-width: 1;
}
.resource-symbol,
.resource-terminal-cursor {
  stroke: var(--resource-symbol);
  transition: stroke 200ms ease;
}
.resource-symbol { stroke-width: 1.7; }
.resource-detail {
  stroke: color-mix(in srgb, var(--ra-muted) 75%, var(--ra-accent));
  stroke-width: 1.4;
}
.resource-line {
  stroke: color-mix(in srgb, var(--ra-muted) 30%, var(--ra-line));
  stroke-width: 1.4;
}
.resource-block {
  fill: color-mix(in srgb, var(--ra-accent) 14%, var(--ra-surface));
  stroke: none;
}
.resource-block-mark {
  stroke: color-mix(in srgb, var(--ra-accent) 55%, var(--ra-surface));
  stroke-width: 1.2;
}
.resource-select {
  stroke: var(--resource-symbol);
  stroke-width: 1.1;
  stroke-dasharray: 2.5 2;
}
.resource-cursor {
  fill: var(--ra-surface);
  stroke: var(--ra-ink);
  stroke-width: 1.1;
}
.resource-panel {
  fill: color-mix(in srgb, var(--ra-accent) 4%, var(--ra-surface));
  stroke: color-mix(in srgb, var(--ra-muted) 15%, var(--ra-line));
  stroke-width: 1;
}
.resource-bars {
  stroke: var(--resource-symbol);
  stroke-width: 3.4;
  transition: stroke 200ms ease;
}
.resource-live {
  fill: var(--ra-green);
  stroke: color-mix(in srgb, var(--ra-green) 25%, transparent);
  stroke-width: 2.5;
}
.resource-log-dots {
  fill: var(--resource-symbol);
  stroke: none;
}
@media (prefers-reduced-motion: reduce) {
  .resource-illustration * { transition: none; }
}
</style>
