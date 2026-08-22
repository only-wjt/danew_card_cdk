<template>
  <teleport to="body">
    <transition name="modal-fade">
      <div v-if="open" class="modal-overlay" @click.self="close">
        <div class="modal-card card" :class="wide ? 'modal-wide' : ''" role="dialog" aria-modal="true">
          <div class="modal-head">
            <h3 class="modal-title">{{ title }}</h3>
            <button class="modal-close" aria-label="关闭" @click="close">×</button>
          </div>
          <div class="modal-body">
            <slot />
          </div>
          <div v-if="$slots.footer" class="modal-foot">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </transition>
  </teleport>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ open: boolean; title?: string; wide?: boolean }>(), {
  title: '',
  wide: false,
})
const emit = defineEmits<{ (e: 'close'): void }>()
function close() {
  emit('close')
}
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 9990;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}
.modal-card {
  width: 100%;
  max-width: 460px;
  max-height: 90vh;
  overflow-y: auto;
  padding: 0 !important;
}
.modal-wide { max-width: 640px; }
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--brd);
}
.modal-title {
  font-size: 1.1rem;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}
.modal-close {
  font-size: 22px;
  line-height: 1;
  color: var(--ink-3);
  padding: 0 4px;
  transition: color var(--dur-2) var(--ease-standard);
}
.modal-close:hover { color: var(--ink); }
.modal-body { padding: 20px; }
.modal-foot {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 16px 20px;
  border-top: 1px solid var(--brd);
}
.modal-fade-enter-active,
.modal-fade-leave-active { transition: opacity var(--dur-3) var(--ease-standard); }
.modal-fade-enter-from,
.modal-fade-leave-to { opacity: 0; }
</style>
