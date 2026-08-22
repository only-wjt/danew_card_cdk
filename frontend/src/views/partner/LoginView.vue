<template>
  <div class="login-shell">
    <div class="login-panel">
      <div class="login-brand">
        <BrandMark :size="36" label="代理" />
        <h1 class="login-title">代理控制台</h1>
        <p class="login-desc">账号由管理员开通，用于查看记录、管理 API 密钥与对接配置。</p>
      </div>
      <div class="card login-card">
        <h2 class="text-xl font-semibold text-ink mb-1">登录</h2>
        <p class="text-sm text-muted mb-6">无自助注册，请联系管理员获取账号</p>
        <div class="space-y-4">
          <div class="form-group">
            <label>用户名</label>
            <input v-model="form.username" class="input" autocomplete="username" placeholder="请输入用户名" @keyup.enter="submit" />
          </div>
          <div class="form-group">
            <label>密码</label>
            <input v-model="form.password" class="input" type="password" autocomplete="current-password" placeholder="请输入密码" @keyup.enter="submit" />
          </div>
          <div v-if="error" class="alert alert-error">{{ error }}</div>
          <button class="btn-primary w-full" :disabled="loading" @click="submit">
            {{ loading ? '登录中…' : '进入控制台' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import BrandMark from '../../components/BrandMark.vue'
import { useAgentAuthStore } from '../../stores/agentAuth'

const router = useRouter()
const route = useRoute()
const auth = useAgentAuthStore()
const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')

async function submit() {
  loading.value = true
  error.value = ''
  try {
    const res = await fetch('/api/v1/auth/agent/login', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: form.username.trim(), password: form.password }),
    })
    const data = await res.json()
    if (!res.ok) {
      error.value = data.error || '登录失败'
      return
    }
    auth.save({
      token: data.token,
      username: data.username,
      name: data.name || data.username,
      expiresAt: data.expires_at,
    })
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/partner'
    router.replace(redirect)
  } catch {
    error.value = '网络错误，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-shell {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px 20px;
  background: var(--bg);
  background-image:
    radial-gradient(ellipse 80% 60% at 20% 0%, color-mix(in srgb, var(--primary) 12%, transparent), transparent),
    var(--bg-tint);
}
.login-panel {
  width: 100%;
  max-width: 920px;
  display: grid;
  gap: 32px;
  align-items: center;
}
@media (min-width: 768px) {
  .login-panel { grid-template-columns: 1fr 400px; }
}
.login-brand { padding: 12px 8px; }
.login-title {
  margin-top: 20px;
  font-size: 2rem;
  font-weight: 800;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
  letter-spacing: -0.03em;
}
.login-desc {
  margin-top: 12px;
  font-size: 15px;
  line-height: 1.65;
  color: var(--ink-2);
  max-width: 36ch;
}
.login-card { padding: 28px !important; }
</style>
