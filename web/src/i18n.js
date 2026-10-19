import { createI18n } from 'vue-i18n'
import ar from './locales/ar.json'
import en from './locales/en.json'

export const i18n = createI18n({
  legacy: false,
  locale: 'ar',
  fallbackLocale: 'en',
  messages: { ar, en },
  missingWarn: false,
  fallbackWarn: false,
})

export function setLocale(lang) {
  const l = lang === 'en' ? 'en' : 'ar'
  i18n.global.locale.value = l
  document.documentElement.lang = l
  document.documentElement.dir = l === 'ar' ? 'rtl' : 'ltr'
}

// errText translates an API error key, falling back to a generic message.
export function errText(key) {
  const { t, te } = i18n.global
  if (key && te(key)) return t(key)
  if (key && te(key, 'en')) return t(key, {}, { locale: 'en' })
  return t('errors.unknown')
}

export function applyTheme(theme) {
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
}
