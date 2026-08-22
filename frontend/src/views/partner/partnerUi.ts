const STATUS_MAP: Record<string, { label: string; pill: string }> = {
  success: { label: '成功', pill: 'pill-good' },
  failed: { label: '失败', pill: 'pill-err' },
  processing: { label: '处理中', pill: 'pill-info' },
  pending: { label: '排队中', pill: 'pill' },
  running: { label: '进行中', pill: 'pill-info' },
  unknown: { label: '待确认', pill: 'pill-warn' },
  skipped: { label: '已跳过', pill: 'pill' },
}

export function statusLabel(s: string) {
  return STATUS_MAP[s]?.label || s || '—'
}

export function statusClass(s: string) {
  return `pill ${STATUS_MAP[s]?.pill || ''}`.trim()
}

export function formatPartnerDate(v: string) {
  return v ? new Date(v).toLocaleString('zh-CN') : '—'
}
