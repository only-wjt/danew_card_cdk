<template>
  <div class="card space-y-3">
    <div>
      <div class="font-semibold">每个 X 套餐从哪家卡台出码</div>
      <p class="text-xs text-muted mt-1">一个套餐同时只用一家。切换只影响之后发的码，已发出的码仍按原卡台兑换。每次切换都会写审计。</p>
    </div>
    <div class="overflow-x-auto">
      <table class="data-table">
        <thead>
          <tr>
            <th>套餐</th>
            <th>出码卡台</th>
            <th>说明</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in plans" :key="p.key">
            <td class="font-medium">{{ p.label }}</td>
            <td>
              <el-radio-group :model-value="p.source" size="small" :disabled="saving === p.key" @change="(v: any) => switchSupply(p, String(v))">
                <el-radio-button value="spacex" :disabled="!p.options.includes('spacex')">SpaceX</el-radio-button>
                <el-radio-button value="avan" :disabled="!p.options.includes('avan')">Avanfinity</el-radio-button>
                <el-radio-button value="off">停售</el-radio-button>
              </el-radio-group>
            </td>
            <td class="text-xs text-muted">{{ hint(p) }}</td>
          </tr>
          <tr v-if="!plans.length">
            <td colspan="3" class="text-center text-sm text-muted">{{ loaded ? '没有可配置的套餐' : '加载中…' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { authFetch } from '../lib/api'
import { dialog } from '../lib/dialog'

export interface SupplyRow {
  key: string
  label: string
  source: string
  options: string[]
  [k: string]: unknown
}

const emit = defineEmits<{ (e: 'change', plans: SupplyRow[]): void }>()
const plans = ref<SupplyRow[]>([])
const saving = ref('')
const loaded = ref(false)

function sourceName(s: string) { return s === 'spacex' ? 'SpaceX' : s === 'avan' ? 'Avanfinity' : '停售' }
function hint(p: SupplyRow) {
  if (p.source === 'off') return '不在发码页显示'
  if (p.source === 'spacex') return '客户兑换时填 X Cookie，按付款地区扣费'
  return '客户只填 X 用户名，从 Avanfinity 钱包扣费'
}

async function load() {
  const r = await authFetch('/api/v1/admin/x/supply')
  const d = await r.json().catch(() => ({}))
  loaded.value = true
  if (!r.ok) return
  plans.value = d.plans || []
  emit('change', plans.value)
}

async function switchSupply(p: SupplyRow, source: string) {
  if (source === p.source) return
  const ok = await dialog.confirm(`把「${p.label}」改为 ${sourceName(source)}？之后发的码都走这里，已发出的码不受影响。`, { title: '切换出码卡台', okText: '切换' })
  if (!ok) return
  saving.value = p.key
  try {
    const r = await authFetch('/api/v1/admin/x/supply', { method: 'PUT', body: JSON.stringify({ key: p.key, source }) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) { dialog.toast(d.error || '切换失败', 'err'); return }
    plans.value = d.plans || plans.value
    emit('change', plans.value)
    dialog.toast('已切换', 'ok')
  } finally { saving.value = '' }
}

defineExpose({ load })
onMounted(load)
</script>
