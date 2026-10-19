<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { emailReady, refreshState } from '../state'
import { errText } from '../i18n'
import { blankRule, templateRule, ruleSummary, newId, notify, MESSAGE_TYPES, isGroup } from '../util'
import Icon from '../components/Icon.vue'
import Toggle from '../components/Toggle.vue'
import Chips from '../components/Chips.vue'
import PeerPicker from '../components/PeerPicker.vue'

const props = defineProps({ id: { type: String, default: '' } })
const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const rule = ref(blankRule())
const allRules = ref([])
const step = ref(0)
const saving = ref(false)
const error = ref('')
const loaded = ref(false)
const isEdit = computed(() => !!props.id)
const sendersOn = ref(false)

const steps = ['editor.step_when', 'editor.step_if', 'editor.step_then', 'editor.step_save']

onMounted(async () => {
  try {
    allRules.value = await get('/api/rules')
  } catch { allRules.value = [] }
  if (props.id) {
    const r = allRules.value.find((x) => x.id === props.id)
    if (!r) return router.replace('/rules')
    rule.value = JSON.parse(JSON.stringify(r))
  } else if (route.query.from) {
    const r = allRules.value.find((x) => x.id === route.query.from)
    if (r) {
      const c = JSON.parse(JSON.stringify(r))
      c.id = ''
      c.created_at = 0
      c.name = r.name + ' (2)'
      c.actions.forEach((a) => (a.id = newId()))
      rule.value = c
    }
  } else if (route.query.template) {
    rule.value = templateRule(route.query.template)
  }
  const r = rule.value
  r.source.chats ||= []
  r.source.senders ||= []
  r.conditions.items ||= []
  r.actions ||= []
  r.options ||= { include_own_messages: false, cooldown_seconds: 0 }
  sendersOn.value = r.source.senders.length > 0
  if (!isEdit.value && r.actions.some((a) => a.type === 'email') && !emailReady()) r.enabled = false
  loaded.value = true
})

watch(sendersOn, (on) => { if (!on) rule.value.source.senders = [] })

// ---------- conditions ----------
const condTypes = [
  { type: 'contains_link', icon: 'link', label: 'editor.cond_link', text: 'editor.cond_link_text' },
  { type: 'keywords', icon: 'search', label: 'editor.cond_keywords', text: 'editor.cond_keywords_text' },
  { type: 'not_contains', icon: 'x', label: 'editor.cond_not', text: 'editor.cond_not_text' },
  { type: 'message_type', icon: 'file', label: 'editor.cond_type', text: 'editor.cond_type_text' },
]
function addCond(type) {
  const c = { type }
  if (type === 'keywords' || type === 'not_contains') Object.assign(c, { words: [], mode: 'any', whole_word: false })
  if (type === 'message_type') c.types = ['image', 'document']
  rule.value.conditions.items.push(c)
}
const condLabel = (type) => condTypes.find((c) => c.type === type)?.label

// ---------- actions ----------
function addAction(type) {
  rule.value.actions.push(type === 'email'
    ? { id: newId(), type: 'email', emails: [], include_media: true }
    : { id: newId(), type: 'whatsapp', to: null, mode: 'copy_with_header' })
}
const validEmail = (s) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s)
const hasEmail = computed(() => rule.value.actions.some((a) => a.type === 'email'))

// Warn when a destination is also a watched source of any rule (possible loop).
function loopWarning(a) {
  if (a.type !== 'whatsapp' || !a.to) return false
  return [rule.value, ...allRules.value.filter((r) => r.id !== rule.value.id)].some((r) =>
    r.source.kind === 'any' || (r.source.kind === 'groups' && isGroup(a.to.jid)) ||
    (r.source.kind === 'private' && !isGroup(a.to.jid)) || (r.source.chats || []).some((c) => c.jid === a.to.jid))
}

// ---------- validation per step ----------
function stepError(i) {
  const r = rule.value
  if (i === 0 && r.source.kind === 'chats' && !r.source.chats.length) return 'errors.rule.chats_required'
  if (i === 0 && sendersOn.value && !r.source.senders.length) return 'errors.rule.chats_required'
  if (i === 1) {
    for (const c of r.conditions.items) {
      if ((c.type === 'keywords' || c.type === 'not_contains') && !c.words.length) return 'errors.rule.words_required'
      if (c.type === 'message_type' && !c.types.length) return 'errors.rule.types_required'
    }
  }
  if (i === 2) {
    if (!r.actions.length) return 'errors.rule.action_required'
    for (const a of r.actions) {
      if (a.type === 'whatsapp' && !a.to) return 'errors.rule.wa_target_required'
      if (a.type === 'email' && !a.emails.length) return 'errors.rule.email_required'
    }
  }
  if (i === 3 && !r.name.trim()) return 'errors.rule.name_required'
  return ''
}

function next() {
  const e = stepError(step.value)
  if (e) { error.value = t(e); return }
  error.value = ''
  step.value++
}
function goto(i) {
  for (let k = 0; k < i; k++) {
    const e = stepError(k)
    if (e) { step.value = k; error.value = t(e); return }
  }
  error.value = ''
  step.value = i
}

async function save() {
  for (let k = 0; k < steps.length; k++) {
    const e = stepError(k)
    if (e) { step.value = k; error.value = t(e); return }
  }
  saving.value = true
  error.value = ''
  try {
    const body = JSON.parse(JSON.stringify(rule.value))
    body.options.cooldown_seconds = Number(body.options.cooldown_seconds) || 0
    await post('/api/rules', body)
    notify(t('editor.saved'))
    refreshState()
    router.push('/rules')
  } catch (e) {
    error.value = errText(e.key)
  } finally {
    saving.value = false
  }
}

// ---------- test ----------
const test = ref({ body: '', chat_kind: 'group', result: null, busy: false })
async function runTest() {
  test.value.busy = true
  try {
    const r = rule.value
    const chat = r.source.kind === 'chats' && r.source.chats.length ? r.source.chats[0].jid : ''
    const sender = r.source.senders.length ? r.source.senders[0].jid : ''
    const res = await post('/api/rules/test', {
      rule: r, body: test.value.body, chat_kind: chat ? (isGroup(chat) ? 'group' : 'private') : test.value.chat_kind,
      chat_jid: chat, sender,
    })
    test.value.result = res.results[0]
  } catch (e) {
    notify(errText(e.key), 'error')
  } finally {
    test.value.busy = false
  }
}

const summary = computed(() => ruleSummary(rule.value))
const sources = [
  { kind: 'any', icon: 'globe' },
  { kind: 'groups', icon: 'users' },
  { kind: 'private', icon: 'user' },
  { kind: 'chats', icon: 'filter' },
]
</script>

<template>
  <div v-if="loaded" class="mx-auto max-w-4xl space-y-5 p-6">
    <div class="flex items-center gap-3">
      <router-link to="/rules" class="btn-ghost btn-sm"><Icon name="chevron" :size="16" class="rotate-180 rtl:rotate-0" />{{ $t('common.back') }}</router-link>
      <h1 class="text-2xl font-bold">{{ isEdit ? $t('editor.edit_title') : $t('editor.new_title') }}</h1>
    </div>

    <!-- Stepper -->
    <ol class="grid grid-cols-4 gap-2">
      <li v-for="(s, i) in steps" :key="s">
        <button
          class="flex w-full items-center gap-2 rounded-xl border-2 px-3 py-2 text-start text-sm font-semibold transition"
          :class="i === step ? 'border-brand-500 bg-brand-50 text-brand-800 dark:bg-brand-900/30 dark:text-brand-100' : i < step ? 'border-brand-200 text-brand-700 dark:border-brand-900 dark:text-brand-300' : 'border-slate-200 text-slate-500 dark:border-slate-800'"
          @click="goto(i)"
        >
          <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs" :class="i <= step ? 'bg-brand-600 text-white' : 'bg-slate-200 dark:bg-slate-700'">
            <Icon v-if="i < step" name="check" :size="14" /><template v-else>{{ i + 1 }}</template>
          </span>
          <span class="truncate">{{ $t(s) }}</span>
        </button>
      </li>
    </ol>

    <div class="card space-y-5 p-6">
      <!-- Step 1: source -->
      <template v-if="step === 0">
        <h2 class="section-title">{{ $t('editor.source_q') }}</h2>
        <div class="grid gap-3 sm:grid-cols-2">
          <button v-for="s in sources" :key="s.kind" type="button" class="choice flex items-center gap-3" :class="rule.source.kind === s.kind && 'choice-on'" @click="rule.source.kind = s.kind">
            <Icon :name="s.icon" :size="22" class="shrink-0 text-brand-600" />
            <span>
              <span class="block font-semibold">{{ $t('editor.src_' + s.kind) }}</span>
              <span class="block text-xs text-slate-500">{{ $t('editor.src_' + s.kind + '_text') }}</span>
            </span>
          </button>
        </div>
        <div v-if="rule.source.kind === 'chats'">
          <label class="label">{{ $t('editor.pick_chats') }}</label>
          <PeerPicker v-model="rule.source.chats" :title="$t('editor.pick_chats')" :button-text="$t('editor.pick_chats')" />
        </div>
        <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <div class="flex items-center gap-3">
            <Toggle v-model="sendersOn" :label="$t('editor.senders_toggle')" />
            <div>
              <div class="font-semibold">{{ $t('editor.senders_toggle') }}</div>
              <div class="text-xs text-slate-500">{{ $t('editor.senders_text') }}</div>
            </div>
          </div>
          <div v-if="sendersOn" class="mt-3">
            <PeerPicker v-model="rule.source.senders" :kinds="['contact']" :title="$t('editor.pick_senders')" :button-text="$t('editor.pick_senders')" />
          </div>
        </div>
      </template>

      <!-- Step 2: conditions -->
      <template v-if="step === 1">
        <h2 class="section-title">{{ $t('editor.conditions_q') }}</h2>
        <div v-if="!rule.conditions.items.length" class="rounded-xl bg-slate-100 p-4 text-sm dark:bg-slate-800">
          <Icon name="info" :size="16" class="inline" /> {{ $t('editor.all_messages') }}
        </div>
        <div v-if="rule.conditions.items.length > 1" class="flex gap-2">
          <button class="choice !py-2 text-sm" :class="rule.conditions.match === 'all' && 'choice-on'" @click="rule.conditions.match = 'all'">{{ $t('editor.match_all') }}</button>
          <button class="choice !py-2 text-sm" :class="rule.conditions.match === 'any' && 'choice-on'" @click="rule.conditions.match = 'any'">{{ $t('editor.match_any') }}</button>
        </div>
        <div v-for="(c, i) in rule.conditions.items" :key="i" class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <div class="mb-3 flex items-center justify-between">
            <b>{{ $t(condLabel(c.type)) }}</b>
            <button class="btn-ghost btn-sm text-red-600" @click="rule.conditions.items.splice(i, 1)"><Icon name="trash" :size="14" /></button>
          </div>
          <template v-if="c.type === 'keywords' || c.type === 'not_contains'">
            <Chips v-model="c.words" :placeholder="$t('editor.words_placeholder')" />
            <p class="hint">{{ $t('editor.words_hint') }}</p>
            <div class="mt-3 flex flex-wrap items-center gap-4 text-sm">
              <template v-if="c.type === 'keywords'">
                <label class="flex items-center gap-1.5"><input v-model="c.mode" type="radio" value="any" class="accent-brand-600" />{{ $t('editor.mode_any') }}</label>
                <label class="flex items-center gap-1.5"><input v-model="c.mode" type="radio" value="all" class="accent-brand-600" />{{ $t('editor.mode_all') }}</label>
              </template>
              <label class="flex items-center gap-1.5"><input v-model="c.whole_word" type="checkbox" class="accent-brand-600" />{{ $t('editor.whole_word') }}</label>
            </div>
          </template>
          <div v-else-if="c.type === 'message_type'" class="flex flex-wrap gap-2">
            <label v-for="mt in MESSAGE_TYPES" :key="mt" class="choice flex items-center gap-2 !px-3 !py-1.5 text-sm" :class="c.types.includes(mt) && 'choice-on'">
              <input v-model="c.types" type="checkbox" :value="mt" class="accent-brand-600" />{{ $t('types.' + mt) }}
            </label>
          </div>
          <p v-else class="text-sm text-slate-500">{{ $t('editor.cond_link_text') }}</p>
        </div>
        <div>
          <div class="label">{{ $t('editor.add_condition') }}</div>
          <div class="grid gap-2 sm:grid-cols-2">
            <button v-for="ct in condTypes" :key="ct.type" type="button" class="choice flex items-center gap-3 !py-2.5" @click="addCond(ct.type)">
              <Icon :name="ct.icon" :size="18" class="shrink-0 text-brand-600" />
              <span><span class="block text-sm font-semibold">{{ $t(ct.label) }}</span><span class="block text-xs text-slate-500">{{ $t(ct.text) }}</span></span>
            </button>
          </div>
        </div>
      </template>

      <!-- Step 3: actions -->
      <template v-if="step === 2">
        <h2 class="section-title">{{ $t('editor.actions_q') }}</h2>
        <div v-for="(a, i) in rule.actions" :key="a.id" class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <div class="mb-3 flex items-center justify-between">
            <b class="flex items-center gap-2"><Icon :name="a.type === 'email' ? 'mail' : 'chat'" :size="18" class="text-brand-600" />{{ $t(a.type === 'email' ? 'editor.add_email' : 'editor.add_wa') }}</b>
            <button class="btn-ghost btn-sm text-red-600" @click="rule.actions.splice(i, 1)"><Icon name="trash" :size="14" /></button>
          </div>
          <template v-if="a.type === 'whatsapp'">
            <label class="label">{{ $t('editor.wa_to') }}</label>
            <PeerPicker v-model="a.to" :multiple="false" :title="$t('editor.wa_pick')" :button-text="$t('editor.wa_pick')" />
            <div class="mt-3 grid gap-2 sm:grid-cols-2">
              <button type="button" class="choice" :class="a.mode === 'copy_with_header' && 'choice-on'" @click="a.mode = 'copy_with_header'">
                <span class="block text-sm font-semibold">{{ $t('editor.wa_mode_copy') }}</span>
                <span class="block text-xs text-slate-500">{{ $t('editor.wa_mode_copy_text') }}</span>
              </button>
              <button type="button" class="choice" :class="a.mode === 'forward' && 'choice-on'" @click="a.mode = 'forward'">
                <span class="block text-sm font-semibold">{{ $t('editor.wa_mode_forward') }}</span>
                <span class="block text-xs text-slate-500">{{ $t('editor.wa_mode_forward_text') }}</span>
              </button>
            </div>
            <p v-if="loopWarning(a)" class="mt-3 flex gap-2 rounded-xl bg-amber-50 p-3 text-xs text-amber-900 dark:bg-amber-900/30 dark:text-amber-100"><Icon name="alert" :size="14" class="shrink-0" />{{ $t('editor.loop_warning') }}</p>
          </template>
          <template v-else>
            <label class="label">{{ $t('editor.email_to') }}</label>
            <Chips v-model="a.emails" :placeholder="$t('editor.email_placeholder')" :validate="validEmail" dir="ltr" />
            <label class="mt-3 flex items-center gap-2 text-sm"><input v-model="a.include_media" type="checkbox" class="accent-brand-600" />{{ $t('editor.include_media') }}</label>
          </template>
        </div>
        <div v-if="hasEmail && !emailReady()" class="flex items-center gap-3 rounded-xl bg-sky-50 p-3 text-sm text-sky-900 dark:bg-sky-900/30 dark:text-sky-100">
          <Icon name="info" :size="16" class="shrink-0" />{{ $t('editor.email_not_ready') }}
          <router-link to="/settings/email" class="btn-secondary btn-sm ms-auto shrink-0">{{ $t('editor.setup_email') }}</router-link>
        </div>
        <div class="grid gap-2 sm:grid-cols-2">
          <button type="button" class="choice flex items-center gap-3" @click="addAction('whatsapp')">
            <Icon name="chat" :size="20" class="text-brand-600" />
            <span><span class="block font-semibold">{{ $t('editor.add_wa') }}</span><span class="block text-xs text-slate-500">{{ $t('editor.add_wa_text') }}</span></span>
          </button>
          <button type="button" class="choice flex items-center gap-3" @click="addAction('email')">
            <Icon name="mail" :size="20" class="text-brand-600" />
            <span><span class="block font-semibold">{{ $t('editor.add_email') }}</span><span class="block text-xs text-slate-500">{{ $t('editor.add_email_text') }}</span></span>
          </button>
        </div>
      </template>

      <!-- Step 4: name & save -->
      <template v-if="step === 3">
        <div>
          <label class="label">{{ $t('editor.name') }}</label>
          <input v-model="rule.name" class="input" :placeholder="$t('editor.name_placeholder')" maxlength="120" />
        </div>
        <div class="flex items-center gap-3">
          <Toggle v-model="rule.enabled" :disabled="hasEmail && !emailReady()" :label="$t('editor.enabled')" />
          <span class="font-semibold">{{ $t('editor.enabled') }}</span>
        </div>
        <div class="rounded-xl bg-brand-50 p-4 dark:bg-brand-900/20">
          <div class="mb-1 text-xs font-semibold uppercase text-brand-700 dark:text-brand-300">{{ $t('editor.summary') }}</div>
          <p class="leading-relaxed">{{ summary }}</p>
          <p class="mt-2 text-xs text-slate-500">{{ $t('editor.only_new') }}</p>
        </div>

        <!-- Test -->
        <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <div class="mb-1 font-semibold">{{ $t('editor.test_title') }}</div>
          <p class="mb-3 text-xs text-slate-500">{{ $t('editor.test_text') }}</p>
          <textarea v-model="test.body" class="input" rows="2" dir="auto" :placeholder="$t('editor.test_body')" />
          <div class="mt-2 flex flex-wrap items-center gap-3 text-sm">
            <template v-if="rule.source.kind !== 'chats'">
              <span class="text-slate-500">{{ $t('editor.test_chat') }}</span>
              <label class="flex items-center gap-1"><input v-model="test.chat_kind" type="radio" value="group" class="accent-brand-600" />{{ $t('editor.test_group') }}</label>
              <label class="flex items-center gap-1"><input v-model="test.chat_kind" type="radio" value="private" class="accent-brand-600" />{{ $t('editor.test_private') }}</label>
            </template>
            <button class="btn-secondary btn-sm ms-auto" :disabled="test.busy" @click="runTest"><Icon name="play" :size="14" />{{ $t('common.test') }}</button>
          </div>
          <p v-if="test.result" class="mt-3 flex items-center gap-2 rounded-lg p-2 text-sm" :class="test.result.matched ? 'bg-brand-50 text-brand-800 dark:bg-brand-900/30 dark:text-brand-200' : 'bg-slate-100 dark:bg-slate-800'">
            <Icon :name="test.result.matched ? 'check' : 'x'" :size="16" />
            <span v-if="test.result.matched">{{ $t('editor.test_match') }}</span>
            <span v-else>{{ $t('editor.test_nomatch') }}: {{ $t(test.result.reason) }}</span>
          </p>
        </div>

        <details class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <summary class="cursor-pointer font-semibold">{{ $t('editor.advanced') }}</summary>
          <div class="mt-3 space-y-3">
            <label class="flex items-center gap-2 text-sm"><input v-model="rule.options.include_own_messages" type="checkbox" class="accent-brand-600" />{{ $t('editor.own_messages') }}</label>
            <div class="max-w-xs">
              <label class="label">{{ $t('editor.cooldown') }}</label>
              <input v-model.number="rule.options.cooldown_seconds" type="number" min="0" class="input ltr" dir="ltr" />
              <p class="hint">{{ $t('editor.cooldown_hint') }}</p>
            </div>
          </div>
        </details>
      </template>

      <p v-if="error" class="flex items-center gap-2 rounded-xl bg-red-50 p-3 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200"><Icon name="alert" :size="16" />{{ error }}</p>

      <div class="flex items-center gap-2 border-t border-slate-200 pt-4 dark:border-slate-800">
        <button v-if="step > 0" class="btn-secondary" @click="step--; error = ''">{{ $t('common.back') }}</button>
        <div class="flex-1" />
        <button v-if="isEdit && step < 3" class="btn-secondary" :disabled="saving" @click="save">{{ $t('common.save') }}</button>
        <button v-if="step < 3" class="btn-primary" @click="next">{{ $t('common.next') }}<Icon name="chevron" :size="16" class="rtl:rotate-180" /></button>
        <button v-else class="btn-primary" :disabled="saving" @click="save"><Icon name="check" :size="16" />{{ $t('common.save') }}</button>
      </div>
    </div>
  </div>
</template>
