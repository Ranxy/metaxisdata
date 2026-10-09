-- When a user last opened the inbox. The bell's badge counts unread messages
-- created after this instant, so opening the inbox clears the badge without
-- writing read_at: a message is read only when it is clicked. NULL means the inbox
-- was never opened, which reads as "every unread message is new".
ALTER TABLE principal ADD COLUMN IF NOT EXISTS notification_seen_at timestamptz;
