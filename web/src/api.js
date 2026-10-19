// Thin client for the local API. The per-launch token arrives once in the
// URL (#/?t=...) and is kept in sessionStorage for reloads.

const KEY = 'wam_token'

function captureToken() {
  const m = /[?&]t=([a-f0-9]+)/i.exec(window.location.hash || '')
  if (m) {
    try { sessionStorage.setItem(KEY, m[1]) } catch { /* storage unavailable */ }
    return m[1]
  }
  try { return sessionStorage.getItem(KEY) || '' } catch { return '' }
}

const token = captureToken()

export class ApiError extends Error {
  constructor(status, key, detail) {
    super(key || `HTTP ${status}`)
    this.status = status
    this.key = key || 'errors.unknown'
    this.detail = detail || ''
  }
}

export async function api(method, path, body) {
  let res
  try {
    res = await fetch(path, {
      method,
      headers: {
        'X-Auth-Token': token,
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch (e) {
    throw new ApiError(0, 'errors.app_unreachable', String(e))
  }
  const text = await res.text()
  let data
  try { data = text ? JSON.parse(text) : null } catch { data = null }
  if (!res.ok) {
    throw new ApiError(res.status, data && data.error, data && data.detail)
  }
  return data
}

export const get = (p) => api('GET', p)
export const post = (p, b = {}) => api('POST', p, b)
export const put = (p, b) => api('PUT', p, b)
export const del = (p) => api('DELETE', p)

// downloadUrl returns a URL for file downloads (token in the query string).
export function downloadUrl(path) {
  return `${path}${path.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}`
}

export const hasToken = () => !!token
