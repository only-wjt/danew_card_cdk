<template>
  <div class="card overflow-x-auto">
    <div class="mb-2 flex items-center justify-between">
      <div class="text-sm font-semibold text-ink">产品 × 卡台</div>
      <div class="text-xs text-muted">GPT 按主台优先、失败切备台；X 每个套餐只从一家卡台取码</div>
    </div>
    <table class="data-table">
      <thead>
        <tr>
          <th class="w-24">产品</th>
          <th v-for="v in vendors" :key="v.key">{{ v.label }}</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td>GPT</td>
          <td v-for="v in vendors" :key="v.key">
            <template v-if="gptCells[v.key].length">
              <button v-for="c in gptCells[v.key]" :key="c.id" type="button" class="cell-btn" @click="emit('open', c.id)">
                <span :class="c.cls">{{ c.text }}</span>
                <span class="text-xs text-muted">· {{ c.name }}</span>
              </button>
            </template>
            <span v-else class="text-muted">不用</span>
          </td>
        </tr>
        <tr>
          <td>X 会员</td>
          <td v-for="v in vendors" :key="v.key">
            <span v-if="!hasX(v.key)" class="text-muted">未接入</span>
            <span v-else-if="supplyCounts[v.key]" class="text-emerald-600">供 {{ supplyCounts[v.key] }} 个套餐</span>
            <span v-else class="text-muted">已接入 · 没有套餐走这家</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { gptRoleOf, vendorOf, type Vendor, type VendorAcc } from '../../lib/platformVendor'

const props = defineProps<{
  accounts: (VendorAcc & { name: string })[]
  supplyCounts: Record<string, number>
}>()
const emit = defineEmits<{ (e: 'open', id: number): void }>()

const vendors: { key: Vendor; label: string }[] = [
  { key: 'spacex', label: 'SpaceX' },
  { key: 'avan', label: 'Avanfinity' },
]

const openaiSorted = computed(() =>
  props.accounts.filter((a) => a.serves_openai).sort((a, b) => a.priority - b.priority || a.id - b.id),
)

const gptCells = computed(() => {
  const out: Record<Vendor, { id: number; name: string; text: string; cls: string }[]> = { spacex: [], avan: [] }
  for (const a of openaiSorted.value) {
    const role = gptRoleOf(a, openaiSorted.value)
    out[vendorOf(a.protocol)].push({
      id: a.id,
      name: a.name,
      text: role === 'primary' ? '主台' : role === 'backup' ? '备台' : '已关闭',
      cls: role === 'primary' ? 'text-emerald-600 font-semibold' : role === 'backup' ? 'text-amber-600' : 'text-muted',
    })
  }
  return out
})

function hasX(v: Vendor) {
  return props.accounts.some((a) => {
    if (vendorOf(a.protocol) !== v || a.status !== 'active') return false
    // SpaceX 用同一把 OpenAPI Key 发 X CDK；Avan 需要 X 账户带 x_cdk 能力
    return v === 'spacex' ? a.serves_openai : (a.capabilities || '').split(',').includes('x_cdk')
  })
}
</script>

<style scoped>
.cell-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 1px 0;
}
.cell-btn:hover span:first-child {
  text-decoration: underline;
}
</style>
