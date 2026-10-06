<template>
  <el-dialog :model-value="modelValue" title="怎么获取 X 的 Cookie" width="560px" @update:model-value="emit('update:modelValue', $event)">
    <ol class="guide space-y-2 text-sm">
      <li>用电脑浏览器（Chrome / Edge）打开 <a class="app-link" href="https://x.com" target="_blank" rel="noopener">x.com</a>，登录要开通会员的那个账号。</li>
      <li>按 <kbd>F12</kbd> 打开开发者工具，切到 <b>Application</b>（应用）页签。</li>
      <li>左侧展开 <b>Storage → Cookies</b>，点 <code>https://x.com</code>。</li>
      <li>在列表里找到 <code>auth_token</code> 和 <code>ct0</code>，分别双击 Value 列复制，填到下面。</li>
      <li>填一个能收到 X 账单的邮箱，点「生成并填入」。</li>
    </ol>

    <div class="mt-4 space-y-2">
      <input v-model.trim="authToken" class="input mono text-xs" placeholder="auth_token" autocomplete="off" />
      <input v-model.trim="ct0" class="input mono text-xs" placeholder="ct0" autocomplete="off" />
      <input v-model.trim="email" class="input text-sm" placeholder="账单邮箱 billing_email" autocomplete="email" />
      <p v-if="err" class="text-xs" style="color: var(--err)">{{ err }}</p>
    </div>

    <p class="mt-4 rounded-lg p-3 text-xs" style="background: var(--warn-soft, #fff7ed)">
      Cookie 相当于登录凭证，只用于这一次开通，本站不会保存。开通成功后建议在 X 里退出登录一次，让这份 Cookie 失效。不要把它发给其他任何人。
    </p>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
      <el-button type="primary" @click="apply">生成并填入</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { xPremiumCredential } from '../lib/x-premium'

defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'fill', raw: string): void }>()

const authToken = ref('')
const ct0 = ref('')
const email = ref('')
const err = ref('')

function apply() {
  const raw = JSON.stringify({ auth_token: authToken.value, ct0: ct0.value, billing_email: email.value })
  if (!xPremiumCredential(raw)) {
    err.value = 'auth_token / ct0 格式不对，或邮箱无效，请重新复制'
    return
  }
  err.value = ''
  emit('fill', raw)
  emit('update:modelValue', false)
}
</script>

<style scoped>
.guide { list-style: decimal; padding-left: 1.25rem; }
kbd { padding: 0 4px; border: 1px solid currentColor; border-radius: 3px; font-size: 11px; }
</style>
