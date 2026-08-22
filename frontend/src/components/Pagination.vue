<template>
  <div class="pager">
    <span class="pager-total">共 <b class="text-ink">{{ total }}</b> 条</span>
    <div class="pager-controls">
      <button class="pager-btn" :disabled="page <= 1" @click="go(page - 1)">上一页</button>
      <button
        v-for="p in pages"
        :key="p.key"
        class="pager-num"
        :class="{ active: p.value === page, ellipsis: p.value === 0 }"
        :disabled="p.value === 0"
        @click="p.value && go(p.value)"
      >
        {{ p.value === 0 ? '…' : p.value }}
      </button>
      <button class="pager-btn" :disabled="page >= totalPages" @click="go(page + 1)">下一页</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ page: number; pageSize: number; total: number }>(), {
  page: 1,
  pageSize: 20,
  total: 0,
})
const emit = defineEmits<{ (e: 'update:page', v: number): void }>()

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))

const pages = computed(() => {
  const tp = totalPages.value
  const cur = props.page
  const out: { key: string; value: number }[] = []
  const push = (v: number) => out.push({ key: `${v}-${out.length}`, value: v })
  if (tp <= 7) {
    for (let i = 1; i <= tp; i++) push(i)
    return out
  }
  push(1)
  const start = Math.max(2, cur - 1)
  const end = Math.min(tp - 1, cur + 1)
  if (start > 2) push(0)
  for (let i = start; i <= end; i++) push(i)
  if (end < tp - 1) push(0)
  push(tp)
  return out
})

function go(p: number) {
  const next = Math.min(totalPages.value, Math.max(1, p))
  if (next !== props.page) emit('update:page', next)
}
</script>

<style scoped>
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.pager-total { font-size: 13px; color: var(--ink-2); }
.pager-controls { display: flex; align-items: center; gap: 4px; }
.pager-btn,
.pager-num {
  min-width: 34px;
  height: 34px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--brd);
  background: var(--surface);
  color: var(--ink-2);
  font-size: 13px;
  transition: all var(--dur-2) var(--ease-standard);
}
.pager-btn:hover:not(:disabled),
.pager-num:hover:not(:disabled):not(.ellipsis) {
  border-color: var(--primary);
  color: var(--primary);
}
.pager-num.active {
  background: var(--primary);
  border-color: var(--primary);
  color: var(--primary-on);
  font-weight: 600;
}
.pager-num.ellipsis { border-color: transparent; background: transparent; cursor: default; }
.pager-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
