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

      <div class="flex items-center text-sm" :class="embedded ? '' : 'mt-6'">
        <template v-for="(label, i) in steps" :key="label">
          <span :class="i <= stepIndex ? 'text-ink font-medium' : 'text-muted'">{{ i + 1 }}. {{ label }}</span>
          <span v-if="i < steps.length - 1" class="mx-2 h-px flex-1 bg-current opacity-20" />
        </template>
      </div>

      <div class="card mt-6 space-y-4">
        <div v-if="error" class="rounded-lg bg-soft p-3 text-sm" style="color: var(--err)">{{ error }}</div>

        <template v-if="phase === 'code'">
          <label class="text-sm font-medium">{{ t('xRedeem.codeLabel') }}</label>
          <input v-model="code" class="input mono" :placeholder="t('xRedeem.codePlaceholder')" @keyup.enter="preview" />
          <p class="text-xs text-muted">{{ t('xRedeem.codeHint') }}</p>
          <button class="btn-primary" :disabled="busy" @click="preview">{{ t('xRedeem.next') }}</button>
        </template>

        <template v-else-if="phase === 'user' || phase === 'ineligible'">
          <div v-if="phase === 'ineligible'" class="rounded-lg p-3 text-sm" style="background: var(--warn-soft, #fff7ed)">
            {{ t('xRedeem.ineligible') }}
          </div>
          <div class="rounded-lg bg-soft p-3">
            <div class="text-xs text-muted">{{ t('xRedeem.valid') }}</div>
            <div class="font-semibold">{{ state?.plan_label }}</div>
          </div>
          <label class="text-sm font-medium">{{ t('xRedeem.handleLabel') }}</label>
          <input v-model="handle" class="input" :placeholder="t('xRedeem.handlePlaceholder')" />
          <p v-if="normalized" class="text-sm">{{ t('xRedeem.willOpen') }} <strong>@{{ normalized }}</strong></p>
          <p v-else-if="handle.trim()" class="text-sm" style="color: var(--err)">{{ t('xRedeem.handleInvalid') }}</p>
          <div class="flex gap-2">
            <button class="btn-secondary" @click="phase = 'code'">{{ t('xRedeem.back') }}</button>
            <button class="btn-primary" :disabled="!normalized || busy" @click="quote">{{ t('xRedeem.next') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'confirm'">
          <div class="space-y-2 text-sm">
            <div class="flex justify-between"><span class="text-muted">{{ t('xRedeem.plan') }}</span><strong>{{ state?.plan_label }}</strong></div>
            <div class="flex justify-between"><span class="text-muted">{{ t('xRedeem.recipient') }}</span><strong>@{{ state?.recipient }}</strong></div>
          </div>
          <p v-if="state?.detail" class="text-sm">{{ state.detail }}</p>
          <a class="app-link text-sm" :href="`https://x.com/${state?.recipient}`" target="_blank" rel="noopener">{{ t('xRedeem.checkOnX') }}</a>
          <p class="text-sm">{{ t('xRedeem.confirmHint') }}</p>
          <div class="flex gap-2">
            <button class="btn-secondary" @click="phase = 'user'">{{ t('xRedeem.backEdit') }}</button>
            <button class="btn-primary" :disabled="busy" @click="confirm">{{ t('xRedeem.confirm') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'queued'">
          <h2 class="font-semibold">{{ t('xRedeem.queuedTitle') }}</h2>
          <p class="text-sm text-muted">{{ t('xRedeem.queuedBody', { n: state?.queue_ahead || 0 }) }}</p>
        </template>

        <template v-else-if="phase === 'progress'">
          <h2 class="font-semibold">{{ state?.headline || t('xRedeem.title') }}</h2>
          <p class="text-sm text-muted">{{ t('xRedeem.progressBody', { name: state?.recipient || '' }) }}</p>
          <p class="text-sm">{{ t('xRedeem.progressKeep') }}</p>
        </template>

        <template v-else-if="phase === 'done'">
          <h2 class="font-semibold">{{ t('xRedeem.doneTitle') }}</h2>
          <p class="text-sm">{{ t('xRedeem.doneBody', { name: state?.recipient || '', plan: state?.plan_label || '' }) }}</p>
          <div class="flex gap-2">
            <a class="btn-primary" :href="`https://x.com/${state?.recipient}`" target="_blank" rel="noopener">{{ t('xRedeem.openX') }}</a>
            <button class="btn-secondary" @click="reset">{{ t('xRedeem.again') }}</button>
          </div>
        </template>

        <template v-else-if="phase === 'locked'">
          <h2 class="font-semibold">{{ t('xRedeem.lockedTitle') }}</h2>
          <p class="text-sm">{{ t('xRedeem.lockedBody') }}</p>
          <p class="mono text-sm">{{ code }}</p>
          <button class="btn-secondary" @click="copyCode">{{ t('xRedeem.copyCode') }}</button>
        </template>

        <template v-else>
          <h2 class="font-semibold">{{ t('xRedeem.deadTitle') }}</h2>
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

interface State {
  plan_label: string
  step: string
  headline: string
  detail: string
  recipient: string
  reusable: boolean
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

function apply(st: State) {
  state.value = st
  error.value = st.detail && st.step !== 'confirm' && st.step !== 'done' ? '' : error.value
  if (st.queued) phase.value = 'queued'
  else if (st.step === 'ineligible') phase.value = 'ineligible'
  else phase.value = st.step === 'code' ? 'user' : st.step
  if (phase.value === 'progress' || phase.value === 'queued' || phase.value === 'locked') startPoll()
  else stopPoll()
}

async function post(path: string, body: Record<string, string>) {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(data.error || t('xRedeem.requestFailed'))
  return data as State
}

async function preview() {
  const raw = code.value.trim()
  if (/^DN-/i.test(raw) && !/^DNX-/i.test(raw)) {
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
  try {
    apply(await post('/api/v1/public/x/confirm', { code: code.value.trim() }))
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : t('xRedeem.requestFailed')
  } finally {
    busy.value = false
  }
}

async function poll() {
  const r = await fetch('/api/v1/public/x/result?code=' + encodeURIComponent(code.value.trim()))
  const data = await r.json().catch(() => null)
  if (r.ok && data) apply(data)
}
function startPoll() {
  if (timer) return
  timer = setInterval(() => void poll(), 4000)
}
function stopPoll() {
  if (timer) clearInterval(timer)
  timer = null
}
function reset() {
  stopPoll()
  code.value = ''
  handle.value = ''
  state.value = null
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
