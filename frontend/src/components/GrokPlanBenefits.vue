<template>
  <div v-if="items.length" class="space-y-2">
    <div class="flex flex-wrap items-center gap-2">
      <span class="text-sm font-semibold text-ink">套餐包含</span>
      <el-tag v-if="promo" size="small" type="warning">首月半价卡密 · 仅限有首月优惠资格的账号</el-tag>
    </div>
    <ul class="space-y-1.5">
      <li v-for="b in items" :key="b.name" class="flex gap-3 text-sm leading-6">
        <span class="text-good shrink-0">✓</span>
        <span class="w-14 shrink-0 font-medium text-ink">{{ b.name }}</span>
        <span class="text-muted">{{ b.detail }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { grokPlanBenefits } from '../lib/grok'

// Grok 档位的权益清单（Grok / Cursor / X / 用量）。半价卡密额外提示只能兑到有首月优惠的账号。
const props = defineProps<{ plan: string; promo?: boolean }>()
const items = computed(() => grokPlanBenefits(props.plan))
</script>
