<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { get, put, post } from '../api'
import { refreshState } from '../state'
import { errText } from '../i18n'
import { notify } from '../util'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'

const emit = defineEmits(['ok'])
const { t } = useI18n()

const loaded = ref(false)
const presets = ref([])
const form = reactive({
  preset: 'custom', host: '', port: 587, security: 'starttls', auth: 'plain', username: '',
  from_address: '', from_name: 'WhatsApp Manager', reply_to: '', skip_verify: false, allow_insecure_auth: false, timeout_sec: 30,
})
const password = ref('')
const hasPassword = ref(false)
const changePw = ref(false)
const status = ref({ state: 'not_configured' })
const advanced = ref(false)
const saving = ref(false)
const testing = ref(false)
const testTo = ref('')
const result = ref(null) // { ok, text, detail }
const quick = reactive({ email: '', password: '', busy: false, err: '' })
const showDetail = ref(false)

function apply(r) {
  Object.assign(form, r.settings)
  if (!form.preset) form.preset = 'custom'
  hasPassword.value = r.has_password
  status.value = r.status || { state: 'not_configured' }
  presets.value = r.presets || []
  changePw.value = !r.has_password
  password.value = ''
  if (!testTo.value) testTo.value = form.from_address
}

onMounted(async () => {
  try { apply(await get('/api/smtp')) } catch (e) { notify(errText(e.key), 'error') }
  loaded.value = true
})

const preset = computed(() => presets.value.find((p) => p.id === form.preset))

function pickPreset(id) {
  form.preset = id
  const p = presets.value.find((x) => x.id === id)
  if (p && p.id !== 'custom') {
    form.host = p.host
    form.port = p.port
    form.security = p.security
  }
}

function setSecurity(s) {
  form.security = s
  form.port = s === 'ssl' ? 465 : s === 'none' ? 25 : 587
}

async function save(silent = false) {
  saving.value = true
  result.value = null
  try {
    const body = { ...form, port: Number(form.port), timeout_sec: Number(form.timeout_sec) }
    if (!form.username && form.auth !== 'none') body.username = form.from_address
    if (changePw.value && password.value) body.password = password.value
    apply(await put('/api/smtp', body))
    if (!silent) notify(t('smtp.saved_untested'))
    refreshState()
    return true
  } catch (e) {
    result.value = { ok: false, text: errText(e.key), detail: e.detail }
    return false
  } finally {
    saving.value = false
  }
}

async function test() {
  if (!(await save(true))) return
  testing.value = true
  result.value = null
  try {
    const r = await post('/api/smtp/test', { to: testTo.value })
    status.value = r.status
    result.value = r.ok
      ? { ok: true, text: testTo.value ? t('smtp.test_ok') : t('smtp.conn_ok') }
      : { ok: false, text: errText(r.status.error_key), detail: r.status.detail }
    if (r.ok) emit('ok')
    refreshState()
  } catch (e) {
    result.value = { ok: false, text: errText(e.key), detail: e.detail }
  } finally {
    testing.value = false
  }
}

async function detect() {
  quick.busy = true
  quick.err = ''
  try {
    const r = await post('/api/smtp/autodetect', { email: quick.email, password: quick.password })
    apply(r)
    testTo.value = quick.email
    if (r.status.state === 'ok') {
      result.value = { ok: true, text: t('smtp.conn_ok') }
      emit('ok')
    } else {
      result.value = { ok: false, text: errText(r.status.error_key), detail: r.status.detail }
    }
    refreshState()
  } catch (e) {
    quick.err = errText(e.key)
  } finally {
    quick.busy = false
  }
}

const statusBadge = computed(() => ({
  ok: ['badge-green', 'smtp.status_ok'],
  untested: ['badge-yellow', 'smtp.status_untested'],
  auth_failed: ['badge-red', 'smtp.status_auth_failed'],
  not_configured: ['badge-gray', 'smtp.status_not_configured'],
}[status.value.state] || ['badge-gray', 'smtp.status_not_configured']))

async function openHelp() {
  if (preset.value?.help_url) await post('/api/open-url', { url: preset.value.help_url })
}
</script>

<template>
  <div v-if="loaded" class="space-y-5">
    <div class="flex items-start justify-between gap-3">
      <p class="text-sm text-slate-600 dark:text-slate-300">{{ $t('smtp.intro') }}</p>
      <span :class="statusBadge[0]" class="shrink-0">{{ $t(statusBadge[1]) }}</span>
    </div>

    <!-- Quick setup -->
    <div class="rounded-2xl border border-brand-200 bg-brand-50 p-4 dark:border-brand-900 dark:bg-brand-900/20">
      <div class="mb-2 flex items-center gap-2 font-semibold"><Icon name="zap" :size="16" class="text-brand-600" />{{ $t('smtp.quick') }}</div>
      <p class="mb-3 text-xs text-slate-600 dark:text-slate-300">{{ $t('smtp.quick_text') }}</p>
      <div class="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <input v-model="quick.email" class="input ltr" dir="ltr" type="email" placeholder="you@example.com" autocomplete="off" />
        <input v-model="quick.password" class="input ltr" dir="ltr" type="password" :placeholder="$t('smtp.password')" autocomplete="new-password" />
        <button class="btn-primary" :disabled="quick.busy || !quick.email || !quick.password" @click="detect">{{ quick.busy ? $t('smtp.detecting') : $t('smtp.detect') }}</button>
      </div>
      <p v-if="quick.err" class="mt-2 text-sm text-red-600">{{ quick.err }}</p>
    </div>

    <!-- Provider -->
    <div>
      <label class="label">{{ $t('smtp.provider') }}</label>
      <div class="flex flex-wrap gap-2">
        <button v-for="p in presets" :key="p.id" type="button" class="choice !px-3 !py-2 text-sm font-medium" :class="form.preset === p.id && 'choice-on'" @click="pickPreset(p.id)">{{ p.id === 'custom' ? '⚙︎ ' + p.name : p.name }}</button>
      </div>
    </div>

    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label">{{ $t('smtp.from_address') }}</label>
        <input v-model="form.from_address" class="input ltr" dir="ltr" type="email" placeholder="alerts@example.com" />
      </div>
      <div>
        <label class="label">{{ $t('smtp.from_name') }}</label>
        <input v-model="form.from_name" class="input" />
      </div>
      <div>
        <label class="label">{{ $t('smtp.host') }}</label>
        <input v-model="form.host" class="input ltr" dir="ltr" placeholder="smtp.example.com" />
      </div>
      <div class="grid grid-cols-[1fr_6rem] gap-2">
        <div>
          <label class="label">{{ $t('smtp.security') }}</label>
          <select class="input" :value="form.security" @change="setSecurity($event.target.value)">
            <option value="starttls">{{ $t('smtp.sec_starttls') }}</option>
            <option value="ssl">{{ $t('smtp.sec_ssl') }}</option>
            <option value="none">{{ $t('smtp.sec_none') }}</option>
          </select>
        </div>
        <div>
          <label class="label">{{ $t('smtp.port') }}</label>
          <input v-model.number="form.port" class="input ltr" dir="ltr" type="number" min="1" max="65535" />
        </div>
      </div>
      <div>
        <label class="label">{{ $t('smtp.username') }}</label>
        <input v-model="form.username" class="input ltr" dir="ltr" :placeholder="form.from_address" autocomplete="off" />
        <p class="hint">{{ $t('smtp.username_hint') }}</p>
      </div>
      <div>
        <label class="label">{{ $t('smtp.password') }}</label>
        <div v-if="hasPassword && !changePw" class="flex items-center gap-2">
          <span class="badge-green"><Icon name="shield" :size="12" />{{ $t('smtp.password_saved') }}</span>
          <button class="btn-ghost btn-sm" @click="changePw = true">{{ $t('smtp.password_change') }}</button>
        </div>
        <input v-else v-model="password" class="input ltr" dir="ltr" type="password" autocomplete="new-password" />
        <p class="hint">
          {{ $t('smtp.password_hint') }}
          <button v-if="preset?.help_url" type="button" class="font-semibold text-brand-700 underline dark:text-brand-300" @click="openHelp">{{ $t('smtp.app_password_help') }}</button>
        </p>
      </div>
    </div>

    <div>
      <button type="button" class="btn-ghost btn-sm !px-0" @click="advanced = !advanced">
        <Icon name="down" :size="14" :class="advanced && 'rotate-180'" />{{ $t('smtp.advanced') }}
      </button>
      <div v-if="advanced" class="mt-3 grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label">{{ $t('smtp.auth') }}</label>
          <select v-model="form.auth" class="input">
            <option value="plain">{{ $t('smtp.auth_plain') }}</option>
            <option value="login">{{ $t('smtp.auth_login') }}</option>
            <option value="crammd5">{{ $t('smtp.auth_crammd5') }}</option>
            <option value="none">{{ $t('smtp.auth_none') }}</option>
          </select>
        </div>
        <div>
          <label class="label">{{ $t('smtp.reply_to') }} <span class="text-slate-400">({{ $t('common.optional') }})</span></label>
          <input v-model="form.reply_to" class="input ltr" dir="ltr" type="email" />
        </div>
        <div>
          <label class="label">{{ $t('smtp.timeout') }}</label>
          <input v-model.number="form.timeout_sec" class="input ltr" dir="ltr" type="number" min="5" max="120" />
        </div>
        <div class="space-y-2 self-end">
          <label class="flex items-center gap-2 text-sm"><input v-model="form.skip_verify" type="checkbox" class="accent-brand-600" />{{ $t('smtp.skip_verify') }}</label>
          <label class="flex items-center gap-2 text-sm"><input v-model="form.allow_insecure_auth" type="checkbox" class="accent-brand-600" />{{ $t('smtp.allow_insecure') }}</label>
        </div>
      </div>
    </div>

    <div class="flex flex-wrap items-end gap-3 border-t border-slate-200 pt-4 dark:border-slate-800">
      <button class="btn-secondary" :disabled="saving" @click="save()">{{ $t('smtp.save') }}</button>
      <div class="flex-1" />
      <div class="min-w-[14rem]">
        <label class="label">{{ $t('smtp.test_to') }}</label>
        <input v-model="testTo" class="input ltr" dir="ltr" type="email" />
      </div>
      <button class="btn-primary" :disabled="testing || saving" @click="test"><Icon name="mail" :size="16" />{{ testing ? $t('smtp.testing') : $t('smtp.test') }}</button>
    </div>

    <div v-if="result" class="rounded-xl p-3 text-sm" :class="result.ok ? 'bg-brand-50 text-brand-800 dark:bg-brand-900/30 dark:text-brand-200' : 'bg-red-50 text-red-800 dark:bg-red-900/30 dark:text-red-200'">
      <div class="flex items-start gap-2">
        <Icon :name="result.ok ? 'check' : 'alert'" :size="18" class="shrink-0" />
        <div class="flex-1">
          {{ result.text }}
          <button v-if="result.detail" class="ms-2 text-xs underline" @click="showDetail = !showDetail">{{ $t('common.details') }}</button>
          <pre v-if="showDetail && result.detail" class="ltr mt-2 whitespace-pre-wrap text-xs opacity-80">{{ result.detail }}</pre>
        </div>
      </div>
    </div>
  </div>
  <p v-else class="text-sm text-slate-500">{{ $t('common.loading') }}</p>
</template>
