import { createApp, watch } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import '@fontsource/ibm-plex-sans-arabic/400.css'
import '@fontsource/ibm-plex-sans-arabic/500.css'
import '@fontsource/ibm-plex-sans-arabic/600.css'
import '@fontsource/ibm-plex-sans-arabic/700.css'
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import './style.css'

import App from './App.vue'
import { i18n, setLocale, applyTheme } from './i18n'
import { app as state, startPolling } from './state'

import Dashboard from './views/Dashboard.vue'
import Rules from './views/Rules.vue'
import RuleEdit from './views/RuleEdit.vue'
import Activity from './views/Activity.vue'
import Settings from './views/Settings.vue'
import Welcome from './views/Welcome.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: Dashboard },
    { path: '/rules', component: Rules },
    { path: '/rules/new', component: RuleEdit },
    { path: '/rules/:id', component: RuleEdit, props: true },
    { path: '/activity', component: Activity },
    { path: '/settings/:tab?', component: Settings, props: true },
    { path: '/welcome', component: Welcome },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// Strip the token from the visible URL once captured.
router.beforeEach((to) => {
  if (to.query && to.query.t) {
    const q = { ...to.query }
    delete q.t
    return { path: to.path, query: q, replace: true }
  }
})

setLocale(state.general.language)
applyTheme(state.general.theme)
watch(() => state.general.language, (l) => setLocale(l))
watch(() => state.general.theme, (t) => applyTheme(t))
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyTheme(state.general.theme))

startPolling()
createApp(App).use(i18n).use(router).mount('#app')
