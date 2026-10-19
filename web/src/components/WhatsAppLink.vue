<script setup>
import { ref, watch, onBeforeUnmount, computed } from 'vue'
import { post } from '../api'
import { app, refreshState } from '../state'
import { errText } from '../i18n'
import { phoneOf, notify } from '../util'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'

const { t } = useI18n()
const qr = ref('')
const qrErr = ref('')
const loadingQR = ref(false)
const mode = ref('qr')
const phone = ref('')
const pairCode = ref('')
const pairErr = ref('')
const pairing = ref(false)
const busy = ref(false)
let qrTimer = null

const wa = computed(() => app.wa.wa)
const phase = computed(() => app.wa.phase)

async function loadQR() {
  if (loadingQR.value) return
  loadingQR.value = true
  qrErr.value = ''
  try {
    const r = await post('/api/wa/login')
    if (r.already) { qr.value = ''; refreshState(); return }
    qr.value = r.qr
    clearTimeout(qrTimer)
    // Refresh a little before the code expires.
    qrTimer = setTimeout(() => { if (app.wa.wa === 'logged_out' && mode.value === 'qr') loadQR() }, Math.max(10, (r.duration || 30) - 3) * 1000)
  } catch (e) {
    qrErr.value = errText(e.key)
  } finally {
    loadingQR.value = false
  }
}

watch([wa, phase, mode], ([w, p, m]) => {
  if (w === 'logged_out' && p === 'running' && m === 'qr' && !qr.value) loadQR()
  if (w === 'connected') { qr.value = ''; pairCode.value = ''; clearTimeout(qrTimer) }
}, { immediate: true })

onBeforeUnmount(() => clearTimeout(qrTimer))

async function getCode() {
  pairErr.value = ''
  pairing.value = true
  try {
    const r = await post('/api/wa/pair', { phone: phone.value })
    pairCode.value = r.code
  } catch (e) {
    pairErr.value = errText(e.key)
  } finally {
    pairing.value = false
  }
}

async function logout() {
  if (!confirm(t('wa.logout_confirm'))) return
  busy.value = true
  try {
    await post('/api/wa/logout')
    qr.value = ''
    await refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  } finally {
    busy.value = false
  }
}

async function reconnect() {
  busy.value = true
  try { await post('/api/wa/reconnect') } catch (e) { notify(errText(e.key), 'error') } finally { busy.value = false }
}

async function restart() {
  busy.value = true
  try { await post('/api/wa/restart'); qr.value = '' } catch (e) { notify(errText(e.key), 'error') } finally { busy.value = false }
}

function copyCode() {
  navigator.clipboard?.writeText(pairCode.value.replace('-', ''))
  notify(t('common.copied'))
}
</script>

<template>
  <div>
    <!-- Service error -->
    <div v-if="phase === 'error'" class="flex flex-col items-center gap-3 py-6 text-center">
      <Icon name="alert" :size="40" class="text-red-500" />
      <p class="font-semibold">{{ errText(app.wa.last_error) }}</p>
      <button class="btn-secondary" :disabled="busy" @click="restart"><Icon name="refresh" :size="16" />{{ $t('wa.restart') }}</button>
    </div>

    <!-- Starting -->
    <div v-else-if="phase !== 'running' || wa === 'unknown'" class="flex flex-col items-center gap-3 py-10 text-center text-slate-500">
      <span class="h-10 w-10 animate-spin rounded-full border-4 border-brand-200 border-t-brand-600" />
      <p>{{ $t('wa.starting') }}</p>
    </div>

    <!-- Connected -->
    <div v-else-if="wa === 'connected'" class="flex flex-col items-center gap-3 py-6 text-center">
      <span class="flex h-16 w-16 items-center justify-center rounded-full bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-200"><Icon name="check" :size="34" /></span>
      <h3 class="text-xl font-bold">{{ $t('wa.connected_title') }}</h3>
      <p v-if="app.wa.jid" class="text-sm text-slate-500">{{ $t('wa.linked_as') }} <b class="ltr">{{ phoneOf(app.wa.jid) }}</b></p>
      <p class="max-w-md text-sm text-slate-500">{{ $t('wa.connected_text') }}</p>
      <slot name="connected-actions">
        <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="logout"><Icon name="logout" :size="14" />{{ $t('wa.logout') }}</button>
      </slot>
    </div>

    <!-- Disconnected (session exists) -->
    <div v-else-if="wa === 'disconnected'" class="flex flex-col items-center gap-3 py-6 text-center">
      <span class="h-10 w-10 animate-spin rounded-full border-4 border-amber-200 border-t-amber-500" />
      <p class="font-semibold">{{ $t('status.disconnected') }}</p>
      <p class="text-sm text-slate-500">{{ $t('wa.disconnected_text') }}</p>
      <div class="flex gap-2">
        <button class="btn-secondary btn-sm" :disabled="busy" @click="reconnect">{{ $t('wa.reconnect') }}</button>
        <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="logout">{{ $t('wa.logout') }}</button>
      </div>
    </div>

    <!-- Logged out: link -->
    <div v-else class="grid items-center gap-6 md:grid-cols-[1fr_auto]">
      <div>
        <h3 class="mb-2 text-xl font-bold">{{ $t('wa.link_title') }}</h3>
        <ol v-if="mode === 'qr'" class="space-y-2 text-sm text-slate-600 dark:text-slate-300">
          <li class="flex gap-2"><Icon name="phone" :size="18" class="shrink-0 text-brand-600" />{{ $t('wa.link_steps') }}</li>
        </ol>
        <div v-else>
          <p class="mb-3 text-sm text-slate-600 dark:text-slate-300">{{ $t('wa.code_steps') }}</p>
          <label class="label">{{ $t('wa.phone_label') }}</label>
          <div class="flex gap-2">
            <input v-model="phone" class="input ltr" dir="ltr" inputmode="tel" :placeholder="$t('wa.phone_placeholder')" @keydown.enter="getCode" />
            <button class="btn-primary" :disabled="pairing || !phone" @click="getCode">{{ $t('wa.get_code') }}</button>
          </div>
          <p v-if="pairErr" class="mt-2 text-sm text-red-600">{{ pairErr }}</p>
        </div>
        <button class="btn-ghost btn-sm mt-4 !px-0 text-brand-700 dark:text-brand-300" @click="mode = mode === 'qr' ? 'code' : 'qr'">
          <Icon :name="mode === 'qr' ? 'phone' : 'qr'" :size="14" />{{ mode === 'qr' ? $t('wa.use_phone') : $t('wa.use_qr') }}
        </button>
        <p class="mt-4 rounded-xl bg-slate-100 p-3 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-300">{{ $t('wa.dedicated_tip') }}</p>
      </div>

      <div class="flex flex-col items-center">
        <template v-if="mode === 'qr'">
          <div class="flex h-64 w-64 items-center justify-center rounded-2xl border border-slate-200 bg-white p-3 dark:border-slate-700">
            <img v-if="qr" :src="qr" alt="QR" class="h-full w-full" />
            <span v-else-if="loadingQR" class="h-10 w-10 animate-spin rounded-full border-4 border-brand-200 border-t-brand-600" />
            <p v-else-if="qrErr" class="p-2 text-center text-sm text-red-600">{{ qrErr }}</p>
          </div>
          <div class="mt-2 flex items-center gap-2 text-xs text-slate-500">
            {{ $t('wa.qr_expires') }}
            <button class="btn-ghost btn-sm" :disabled="loadingQR" @click="loadQR"><Icon name="refresh" :size="12" />{{ $t('wa.qr_refresh') }}</button>
          </div>
        </template>
        <div v-else-if="pairCode" class="flex flex-col items-center gap-2">
          <p class="text-sm text-slate-500">{{ $t('wa.code_title') }}</p>
          <div class="ltr rounded-2xl border-2 border-dashed border-brand-400 px-6 py-4 font-mono text-4xl font-bold tracking-widest text-brand-700 dark:text-brand-300">{{ pairCode }}</div>
          <button class="btn-ghost btn-sm" @click="copyCode"><Icon name="copy" :size="14" />{{ $t('common.copy') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
