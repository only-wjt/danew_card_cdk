<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <p class="text-sm text-muted max-w-xl">
        创建 API Key 后，在请求头携带 <code class="mono">Authorization: Bearer &lt;key&gt;</code> 调用代理接口。
      </p>
      <button class="btn-primary" @click="createOpen = true">创建密钥</button>
    </div>

    <section class="card overflow-hidden !p-0">
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>Key 前缀</th>
              <th>创建时间</th>
              <th>最后使用</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="5" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!keys.length">
              <td colspan="5" class="py-10 text-center text-muted">还没有 API Key，点击右上角创建</td>
            </tr>
            <tr v-for="row in keys" :key="row.id">
              <td class="font-medium text-ink">{{ row.name }}</td>
              <td class="mono text-sm">{{ row.key_prefix }}…</td>
              <td class="text-muted text-sm">{{ formatDate(row.created_at) }}</td>
              <td class="text-muted text-sm">{{ row.last_used_at ? formatDate(row.last_used_at) : '从未使用' }}</td>
              <td class="text-right">
                <button class="btn-ghost !text-[var(--err)] !px-3" @click="revoke(row)">吊销</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="card space-y-3">
      <h3 class="section-title">安全提示</h3>
      <ul class="tips-list">
        <li>密钥明文只在创建时显示一次，请立即复制保存。</li>
        <li>不要把 Key 写进前端页面或公开仓库。</li>
        <li>怀疑泄露时请立即吊销并重新创建。</li>
      </ul>
    </section>

    <Modal :open="createOpen" title="创建 API 密钥" @close="createOpen = false">
      <div class="form-group">
        <label>密钥名称</label>
        <input v-model="createName" class="input" placeholder="例如 production / staging" @keyup.enter="submitCreate" />
        <p class="text-xs text-muted">用于区分不同环境或业务线。</p>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="createOpen = false">取消</button>
        <button class="btn-primary" :disabled="creating" @click="submitCreate">{{ creating ? '创建中…' : '创建' }}</button>
      </template>
    </Modal>

    <Modal :open="keyOpen" title="请妥善保存 API Key" wide @close="keyOpen = false">
      <p class="text-sm text-muted mb-4">此 Key 只显示一次，关闭后无法再次查看完整内容。</p>
      <div class="key-box mono break-all select-all">{{ newKey }}</div>
      <template #footer>
        <button class="btn-secondary" @click="copyKey">复制 Key</button>
        <button class="btn-primary" @click="keyOpen = false">我已保存</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { agentFetch } from '../../lib/agentApi'
import { dialog } from '../../lib/dialog'
import Modal from '../../components/Modal.vue'
import { formatPartnerDate } from './partnerUi'

const keys = ref<any[]>([])
const loading = ref(false)
const createOpen = ref(false)
const creating = ref(false)
const createName = ref('production')
const keyOpen = ref(false)
const newKey = ref('')

const formatDate = formatPartnerDate

async function load() {
  loading.value = true
  try {
    const res = await agentFetch('/api/v1/agent/api-keys')
    if (res.ok) keys.value = (await res.json()).list || []
  } finally {
    loading.value = false
  }
}

async function submitCreate() {
  const name = createName.value.trim() || 'default'
  creating.value = true
  try {
    const res = await agentFetch('/api/v1/agent/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '创建失败', 'err')
      return
    }
    createOpen.value = false
    newKey.value = d.api_key
    keyOpen.value = true
    createName.value = 'production'
    await load()
  } finally {
    creating.value = false
  }
}

async function revoke(row: any) {
  const ok = await dialog.confirm(`确定吊销密钥「${row.name}」？吊销后立即失效。`, {
    title: '吊销密钥',
    danger: true,
  })
  if (!ok) return
  const res = await agentFetch(`/api/v1/agent/api-keys/${row.id}`, { method: 'DELETE' })
  if (!res.ok) {
    dialog.toast('吊销失败', 'err')
    return
  }
  dialog.toast('已吊销', 'ok')
  await load()
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newKey.value)
    dialog.toast('已复制到剪贴板', 'ok')
  } catch {
    dialog.toast('复制失败，请手动选中复制', 'err')
  }
}

onMounted(load)
</script>

<style scoped>
.section-title {
  font-size: 1rem;
  font-weight: 700;
  color: var(--ink);
}
.tips-list {
  margin: 0;
  padding-left: 1.25rem;
  font-size: 14px;
  color: var(--ink-2);
  line-height: 1.7;
}
.key-box {
  padding: 16px;
  border-radius: var(--radius-md, 12px);
  background: var(--surface-2);
  border: 1px solid var(--brd);
  font-size: 13px;
  color: var(--ink);
}
</style>
