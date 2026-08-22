<template>
  <div class="space-y-5">
    <section class="card">
      <div class="doc-head">
        <div>
          <h3 class="section-title !mb-1">{{ spec?.info?.title || '代理开放 API' }}</h3>
          <p class="text-sm text-subtle">
            版本 <code class="mono">{{ spec?.info?.version || '—' }}</code>
            <span v-if="baseUrl"> · 基地址 <code class="mono">{{ baseUrl }}</code></span>
          </p>
        </div>
        <div class="doc-actions">
          <button class="btn-secondary !min-h-0 !py-1.5 text-sm" @click="download('json')">
            下载 OpenAPI (JSON)
          </button>
          <button class="btn-secondary !min-h-0 !py-1.5 text-sm" @click="download('yaml')">
            下载 OpenAPI (YAML)
          </button>
          <button class="btn-secondary !min-h-0 !py-1.5 text-sm" @click="download('md')">
            下载 Markdown
          </button>
        </div>
      </div>
      <p class="alert alert-info mt-4">
        JSON / YAML 可直接导入 Postman、Apifox 等工具，导入后把 API Key 填进 Bearer Token 即可调试。
      </p>
    </section>

    <section v-if="loading" class="card text-center text-muted py-10">加载文档中…</section>
    <section v-else-if="loadError" class="card">
      <p class="alert alert-warn">{{ loadError }}</p>
    </section>

    <template v-else>
      <!-- 规范里的总述：鉴权、异步模型、幂等、限额、错误码 -->
      <section v-if="overviewHtml" class="card">
        <!-- eslint-disable-next-line vue/no-v-html -- 内容来自后端内嵌的 OpenAPI 规范，非用户输入，且渲染前已转义 -->
        <div class="md" v-html="overviewHtml" />
      </section>

      <!-- 接口参考 -->
      <section class="card">
        <h3 class="section-title">接口参考</h3>
        <div class="space-y-2">
          <div v-for="op in operations" :key="op.id" class="op">
            <button class="op-head" @click="toggle(op.id)">
              <span class="method" :class="`m-${op.method}`">{{ op.method.toUpperCase() }}</span>
              <code class="op-path mono">{{ op.path }}</code>
              <span class="op-summary">{{ op.summary }}</span>
              <span class="op-caret">{{ expanded.has(op.id) ? '−' : '+' }}</span>
            </button>

            <div v-if="expanded.has(op.id)" class="op-body">
              <!-- eslint-disable-next-line vue/no-v-html -- 同上，来源为内嵌规范 -->
              <div v-if="op.descHtml" class="md" v-html="op.descHtml" />

              <div v-if="op.params.length" class="sub">
                <h4 class="sub-title">参数</h4>
                <table class="data-table compact">
                  <thead>
                    <tr><th>名称</th><th>位置</th><th>必填</th><th>说明</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="p in op.params" :key="p.name + p.in">
                      <td class="mono text-xs">{{ p.name }}</td>
                      <td class="text-xs text-muted">{{ p.in }}</td>
                      <td class="text-xs">{{ p.required ? '是' : '否' }}</td>
                      <td class="text-xs text-muted">{{ p.description || '—' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <div v-if="op.example" class="sub">
                <h4 class="sub-title">请求体示例</h4>
                <pre class="md-pre"><code>{{ op.example }}</code></pre>
              </div>

              <div class="sub">
                <div class="sub-title-row">
                  <h4 class="sub-title !mb-0">cURL</h4>
                  <button class="btn-secondary !min-h-0 !py-1 !px-3 text-xs" @click="copy(op.id, op.curl)">
                    {{ copiedId === op.id ? '已复制' : '复制' }}
                  </button>
                </div>
                <pre class="md-pre"><code>{{ op.curl }}</code></pre>
              </div>

              <div v-if="op.responses.length" class="sub">
                <h4 class="sub-title">响应</h4>
                <table class="data-table compact">
                  <thead><tr><th>状态码</th><th>说明</th></tr></thead>
                  <tbody>
                    <tr v-for="r in op.responses" :key="r.code">
                      <td class="mono text-xs">{{ r.code }}</td>
                      <td class="text-xs text-muted">{{ r.description || '—' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Webhook 说明 -->
      <section v-if="webhookHtml" class="card">
        <h3 class="section-title">Webhook 回调</h3>
        <!-- eslint-disable-next-line vue/no-v-html -- 同上，来源为内嵌规范 -->
        <div class="md" v-html="webhookHtml" />
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { agentFetch } from '../../lib/agentApi'
import { renderMarkdown } from './miniMarkdown'

interface ParamRow { name: string; in: string; required: boolean; description: string }
interface RespRow { code: string; description: string }
interface OpRow {
  id: string
  method: string
  path: string
  summary: string
  descHtml: string
  params: ParamRow[]
  example: string
  curl: string
  responses: RespRow[]
}

const spec = ref<any>(null)
const loading = ref(true)
const loadError = ref('')
const expanded = ref(new Set<string>())
const copiedId = ref('')

const METHODS = ['get', 'post', 'put', 'patch', 'delete']

const baseUrl = computed(() => spec.value?.servers?.[0]?.url || '')

const overviewHtml = computed(() =>
  spec.value?.info?.description ? renderMarkdown(spec.value.info.description) : '',
)

const webhookHtml = computed(() => {
  const d = spec.value?.components?.schemas?.WebhookEvent?.description
  return d ? renderMarkdown(d) : ''
})

/** 顺着 `#/a/b/c` 取规范里的节点，复用的响应靠它才拿得到 description。 */
function deref(node: any): any {
  if (!node || typeof node.$ref !== 'string') return node
  let cur: any = spec.value
  for (const seg of node.$ref.replace(/^#\//, '').split('/')) {
    cur = cur?.[seg]
    if (!cur) return node
  }
  return cur
}

const operations = computed<OpRow[]>(() => {
  const paths = spec.value?.paths || {}
  const out: OpRow[] = []
  for (const path of Object.keys(paths).sort()) {
    for (const method of METHODS) {
      const op = paths[path]?.[method]
      if (!op) continue
      const params: ParamRow[] = (op.parameters || []).map((raw: any) => {
        const p = deref(raw)
        return {
          name: p.name,
          in: p.in,
          required: !!p.required,
          description: p.description || '',
        }
      })
      const example = requestExample(op)
      out.push({
        id: `${method}:${path}`,
        method,
        path,
        summary: op.summary || '',
        descHtml: op.description ? renderMarkdown(op.description) : '',
        params,
        example,
        curl: buildCurl(method, path, example),
        responses: Object.keys(op.responses || {})
          .sort()
          .map((code) => ({ code, description: deref(op.responses[code])?.description || '' })),
      })
    }
  }
  return out
})

function requestExample(op: any): string {
  const json = op.requestBody?.content?.['application/json']
  if (!json) return ''
  if (json.example !== undefined) return JSON.stringify(json.example, null, 2)
  const first = Object.values(json.examples || {})[0] as any
  return first?.value !== undefined ? JSON.stringify(first.value, null, 2) : ''
}

function buildCurl(method: string, path: string, example: string): string {
  const url = `${baseUrl.value || '/api/v1'}${path}`
  const lines = [`curl -X ${method.toUpperCase()} '${url}' \\`, `  -H 'Authorization: Bearer ak_live_你的密钥' \\`]
  if (example) {
    lines.push(`  -H 'Content-Type: application/json' \\`)
    if (method === 'post' && path.includes('batch-recharge')) {
      lines.push(`  -H 'Idempotency-Key: $(uuidgen)' \\`)
    }
    lines.push(`  -d '${example.replace(/\n\s*/g, '')}'`)
  } else {
    lines[lines.length - 1] = lines[lines.length - 1].replace(/ \\$/, '')
  }
  return lines.join('\n')
}

function toggle(id: string) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}

async function copy(id: string, text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedId.value = id
    setTimeout(() => (copiedId.value = ''), 1500)
  } catch {
    /* 剪贴板不可用时用户仍可手动选中复制 */
  }
}

async function download(kind: 'json' | 'yaml' | 'md') {
  const res = await agentFetch(`/api/v1/agent/openapi.${kind}?download=1`)
  if (!res.ok) return
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = kind === 'md' ? 'agent-api-zh.md' : `agent-openapi.${kind}`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(async () => {
  try {
    const res = await agentFetch('/api/v1/agent/openapi.json')
    if (!res.ok) {
      loadError.value = '文档加载失败，请稍后重试'
      return
    }
    spec.value = await res.json()
  } catch {
    loadError.value = '文档加载失败，请检查网络'
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.section-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
  margin-bottom: 16px;
}
.doc-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.doc-actions { display: flex; flex-wrap: wrap; gap: 8px; }

.op {
  border: 1px solid var(--brd);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.op-head {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  background: var(--surface);
  text-align: left;
  transition: background var(--dur-2) var(--ease-standard);
}
.op-head:hover { background: var(--primary-soft); }
.method {
  flex-shrink: 0;
  min-width: 54px;
  text-align: center;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  font-family: var(--font-mono);
  color: #fff;
}
.m-get { background: #2563eb; }
.m-post { background: #16a34a; }
.m-put { background: #d97706; }
.m-patch { background: #d97706; }
.m-delete { background: #dc2626; }
.op-path { font-size: 13px; color: var(--ink); }
.op-summary {
  flex: 1;
  font-size: 12px;
  color: var(--ink-3);
  text-align: right;
}
.op-caret {
  flex-shrink: 0;
  width: 18px;
  text-align: center;
  color: var(--ink-3);
  font-size: 15px;
}
.op-body {
  padding: 16px;
  border-top: 1px solid var(--brd);
  background: var(--bg);
}
.sub { margin-top: 16px; }
.sub:first-child { margin-top: 0; }
.sub-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-2);
  margin-bottom: 8px;
}
.sub-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}
.compact :deep(th),
.compact :deep(td) { padding: 6px 10px; }
</style>

<style>
/* md 内容由 v-html 注入，作用域样式选不中，只能全局限定在 .md 下 */
.md { font-size: 14px; color: var(--ink-2); line-height: 1.7; }
.md .md-h {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
  margin: 20px 0 8px;
}
.md .md-h:first-child { margin-top: 0; }
.md .md-p { margin: 8px 0; }
.md .md-list { margin: 8px 0 8px 20px; list-style: disc; }
.md ol.md-list { list-style: decimal; }
.md .md-list li { margin: 4px 0; }
.md code {
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--primary-soft);
  color: var(--primary);
}
/* 代码块在 .md 之外也会用到（请求体示例、cURL），所以不限定在 .md 下 */
.md-pre {
  margin: 10px 0;
  padding: 12px 14px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--brd);
  background: var(--surface);
  overflow-x: auto;
}
.md-pre code {
  font-family: var(--font-mono);
  background: none;
  color: var(--ink);
  padding: 0;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre;
}
.md .md-table {
  width: 100%;
  margin: 10px 0;
  border-collapse: collapse;
  font-size: 13px;
}
.md .md-table th,
.md .md-table td {
  border: 1px solid var(--brd);
  padding: 6px 10px;
  text-align: left;
}
.md .md-table th {
  background: var(--surface);
  font-weight: 600;
  color: var(--ink);
}
</style>
