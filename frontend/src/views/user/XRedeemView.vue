<template>
  <div :class="embedded ? '' : 'min-h-screen py-10'">
    <div :class="embedded ? '' : 'max-w-lg mx-auto px-4'">
      <div v-if="!embedded" class="mb-6 flex items-center justify-between">
        <router-link to="/" class="app-link text-sm">{{ t('common.backHome') }}</router-link>
        <div class="flex items-center gap-3">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </div>
      <h1 v-if="!embedded" class="text-2xl font-bold text-ink">{{ t('xRedeem.title') }}</h1>
      <p v-if="!embedded" class="mt-1 text-sm text-muted">{{ t('xRedeem.subtitle') }}</p>

      <!-- 步骤条与卡片样式和 ChatGPT 兑换页保持一致 -->
      <div class="card mb-6" :class="embedded ? '' : 'mt-6'">
        <div class="flex gap-2 text-sm flex-wrap">
          <span v-for="(label, i) in steps" :key="label" class="pill" :class="i === stepIndex ? 'pill-info' : ''">{{ i + 1 }}. {{ label }}</span>
        </div>
      </div>

      <div class="card space-y-4">
        <template v-if="phase === 'code'">
          <div class="flex items-baseline justify-between gap-3">
            <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.codeLabel') }}</h2>
            <a :href="CARD_SHOP_URL" target="_blank" rel="noopener noreferrer" class="text-sm app-link shrink-0">{{ t('recharge.shopHint') }}</a>
          </div>
          <input v-model="code" class="input mono" :placeholder="t('xRedeem.codePlaceholder')" @keyup.enter="preview" />
          <div v-if="error" class="alert alert-error">{{ error }}</div>
          <button class="btn-primary w-full" :disabled="busy" @click="preview">{{ t('xRedeem.next') }}</button>
        </template>

        <template v-else-if="phase === 'user' || phase === 'ineligible'">
          <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.handleLabel') }}</h2>
          <div v-if="phase === 'ineligible'" class="rounded-lg p-3 text-sm" style="background: var(--warn-soft, #fff7ed)">
            {{ t('xRedeem.ineligible') }}
          </div>
          <div class="rounded-xl bg-soft p-4 text-sm space-y-1">
            <div class="text-muted">{{ t('xRedeem.valid') }}</div>
            <div class="font-semibold">{{ state?.plan_label }}</div>
          </div>
          <input v-model="handle" class="input" :placeholder="t('xRedeem.handlePlaceholder')" />
          <p v-if="normalized" class="text-sm">{{ t('xRedeem.willOpen') }} <strong>@{{ normalized }}</strong></p>
          <p v-else-if="handle.trim()" class="text-sm" style="color: var(--err)">{{ t('xRedeem.handleInvalid') }}</p>
          <div v-if="error" class="alert alert-error">{{ error }}</div>
          <div class="flex gap-3">
            <button class="btn-secondary flex-1" @click="phase = 'code'">{{ t('xRedeem.back') }}</button>
            <button class="btn-primary flex-1" :disabled="!normalized || busy" @click="quote">{{ t('xRedeem.next') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'confirm'">
          <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.confirm') }}</h2>
          <div class="rounded-xl bg-soft p-4 space-y-2 text-sm">
            <div class="flex justify-between"><span class="text-muted">{{ t('xRedeem.plan') }}</span><strong>{{ state?.plan_label }}</strong></div>
            <div class="flex justify-between"><span class="text-muted">{{ t('xRedeem.recipient') }}</span><strong>@{{ state?.recipient }}</strong></div>
          </div>
          <p v-if="state?.detail" class="text-sm">{{ state.detail }}</p>
          <a class="app-link text-sm" :href="`https://x.com/${state?.recipient}`" target="_blank" rel="noopener">{{ t('xRedeem.checkOnX') }}</a>
          <p class="text-sm text-muted">{{ t('xRedeem.confirmHint') }}</p>
          <div v-if="error" class="alert alert-error">{{ error }}</div>
          <div class="flex gap-3">
            <button class="btn-secondary flex-1" @click="phase = 'user'">{{ t('xRedeem.backEdit') }}</button>
            <button class="btn-primary flex-1" :disabled="busy" @click="confirm">{{ t('xRedeem.confirm') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'queued' || phase === 'progress'">
          <h2 class="text-xl font-bold text-ink">{{ error ? t('xRedeem.failedTitle') : phase === 'queued' ? t('xRedeem.queuedTitle') : (state?.headline || t('xRedeem.title')) }}</h2>
          <div v-if="error" class="alert alert-error">{{ error }}</div>
          <p v-else-if="polling" class="text-sm text-muted">
            <span class="inline-block animate-pulse">●</span> {{ t('xRedeem.polling') }}
          </p>
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-2 text-center text-xs">
            <div
              v-for="p in openSteps"
              :key="p.key"
              class="rounded-lg border px-2 py-2"
              :class="p.active ? 'font-semibold text-ink' : 'text-muted'"
              :style="p.active ? { borderColor: 'var(--primary)', background: 'var(--primary-soft)' } : { borderColor: 'var(--brd)' }"
            >{{ p.label }}</div>
          </div>
          <p v-if="phase === 'queued'" class="text-sm text-muted">{{ t('xRedeem.queuedBody', { n: state?.queue_ahead || 0 }) }}</p>
          <template v-else-if="!error">
            <p class="text-sm text-muted">{{ t('xRedeem.progressBody', { name: state?.recipient || '' }) }}</p>
            <p class="text-sm">{{ t('xRedeem.progressKeep') }}</p>
          </template>
          <p v-if="state?.detail && !error" class="text-sm text-muted">{{ state.detail }}</p>
          <button v-if="error && state?.reusable" class="btn-secondary" @click="backToAccount">{{ t('xRedeem.backEdit') }}</button>
        </template>

        <template v-else-if="phase === 'done'">
          <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.doneTitle') }}</h2>
          <p class="text-sm">{{ t('xRedeem.doneBody', { name: state?.recipient || '', plan: state?.plan_label || '' }) }}</p>
          <div class="flex gap-3">
            <a class="btn-primary flex-1 text-center" :href="`https://x.com/${state?.recipient}`" target="_blank" rel="noopener">{{ t('xRedeem.openX') }}</a>
            <button class="btn-secondary flex-1" @click="reset">{{ t('xRedeem.again') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'locked'">
          <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.lockedTitle') }}</h2>
          <p v-if="polling" class="text-sm text-muted">
            <span class="inline-block animate-pulse">●</span> {{ t('xRedeem.polling') }}
          </p>
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-2 text-center text-xs">
            <div
              v-for="p in openSteps"
              :key="p.key"
              class="rounded-lg border px-2 py-2"
              :class="p.active ? 'font-semibold text-ink' : 'text-muted'"
              :style="p.active ? { borderColor: 'var(--primary)', background: 'var(--primary-soft)' } : { borderColor: 'var(--brd)' }"
            >{{ p.label }}</div>
          </div>
          <p class="text-sm">{{ t('xRedeem.lockedBody') }}</p>
          <p class="mono text-sm">{{ code }}</p>
          <button class="btn-secondary" @click="copyCode">{{ t('xRedeem.copyCode') }}</button>
        </template>

        <template v-else>
          <h2 class="text-xl font-bold text-ink">{{ t('xRedeem.deadTitle') }}</h2>
          <p class="text-sm text-muted">{{ t('xRedeem.deadBody') }}</p>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import LanguageToggle from '../../components/LanguageToggle.vue'
import ThemeToggle from '../../components/ThemeToggle.vue'

const CARD_SHOP_URL = 'https://card.danew.cc'

interface State {
  plan_label: string
  step: string
  headline: string
  detail: string
  recipient: string
  reusable?: boolean
  queued: boolean
  queue_ahead: number
  status: string
}

const props = defineProps<{ embedded?: boolean; initialCode?: string }>()
const emit = defineEmits<{ 'switch-gpt': [code: string] }>()
const { t, tm } = useI18n({ useScope: 'global' })
const route = useRoute()
const router = useRouter()
const steps = computed(() => {
  const raw = tm('xRedeem.steps') as unknown
  if (!Array.isArray(raw)) return []
  return raw.map((item) => String(item))
})
const code = ref('')
const handle = ref('')
const phase = ref('code')
const state = ref<State | null>(null)
const error = ref('')
const busy = ref(false)
const polling = ref(false)
const activating = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

const normalized = computed(() => {
  let s = handle.value.trim().replace(/^https?:\/\//i, '').replace(/^(www\.)?(x|twitter)\.com\//i, '')
  s = s.split(/[/?#]/)[0].replace(/^@/, '')
  return /^[A-Za-z0-9_]{1,15}$/.test(s) ? s : ''
})
const stepIndex = computed(() => {
  if (phase.value === 'code') return 0
  if (phase.value === 'user' || phase.value === 'ineligible') return 1
  if (phase.value === 'confirm') return 2
  return 3
})
const openSteps = computed(() => {
  const raw = tm('xRedeem.progressSteps') as unknown
  const labels = Array.isArray(raw) ? raw.map((item) => String(item)) : []
  const keys = ['accept', 'pay', 'wait', 'done']
  const st = String(state.value?.status || '').toLowerCase()
  let idx = 1
  if (phase.value === 'queued') idx = 0
  else if (phase.value === 'done' || st === 'completed') idx = 3
  else if (st === 'paid_pending_delivery') idx = 2
  else if (phase.value === 'locked') idx = 1
  return keys.map((key, i) => ({ key, label: labels[i] || key, active: i <= idx }))
})

function phaseOf(st: State) {
  if (st.queued) return 'queued'
  if (st.step === 'ineligible') return 'ineligible'
  if (st.step === 'code') return 'user'
  return st.step
}
function movingForward(next: string) {
  return next === 'progress' || next === 'queued' || next === 'locked' || next === 'done'
}
function apply(st: State) {
  state.value = st
  const next = phaseOf(st)
  if (activating.value) {
    // 点过开通后锁在第四步。报价还在、或轮询仍返回「待确认」，都继续等，不要跳回第三步。
    if (movingForward(next)) {
      error.value = ''
      phase.value = next
      if (next === 'done') stopPoll()
      else startPoll()
      return
    }
    if (next === 'confirm') {
      phase.value = 'progress'
      startPoll()
      return
    }
    error.value = st.detail || st.headline || t('xRedeem.requestFailed')
    phase.value = 'progress'
    stopPoll()
    return
  }
  error.value = ''
  phase.value = next
  if (phase.value === 'progress' || phase.value === 'queued' || phase.value === 'locked') startPoll()
  else stopPoll()
}
function backToAccount() {
  activating.value = false
  error.value = ''
  phase.value = 'user'
}

async function post(path: string, body: Record<string, string>) {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(data.error || t('xRedeem.requestFailed'))
  return data as State
}

async function preview() {
  const raw = code.value.trim()
  if (!raw) {
    error.value = t('xRedeem.codeRequired')
    return
  }
  // 只有本站 DNX- 码走用户名开通；其余码（含卡台 X 订阅码）交给 ChatGPT 兑换页识别，和那边只认 DNX- 的逻辑对称。
  if (!/^DNX-/i.test(raw)) {
    if (props.embedded) emit('switch-gpt', raw)
    else router.replace({ path: '/recharge', query: { code: raw } })
    return
  }
  busy.value = true
  error.value = ''
  try {
    apply(await post('/api/v1/public/x/preview', { code: raw }))
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : t('xRedeem.requestFailed')
  } finally {
    busy.value = false
  }
}

async function quote() {
  busy.value = true
  error.value = ''
  try {
    apply(await post('/api/v1/public/x/quote', { code: code.value.trim(), recipient: handle.value }))
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : t('xRedeem.requestFailed')
  } finally {
    busy.value = false
  }
}

async function confirm() {
  busy.value = true
  error.value = ''
  activating.value = true
  // 先进入开通进度，避免确认请求还在付款时页面停在确认按钮上。
  phase.value = 'progress'
  if (state.value) state.value = { ...state.value, status: 'paying' }
  startPoll()
  try {
    apply(await post('/api/v1/public/x/confirm', { code: code.value.trim() }))
  } catch (e: unknown) {
    phase.value = 'progress'
    stopPoll()
    error.value = e instanceof Error ? e.message : t('xRedeem.requestFailed')
  } finally {
    busy.value = false
  }
}

async function poll() {
  if (busy.value) return
  const r = await fetch('/api/v1/public/x/result?code=' + encodeURIComponent(code.value.trim()))
  const data = await r.json().catch(() => null)
  if (r.ok && data) apply(data)
}
function startPoll() {
  polling.value = true
  if (timer) return
  timer = setInterval(() => void poll(), 4000)
}
function stopPoll() {
  polling.value = false
  if (timer) clearInterval(timer)
  timer = null
}
function reset() {
  stopPoll()
  activating.value = false
  code.value = ''
  handle.value = ''
  state.value = null
  error.value = ''
  phase.value = 'code'
}
async function copyCode() {
  try { await navigator.clipboard.writeText(code.value.trim()) } catch { /* ignore */ }
}

onMounted(() => {
  const q = String(props.initialCode || route.query.code || '').trim()
  if (q) {
    code.value = q
    void preview()
  }
})
onUnmounted(stopPoll)
</script>
