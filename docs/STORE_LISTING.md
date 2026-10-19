# Microsoft Store submission kit

Everything needed to publish the app in the Microsoft Store: the steps, the identity file, and listing text
ready to paste in English and Arabic.

The Store build is an **MSIX** package. You don't need a code-signing certificate, because Microsoft signs
the package when you submit it. The build was checked on 2026-10-20 with the Windows App Certification Kit:
every applicable test passed. The only warning, "DPI awareness", is informational; the app declares
per-monitor DPI awareness.

---

## 1. Partner Center account

1. Sign up at **https://storedeveloper.microsoft.com**. You'll verify your ID with a document and a selfie.
   - An **individual** account is free and shows your personal name as the publisher.
   - To show **Quantum Billing LLC** as the publisher, open a **company** account. Company accounts need
     business verification and may have a one-time fee; check this during sign-up.
2. In Partner Center, go to **Apps and games → New product → MSIX or PWA app**.

## 2. Choose and reserve the app name

> ⚠️ **Avoid "WhatsApp" in the Store name.** WhatsApp is Meta's trademark, and Microsoft often refuses or
> removes Store apps that use another company's trademark in their name. The **description** may still say
> the app works with WhatsApp, together with the "not affiliated" sentence (included below).

Names to try (first available wins). The name must be unique in the Store:

| English | Arabic | Notes |
|---|---|---|
| **Wasil – Message Forwarder** | **واصل – محوّل الرسائل** | "Wasil" means "it arrives / delivered"; short and brandable |
| Message Router | موجّه الرسائل | Plain and descriptive |
| ChatRelay | تشات ريلاي | Short |

Reserve the English name, then add the Arabic name as a second reserved name for the Arabic listing.

## 3. Build the package

The identity is already in `build/msix/store.json`, so just run
`./build.ps1 -Version 0.1.0 -Channel store -Msix`. The steps below are only needed for a different
Partner Center product.

### Identity for another product

1. Partner Center → your app → **Product management → Product identity**. Copy:
   - `Package/Identity/Name`
   - `Package/Identity/Publisher` (starts with `CN=`)
   - `Package/Properties/PublisherDisplayName`
2. Copy `build/msix/store.local.example.json` to `build/msix/store.local.json` (git ignores it) and fill it in:
   ```json
   {
     "identityName": "<Package/Identity/Name>",
     "publisher": "<Package/Identity/Publisher>",
     "publisherDisplayName": "<Package/Properties/PublisherDisplayName>",
     "displayName": "<the exact name you reserved>"
   }
   ```
3. Build:
   ```powershell
   ./build.ps1 -Version 0.1.0 -Channel store -Msix
   ```
   The output is `dist\WhatsAppManager-0.1.0.0.msix`. Store versions must end in `.0`; the build does this.

## 4. Create the submission

Partner Center → your app → **Start your submission**.

### Pricing and availability
- Markets: all, or choose. Price: **Free**. Visibility: public.

### Properties
- Category: **Productivity**. Subcategory: none.
- Privacy policy URL: **https://codejq.github.io/whatsapp-manager/privacy.html**
- Website: **https://codejq.github.io/whatsapp-manager/**
- Support contact: **info@Quantum-Billing.com**
- System requirements: Windows 10 version 1809 or later, x64; internet connection.

### Age ratings
Complete the IARC questionnaire honestly. Key answers:
- The app does **not** let users chat with strangers or share content publicly. It forwards the user's
  own WhatsApp messages to destinations the user chooses.
- No violence, gambling, purchases or ads. *(The sidebar sponsor card is static and does not collect
  data; if the questionnaire asks about advertising, answer that the app shows a static promotional link.)*

### Packages
- Upload `dist\WhatsAppManager-0.1.0.0.msix`.
- Device families: **Desktop** only.

### Store listings
Add two languages: **English (United States)** and **Arabic (Saudi Arabia)**. The text is in sections 5
and 6 below.

- Screenshots (1366×768 or larger, PNG): from `docs/screenshots/`
  - English: `dashboard-en.png`, `rules-en.png`, `activity-en.png`, `link-whatsapp-en.png`
  - Arabic: `dashboard-ar.png`, `rules-ar.png`, `rule-editor-ar.png`, `activity-ar.png`, `email-settings-ar.png`, `link-whatsapp-ar.png`
- Store logos (optional but recommended), from `assets/store/`:
  - 1:1 box art 1080×1080: `box-art-1080.png`
  - 2:3 poster art 720×1080: `poster-720x1080.png`
  - 300×300 app tile icon: `app-icon-300.png`

### Submission options → restricted capabilities
The package uses **runFullTrust**. Paste this justification:

> This is a desktop (Win32) application packaged with MSIX. It needs runFullTrust to run its bundled local
> WhatsApp client as a child process, show a system-tray icon, host its interface in WebView2, and send email
> through the user's own SMTP server. All processing happens locally on the user's PC; no data is sent to
> the developer.

### Notes for certification (optional field)
> To test: install, choose a language, then link a WhatsApp account with the QR code (Settings → Linked
> devices on a phone). Create a rule from a template, for example "Keyword anywhere → group". The app keeps
> running in the system tray after its window is closed; use Quit in the tray menu to exit. A WhatsApp
> account is required for the forwarding features. Without one, the app still opens, shows the QR linking
> screen, and all settings can be reviewed.

Submit. Certification usually takes 1–3 business days.

---

## 5. Listing text: English (United States)

**Product name:** *(the name you reserved)*

**Short description** (shown in search results):
> Automatically forward the WhatsApp messages that matter to the right people, by WhatsApp or email. Rules
> in plain language, Arabic and English.

**Description:**
> Never miss an important message in a busy WhatsApp group again.
>
> This app watches the WhatsApp groups and chats you choose and automatically forwards the messages that
> match your rules to an email address, a phone number or another WhatsApp group. It's built for teachers,
> parents, sales teams and small businesses, and is easy for anyone to use.
>
> EXAMPLES
> • A lesson link (Zoom, Google Meet, Teams…) posted in a class group → emailed to the parent
> • Any message containing "urgent" → sent to the managers' group
> • Everything from one important contact → forwarded to a colleague
>
> HOW IT WORKS
> 1. Install and link your WhatsApp by scanning a QR code, just like WhatsApp Web.
> 2. Create a rule in three steps: where from → which messages → send to.
> 3. That's it. The app runs quietly in the system tray and starts with Windows.
>
> RELIABLE
> • Never sends the same message twice, even when several rules match.
> • Reconnects automatically; messages wait while you're offline and are sent when you're back.
> • An activity log shows every message, which rule matched and where it went.
>
> PRIVATE
> Everything stays on your PC. There are no accounts, no cloud and no tracking. Email is sent through your
> own email account.
>
> Free and open source (MIT). This app is not affiliated with, endorsed by, or connected to WhatsApp or Meta.

**What's new in this version:**
> First release.

**Product features** (one per line, maximum 20):
> Forward WhatsApp messages automatically by rules
> Watch any chat, all groups, or specific groups and people
> Detect links (Zoom, Meet, Teams, any website), keywords and message types
> Arabic-aware keyword matching (diacritics and letter variants)
> Send to WhatsApp contacts, numbers or groups
> Send by email through your own account, with attachments
> Rule wizard with templates and a "try it" test
> Activity log with the reason for every decision
> No duplicate deliveries, no forwarding loops
> Runs in the system tray and starts with Windows
> Arabic and English interface
> Everything stays on your PC

**Search terms** (maximum 7):
> forward messages · auto forward · whatsapp forward · message rules · email forwarding · group messages · تحويل رسائل

**Copyright and trademark info:**
> © 2026 Quantum Billing LLC. WhatsApp is a trademark of WhatsApp LLC / Meta Platforms, Inc.; this app is
> not affiliated with them.

**Additional license terms:**
> Open source under the MIT License: https://github.com/codejq/whatsapp-manager/blob/main/LICENSE

---

## 6. Listing text: Arabic (Saudi Arabia)

**اسم المنتج:** *(الاسم الذي حجزته)*

**وصف مختصر:**
> حوّل رسائل واتساب المهمة تلقائياً إلى الأشخاص المناسبين عبر واتساب أو البريد الإلكتروني. قواعد بلغة بسيطة، بالعربية والإنجليزية.

**الوصف:**
> لا تفوّت رسالة مهمة في مجموعات واتساب المزدحمة بعد اليوم.
>
> يراقب هذا التطبيق المجموعات والمحادثات التي تختارها، ويحوّل الرسائل التي تطابق قواعدك تلقائياً إلى
> بريد إلكتروني أو رقم هاتف أو مجموعة واتساب أخرى. مناسب للمعلمين وأولياء الأمور وفرق المبيعات
> والأعمال الصغيرة، وسهل للجميع.
>
> أمثلة
> • رابط درس (زووم، جوجل ميت، تيمز…) في مجموعة الصف ← يُرسل بالبريد لولي الأمر
> • أي رسالة فيها «عاجل» ← تُرسل إلى مجموعة المدراء
> • كل رسائل جهة اتصال مهمة ← تُحوّل إلى زميل
>
> كيف يعمل
> 1. ثبّت التطبيق واربط واتساب بمسح رمز QR مثل واتساب ويب.
> 2. أنشئ قاعدة في ثلاث خطوات: من أين ← أي رسائل ← إلى أين.
> 3. وهذا كل شيء. يعمل التطبيق بهدوء في شريط المهام ويبدأ مع ويندوز.
>
> موثوق
> • لا يرسل الرسالة نفسها مرتين حتى لو طابقتها عدة قواعد.
> • يعيد الاتصال تلقائياً، وتنتظر الرسائل أثناء انقطاع الإنترنت ثم تُرسل.
> • سجل يعرض كل رسالة والقاعدة التي طابقتها وإلى أين أُرسلت.
>
> خاص
> كل شيء يبقى على جهازك. بدون حسابات أو سحابة أو تتبع، ويُرسل البريد عبر حسابك الخاص.
>
> مجاني ومفتوح المصدر (MIT). التطبيق لا يتبع لواتساب أو ميتا ولا يحظى باعتمادهما.

**ما الجديد في هذا الإصدار:**
> الإصدار الأول.

**مزايا المنتج:**
> تحويل رسائل واتساب تلقائياً حسب القواعد
> مراقبة أي محادثة أو كل المجموعات أو مجموعات وأشخاص محددين
> اكتشاف الروابط (زووم، ميت، تيمز، أي موقع) والكلمات وأنواع الرسائل
> مطابقة ذكية للكلمات العربية (التشكيل والهمزات)
> الإرسال إلى جهات اتصال أو أرقام أو مجموعات واتساب
> الإرسال بالبريد عبر حسابك الخاص مع المرفقات
> معالج قواعد بقوالب جاهزة وزر «جرّبها»
> سجل يوضح سبب كل قرار
> لا رسائل مكررة ولا حلقات تحويل
> يعمل في شريط المهام ويبدأ مع ويندوز
> واجهة عربية وإنجليزية
> كل شيء يبقى على جهازك

**كلمات البحث:**
> تحويل رسائل · واتساب · تحويل تلقائي · مجموعات واتساب · بريد إلكتروني · forward messages · auto forward

---

## 7. Automatic publishing with GitHub Actions

The product identity is committed in `build/msix/store.json` (Store ID **9PNPC9GBHQPQ**,
https://apps.microsoft.com/detail/9PNPC9GBHQPQ). After the first submission is published,
`.github/workflows/store.yml` submits every new stable release by itself.

**One-time setup:**

1. **First submission by hand.** The Store API only works once the app has a completed submission. Upload
   `dist\WhatsAppManager-0.1.0.0.msix` (built with `./build.ps1 -Version 0.1.0 -Channel store -Msix`),
   complete the other sections and submit.
2. **Create an API identity.** In Partner Center, go to ⚙ **Account settings → User management →
   Microsoft Entra applications → + Create Microsoft Entra application** (or add an existing one).
   - Role: **Manager**.
   - Open the app, then **Add new key**. Copy the **Key** (the client secret, shown once), the
     **Client ID** and the **Tenant ID**.
3. **Find your Seller ID** under ⚙ **Account settings → Legal info / Organization profile → Seller ID**.
4. **Add the values to GitHub:** repository **Settings → Environments → New environment** named
   `store`. Add these **environment secrets**:

   | Secret | Value |
   |---|---|
   | `STORE_TENANT_ID` | Tenant ID |
   | `STORE_SELLER_ID` | Seller ID |
   | `STORE_CLIENT_ID` | Client ID |
   | `STORE_CLIENT_SECRET` | Key (client secret) |

5. **Turn it on:** **Settings → Secrets and variables → Actions → Variables → New repository variable**
   `STORE_ENABLED` = `true`.

**Releasing after that:** push a tag such as `v0.1.1`. The **Release** workflow publishes the GitHub
release, then **Microsoft Store** builds `0.1.1.0` and creates a Store submission with it. The submission
keeps the existing listing, screenshots and settings, and Microsoft certifies it as usual. You can also run
**Actions → Microsoft Store → Run workflow** with a version number.

Each Store version must be higher than the previous one. The client secret expires (1–2 years); renew
it in Partner Center and update `STORE_CLIENT_SECRET`.
