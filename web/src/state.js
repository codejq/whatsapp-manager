import { reactive } from 'vue'
import { get, ApiError } from './api'

// Global app state, refreshed by polling /api/state.
export const app = reactive({
  loaded: false,
  error: '',
  version: '',
  channel: 'github',
  packaged: false,
  wa: { phase: 'starting', wa: 'unknown', jid: '', last_error: '' },
  smtp: { state: 'not_configured', configured: false },
  stats: {},
  general: { language: 'ar', theme: 'system', first_run_done: true },
  notices: [],
  autostart: false,
  rules: { total: 0, enabled: 0 },
})

let timer = null
const listeners = new Set()

export function onStateChange(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export async function refreshState() {
  try {
    const s = await get('/api/state')
    Object.assign(app, s, { loaded: true, error: '' })
    listeners.forEach((fn) => fn(app))
  } catch (e) {
    app.error = e instanceof ApiError ? e.key : 'errors.unknown'
  }
}

export function startPolling() {
  if (timer) return
  refreshState()
  timer = setInterval(refreshState, 2500)
}

export const waConnected = () => app.wa.wa === 'connected'
export const emailReady = () => app.smtp.configured && app.smtp.state === 'ok'
