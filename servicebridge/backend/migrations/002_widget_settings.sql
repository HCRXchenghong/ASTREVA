ALTER TABLE keyword_rules
    ADD COLUMN IF NOT EXISTS show_in_quick_replies BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE keyword_rules
    ADD COLUMN IF NOT EXISTS quick_reply_text TEXT NOT NULL DEFAULT '';

ALTER TABLE contact_settings
    ADD COLUMN IF NOT EXISTS entry_reply TEXT NOT NULL DEFAULT '';

UPDATE contact_settings
SET entry_reply = '您好，欢迎咨询在线客服，请问有什么可以帮您？'
WHERE id = 1 AND entry_reply = '';
