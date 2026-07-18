// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
)

var portalPageTemplate = template.Must(template.New("portal-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <title>{{.TenantName}} | {{.Headline}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4efe7;
      --panel: rgba(255, 255, 255, 0.92);
      --panel-strong: #ffffff;
      --text: #13151a;
      --muted: #5f6472;
      --border: rgba(19, 21, 26, 0.1);
      --accent: #1f6feb;
      --accent-strong: #174fbd;
      --accent-2: #0f766e;
      --success: #166534;
      --danger: #b42318;
      --shadow: 0 28px 90px -62px rgba(19, 21, 26, 0.45);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(255, 255, 255, 0.85), transparent 26%),
        radial-gradient(circle at top right, rgba(31, 111, 235, 0.08), transparent 32%),
        linear-gradient(180deg, #f8f3ec 0%, var(--bg) 100%);
      color: var(--text);
    }
    .shell {
      max-width: 920px;
      margin: 0 auto;
      padding: 32px 20px 48px;
    }
    .hero {
      display: grid;
      gap: 14px;
      margin-bottom: 20px;
      padding: 30px 28px;
      border: 1px solid var(--border);
      border-radius: 28px;
      background: linear-gradient(180deg, rgba(255,255,255,0.95), var(--panel));
      box-shadow: var(--shadow);
    }
    .eyebrow {
      margin: 0;
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.2em;
      text-transform: uppercase;
      color: var(--accent-strong);
    }
    h1 {
      margin: 0;
      font-size: clamp(2rem, 5vw, 3.4rem);
      line-height: 1.04;
      letter-spacing: -0.04em;
    }
    .lede {
      margin: 0;
      max-width: 62ch;
      color: var(--muted);
      font-size: 1rem;
      line-height: 1.7;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
      color: var(--muted);
      font-size: 0.92rem;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 10px;
      border-radius: 999px;
      background: rgba(31, 111, 235, 0.08);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 600;
    }
    .link {
      color: var(--accent-2);
      text-decoration: none;
      font-weight: 650;
    }
    .link:hover { text-decoration: underline; }
    .card {
      border: 1px solid var(--border);
      border-radius: 24px;
      background: var(--panel-strong);
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .card-body {
      padding: 26px 24px;
    }
    .notice {
      margin-bottom: 18px;
      padding: 14px 16px;
      border-radius: 16px;
      border: 1px solid rgba(20, 83, 45, 0.18);
      background: rgba(240, 253, 244, 0.85);
      color: var(--success);
      font-size: 0.95rem;
      line-height: 1.6;
    }
    form {
      display: grid;
      gap: 18px;
    }
    fieldset {
      margin: 0;
      padding: 0;
      border: 0;
      min-inline-size: 0;
    }
    .field-grid {
      display: grid;
      gap: 16px;
    }
    .field {
      display: grid;
      gap: 8px;
    }
    .field label,
    .field legend {
      font-size: 0.95rem;
      font-weight: 650;
    }
    .hint {
      color: var(--muted);
      font-size: 0.88rem;
      line-height: 1.5;
    }
    input, textarea, select, button {
      font: inherit;
    }
    input, textarea, select {
      width: 100%;
      padding: 13px 14px;
      border-radius: 14px;
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.96);
      color: var(--text);
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.7);
    }
    textarea {
      min-height: 150px;
      resize: vertical;
    }
    input:focus, textarea:focus, select:focus, button:focus {
      outline: 2px solid rgba(31, 111, 235, 0.35);
      outline-offset: 2px;
    }
    .kind-list {
      display: grid;
      gap: 12px;
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .kind-option {
      display: grid;
      gap: 6px;
      padding: 14px;
      border-radius: 16px;
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.8);
    }
    .kind-option strong {
      font-size: 0.96rem;
    }
    .kind-option span {
      color: var(--muted);
      font-size: 0.86rem;
      line-height: 1.45;
    }
    .field-row {
      display: grid;
      gap: 14px;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .field-custom {
      display: grid;
      gap: 10px;
      padding: 16px;
      border-radius: 18px;
      border: 1px solid var(--border);
      background: linear-gradient(180deg, rgba(255,255,255,0.95), rgba(250,251,252,0.96));
    }
    .field-custom-header {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
    }
    .field-custom-header strong {
      font-size: 0.96rem;
    }
    .field-custom-header span {
      color: var(--muted);
      font-size: 0.84rem;
    }
    .field-custom-grid {
      display: grid;
      gap: 10px;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .field-custom-full {
      grid-column: 1 / -1;
    }
    .hidden-honeypot {
      position: absolute;
      left: -10000px;
      top: auto;
      width: 1px;
      height: 1px;
      overflow: hidden;
    }
    .footer {
      display: flex;
      flex-wrap: wrap;
      gap: 14px;
      align-items: center;
      justify-content: space-between;
      margin-top: 4px;
    }
    .button {
      appearance: none;
      border: 0;
      border-radius: 999px;
      padding: 14px 20px;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      color: white;
      cursor: pointer;
      font-weight: 700;
      box-shadow: 0 14px 30px -18px rgba(31, 111, 235, 0.7);
    }
    .button:disabled {
      cursor: not-allowed;
      opacity: 0.55;
    }
    .button-secondary {
      background: rgba(19, 21, 26, 0.06);
      color: var(--text);
      box-shadow: none;
    }
    .status {
      min-height: 1.5rem;
      font-size: 0.95rem;
      line-height: 1.6;
    }
    .status[data-state="success"] { color: var(--success); }
    .status[data-state="error"] { color: var(--danger); }
    @media (max-width: 760px) {
      .shell { padding-inline: 16px; }
      .hero, .card-body { padding-inline: 18px; }
      .kind-list, .field-row, .field-custom-grid { grid-template-columns: 1fr; }
      .footer { align-items: stretch; }
      .button { width: 100%; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">{{.TenantName}}</p>
      <h1>{{.Headline}}</h1>
      <p class="lede">{{.Description}}</p>
      <div class="meta">
        <span class="pill">{{.IdentityLabel}}</span>
        {{if .ShowPageURL}}<span class="pill">Page URL enabled</span>{{end}}
        {{if .CanSubmit}}<span class="pill">Submissions open</span>{{else}}<span class="pill">Submissions closed</span>{{end}}
        <a class="link" href="{{.BoardURL}}">Browse requests</a>
        <a class="link" href="{{.RoadmapURL}}">Roadmap</a>
        {{if .ChangelogURL}}<a class="link" href="{{.ChangelogURL}}">Changelog</a>{{end}}
      </div>
    </section>

    <section class="card">
      <div class="card-body">
        {{if not .CanSubmit}}
        <div class="notice">This portal is currently closed to new submissions. You can still review the published information below.</div>
        {{end}}
        <form id="portal-form" data-tenant-slug="{{.TenantSlug}}" data-submit-url="{{.SubmitURL}}">
          <input type="hidden" name="idempotencyKey" id="portal-idempotency-key" value="">
          <input type="text" name="honeypot" class="hidden-honeypot" tabindex="-1" autocomplete="off" aria-hidden="true">

          <fieldset {{if not .CanSubmit}}disabled{{end}}>
            <div class="field">
              <legend>Submission kind</legend>
              <div class="kind-list" role="radiogroup" aria-label="Submission kind">
                {{range .Kinds}}
                <label class="kind-option">
                  <input type="radio" name="kind" value="{{.Value}}" {{if .Checked}}checked{{end}}>
                  <strong>{{.Label}}</strong>
                  <span>{{.Description}}</span>
                </label>
                {{end}}
              </div>
            </div>

            <div class="field-grid">
              <div class="field-row">
                <label class="field">
                  <span>Title</span>
                  <input name="title" type="text" maxlength="120" required placeholder="Summarize the problem or idea">
                </label>
                {{if .ShowPageURL}}
                <label class="field">
                  <span>Page URL</span>
                  <input name="pageUrl" type="url" maxlength="2048" placeholder="https://app.example.com/...">
                </label>
                {{end}}
              </div>

              <label class="field">
                <span>Details</span>
                <textarea name="details" maxlength="4000" required placeholder="Tell us what happened, what you expected, and any helpful context."></textarea>
              </label>

              {{if .ShowIdentityInput}}
              <label class="field">
                <span>{{.IdentityFieldLabel}}</span>
                <input name="{{.IdentityFieldName}}" type="text" maxlength="120" {{if .IdentityRequired}}required{{end}} placeholder="{{.IdentityFieldPlaceholder}}">
              </label>
              {{end}}

              {{range .Fields}}
              <div class="field-custom" data-portal-field="true" data-kind="{{.Kind}}" data-key="{{.Key}}">
                <div class="field-custom-header">
                  <strong>{{.Label}}{{if .Required}} *{{end}}</strong>
                  <span>{{.KindLabel}}</span>
                </div>
                {{if eq .Kind "boolean"}}
                <label class="field">
                  <span>{{.Placeholder}}</span>
                  <input type="checkbox" name="custom_{{.Key}}" value="true" {{if .Required}}required{{end}}>
                </label>
                {{else if eq .Kind "textarea"}}
                <label class="field field-custom-full">
                  <span class="hint">{{.Placeholder}}</span>
                  <textarea name="custom_{{.Key}}" maxlength="500" {{if .Required}}required{{end}} placeholder="{{.Placeholder}}"></textarea>
                </label>
                {{else if eq .Kind "select"}}
                <label class="field">
                  <span class="hint">{{.Placeholder}}</span>
                  <select name="custom_{{.Key}}" {{if .Required}}required{{end}}>
                    <option value="">Select one</option>
                    {{range .Options}}
                    <option value="{{.}}">{{.}}</option>
                    {{end}}
                  </select>
                </label>
                {{else if eq .Kind "multiselect"}}
                <label class="field">
                  <span class="hint">{{.Placeholder}}</span>
                  <select name="custom_{{.Key}}" multiple size="{{.MultiSelectSize}}">
                    {{range .Options}}
                    <option value="{{.}}">{{.}}</option>
                    {{end}}
                  </select>
                </label>
                {{else}}
                <label class="field">
                  <span class="hint">{{.Placeholder}}</span>
                  <input name="custom_{{.Key}}" type="text" maxlength="500" {{if .Required}}required{{end}} placeholder="{{.Placeholder}}">
                </label>
                {{end}}
              </div>
              {{end}}
            </div>

            <div class="hint">{{.Acknowledgement}}</div>
            <div class="footer">
              <div class="status" id="portal-status" aria-live="polite"></div>
              <button class="button" id="portal-submit" type="submit" {{if not .CanSubmit}}disabled{{end}}>{{.SubmitButtonLabel}}</button>
            </div>
          </fieldset>
        </form>
      </div>
    </section>
  </main>
  <script>
    (() => {
      const form = document.getElementById('portal-form');
      const status = document.getElementById('portal-status');
      const submit = document.getElementById('portal-submit');
      const idempotency = document.getElementById('portal-idempotency-key');
      if (!form || !status || !submit || !idempotency) return;
      const makeToken = () => {
        if (window.crypto && typeof window.crypto.randomUUID === 'function') {
          return window.crypto.randomUUID();
        }
        return 'portal-' + Date.now() + '-' + Math.random().toString(16).slice(2);
      };
      if (!idempotency.value) {
        idempotency.value = makeToken();
      }
      const buildCustomFields = () => {
        const out = {};
        for (const wrapper of form.querySelectorAll('[data-portal-field="true"]')) {
          const key = wrapper.dataset.key;
          const kind = wrapper.dataset.kind;
          if (!key || !kind) continue;
          if (kind === 'boolean') {
            const input = wrapper.querySelector('input[type="checkbox"]');
            out[key] = Boolean(input && input.checked);
            continue;
          }
          if (kind === 'multiselect') {
            const select = wrapper.querySelector('select');
            if (!select) continue;
            const values = Array.from(select.selectedOptions).map((option) => option.value).filter(Boolean);
            if (values.length > 0) {
              out[key] = values;
            }
            continue;
          }
          const control = wrapper.querySelector('input, textarea, select');
          if (!control) continue;
          const value = control.value.trim();
          if (value) {
            out[key] = value;
          }
        }
        return out;
      };
      form.addEventListener('submit', async (event) => {
        event.preventDefault();
        submit.disabled = true;
        status.textContent = '';
        status.dataset.state = '';
        const payload = {
          tenantSlug: form.dataset.tenantSlug || '',
          kind: (form.querySelector('input[name="kind"]:checked') || {}).value || 'request',
          title: form.elements.title.value.trim(),
          details: form.elements.details.value.trim(),
          pageUrl: form.elements.pageUrl ? form.elements.pageUrl.value.trim() : '',
          displayName: form.elements.displayName ? form.elements.displayName.value.trim() : '',
          organization: form.elements.organization ? form.elements.organization.value.trim() : '',
          customFields: buildCustomFields(),
          idempotencyKey: idempotency.value,
          honeypot: form.elements.honeypot.value || '',
        };
        try {
          const response = await fetch(form.dataset.submitUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          });
          const raw = await response.text();
          let data = null;
          if (raw) {
            try {
              data = JSON.parse(raw);
            } catch {
              data = null;
            }
          }
          if (!response.ok) {
            throw new Error((data && (data.message || data.error)) || 'Request failed (' + response.status + ')');
          }
          status.textContent = (data && data.acknowledgement) || 'Thanks. We will review your submission.';
          status.dataset.state = 'success';
          idempotency.value = makeToken();
          form.reset();
          const kind = form.querySelector('input[name="kind"][value="PORTAL_SUBMISSION_KIND_REQUEST"]');
          if (kind) kind.checked = true;
        } catch (error) {
          status.textContent = error instanceof Error ? error.message : 'Submission failed.';
          status.dataset.state = 'error';
        } finally {
          submit.disabled = false;
        }
      });
    })();
  </script>
</body>
</html>`))

type portalPageData struct {
	TenantSlug               string
	TenantName               string
	Headline                 string
	Description              string
	SubmitButtonLabel        string
	Acknowledgement          string
	ShowPageURL              bool
	CanSubmit                bool
	IdentityLabel            string
	ShowIdentityInput        bool
	IdentityRequired         bool
	IdentityFieldLabel       string
	IdentityFieldName        string
	IdentityFieldPlaceholder string
	SubmitURL                string
	BoardURL                 string
	RoadmapURL               string
	ChangelogURL             string
	Kinds                    []portalPageKindOption
	Fields                   []portalPageField
}

type portalPageKindOption struct {
	Value       string
	Label       string
	Description string
	Checked     bool
}

type portalPageField struct {
	Key             string
	Label           string
	Kind            string
	KindLabel       string
	Required        bool
	Placeholder     string
	Options         []string
	MultiSelectSize int
}

func (h *Handler) Page(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", publicRequestCacheControl)
	w.Header().Set("X-Robots-Tag", "noindex")
	if h.submission == nil {
		http.Error(w, "portal not configured", http.StatusNotImplemented)
		return
	}
	cfg, err := h.submission.GetSubmissionConfig(ctx, strings.TrimSpace(chi.URLParam(r, "tenant_slug")))
	if err != nil {
		switch {
		case errors.Is(err, portalsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
			http.NotFound(w, r)
		default:
			http.Error(w, "portal unavailable", http.StatusInternalServerError)
		}
		return
	}
	page := portalPageDataFromConfig(cfg)
	buf := ptrext.Of(bytes.Buffer{})
	if err := portalPageTemplate.Execute(buf, page); err != nil {
		http.Error(w, "portal render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func portalPageDataFromConfig(cfg portalsvc.SubmissionConfig) portalPageData {
	form := cfg.Form
	kinds := []portalPageKindOption{
		{Value: "PORTAL_SUBMISSION_KIND_REQUEST", Label: "Request", Description: "A product idea or enhancement request", Checked: true},
		{Value: "PORTAL_SUBMISSION_KIND_BUG", Label: "Bug", Description: "Something is broken or behaves unexpectedly"},
		{Value: "PORTAL_SUBMISSION_KIND_GENERAL", Label: "General", Description: "Anything else worth sharing"},
	}
	fields := make([]portalPageField, 0, len(form.Fields))
	for _, field := range form.Fields {
		fields = append(fields, portalPageField{
			Key:             field.Key,
			Label:           field.Label,
			Kind:            portalFieldKindName(field.Kind),
			KindLabel:       portalFieldKindLabel(field.Kind),
			Required:        field.Required,
			Placeholder:     field.Placeholder,
			Options:         append([]string{}, field.Options...),
			MultiSelectSize: maxInt(3, len(field.Options)),
		})
	}
	identityLabel, identityFieldLabel, identityFieldName, identityPlaceholder, showIdentityInput, identityRequired := portalIdentityMeta(cfg)
	return portalPageData{
		TenantSlug:               cfg.TenantSlug,
		TenantName:               cfg.TenantName,
		Headline:                 form.Headline,
		Description:              form.Description,
		SubmitButtonLabel:        form.SubmitButtonLabel,
		Acknowledgement:          form.Acknowledgement,
		ShowPageURL:              form.ShowPageURL,
		CanSubmit:                cfg.CanSubmit,
		IdentityLabel:            identityLabel,
		ShowIdentityInput:        showIdentityInput,
		IdentityRequired:         identityRequired,
		IdentityFieldLabel:       identityFieldLabel,
		IdentityFieldName:        identityFieldName,
		IdentityFieldPlaceholder: identityPlaceholder,
		SubmitURL:                "/v1/portal/" + url.PathEscape(cfg.TenantSlug) + "/submissions",
		BoardURL:                 "/portal/" + url.PathEscape(cfg.TenantSlug) + "/requests",
		RoadmapURL:               "/portal/" + url.PathEscape(cfg.TenantSlug) + "/roadmap",
		ChangelogURL: func() string {
			if cfg.ChangelogEnabled {
				return "/portal/" + url.PathEscape(cfg.TenantSlug) + "/changelog"
			}
			return ""
		}(),
		Kinds:  kinds,
		Fields: fields,
	}
}

func portalIdentityMeta(cfg portalsvc.SubmissionConfig) (groupLabel, fieldLabel, fieldName, placeholder string, showIdentity, required bool) {
	if cfg.SubmissionWriteMode != pvrepo.WriteModeIdentified {
		return "Anonymous submissions", "", "", "", false, false
	}
	switch cfg.SubmitterIdentityMode {
	case pvrepo.IdentityModeOrganization:
		return "Organization required", "Organization", "organization", "Company or team name", true, true
	case pvrepo.IdentityModeDisplayName, pvrepo.IdentityModeAnonymous:
		return "Display name required", "Display name", "displayName", "Your name or handle", true, true
	default:
		return "Display name required", "Display name", "displayName", "Your name or handle", true, true
	}
}

func portalFieldKindName(kind pvrepo.PortalSubmissionFieldKind) string {
	switch kind {
	case pvrepo.PortalSubmissionFieldKindTextarea:
		return "textarea"
	case pvrepo.PortalSubmissionFieldKindSelect:
		return "select"
	case pvrepo.PortalSubmissionFieldKindMultiSelect:
		return "multiselect"
	case pvrepo.PortalSubmissionFieldKindBoolean:
		return "boolean"
	default:
		return "text"
	}
}

func portalFieldKindLabel(kind pvrepo.PortalSubmissionFieldKind) string {
	switch kind {
	case pvrepo.PortalSubmissionFieldKindTextarea:
		return "Paragraph"
	case pvrepo.PortalSubmissionFieldKindSelect:
		return "Single select"
	case pvrepo.PortalSubmissionFieldKindMultiSelect:
		return "Multi select"
	case pvrepo.PortalSubmissionFieldKindBoolean:
		return "Checkbox"
	default:
		return "Short text"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
