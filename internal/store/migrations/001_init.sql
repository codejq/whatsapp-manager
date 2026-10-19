CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE rules (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    def        TEXT NOT NULL
);

-- Every webhook event, stored once. GoWA retries deliveries, so the unique
-- key makes the receiver idempotent.
CREATE TABLE inbox (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id    TEXT NOT NULL,
    message_id   TEXT NOT NULL,
    event        TEXT NOT NULL,
    received_at  INTEGER NOT NULL,
    msg_time     INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'new', -- new | processed | ignored | error
    note         TEXT NOT NULL DEFAULT '',
    chat_id      TEXT NOT NULL DEFAULT '',
    chat_name    TEXT NOT NULL DEFAULT '',
    sender       TEXT NOT NULL DEFAULT '',
    sender_name  TEXT NOT NULL DEFAULT '',
    msg_type     TEXT NOT NULL DEFAULT '',
    preview      TEXT NOT NULL DEFAULT '',
    matched      TEXT NOT NULL DEFAULT '',
    payload      TEXT NOT NULL,
    UNIQUE(device_id, message_id, event)
);
CREATE INDEX idx_inbox_status ON inbox(status, id);
CREATE INDEX idx_inbox_received ON inbox(received_at);

-- One row per (message, destination). The unique key guarantees that a
-- destination never receives the same message twice, even when several
-- rules match.
CREATE TABLE outbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    inbox_id          INTEGER NOT NULL,
    device_id         TEXT NOT NULL,
    message_id        TEXT NOT NULL,
    rule_id           TEXT NOT NULL,
    rule_name         TEXT NOT NULL,
    channel           TEXT NOT NULL,          -- whatsapp | email
    destination_key   TEXT NOT NULL,          -- wa:<jid> | mail:<address>
    destination_label TEXT NOT NULL DEFAULT '',
    action            TEXT NOT NULL,          -- JSON of the action
    status            TEXT NOT NULL DEFAULT 'pending', -- pending | sending | sent | failed | uncertain | waiting_email | cancelled
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT NOT NULL DEFAULT '',
    sent_message_id   TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE(device_id, message_id, destination_key)
);
CREATE INDEX idx_outbox_due ON outbox(status, channel, next_attempt_at);
CREATE INDEX idx_outbox_inbox ON outbox(inbox_id);

-- IDs of messages this app sent, so their echoes are never processed.
CREATE TABLE sent_ids (
    message_id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL
);

-- Cached chat names (groups and contacts) for display and matching.
CREATE TABLE chats (
    jid        TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL, -- group | contact
    updated_at INTEGER NOT NULL
);
