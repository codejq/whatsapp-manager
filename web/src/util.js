import { reactive } from 'vue'
import { i18n } from './i18n'

// ---------- toasts ----------
export const toasts = reactive([])
let seq = 0
export function notify(text, kind = 'ok', ms = 3500) {
  const id = ++seq
  toasts.push({ id, text, kind })
  setTimeout(() => {
    const i = toasts.findIndex((x) => x.id === id)
    if (i >= 0) toasts.splice(i, 1)
  }, ms)
}

// ---------- formatting ----------
export function fmtTime(ms) {
  if (!ms) return ''
  const d = new Date(ms)
  const locale = i18n.global.locale.value === 'ar' ? 'ar-EG-u-nu-latn' : 'en-GB'
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  return sameDay
    ? d.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString(locale, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

export function phoneOf(jid) {
  if (!jid) return ''
  const user = jid.split('@')[0].split(':')[0]
  return jid.endsWith('@s.whatsapp.net') ? '+' + user : user
}

export function peerLabel(p) {
  if (!p) return ''
  return p.name || phoneOf(p.jid)
}

export const isGroup = (jid) => (jid || '').endsWith('@g.us')

// ---------- rules ----------
export function newId() {
  return Math.random().toString(16).slice(2, 10) + Date.now().toString(16).slice(-6)
}

export function blankRule() {
  return {
    id: '',
    name: '',
    enabled: true,
    priority: 0,
    source: { kind: 'any', chats: [], senders: [] },
    conditions: { match: 'all', items: [] },
    actions: [],
    options: { include_own_messages: false, cooldown_seconds: 0 },
  }
}

export function templateRule(kind) {
  const { t } = i18n.global
  const r = blankRule()
  switch (kind) {
    case 'links_email':
      r.name = t('rules.tpl_links_email')
      r.source.kind = 'chats'
      r.conditions.items = [{ type: 'contains_link' }]
      r.actions = [{ id: newId(), type: 'email', emails: [], include_media: false }]
      break
    case 'contact_wa':
      r.name = t('rules.tpl_contact_wa')
      r.source.kind = 'chats'
      r.actions = [{ id: newId(), type: 'whatsapp', to: null, mode: 'copy_with_header' }]
      break
    case 'keyword_group':
      r.name = t('rules.tpl_keyword_group')
      r.source.kind = 'any'
      r.conditions.items = [{ type: 'keywords', words: [i18n.global.locale.value === 'ar' ? 'عاجل' : 'urgent'], mode: 'any', whole_word: false }]
      r.actions = [{ id: newId(), type: 'whatsapp', to: null, mode: 'copy_with_header' }]
      break
  }
  return r
}

const listJoin = (arr, max = 3) => {
  const shown = arr.slice(0, max).join('، ')
  return arr.length > max ? `${shown} +${arr.length - max}` : shown
}

// ruleSummary renders a rule as one plain-language sentence.
export function ruleSummary(r) {
  const { t } = i18n.global
  const parts = []
  const src = r.source || {}
  const source = src.kind === 'chats'
    ? t('rules.src_chats', { chats: listJoin((src.chats || []).map(peerLabel)) || '…' })
    : t('rules.src_' + (src.kind || 'any'))
  parts.push(t('rules.summary_when', { source }))
  if ((src.senders || []).length) parts.push(t('rules.summary_from', { senders: listJoin(src.senders.map(peerLabel)) }))
  const conds = (r.conditions?.items || []).map((c) => {
    const words = listJoin((c.words || []).map((w) => `«${w}»`))
    switch (c.type) {
      case 'contains_link': return t('rules.cond_link')
      case 'keywords': return t(c.mode === 'all' ? 'rules.cond_keywords_all' : 'rules.cond_keywords_any', { words })
      case 'not_contains': return t('rules.cond_not', { words })
      case 'message_type': return t('rules.cond_type', { types: (c.types || []).map((x) => t('types.' + x)).join('، ') })
      default: return ''
    }
  }).filter(Boolean)
  if (conds.length) parts.push(t('rules.summary_if', { conditions: conds.join(t(r.conditions.match === 'any' ? 'rules.join_or' : 'rules.join_and')) }))
  const acts = (r.actions || []).map((a) => {
    if (a.type === 'whatsapp') return t(a.mode === 'copy_with_header' ? 'rules.act_wa_copy' : 'rules.act_wa', { to: peerLabel(a.to) || '…' })
    if (a.type === 'email') return t('rules.act_email', { to: listJoin(a.emails || []) || '…' })
    return ''
  }).filter(Boolean)
  if (acts.length) parts.push(t('rules.summary_then', { actions: acts.join(t('rules.join_and')) }))
  return parts.join('، ')
}

export const MESSAGE_TYPES = ['text', 'image', 'video', 'document', 'audio', 'sticker', 'location', 'contact', 'poll']
