<template>
  <div class="space-y-4">
    <div>
      <h2 class="text-xl font-bold text-ink">通知</h2>
      <p class="text-sm text-muted mt-1">ChatGPT、X、Telegram 开通成功，以及 X 超时，都会发到这个 Telegram 聊天。</p>
    </div>
    <div class="card max-w-xl space-y-3">
      <el-form label-position="top">
        <el-form-item label="机器人 Token">
          <el-input v-model="token" type="password" show-password :placeholder="tokenSet ? '已设置 ' + tokenHint + '，留空不修改' : '从 BotFather 复制'" />
        </el-form-item>
        <el-form-item label="接收聊天 ID">
          <el-input v-model="chatId" placeholder="个人或群的 chat id" />
        </el-form-item>
      </el-form>
      <p class="text-xs text-muted">这里保存的优先使用。两项都空着时，才用服务器上的环境变量。</p>
      <div class="flex flex-wrap gap-2">
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        <el-button :loading="testing" @click="testSend">发送测试</el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'

const token = ref('')
const chatId = ref('')
const tokenSet = ref(false)
const tokenHint = ref('')
const saving = ref(false)
const testing = ref(false)

function apply(d: any) {
  tokenSet.value = !!d.telegram_token_configured
  tokenHint.value = d.telegram_token_hint || ''
  chatId.value = d.telegram_chat_id || ''
}

onMounted(async () => {
  const r = await authFetch('/api/v1/admin/settings')
  if (!r.ok) return
  apply(await r.json().catch(() => ({})))
})

async function save() {
  saving.value = true
  try {
    const body: Record<string, string> = { telegram_chat_id: chatId.value.trim() }
    if (token.value.trim()) body.telegram_token = token.value.trim()
    const r = await authFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    token.value = ''
    apply(d)
    dialog.toast('已保存', 'ok')
  } finally {
    saving.value = false
  }
}

async function testSend() {
  testing.value = true
  try {
    const r = await authFetch('/api/v1/admin/settings/telegram-test', { method: 'POST' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '发送失败', 'err')
      return
    }
    dialog.toast('测试消息已发出', 'ok')
  } finally {
    testing.value = false
  }
}
</script>
