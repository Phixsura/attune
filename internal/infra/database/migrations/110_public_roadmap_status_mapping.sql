-- SPDX-License-Identifier: Apache-2.0
--
-- Public roadmap projection from workflow states (#222).

ALTER TABLE public_visibility_policies
    ADD COLUMN IF NOT EXISTS roadmap_status_mapping JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public_visibility_policies
    ADD CONSTRAINT chk_public_visibility_policy_roadmap_status_mapping_array
        CHECK (jsonb_typeof(roadmap_status_mapping) = 'array');

ALTER TABLE public_request_profiles
    ADD COLUMN IF NOT EXISTS roadmap_order INTEGER NOT NULL DEFAULT 0;

ALTER TABLE public_request_profiles
    ADD COLUMN IF NOT EXISTS roadmap_visible BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE public_request_profiles
    ADD CONSTRAINT chk_public_request_profiles_roadmap_order CHECK (roadmap_order >= 0);

CREATE OR REPLACE FUNCTION public_visibility_default_roadmap_status_mapping()
RETURNS JSONB
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT '[
        {"status":"open","label":"under consideration","order":1,"included":true},
        {"status":"planned","label":"planned","order":2,"included":true},
        {"status":"in_progress","label":"in progress","order":3,"included":true},
        {"status":"shipped","label":"shipped","order":4,"included":true},
        {"status":"cancelled","label":"cancelled","order":5,"included":false}
    ]'::jsonb
$$;

UPDATE public_visibility_policies
SET roadmap_status_mapping = public_visibility_default_roadmap_status_mapping()
WHERE roadmap_status_mapping = '[]'::jsonb;

CREATE OR REPLACE FUNCTION sync_public_request_profile_roadmap()
RETURNS TRIGGER AS $$
DECLARE
    request_status TEXT;
    roadmap_mapping JSONB;
BEGIN
    NEW.roadmap_column := '';
    NEW.roadmap_order := 0;
    NEW.roadmap_visible := FALSE;

    SELECT cr.status
    INTO request_status
    FROM customer_requests cr
    WHERE cr.tenant_id = NEW.tenant_id
      AND cr.id = NEW.request_id;

    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(NULLIF(pol.roadmap_status_mapping, '[]'::jsonb), public_visibility_default_roadmap_status_mapping())
    INTO roadmap_mapping
    FROM public_visibility_policies pol
    WHERE pol.tenant_id = NEW.tenant_id;

    IF roadmap_mapping IS NULL OR jsonb_typeof(roadmap_mapping) <> 'array' THEN
        roadmap_mapping := public_visibility_default_roadmap_status_mapping();
    END IF;

    SELECT
        left(btrim(COALESCE(item.label, '')), 80),
        COALESCE(item."order", 0),
        COALESCE(item.included, FALSE)
    INTO NEW.roadmap_column, NEW.roadmap_order, NEW.roadmap_visible
    FROM jsonb_to_recordset(roadmap_mapping) AS item(
        status TEXT,
        label TEXT,
        "order" INTEGER,
        included BOOLEAN
    )
    WHERE lower(btrim(COALESCE(item.status, ''))) = lower(btrim(COALESCE(request_status, '')))
    LIMIT 1;

    IF NOT FOUND THEN
        NEW.roadmap_column := '';
        NEW.roadmap_order := 0;
        NEW.roadmap_visible := FALSE;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_public_request_profiles_roadmap_sync ON public_request_profiles;
CREATE TRIGGER trg_public_request_profiles_roadmap_sync
    BEFORE INSERT OR UPDATE ON public_request_profiles
    FOR EACH ROW
    EXECUTE FUNCTION sync_public_request_profile_roadmap();

CREATE OR REPLACE FUNCTION refresh_public_request_profile_roadmap_from_request()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE public_request_profiles
    SET updated_by = updated_by
    WHERE tenant_id = NEW.tenant_id
      AND request_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_customer_requests_roadmap_sync ON customer_requests;
CREATE TRIGGER trg_customer_requests_roadmap_sync
    AFTER UPDATE OF status ON customer_requests
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION refresh_public_request_profile_roadmap_from_request();

CREATE OR REPLACE FUNCTION refresh_public_request_profiles_roadmap_from_policy()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE public_request_profiles
    SET updated_by = updated_by
    WHERE tenant_id = NEW.tenant_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_public_visibility_policies_roadmap_refresh ON public_visibility_policies;
CREATE TRIGGER trg_public_visibility_policies_roadmap_refresh
    AFTER INSERT OR UPDATE OF roadmap_status_mapping ON public_visibility_policies
    FOR EACH ROW
    EXECUTE FUNCTION refresh_public_request_profiles_roadmap_from_policy();

DROP INDEX IF EXISTS idx_public_request_profiles_roadmap;
CREATE INDEX IF NOT EXISTS idx_public_request_profiles_roadmap
    ON public_request_profiles (tenant_id, roadmap_order, roadmap_column, updated_at DESC, id DESC)
    WHERE included_in_roadmap AND roadmap_visible;

UPDATE public_request_profiles
SET updated_by = updated_by;
