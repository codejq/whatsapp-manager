// Static sponsor and publisher details shown in the UI.
import { post } from './api'

export const SPONSOR = {
  url: { ar: 'https://www.s-i-u.com/ar/', en: 'https://www.s-i-u.com/en/' },
}

export const PUBLISHER = {
  name: 'Quantum Billing LLC',
  website: 'https://quantum-billing.com/',
  phone: '+1 361-405-5427',
  email: 'info@Quantum-Billing.com',
}

// openExternal opens a link in the default browser (or mail app for mailto:).
export async function openExternal(url) {
  try {
    await post('/api/open-url', { url })
  } catch {
    window.open(url, '_blank', 'noopener')
  }
}
