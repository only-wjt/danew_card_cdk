<template>
  <svg
    class="brand-mark"
    :width="px"
    :height="px"
    viewBox="0 0 52 52"
    role="img"
    :aria-label="label"
    focusable="false"
  >
    <title>{{ label }}</title>
    <rect class="plate" x="1" y="1" width="50" height="50" rx="15" />
    <path class="bowl" d="M18 34V18h8a8 8 0 0 1 0 16h-8z" fill="none" stroke-width="3.4" />
    <path class="notch" d="M26 26h12" stroke-width="3.4" stroke-linecap="round" />
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ size?: number | string; label?: string }>(), {
  size: 20,
  label: 'danew',
})

const px = computed(() => (typeof props.size === 'number' ? `${props.size}` : props.size))
</script>

<style scoped>
.brand-mark {
  display: block;
  flex-shrink: 0;
}
.plate {
  fill: var(--primary, currentColor);
}
.bowl {
  stroke: var(--primary-on, #fff);
}
.notch {
  stroke: var(--accent, var(--primary-on, #fff));
}
</style>
