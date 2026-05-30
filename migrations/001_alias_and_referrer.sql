-- Custom aliases need more than 10 chars; widen short_code.
ALTER TABLE urls ALTER COLUMN short_code TYPE VARCHAR(30);

-- Record the Referer header on each click for analytics.
ALTER TABLE clicks ADD COLUMN IF NOT EXISTS referrer TEXT;

-- Time-bucket queries (date_trunc on clicked_at) are the hot path.
CREATE INDEX IF NOT EXISTS idx_clicks_url_id_clicked_at
    ON clicks (url_id, clicked_at);
