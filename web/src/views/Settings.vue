<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { put, post, downloadUrl, get } from '../api'
import { app, refreshState } from '../state'
import { errText } from '../i18n'
import { notify } from '../util'
import Icon from '../components/Icon.vue'
import Toggle from '../components/Toggle.vue'
import WhatsAppLink from '../components/WhatsAppLink.vue'
import SmtpForm from '../components/SmtpForm.vue'
import { PUBLISHER, openExternal } from '../branding'

const props = defineProps({ tab: { type: String, default: '' } })
const { t } = useI18n()
const router = useRouter()

const tabs = [
  { id: 'general', icon: 'settings' },
  { id: 'whatsapp', icon: 'chat' },
  { id: 'email', icon: 'mail' },
  { id: 'backup', icon: 'download' },
  { id: 'help', icon: 'info' },
  { id: 'about', icon: 'shield' },
]
const current = computed(() => props.tab || 'general')

const g = ref({ ...app.general })
watch(() => app.general, (v) => { if (!dirty.value) g.value = { ...v } }, { deep: true })
const dirty = ref(false)

async function saveGeneral(patch) {
  Object.assign(g.value, patch)
  dirty.value = true
  try {
    await put('/api/settings', g.value)
    await refreshState()
    notify(t('common.saved'))
  } catch (e) {
    notify(errText(e.key), 'error')
  } finally {
    dirty.value = false
  }
}

const importInput = ref(null)
async function doImport(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f) return
  try {
    const data = JSON.parse(await f.text())
    const r = await post('/api/rules/import', { rules: data.rules || data })
    notify(t('settings.imported', { n: r.imported }))
    if (r.disabled) notify(t('settings.imported_disabled', { n: r.disabled }))
    if (r.error) notify(errText(r.error), 'error')
    refreshState()
  } catch (e) {
    notify(errText(e.key || 'errors.import_invalid'), 'error')
  }
}

function download(path) {
  const a = document.createElement('a')
  a.href = downloadUrl(path)
  a.click()
}

async function openLogs() { await post('/api/open-logs') }

const update = ref(null)
async function checkUpdate() {
  try { update.value = await get('/api/update') } catch { /* ignore */ }
}
</script>

<template>
  <div class="mx-auto max-w-5xl p-6">
    <h1 class="mb-5 text-2xl font-bold">{{ $t('settings.title') }}</h1>
    <div class="flex flex-col gap-6 md:flex-row">
      <nav class="flex shrink-0 flex-row gap-1 overflow-x-auto md:w-48 md:flex-col">
        <button
          v-for="tb in tabs" :key="tb.id" class="flex items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium"
          :class="current === tb.id ? 'bg-white shadow-sm dark:bg-slate-800' : 'text-slate-600 hover:bg-white/60 dark:text-slate-400 dark:hover:bg-slate-800/60'"
          @click="router.replace('/settings/' + tb.id)"
        >
          <Icon :name="tb.icon" :size="16" />{{ $t('settings.tab_' + tb.id) }}
        </button>
      </nav>

      <div class="card min-w-0 flex-1 p-6">
        <!-- General -->
        <div v-if="current === 'general'" class="space-y-6">
          <div>
            <label class="label">{{ $t('settings.language') }}</label>
            <div class="flex gap-2">
              <button class="choice !py-2 font-semibold" :class="g.language === 'ar' && 'choice-on'" @click="saveGeneral({ language: 'ar' })">العربية</button>
              <button class="choice !py-2 font-semibold" :class="g.language === 'en' && 'choice-on'" @click="saveGeneral({ language: 'en' })">English</button>
            </div>
          </div>
          <div>
            <label class="label">{{ $t('settings.theme') }}</label>
            <div class="flex flex-wrap gap-2">
              <button v-for="th in ['system', 'light', 'dark']" :key="th" class="choice !py-2 text-sm" :class="g.theme === th && 'choice-on'" @click="saveGeneral({ theme: th })">{{ $t('settings.theme_' + th) }}</button>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <Toggle :model-value="app.packaged ? true : g.autostart" :disabled="app.packaged" :label="$t('settings.autostart')" @update:model-value="saveGeneral({ autostart: $event })" />
            <div>
              <div class="font-semibold">{{ $t('settings.autostart') }}</div>
              <div class="text-sm text-slate-500">{{ app.packaged ? $t('settings.autostart_store') : $t('settings.autostart_text') }}</div>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <Toggle :model-value="g.paused" :label="$t('settings.pause')" @update:model-value="saveGeneral({ paused: $event })" />
            <div>
              <div class="font-semibold">{{ $t('settings.pause') }}</div>
              <div class="text-sm text-slate-500">{{ $t('settings.pause_text') }}</div>
            </div>
          </div>
          <div class="grid gap-4 sm:grid-cols-3">
            <div>
              <label class="label">{{ $t('settings.catchup') }}</label>
              <input v-model.number="g.catchup_hours" type="number" min="1" max="168" class="input ltr" dir="ltr" @change="saveGeneral({})" />
              <p class="hint">{{ $t('settings.catchup_text') }}</p>
            </div>
            <div>
              <label class="label">{{ $t('settings.rate') }}</label>
              <input v-model.number="g.wa_rate_per_min" type="number" min="1" max="60" class="input ltr" dir="ltr" @change="saveGeneral({})" />
              <p class="hint">{{ $t('settings.rate_text') }}</p>
            </div>
            <div>
              <label class="label">{{ $t('settings.cleanup') }}</label>
              <input v-model.number="g.retention_days" type="number" min="1" max="365" class="input ltr" dir="ltr" @change="saveGeneral({})" />
              <p class="hint">{{ $t('settings.cleanup_text') }}</p>
            </div>
          </div>
          <label v-if="app.channel === 'github' && !app.packaged" class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="accent-brand-600" :checked="g.update_check" @change="saveGeneral({ update_check: $event.target.checked })" />{{ $t('settings.updates') }}
          </label>
        </div>

        <!-- WhatsApp -->
        <div v-else-if="current === 'whatsapp'">
          <h2 class="section-title mb-4">{{ $t('wa.title') }}</h2>
          <WhatsAppLink />
        </div>

        <!-- Email -->
        <div v-else-if="current === 'email'">
          <h2 class="section-title mb-4">{{ $t('smtp.title') }}</h2>
          <div class="mb-6 flex items-start gap-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
            <Toggle :model-value="g.email_subject_counter" :label="$t('settings.subject_counter')" @update:model-value="saveGeneral({ email_subject_counter: $event })" />
            <div>
              <div class="font-semibold">{{ $t('settings.subject_counter') }}</div>
              <div class="text-sm text-slate-500">{{ $t('settings.subject_counter_text') }}</div>
            </div>
          </div>
          <SmtpForm />
        </div>

        <!-- Backup -->
        <div v-else-if="current === 'backup'" class="space-y-6">
          <div class="flex items-start gap-4">
            <Icon name="download" :size="22" class="mt-1 text-brand-600" />
            <div class="flex-1">
              <div class="font-semibold">{{ $t('settings.export') }}</div>
              <p class="text-sm text-slate-500">{{ $t('settings.export_text') }}</p>
            </div>
            <button class="btn-secondary" @click="download('/api/rules/export')">{{ $t('settings.export') }}</button>
          </div>
          <div class="flex items-start gap-4">
            <Icon name="upload" :size="22" class="mt-1 text-brand-600" />
            <div class="flex-1">
              <div class="font-semibold">{{ $t('settings.import') }}</div>
              <p class="text-sm text-slate-500">{{ $t('settings.import_text') }}</p>
            </div>
            <button class="btn-secondary" @click="importInput.click()">{{ $t('settings.import') }}</button>
            <input ref="importInput" type="file" accept=".json,application/json" class="hidden" @change="doImport" />
          </div>
        </div>

        <!-- Help -->
        <div v-else-if="current === 'help'" class="space-y-6">
          <div class="flex items-start gap-4">
            <Icon name="file" :size="22" class="mt-1 text-brand-600" />
            <div class="flex-1">
              <div class="font-semibold">{{ $t('settings.diagnostics') }}</div>
              <p class="text-sm text-slate-500">{{ $t('settings.diagnostics_text') }}</p>
            </div>
            <button class="btn-secondary" @click="download('/api/diagnostics')">{{ $t('settings.diagnostics') }}</button>
          </div>
          <div class="flex items-start gap-4">
            <Icon name="folder" :size="22" class="mt-1 text-brand-600" />
            <div class="flex-1 font-semibold">{{ $t('settings.open_logs') }}</div>
            <button class="btn-secondary" @click="openLogs">{{ $t('settings.open_logs') }}</button>
          </div>
          <p class="rounded-xl bg-slate-100 p-4 text-sm text-slate-600 dark:bg-slate-800 dark:text-slate-300"><Icon name="shield" :size="16" class="inline" /> {{ $t('settings.privacy') }}</p>
        </div>

        <!-- About -->
        <div v-else-if="current === 'about'" class="space-y-4">
          <div class="flex items-center gap-4">
            <img src="../logo.png" alt="" class="h-16 w-16 rounded-2xl" />
            <div>
              <div class="text-xl font-bold">WhatsApp Manager</div>
              <div class="text-sm text-slate-500">{{ $t('settings.version') }} <span class="ltr">{{ app.version }}</span></div>
            </div>
          </div>
          <p class="text-sm">{{ $t('app.tagline') }}</p>
          <p class="text-sm text-slate-500">{{ $t('settings.license') }}</p>
          <p class="text-sm text-slate-500">{{ $t('settings.credits') }}</p>
          <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
            <div class="mb-2 text-sm text-slate-500">{{ $t('powered.by') }}</div>
            <div class="mb-3 text-lg font-bold">{{ PUBLISHER.name }}</div>
            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
              <dt class="text-slate-500">{{ $t('powered.website') }}</dt>
              <dd><button class="ltr font-medium text-brand-700 hover:underline dark:text-brand-300" @click="openExternal(PUBLISHER.website)">quantum-billing.com</button></dd>
              <dt class="text-slate-500">{{ $t('powered.phone') }}</dt>
              <dd class="ltr font-medium" dir="ltr">{{ PUBLISHER.phone }}</dd>
              <dt class="text-slate-500">{{ $t('powered.email') }}</dt>
              <dd><button class="ltr font-medium text-brand-700 hover:underline dark:text-brand-300" @click="openExternal('mailto:' + PUBLISHER.email)">{{ PUBLISHER.email }}</button></dd>
            </dl>
          </div>
          <div v-if="app.channel === 'github' && !app.packaged" class="flex items-center gap-3">
            <button class="btn-secondary btn-sm" @click="checkUpdate"><Icon name="refresh" :size="14" />{{ $t('settings.updates') }}</button>
            <span v-if="update && update.available" class="badge-green">{{ $t('banner.update', { v: update.latest }) }}</span>
            <span v-else-if="update" class="text-sm text-slate-500">✓ {{ update.current }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
