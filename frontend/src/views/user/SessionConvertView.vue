<template>
  <div class="min-h-screen py-12">
    <div class="max-w-3xl mx-auto px-6 space-y-6">
      <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
        <div>
          <router-link to="/" class="app-link mb-4 inline-block text-sm">{{ t('common.backHome') }}</router-link>
          <h1 class="text-3xl font-bold text-ink">{{ t('convert.title') }}</h1>
          <p class="text-sm text-muted mt-1">{{ t('convert.subtitle') }}</p>
        </div>
        <div class="flex gap-2">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </div>

      <div class="card space-y-4">
        <div class="rounded-xl bg-soft p-4 text-sm text-muted">
          {{ t('convert.hintPrefix') }}
          <a class="app-link underline" href="https://chatgpt.com/api/auth/session" target="_blank" rel="noopener">
            chatgpt.com/api/auth/session
          </a>
          {{ t('convert.hintSuffix') }}
        </div>

        <div class="form-group">
          <label>{{ t('convert.pasteLabel') }}</label>
          <textarea
            v-model="pasteText"
            class="input h-40 font-mono text-xs"
            :placeholder="t('convert.pastePlaceholder')"
          />
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <button type="button" class="btn-secondary !py-1.5 text-sm" :disabled="!pasteText.trim()" @click="commitPaste(true)">
              {{ t('convert.addToList') }}
            </button>
            <span v-if="pasteHint" class="text-xs text-muted">{{ pasteHint }}</span>
          </div>
        </div>

        <div v-if="accepted.length" class="rounded-xl bg-soft p-3 space-y-2">
          <div class="flex items-center justify-between gap-2">
            <div class="text-sm font-medium text-ink">{{ t('convert.listTitle', { n: accepted.length }) }}</div>
            <button type="button" class="text-xs hover:underline" style="color: var(--err, #dc2626)" @click="clearAccepted">
              {{ t('convert.clearList') }}
            </button>
          </div>
          <div class="space-y-1.5 max-h-40 overflow-y-auto">
            <div
              v-for="item in accepted"
              :key="item.key"
              class="flex items-center justify-between gap-2 text-xs font-mono text-muted"
            >
              <span class="truncate">{{ item.email || t('convert.noEmail') }}</span>
              <button type="button" class="app-link shrink-0" @click="removeAccepted(item.key)">{{ t('convert.remove') }}</button>
            </div>
          </div>
        </div>

        <div class="space-y-1.5">
          <div class="flex items-center justify-between gap-2 flex-wrap">
            <label class="block text-sm font-medium text-ink">{{ t('convert.excelLabel') }}</label>
            <button
              v-if="sessionPool.length > 0"
              type="button"
              class="text-xs hover:underline"
              style="color: var(--err, #dc2626)"
              @click="clearImport"
            >
              {{ t('convert.clearImport') }}
            </button>
          </div>
          <input
            ref="fileInputRef"
            type="file"
            accept=".xlsx,.xls,.csv"
            class="hidden"
            @change="onFileChange"
          />
          <button
            type="button"
            class="w-full py-2.5 rounded-xl border border-dashed text-sm font-medium disabled:opacity-40"
            style="border-color: color-mix(in srgb, var(--primary) 45%, var(--brd)); color: var(--primary)"
            :disabled="importing"
            @click="fileInputRef?.click()"
          >
            {{
              importing
                ? t('convert.importing')
                : sessionPool.length
                  ? t('convert.imported', { n: sessionPool.length })
                  : t('convert.excelBtn')
            }}
          </button>
          <p v-if="importMsg" class="text-xs text-muted leading-relaxed">{{ importMsg }}</p>
          <div
            v-if="sessionPool.length > 0"
            class="rounded-lg bg-soft px-3 py-2 text-[11px] text-muted max-h-24 overflow-y-auto font-mono"
          >
            <div v-for="(s, i) in sessionPool.slice(0, 8)" :key="i">
              {{ i + 1 }}. {{ s.email || t('convert.noEmail') }} · session {{ s.session.length }}
            </div>
            <div v-if="sessionPool.length > 8">…{{ sessionPool.length }}</div>
          </div>
          <p class="text-[11px] text-subtle">{{ t('convert.excelHint') }}</p>
        </div>

        <div>
          <label class="block text-sm font-medium text-ink mb-2">{{ t('convert.formatLabel') }}</label>
          <div class="flex gap-2">
            <button
              type="button"
              class="btn-secondary !py-1.5"
              :class="{ 'ring-2 ring-offset-1': format === 'sub2api' }"
              style="--tw-ring-color: var(--primary)"
              @click="format = 'sub2api'"
            >{{ t('convert.formatSub2api') }}</button>
            <button
              type="button"
              class="btn-secondary !py-1.5"
              :class="{ 'ring-2 ring-offset-1': format === 'cpa' }"
              style="--tw-ring-color: var(--primary)"
              @click="format = 'cpa'"
            >{{ t('convert.formatCpa') }}</button>
            <button
              type="button"
              class="btn-secondary !py-1.5"
              :class="{ 'ring-2 ring-offset-1': format === 'cockpit' }"
              style="--tw-ring-color: var(--primary)"
              @click="format = 'cockpit'"
            >{{ t('convert.formatCockpit') }}</button>
          </div>
        </div>

        <p class="text-xs text-subtle">{{ t('convert.notice') }}</p>
      </div>

      <div v-if="hasInput && !result.converted.length && result.skipped.length" class="alert alert-error">
        {{ t('convert.noSessions') }}
      </div>

      <div v-if="result.converted.length || result.skipped.length" class="card space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-xl font-semibold text-ink">{{ t('convert.previewTitle') }}</h2>
          <div class="flex gap-2 text-xs">
            <span class="rounded-full px-2.5 py-1" style="background: var(--primary-soft); color: var(--primary)">
              {{ t('convert.okCount', { n: result.converted.length }) }}
            </span>
            <span
              v-if="result.skipped.length"
              class="rounded-full px-2.5 py-1"
              style="background: color-mix(in srgb, var(--err, #dc2626) 12%, transparent); color: var(--err, #dc2626)"
            >
              {{ t('convert.failCount', { n: result.skipped.length }) }}
            </span>
            <button
              v-if="refreshableCount"
              type="button"
              class="btn-secondary !py-1.5 text-xs"
              :disabled="refreshing"
              @click="refreshThenConvert"
            >
              {{ refreshing ? t('convert.refreshingRetry') : t('convert.refreshRetry') }}
            </button>
          </div>
        </div>
        <p v-if="refreshableCount" class="text-xs text-subtle">{{ t('convert.refreshRetryHint') }}</p>
        <p v-if="refreshMsg" class="text-xs" :style="refreshMsgErr ? 'color: var(--err, #dc2626)' : 'color: var(--primary)'">
          {{ refreshMsg }}
        </p>

        <div v-if="result.converted.length" class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="text-muted text-left">
              <tr class="border-b bd">
                <th class="py-2 pr-3 font-medium">{{ t('convert.email') }}</th>
                <th class="py-2 pr-3 font-medium">{{ t('convert.plan') }}</th>
                <th class="py-2 font-medium">{{ t('convert.expires') }}</th>
                <th class="py-2 font-medium w-16"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in result.converted.slice(0, 40)" :key="i" class="border-b bd last:border-0">
                <td class="py-2 pr-3 font-mono text-xs text-ink">{{ row.email || t('convert.noEmail') }}</td>
                <td class="py-2 pr-3 text-muted">{{ row.planType || '—' }}</td>
                <td class="py-2 text-muted font-mono text-xs">
                  {{ formatDisplayDate(row.expiresAt) }}
                  <span
                    v-if="isTimestampExpired(row.expiresAt)"
                    class="ml-1 text-[11px]"
                    style="color: var(--err, #dc2626)"
                  >{{ t('convert.expiredTag') }}</span>
                </td>
                <td class="py-2 text-right">
                  <button
                    v-if="accepted.some((item) => item.key === row.sourceName)"
                    type="button"
                    class="app-link text-xs"
                    @click="removeAccepted(row.sourceName)"
                  >{{ t('convert.remove') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="result.converted.length > 40" class="text-xs text-subtle mt-2">…{{ result.converted.length }}</p>
          <p class="text-xs text-subtle mt-3">
            {{ t('convert.planHint') }}
            <router-link to="/inspect" class="app-link">{{ t('home.tools.inspect.title') }}</router-link>
          </p>
        </div>

        <div v-if="result.skipped.length" class="rounded-xl bg-soft p-3 text-xs text-muted space-y-1">
          <div class="font-medium text-ink">{{ t('convert.skippedTitle') }}</div>
          <div v-for="(item, i) in result.skipped.slice(0, 12)" :key="i">
            {{ item.path }} — {{ item.reason }}
            <span v-if="item.canRefresh" class="text-subtle"> · {{ t('convert.refreshRetry') }}</span>
          </div>
        </div>
      </div>

      <div v-if="outputText" class="card space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-xl font-semibold text-ink">{{ t('convert.outputTitle') }}</h2>
          <div class="flex gap-2">
            <button type="button" class="btn-secondary !py-1.5" @click="copyOutput">
              {{ copied ? t('convert.copied') : t('convert.copy') }}
            </button>
            <button type="button" class="btn-primary !py-1.5" @click="downloadOutput">
              {{ t('convert.download') }}
            </button>
          </div>
        </div>
        <textarea :value="outputText" readonly class="input h-56 font-mono text-xs" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LanguageToggle from '../../components/LanguageToggle.vue'
import ThemeToggle from '../../components/ThemeToggle.vue'
import { parseSessionsFromSheet, readWorkbookRows, type ImportedSession } from '../../lib/batch-session'
import { refreshSessions, SESSION_TOOL_MAX } from '../../lib/session-api'
import {
  buildExportDocument,
  collectSessionEntries,
  convertedRefreshRaw,
  convertImportedSessions,
  convertSessionsFromText,
  exportFilename,
  formatDisplayDate,
  isTimestampExpired,
  mergeConvertResults,
  type ExportFormat,
  type ConvertResult,
  type SessionEntry,
} from '../../lib/session-export'

const { t } = useI18n({ useScope: 'global' })

const pasteText = ref('')
const accepted = ref<SessionEntry[]>([])
const pasteHint = ref('')
const format = ref<ExportFormat>('sub2api')
const sessionPool = ref<ImportedSession[]>([])
const importMsg = ref('')
const importing = ref(false)
const fileInputRef = ref<HTMLInputElement | null>(null)
const copied = ref(false)
const result = ref<ConvertResult>({ converted: [], skipped: [] })
const overrides = ref<Array<{ path: string; sessionRaw: string }>>([])
const refreshing = ref(false)
const refreshMsg = ref('')
const refreshMsgErr = ref(false)

const hasInput = computed(() =>
  Boolean(accepted.value.length || pasteText.value.trim() || sessionPool.value.length),
)

const outputText = computed(() => {
  if (!result.value.converted.length) return ''
  return JSON.stringify(buildExportDocument(result.value.converted, format.value), null, 2)
})

const refreshableCount = computed(() => refreshJobs().length)

function refreshJobs() {
  const jobs: Array<{ path: string; sessionRaw: string }> = []
  const seen = new Set<string>()
  for (const item of result.value.skipped) {
    if (!item.canRefresh || !item.sessionRaw) continue
    const key = item.path || item.sessionRaw
    if (seen.has(key)) continue
    seen.add(key)
    jobs.push({ path: key, sessionRaw: item.sessionRaw })
  }
  result.value.converted.forEach((row, index) => {
    if (!isTimestampExpired(row.expiresAt)) return
    const sessionRaw = convertedRefreshRaw(row)
    if (!sessionRaw) return
    const key = row.sourcePath || `converted[${index}]`
    if (seen.has(key)) return
    seen.add(key)
    jobs.push({ path: key, sessionRaw })
  })
  return jobs.slice(0, SESSION_TOOL_MAX)
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(pasteText, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    commitPaste(false)
  }, 200)
})

watch(sessionPool, () => {
  refreshMsg.value = ''
  runConvert()
}, { deep: true })

function commitPaste(clearBox: boolean) {
  const entries = collectSessionEntries(pasteText.value)
  if (!pasteText.value.trim()) {
    pasteHint.value = ''
    runConvert()
    return
  }
  if (!entries.length) {
    pasteHint.value = ''
    overrides.value = []
    refreshMsg.value = ''
    runConvert()
    return
  }
  let added = 0
  const next = accepted.value.slice()
  for (const entry of entries) {
    if (next.some((item) => item.key === entry.key)) continue
    next.push(entry)
    added += 1
  }
  accepted.value = next
  pasteHint.value = added ? t('convert.addedMsg', { n: added }) : t('convert.alreadyMsg')
  if (clearBox && added) pasteText.value = ''
  overrides.value = []
  refreshMsg.value = ''
  runConvert()
}

function removeAccepted(key: string) {
  accepted.value = accepted.value.filter((item) => item.key !== key)
  overrides.value = overrides.value.filter((item) => item.path !== key)
  runConvert()
}

function clearAccepted() {
  accepted.value = []
  overrides.value = []
  pasteHint.value = ''
  refreshMsg.value = ''
  runConvert()
}

function runConvert() {
  copied.value = false
  if (!hasInput.value && !overrides.value.length) {
    result.value = { converted: [], skipped: [] }
    return
  }
  const parts = [
    ...accepted.value.map((item) => convertSessionsFromText(item.raw, item.key)),
    convertImportedSessions(sessionPool.value),
  ]
  if (pasteText.value.trim() && !collectSessionEntries(pasteText.value).every((e) => accepted.value.some((a) => a.key === e.key))) {
    parts.push(convertSessionsFromText(pasteText.value))
  }
  const base = mergeConvertResults(...parts)
  if (!overrides.value.length) {
    result.value = base
    return
  }
  const extra = mergeConvertResults(
    ...overrides.value.map((o) => convertSessionsFromText(o.sessionRaw, o.path)),
  )
  const done = new Set(overrides.value.map((o) => o.path))
  result.value = {
    converted: [
      ...base.converted.filter((row) => !done.has(row.sourcePath || '')),
      ...extra.converted,
    ],
    skipped: [
      ...base.skipped.filter((s) => !done.has(s.path || s.sessionRaw || '')),
      ...extra.skipped,
    ],
  }
}

async function refreshThenConvert() {
  const jobs = refreshJobs()
  if (!jobs.length) {
    refreshMsgErr.value = true
    refreshMsg.value = t('convert.refreshRetryNone')
    return
  }
  refreshing.value = true
  refreshMsg.value = ''
  refreshMsgErr.value = false
  try {
    const results = await refreshSessions(
      jobs.map((j) => j.sessionRaw),
      { down: t('inspect.backendDown'), fail: t('inspect.refreshFail') },
    )
    let ok = 0
    const next = overrides.value.filter((o) => !jobs.some((j) => j.path === o.path))
    results.forEach((row, i) => {
      if (!row?.ok || !row.session) return
      ok += 1
      next.push({ path: jobs[i].path, sessionRaw: JSON.stringify(row.session) })
    })
    overrides.value = next
    runConvert()
    refreshMsgErr.value = ok === 0
    refreshMsg.value = ok
      ? t('convert.refreshRetryDone', { ok })
      : results[0]?.error || t('inspect.refreshFail')
  } catch (e) {
    refreshMsgErr.value = true
    refreshMsg.value = e instanceof Error ? e.message : t('inspect.refreshFail')
  } finally {
    refreshing.value = false
  }
}

function clearImport() {
  sessionPool.value = []
  importMsg.value = ''
}

async function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  input.value = ''
  if (!file) return
  importing.value = true
  importMsg.value = ''
  try {
    const rows = await readWorkbookRows(file)
    const { sessions, skippedDup } = parseSessionsFromSheet(rows)
    if (!sessions.length) {
      sessionPool.value = []
      importMsg.value = t('convert.excelEmpty')
      return
    }
    sessionPool.value = sessions
    importMsg.value =
      t('convert.excelOk', { name: file.name, n: sessions.length }) +
      (skippedDup > 0 ? t('convert.excelDup', { n: skippedDup }) : '')
  } catch (err) {
    sessionPool.value = []
    importMsg.value = t('convert.excelReadFail', {
      msg: err instanceof Error ? err.message : String(err),
    })
  } finally {
    importing.value = false
  }
}

async function copyOutput() {
  if (!outputText.value) return
  try {
    await navigator.clipboard.writeText(outputText.value)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1600)
  } catch {
    importMsg.value = t('convert.copyFail')
  }
}

function downloadOutput() {
  if (!outputText.value) return
  const blob = new Blob([outputText.value], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = exportFilename(format.value, result.value.converted.length)
  a.click()
  URL.revokeObjectURL(url)
}
</script>
