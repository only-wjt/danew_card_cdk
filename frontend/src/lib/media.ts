import { onMounted, onUnmounted, ref } from 'vue'

/** Track a max-width media query. Safe on first paint (reads window if present). */
export function useMaxWidth(px: number) {
  const query = `(max-width: ${px}px)`
  const compact = ref(typeof window !== 'undefined' ? window.matchMedia(query).matches : false)

  onMounted(() => {
    const mq = window.matchMedia(query)
    const sync = () => {
      compact.value = mq.matches
    }
    sync()
    mq.addEventListener('change', sync)
    onUnmounted(() => mq.removeEventListener('change', sync))
  })

  return compact
}
