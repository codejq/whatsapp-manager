# WhatsApp Manager — User guide

## 1. Install

Run `WhatsAppManager-Setup-x.y.z.exe`. No administrator rights are needed. If Windows shows "Windows
protected your PC", click **More info → Run anyway**.

The app opens a setup wizard:

1. **Language.** Choose العربية or English. You can change it later in Settings or from the tray icon.
2. **Link WhatsApp.** On your phone, open WhatsApp → **Settings → Linked devices → Link a device** and scan
   the QR code. Or choose **Link with phone number instead** and type the 8-character code shown in the app.
3. **Email (optional).** Needed only for rules that send emails (see section 4).
4. **First rule.** Pick a template.

Tip: use a dedicated WhatsApp number for automation.

## 2. Everyday use

- The app lives in the **system tray**, next to the clock. Closing the window does not stop it.
- The tray icon colour shows the status: green means connected, yellow means reconnecting or paused, red
  means WhatsApp is not linked, and grey means starting.
- Right-click the tray icon to **Pause all rules**, change the language, or **Quit**.
- The app starts automatically when you sign in to Windows. You can turn this off in Settings → General.

## 3. Rules

A rule has three parts:

| Step | You choose | Examples |
|---|---|---|
| Where from? | Any chat, any group, any private chat, or specific chats. Optionally, only messages from specific people. | The "Sales" group; only what Ali writes |
| Which messages? | Nothing (all messages), or conditions: contains a link, contains words, does not contain words, message type | contains «عاجل» or «urgent» |
| Send to | WhatsApp contacts, numbers or groups, and/or email addresses | +966 5x…, boss@company.com |

- **Words** ignore Arabic diacritics and spelling variants (أ/ا/إ, ة/ه, ى/ي) and upper/lower case.
- **WhatsApp sending modes:** *Send a copy with sender name* adds "📩 From Ali in Sales". *Forward the
  original* uses WhatsApp's native forward.
- Use **Try it** on the last step to check a sample message. Nothing is sent.
- A rule only forwards messages that arrive **after** it is saved.

## 4. Email setup

Settings → Email:

1. Use **Quick setup**: enter your email address and password, then click *Find settings*.
2. Or choose your provider and fill in the fields. Gmail, Outlook, Yahoo and iCloud need an **app password**
   rather than your normal password; the help link explains how to create one.
3. Click **Send test email**. Email rules only work after a successful test.

If the email password stops working, the app pauses email sending, shows a notification, and keeps the
messages. They are sent automatically once a test succeeds again.

## 5. Activity

The Activity page lists every received message, the rule it matched, and each delivery with its status.

| Status | Meaning |
|---|---|
| Sent | Delivered to WhatsApp or the mail server. |
| Waiting | Will be sent soon. This includes waiting while WhatsApp reconnects. |
| Failed | Gave up after several tries; the reason is shown. Use **Send again**. |
| Unknown | The app was closed while sending, so it may or may not have arrived. It is never re-sent automatically. |
| Waiting for email | Email is not set up or the login failed. |

## 6. Good to know

- The same message is never sent twice to the same destination, even when several rules match.
- Messages you send yourself are ignored unless a rule allows them (Advanced). Messages the app forwarded
  are never forwarded again.
- After the PC was off, messages older than the **catch-up window** (12 hours by default) are not
  forwarded.
- WhatsApp sends are limited to 20 per minute by default (Settings → General).
- Old messages and history are deleted automatically after **5 days** by default. Change the period in
  Settings → General → *Delete messages older than*.
- Each email subject gets a running number, such as "[WhatsApp #42]", so Gmail does not group the emails
  into one conversation. Turn it off in Settings → Email.

## 7. Help

Settings → Help → **Download diagnostics** creates a zip with logs and settings but no passwords. Attach it
when reporting a problem on GitHub.
