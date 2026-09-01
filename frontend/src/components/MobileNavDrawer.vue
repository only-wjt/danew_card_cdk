<template>
  <div class="mnav" :class="breakpoint === 900 ? 'mnav--wide' : 'mnav--narrow'">
    <header class="mnav-bar">
      <button
        type="button"
        class="mnav-burger"
        :aria-expanded="open"
        aria-label="打开菜单"
        @click="open = true"
      >
        <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M4 7h16M4 12h16M4 17h16" />
        </svg>
      </button>
      <div class="mnav-brand" @click="goHome">
        <component :is="BrandSlot" />
      </div>
      <div class="mnav-titles">
        <div class="mnav-title">{{ title }}</div>
        <div v-if="subtitle" class="mnav-sub">{{ subtitle }}</div>
      </div>
      <div class="mnav-actions">
        <slot name="actions" />
      </div>
    </header>

    <Teleport to="body">
      <div v-if="open" class="mnav-root" @keydown.esc="close">
        <div class="mnav-backdrop" @click="close" />
        <aside class="mnav-panel" role="dialog" aria-modal="true" aria-label="导航">
          <div class="mnav-panel-head">
            <div class="mnav-brand mnav-brand--drawer" @click="goHome">
              <component :is="BrandSlot" />
            </div>
            <button type="button" class="mnav-close" aria-label="关闭菜单" @click="close">
              <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
          <nav class="mnav-links">
            <router-link
              v-for="it in items"
              :key="it.path"
              :to="it.path"
              class="mnav-link"
              :class="{ active: isActive(it.path) }"
              @click="close"
            >
              <el-icon v-if="it.icon"><component :is="it.icon" /></el-icon>
              <span>{{ it.label }}</span>
            </router-link>
          </nav>
          <div v-if="$slots.footer" class="mnav-foot">
            <slot name="footer" />
          </div>
        </aside>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { defineComponent, onMounted, onUnmounted, ref, useSlots, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export interface MobileNavItem {
  path: string
  label: string
  icon?: string
  subtitle?: string
}

const props = withDefaults(
  defineProps<{
    items: MobileNavItem[]
    title: string
    subtitle?: string
    homePath: string
    isActive: (path: string) => boolean
    breakpoint?: 768 | 900
  }>(),
  { breakpoint: 768 },
)

const open = ref(false)
const route = useRoute()
const router = useRouter()
const slots = useSlots()
const BrandSlot = defineComponent({
  name: 'MobileNavBrandSlot',
  setup() {
    return () => slots.brand?.()
  },
})

function close() {
  open.value = false
}

function goHome() {
  close()
  router.push(props.homePath)
}

watch(
  () => route.fullPath,
  () => close(),
)

watch(open, (v) => {
  document.body.style.overflow = v ? 'hidden' : ''
})

onMounted(() => {
  const mq = window.matchMedia(`(max-width: ${props.breakpoint - 1}px)`)
  const onChange = () => {
    if (!mq.matches) close()
  }
  mq.addEventListener('change', onChange)
  onUnmounted(() => mq.removeEventListener('change', onChange))
})

onUnmounted(() => {
  document.body.style.overflow = ''
})
</script>

<style scoped>
.mnav-bar {
  display: none;
  position: sticky;
  top: 0;
  z-index: 120;
  align-items: center;
  gap: 10px;
  min-height: 52px;
  padding: 8px 12px;
  padding-top: calc(8px + env(safe-area-inset-top, 0px));
  padding-left: calc(12px + env(safe-area-inset-left, 0px));
  padding-right: calc(12px + env(safe-area-inset-right, 0px));
  background: color-mix(in srgb, var(--surface) 92%, transparent);
  backdrop-filter: saturate(1.2) blur(10px);
  border-bottom: 1px solid var(--brd);
}
@media (max-width: 767px) {
  .mnav--narrow .mnav-bar { display: flex; }
}
@media (max-width: 899px) {
  .mnav--wide .mnav-bar { display: flex; }
}

.mnav-burger,
.mnav-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border: none;
  border-radius: var(--radius-md, 12px);
  background: transparent;
  color: var(--ink);
  cursor: pointer;
}
.mnav-burger:hover,
.mnav-close:hover { background: var(--primary-soft); color: var(--primary); }

.mnav-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  cursor: pointer;
  min-width: 0;
}
.mnav-bar :deep(.brand-text) { display: none; }

.mnav-titles {
  flex: 1;
  min-width: 0;
}
.mnav-title {
  font-family: var(--font-display, var(--font-serif));
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mnav-sub {
  font-size: 11px;
  color: var(--ink-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mnav-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.mnav-root {
  position: fixed;
  inset: 0;
  z-index: 400;
}
.mnav-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
}
.mnav-panel {
  position: absolute;
  top: 0;
  left: 0;
  width: min(280px, 86vw);
  height: 100dvh;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border-right: 1px solid var(--brd);
  box-shadow: var(--shadow-lg, var(--shadow));
  padding-top: env(safe-area-inset-top, 0px);
  padding-bottom: env(safe-area-inset-bottom, 0px);
  padding-left: env(safe-area-inset-left, 0px);
}
.mnav-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 10px 12px 16px;
  border-bottom: 1px solid var(--brd);
}
.mnav-brand--drawer :deep(.brand-text) {
  display: inline;
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}

.mnav-links {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  overflow: auto;
  padding: 12px 10px;
}
.mnav-link {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px;
  min-height: 44px;
  border-radius: var(--radius-md, 12px);
  color: var(--ink-2);
  text-decoration: none;
  font-size: 15px;
}
.mnav-link:hover { background: var(--primary-soft); color: var(--ink); }
.mnav-link.active {
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 600;
}

.mnav-foot {
  padding: 12px 14px calc(12px + env(safe-area-inset-bottom, 0px));
  border-top: 1px solid var(--brd);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
</style>
