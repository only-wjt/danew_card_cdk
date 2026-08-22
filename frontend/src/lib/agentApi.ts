import { useAgentAuthStore } from '../stores/agentAuth'

const UNSAFE_METHODS = ['POST', 'PUT', 'PATCH', 'DELETE']

function getCookie(name: string): string {
  const match = document.cookie.match(new RegExp('(?:^|; )' + name.replace(/([.$?*|{}()[\]\\/+^])/g, '\\$1') + '=([^;]*)'))
  return match ? decodeURIComponent(match[1]) : ''
}

export const agentFetch = async (input: RequestInfo | URL, init: RequestInit = {}) => {
  const store = useAgentAuthStore()
  const headers = new Headers(init.headers || {})
  if (store.token) {
    headers.set('Authorization', `Bearer ${store.token}`)
  }
  const method = (init.method || 'GET').toUpperCase()
  if (UNSAFE_METHODS.includes(method)) {
    const csrf = getCookie('agent_csrf_token')
    if (csrf) headers.set('X-CSRF-Token', csrf)
  }
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(input, { ...init, headers, credentials: 'include' })
  if ((res.status === 401 || res.status === 403) && !String(input).includes('/auth/agent/login')) {
    store.logout()
    if (!window.location.pathname.startsWith('/partner/login')) {
      window.location.href = '/partner/login'
    }
  }
  return res
}
