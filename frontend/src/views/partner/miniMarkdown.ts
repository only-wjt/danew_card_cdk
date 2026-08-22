/**
 * OpenAPI 规范里的描述字段是 Markdown，门户要把它渲染出来。
 *
 * 这里只实现文档实际用到的子集（标题、代码块、表格、列表、粗体、行内代码），
 * 换取不引入 markdown 解析器 + 消毒库两个依赖。输入始终是后端内嵌的规范文件，
 * 不含用户输入，但仍然先做 HTML 转义再拼标签，避免以后被当成通用渲染器误用。
 */

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/** 行内格式：**粗体** 与 `代码`。必须在转义之后执行。 */
function inline(escaped: string): string {
  return escaped
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
}

function isTableRow(line: string): boolean {
  return line.trim().startsWith('|') && line.trim().endsWith('|')
}

function isTableDivider(line: string): boolean {
  return /^\s*\|[\s:|-]+\|\s*$/.test(line)
}

function splitCells(line: string): string[] {
  return line
    .trim()
    .replace(/^\||\|$/g, '')
    .split('|')
    .map((c) => c.trim())
}

export function renderMarkdown(src: string): string {
  const lines = String(src || '').split(/\r?\n/)
  const out: string[] = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    // 代码块
    const fence = line.match(/^\s*```(\w*)\s*$/)
    if (fence) {
      const body: string[] = []
      i++
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) {
        body.push(lines[i])
        i++
      }
      i++ // 跳过收尾的 ```
      out.push(`<pre class="md-pre"><code>${escapeHtml(body.join('\n'))}</code></pre>`)
      continue
    }

    // 表格：一行表头 + 一行分隔线 + 若干数据行
    if (isTableRow(line) && i + 1 < lines.length && isTableDivider(lines[i + 1])) {
      const head = splitCells(line).map((c) => `<th>${inline(escapeHtml(c))}</th>`).join('')
      i += 2
      const body: string[] = []
      while (i < lines.length && isTableRow(lines[i])) {
        const cells = splitCells(lines[i]).map((c) => `<td>${inline(escapeHtml(c))}</td>`).join('')
        body.push(`<tr>${cells}</tr>`)
        i++
      }
      out.push(`<table class="md-table"><thead><tr>${head}</tr></thead><tbody>${body.join('')}</tbody></table>`)
      continue
    }

    // 标题
    const heading = line.match(/^(#{1,4})\s+(.*)$/)
    if (heading) {
      const level = Math.min(6, heading[1].length + 2)
      out.push(`<h${level} class="md-h">${inline(escapeHtml(heading[2]))}</h${level}>`)
      i++
      continue
    }

    // 列表（有序 / 无序）
    if (/^\s*[-*]\s+/.test(line) || /^\s*\d+\.\s+/.test(line)) {
      const ordered = /^\s*\d+\.\s+/.test(line)
      const items: string[] = []
      while (i < lines.length && (/^\s*[-*]\s+/.test(lines[i]) || /^\s*\d+\.\s+/.test(lines[i]))) {
        const text = lines[i].replace(/^\s*(?:[-*]|\d+\.)\s+/, '')
        items.push(`<li>${inline(escapeHtml(text))}</li>`)
        i++
      }
      const tag = ordered ? 'ol' : 'ul'
      out.push(`<${tag} class="md-list">${items.join('')}</${tag}>`)
      continue
    }

    // 空行
    if (!line.trim()) {
      i++
      continue
    }

    // 段落：连续非空行合并
    const para: string[] = []
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^\s*```/.test(lines[i]) &&
      !/^(#{1,4})\s+/.test(lines[i]) &&
      !isTableRow(lines[i]) &&
      !/^\s*[-*]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i])
    ) {
      para.push(lines[i].trim())
      i++
    }
    out.push(`<p class="md-p">${inline(escapeHtml(para.join(' ')))}</p>`)
  }

  return out.join('\n')
}
