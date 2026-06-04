-- Per-tenant locale + timezone so that AI enrichment summaries,
-- weekly digest rendering, and timestamp display follow the tenant's
-- own preferences instead of a hardcoded zh-CN / Asia/Shanghai.
--
-- locale: BCP 47 tag. Domestic-first launch default zh-CN; cross-border
--   tenants override at install time or in console settings.
-- timezone: IANA tz string. Used for aligning "current period" (monthly
--   billing cycle), weekly digest windows, and dashboard date labels.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS locale   TEXT NOT NULL DEFAULT 'zh-CN',
    ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai';
