-- ============================================================================
-- Migration: WhatsApp broadcast campaigns (templates + campaigns + recipients)
-- Date: 2026-09-19
--
-- IMPORTANT: This backend uses GORM AutoMigrate — when you deploy the new
-- binary and it starts, it AUTOMATICALLY creates these tables/columns and seeds
-- the dispatcher cron. So on a live server that runs this Go backend you do NOT
-- normally need to run this file.
--
-- This SQL is provided for manual / DBA-controlled application (e.g. if the live
-- DB is migrated separately from the app). It is IDEMPOTENT — safe to run more
-- than once. Target: PostgreSQL 16, database `vaha_ai`.
-- ============================================================================

BEGIN;

-- 1) New Settings columns: WhatsApp Business Account id + Meta App id ----------
ALTER TABLE settings ADD COLUMN IF NOT EXISTS whatsapp_waba_id varchar(60);
ALTER TABLE settings ADD COLUMN IF NOT EXISTS whatsapp_app_id  varchar(60);

-- 2) wa_templates — local mirror of Meta message templates --------------------
CREATE TABLE IF NOT EXISTS wa_templates (
    id              bigserial PRIMARY KEY,
    meta_id         varchar(120),
    name            varchar(120) NOT NULL,
    language        varchar(16)  NOT NULL,
    category        varchar(32),
    status          varchar(24),
    header_format   varchar(16),
    body_text       text,
    footer          varchar(160),
    body_params     bigint DEFAULT 0,
    rejected_reason varchar(255),
    buttons         text,        -- JSON array of buttons (type/text/url/phone_number)
    image_data      text,        -- base64 header image, for campaign auto-reuse
    last_synced_at  timestamptz,
    created_at      timestamptz,
    updated_at      timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wa_tpl_name_lang     ON wa_templates (name, language);
CREATE INDEX        IF NOT EXISTS idx_wa_templates_meta_id ON wa_templates (meta_id);
CREATE INDEX        IF NOT EXISTS idx_wa_templates_status  ON wa_templates (status);

-- 3) wa_campaigns — one broadcast run -----------------------------------------
CREATE TABLE IF NOT EXISTS wa_campaigns (
    id              bigserial PRIMARY KEY,
    name            varchar(160),
    template_name   varchar(120) NOT NULL,
    template_lang   varchar(16),
    header_image_id varchar(200),          -- Meta media id (uploaded at send/dispatch)
    status          varchar(16) DEFAULT 'draft',  -- draft | scheduled | sending | done
    total           bigint,
    sent            bigint,
    failed          bigint,
    scheduled_at    timestamptz,           -- set = deferred send (dispatcher cron)
    image_data      text,                  -- base64 image for scheduled campaigns
    created_by      bigint,
    created_at      timestamptz,
    updated_at      timestamptz
);
CREATE INDEX IF NOT EXISTS idx_wa_campaigns_scheduled_at ON wa_campaigns (scheduled_at);

-- 4) wa_campaign_recipients — per-recipient rows + delivery status ------------
CREATE TABLE IF NOT EXISTS wa_campaign_recipients (
    id          bigserial PRIMARY KEY,
    campaign_id bigint NOT NULL,
    phone       varchar(24) NOT NULL,
    params      text,                       -- JSON array of body {{n}} values
    status      varchar(12) DEFAULT 'queued', -- queued | sent | failed
    wa_msg_id   varchar(120),
    error       varchar(400),
    sent_at     timestamptz,
    created_at  timestamptz
);
CREATE INDEX IF NOT EXISTS idx_wa_campaign_recipients_campaign_id ON wa_campaign_recipients (campaign_id);
CREATE INDEX IF NOT EXISTS idx_wa_campaign_recipients_status      ON wa_campaign_recipients (status);

-- 5) Seed the scheduled-campaign dispatcher cron ------------------------------
--    The app also seeds this on startup but DISABLED by default. Inserting it
--    enabled here means scheduled campaigns dispatch on live without you having
--    to enable it on the Cron jobs page. (Set enabled=false if you prefer to
--    turn it on manually.)
INSERT INTO cron_jobs (key, name, description, schedule, enabled, created_at, updated_at)
SELECT 'wa_campaign_dispatch',
       'Dispatch scheduled WhatsApp campaigns',
       'Every minute, sends any WhatsApp campaign whose scheduled time has arrived.',
       'minutely', true, now(), now()
WHERE NOT EXISTS (SELECT 1 FROM cron_jobs WHERE key = 'wa_campaign_dispatch');

-- 6) Seed the approved WhatsApp templates (mirror of the Meta account) ---------
--    Same Meta Business account as local, so meta_id values are valid on live.
--    Idempotent: keyed on (name, language). Re-running refreshes the mirror.
--    NOTE: image_data is intentionally NOT overwritten on conflict, so a header
--    image uploaded/reused on live is preserved. These rows can also be
--    recreated any time from the WhatsApp Templates page via the "Sync" button.
INSERT INTO wa_templates (meta_id,name,language,category,status,header_format,body_text,footer,body_params,rejected_reason,buttons,image_data,last_synced_at,created_at,updated_at) VALUES ('2219115922362110','vaha_limited_offer','en_US','MARKETING','APPROVED','','Hi {{1}}, here''s a special offer on Vaha AI. Your child can learn with a personal AI tutor for just ₹{{2}}/month — daily practice, instant doubt-solving and tests to help improve scores.
Open the app to get started. Always Learning First — Vaha AI.','',2,'NONE','',NULL,now(),now(),now()) ON CONFLICT (name,language) DO UPDATE SET meta_id=EXCLUDED.meta_id,category=EXCLUDED.category,status=EXCLUDED.status,header_format=EXCLUDED.header_format,body_text=EXCLUDED.body_text,footer=EXCLUDED.footer,body_params=EXCLUDED.body_params,rejected_reason=EXCLUDED.rejected_reason,buttons=EXCLUDED.buttons,updated_at=now();
INSERT INTO wa_templates (meta_id,name,language,category,status,header_format,body_text,footer,body_params,rejected_reason,buttons,image_data,last_synced_at,created_at,updated_at) VALUES ('1057682960501023','otp_code','en','AUTHENTICATION','APPROVED','','*{{1}}* is your verification code. For your security, do not share this code.','',1,'NONE',E'[{"type":"URL","text":"Copy code","url":"https://www.whatsapp.com/otp/code/?otp_type=COPY_CODE\\u0026code_expiration_minutes=5\\u0026code=otp{{1}}"}]',NULL,now(),now(),now()) ON CONFLICT (name,language) DO UPDATE SET meta_id=EXCLUDED.meta_id,category=EXCLUDED.category,status=EXCLUDED.status,header_format=EXCLUDED.header_format,body_text=EXCLUDED.body_text,footer=EXCLUDED.footer,body_params=EXCLUDED.body_params,rejected_reason=EXCLUDED.rejected_reason,buttons=EXCLUDED.buttons,updated_at=now();
INSERT INTO wa_templates (meta_id,name,language,category,status,header_format,body_text,footer,body_params,rejected_reason,buttons,image_data,last_synced_at,created_at,updated_at) VALUES ('1619415016373381','vaha_welcome','en_US','MARKETING','APPROVED','IMAGE','Welcome aboard, {{1}} 🙌

Thank you for opting in to stay connected with Vaha AI.

Watch this space for exclusive offers and 🔥 updates on your child''s learning — right at your fingertips!','',1,'NONE','[{"type":"URL","text":"Click here","url":"https://vaha.ai.kasoftware.in/"}]',NULL,now(),now(),now()) ON CONFLICT (name,language) DO UPDATE SET meta_id=EXCLUDED.meta_id,category=EXCLUDED.category,status=EXCLUDED.status,header_format=EXCLUDED.header_format,body_text=EXCLUDED.body_text,footer=EXCLUDED.footer,body_params=EXCLUDED.body_params,rejected_reason=EXCLUDED.rejected_reason,buttons=EXCLUDED.buttons,updated_at=now();

COMMIT;
