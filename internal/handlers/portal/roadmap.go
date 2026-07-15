// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

var portalRoadmapTemplate = template.Must(template.New("portal-roadmap").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="{{if .NoIndex}}noindex,nofollow{{else}}index,follow{{end}}">
  <meta name="description" content="{{.TenantName}} public roadmap for planned, in-progress, and shipped requests.">
  <link rel="canonical" href="{{.RoadmapURL}}">
  {{if .NextURL}}<link rel="next" href="{{.NextURL}}">{{end}}
  <title>{{.TenantName}} | Public roadmap{{if .HasQuery}} | {{.Query}}{{end}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4efe7;
      --panel: rgba(255, 255, 255, 0.94);
      --panel-strong: #ffffff;
      --text: #13151a;
      --muted: #5f6472;
      --border: rgba(19, 21, 26, 0.1);
      --accent: #0f766e;
      --accent-strong: #115e59;
      --accent-2: #1f6feb;
      --shadow: 0 28px 90px -62px rgba(19, 21, 26, 0.45);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(255, 255, 255, 0.9), transparent 26%),
        radial-gradient(circle at top right, rgba(15, 118, 110, 0.11), transparent 34%),
        linear-gradient(180deg, #f8f3ec 0%, var(--bg) 100%);
      color: var(--text);
    }
    .shell {
      max-width: 1240px;
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
      background: linear-gradient(180deg, rgba(255,255,255,0.96), var(--panel));
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
      font-size: clamp(2rem, 5vw, 3.5rem);
      line-height: 1.04;
      letter-spacing: -0.05em;
    }
    .lede {
      margin: 0;
      max-width: 76ch;
      color: var(--muted);
      font-size: 1.02rem;
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
      background: rgba(15, 118, 110, 0.08);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 600;
      text-decoration: none;
    }
    .link {
      color: var(--accent-2);
      text-decoration: none;
      font-weight: 650;
    }
    .link:hover { text-decoration: underline; }
    .search {
      display: grid;
      gap: 10px;
      padding: 18px 18px 16px;
      border: 1px solid rgba(19, 21, 26, 0.08);
      border-radius: 20px;
      background: rgba(255, 255, 255, 0.86);
    }
    .search-row {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }
    .search-row input,
    .search-row select {
      min-height: 44px;
      padding: 0 14px;
      border-radius: 14px;
      border: 1px solid rgba(19, 21, 26, 0.12);
      background: #fff;
      color: var(--text);
      font: inherit;
    }
    .search-row input {
      flex: 2 1 320px;
      min-width: 0;
    }
    .search-row input.search-filter {
      flex: 1 1 180px;
      min-width: 0;
    }
    .search-row select {
      min-width: 160px;
    }
    .search-submit {
      min-height: 44px;
      padding-inline: 18px;
      border: 0;
      border-radius: 999px;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      color: #fff;
      font: inherit;
      font-weight: 700;
      cursor: pointer;
      box-shadow: 0 14px 30px -18px rgba(15, 118, 110, 0.72);
    }
    .search-submit:disabled {
      opacity: 0.55;
      cursor: not-allowed;
    }
    .filter-row {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }
    .chip-control {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      min-height: 40px;
      padding: 0 12px;
      border-radius: 999px;
      border: 1px solid rgba(19, 21, 26, 0.12);
      background: rgba(255, 255, 255, 0.92);
      color: var(--text);
      font: inherit;
      font-weight: 650;
      cursor: pointer;
    }
    .chip-control input {
      margin: 0;
      accent-color: var(--accent);
    }
    .chip-control.active {
      border-color: rgba(15, 118, 110, 0.34);
      background: rgba(15, 118, 110, 0.08);
      color: var(--accent-strong);
    }
    .comment-note {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
      line-height: 1.5;
    }
    .roadmap-summary {
      display: grid;
      gap: 10px;
      margin-bottom: 16px;
      padding: 18px 20px;
      border-radius: 20px;
      border: 1px solid rgba(15, 118, 110, 0.12);
      background: rgba(255, 255, 255, 0.9);
    }
    .roadmap-summary-line {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
      justify-content: space-between;
    }
    .roadmap-summary-line strong {
      font-size: 1.02rem;
      letter-spacing: -0.02em;
    }
    .roadmap-grid {
      display: grid;
      gap: 16px;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      align-items: start;
    }
    .roadmap-column {
      display: grid;
      gap: 14px;
      padding: 18px;
      border: 1px solid var(--border);
      border-radius: 24px;
      background: var(--panel-strong);
      box-shadow: var(--shadow);
    }
    .roadmap-column-header {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: start;
    }
    .roadmap-column-header h2 {
      margin: 0;
      font-size: 1.2rem;
      line-height: 1.2;
      letter-spacing: -0.03em;
    }
    .roadmap-column-header p {
      margin: 0;
      color: var(--muted);
      font-size: 0.9rem;
      line-height: 1.45;
    }
    .roadmap-count {
      flex-shrink: 0;
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 5px 10px;
      border-radius: 999px;
      background: rgba(15, 118, 110, 0.08);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 700;
    }
    .roadmap-empty {
      min-height: 132px;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 18px;
      border-radius: 18px;
      border: 1px dashed rgba(19, 21, 26, 0.14);
      background: rgba(247, 248, 249, 0.85);
      color: var(--muted);
      text-align: center;
      line-height: 1.6;
    }
    .roadmap-list {
      display: grid;
      gap: 12px;
    }
    .roadmap-card {
      display: grid;
      gap: 10px;
      padding: 16px 16px 14px;
      border-radius: 18px;
      border: 1px solid rgba(19, 21, 26, 0.08);
      background: linear-gradient(180deg, rgba(255,255,255,0.98), rgba(250,251,252,0.98));
    }
    .roadmap-card {
      position: relative;
      cursor: pointer;
      transition: transform 180ms ease, border-color 180ms ease, box-shadow 180ms ease;
    }
    .roadmap-card > * {
      position: relative;
      z-index: 0;
    }
    .card-overlay {
      position: absolute;
      inset: 0;
      z-index: 1;
      display: block;
      border-radius: inherit;
    }
    .roadmap-card .link,
    .roadmap-card .vote-button {
      position: relative;
      z-index: 2;
    }
    @media (hover: hover) {
      .roadmap-card:hover {
        transform: translateY(-1px);
        border-color: rgba(15, 118, 110, 0.18);
      }
    }
    .roadmap-card:focus-within {
      border-color: rgba(15, 118, 110, 0.22);
      box-shadow: 0 0 0 3px rgba(15, 118, 110, 0.08), var(--shadow);
    }
    .roadmap-card.featured {
      border-color: rgba(15, 118, 110, 0.16);
      background: linear-gradient(180deg, rgba(255,255,255,0.99), rgba(243,251,249,0.98));
    }
    .roadmap-card-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: start;
    }
    .roadmap-card-head h3 {
      margin: 0;
      font-size: 1.04rem;
      line-height: 1.25;
      letter-spacing: -0.02em;
    }
    .summary {
      margin: 0;
      color: var(--muted);
      line-height: 1.6;
    }
    .tags {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    .tag {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 5px 10px;
      border-radius: 999px;
      background: rgba(19, 21, 26, 0.05);
      color: var(--text);
      font-size: 12px;
      font-weight: 600;
    }
    .tag-accent {
      background: rgba(15, 118, 110, 0.1);
      color: var(--accent-strong);
    }
    .tag-muted {
      color: var(--muted);
    }
    .roadmap-card-footer {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      justify-content: space-between;
      align-items: center;
    }
    .vote-group {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
    }
    .vote-button {
      appearance: none;
      border: 0;
      border-radius: 999px;
      padding: 10px 14px;
      font-weight: 700;
      cursor: pointer;
      color: #fff;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      box-shadow: 0 14px 30px -18px rgba(15, 118, 110, 0.72);
    }
    .vote-button[data-action="unvote"] {
      background: rgba(19, 21, 26, 0.08);
      color: var(--text);
      box-shadow: none;
    }
    .vote-button:disabled {
      opacity: 0.55;
      cursor: not-allowed;
    }
    .empty {
      margin-top: 18px;
      padding: 22px 24px;
      border-radius: 20px;
      border: 1px dashed rgba(19, 21, 26, 0.15);
      background: rgba(255,255,255,0.8);
      color: var(--muted);
      line-height: 1.6;
    }
    .pager {
      display: flex;
      justify-content: center;
      margin-top: 18px;
    }
    .pager-link {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 46px;
      padding: 0 18px;
      border-radius: 999px;
      border: 1px solid rgba(15, 118, 110, 0.18);
      background: rgba(255, 255, 255, 0.94);
      color: var(--accent-strong);
      text-decoration: none;
      font-weight: 700;
      box-shadow: 0 14px 34px -24px rgba(15, 118, 110, 0.44);
    }
    .pager-link:hover {
      background: rgba(255, 255, 255, 1);
      text-decoration: none;
    }
    @media (max-width: 760px) {
      .shell { padding-inline: 16px; }
      .hero, .roadmap-summary, .roadmap-column { padding-inline: 18px; }
      .roadmap-card-head, .roadmap-card-footer, .roadmap-column-header, .roadmap-summary-line { align-items: start; flex-direction: column; }
      .vote-button { width: 100%; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">{{.TenantName}}</p>
      <h1>Public roadmap</h1>
      <p class="lede">Track requests through the public workflow, browse by roadmap stage, and open any item to inspect the full public thread.</p>
      <div class="meta">
        <span class="pill">{{.RequestCount}} items</span>
        <a class="link" href="{{.BoardURL}}">Browse requests</a>
        <a class="link" href="{{.PortalURL}}">Submit new feedback</a>
        <a class="link" href="{{.RoadmapURL}}">Reset filters</a>
      </div>
      <form class="search" method="get" action="{{.RoadmapURL}}">
        <label class="sr-only" for="roadmap-search">Search roadmap</label>
        <div class="search-row">
          <input id="roadmap-search" type="search" name="q" value="{{.Query}}" placeholder="Search requests or comments">
          <select name="sort" aria-label="Sort roadmap">
            <option value="top"{{if eq .Sort "top"}} selected{{end}}>Top</option>
            <option value="recent"{{if eq .Sort "recent"}} selected{{end}}>Recent</option>
          </select>
          <input id="roadmap-state" class="search-filter" type="search" name="state" value="{{.State}}" placeholder="Filter by state">
          <input id="roadmap-column" class="search-filter" type="search" name="roadmap" value="{{.Roadmap}}" placeholder="Filter by roadmap">
          <button class="search-submit" type="submit">Search</button>
          {{if or .HasQuery .HasState .HasRoadmap .OnlyVotedByMe .OnlyWithComments (ne .Sort "top")}}
          <a class="link" href="{{.RoadmapURL}}">Clear filters</a>
          {{end}}
        </div>
        <div class="filter-row" aria-label="Quick filters">
          <label class="chip-control{{if .OnlyVotedByMe}} active{{end}}">
            <input type="checkbox" name="voted" value="mine"{{if .OnlyVotedByMe}} checked{{end}}>
            <span>My votes</span>
          </label>
          <label class="chip-control{{if .OnlyWithComments}} active{{end}}">
            <input type="checkbox" name="comments" value="with"{{if .OnlyWithComments}} checked{{end}}>
            <span>With comments</span>
          </label>
        </div>
        <p class="comment-note">Columns are derived from workflow state and ordered by the tenant roadmap mapping. Search spans titles, summaries, and approved public comments.</p>
      </form>
    </section>

    {{if and (eq .RequestCount 0) (gt (len .Columns) 0)}}
    <section class="empty">
      {{if or .HasQuery .HasState .HasRoadmap .OnlyVotedByMe .OnlyWithComments}}
      No public roadmap items matched the current filters. <a class="link" href="{{.RoadmapURL}}">Clear filters</a>
      {{else}}
      No public roadmap items are visible yet. When operators publish requests, they will appear here automatically.
      {{end}}
    </section>
    {{end}}

    <section class="roadmap-summary">
      <div class="roadmap-summary-line">
        <strong>{{.TenantName}} roadmap stages</strong>
        <span class="pill">{{len .Columns}} stages</span>
      </div>
      <div class="comment-note">The public roadmap only includes requests that operators have marked for roadmap visibility.</div>
    </section>

    <section class="roadmap-grid" aria-label="Public roadmap">
      {{range .Columns}}
      <article class="roadmap-column">
        <div class="roadmap-column-header">
          <div>
            <h2>{{.Name}}</h2>
            <p>{{if .Requests}}{{len .Requests}} request{{if ne (len .Requests) 1}}s{{end}} in this stage{{else}}No requests in this stage yet.{{end}}</p>
          </div>
          <span class="roadmap-count">{{len .Requests}}</span>
        </div>

        {{if .Requests}}
        <div class="roadmap-list">
          {{range .Requests}}
          <article class="roadmap-card{{if .IsFeatured}} featured{{end}}">
            <a class="card-overlay" href="{{.DetailURL}}" aria-hidden="true" tabindex="-1"></a>
            <div class="roadmap-card-head">
              <div>
                <h3><a class="link" href="{{.DetailURL}}">{{.Title}}</a></h3>
                <p class="summary">{{.Summary}}</p>
              </div>
              <div class="tags">
                {{if .FreshnessLabel}}<time class="tag tag-muted" data-freshness datetime="{{.FreshnessDateTime}}" title="{{.FreshnessTitle}}" aria-label="{{.FreshnessTitle}}">{{.FreshnessLabel}}</time>{{end}}
                {{if .RoadmapColumn}}<span class="tag tag-accent">{{.RoadmapColumn}}</span>{{end}}
                <span class="tag">{{.State}}</span>
              </div>
            </div>
            <div class="roadmap-card-footer">
              <div class="vote-group">
                {{if .ShowVoteCount}}<span class="tag">{{.VoteLabel}}</span>{{end}}
                {{if .CanVote}}
                <button class="vote-button" type="button" data-vote-action data-method="{{.VoteMethod}}" data-url="{{.VoteURL}}">{{.VoteButtonLabel}}</button>
                {{end}}
              </div>
              <div class="tags">
                {{if .SubmittedByDisplay}}<span class="tag tag-muted">{{.SubmittedByDisplay}}</span>{{end}}
                {{if .ShowCommentCount}}<span class="tag tag-muted">{{.CommentLabel}}</span>{{end}}
              </div>
            </div>
          </article>
          {{end}}
        </div>
        {{else}}
        <div class="roadmap-empty">This stage has no public requests yet.</div>
        {{end}}
      </article>
      {{end}}
    </section>

    {{if .NextURL}}
    <section class="pager" aria-label="Pagination">
      <a class="pager-link" href="{{.NextURL}}">Load more roadmap items</a>
    </section>
    {{end}}
  </main>
  <script>
    document.addEventListener("click", async (event) => {
      const button = event.target.closest("[data-vote-action]");
      if (!button) {
        return;
      }
      event.preventDefault();
      if (button.disabled) {
        return;
      }
      button.disabled = true;
      try {
        const response = await fetch(button.dataset.url, {
          method: button.dataset.method,
          credentials: "same-origin",
          headers: {
            "Accept": "application/json"
          }
        });
        if (!response.ok) {
          throw new Error("vote request failed");
        }
        window.location.reload();
      } catch (error) {
        button.disabled = false;
        alert("Vote action failed. Please try again.");
      }
    });
  </script>
</body>
</html>`))

type roadmapPageData struct {
	TenantSlug       string
	TenantName       string
	PortalURL        string
	BoardURL         string
	RoadmapURL       string
	Query            string
	Sort             string
	HasQuery         bool
	State            string
	Roadmap          string
	HasState         bool
	HasRoadmap       bool
	OnlyVotedByMe    bool
	OnlyWithComments bool
	NoIndex          bool
	RequestCount     int
	Columns          []roadmapPageColumnView
	NextURL          string
}

type roadmapPageColumnView struct {
	Name     string
	Requests []portalBoardRequestView
}

func (h *Handler) RoadmapPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantSlug := strings.TrimSpace(chi.URLParam(r, "tenant_slug"))
	w.Header().Set("Cache-Control", publicRequestCacheControl)
	if !h.portalBoardConfigured(w) {
		return
	}
	cfg, err := h.submission.GetSubmissionConfig(ctx, tenantSlug)
	if portalBoardLoadError(w, r, err) {
		return
	}
	visitorID, err := ensurePortalVisitor(r, func(cookie *http.Cookie) {
		http.SetCookie(w, cookie)
	}, h.secrets, tenantSlug, false)
	if err != nil {
		http.Error(w, "portal unavailable", http.StatusInternalServerError)
		return
	}
	query, sort, state, roadmap, votedOnly, commentsOnly, cursor := portalBoardSearchParams(r)
	list, err := h.read.ListPublicRoadmap(ctx, tenantSlug, portalBoardPageSize, cursor, query, sort, state, roadmap, votedOnly, commentsOnly, visitorID)
	if portalBoardLoadError(w, r, err) {
		return
	}
	if list.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex")
	} else {
		w.Header().Del("X-Robots-Tag")
	}
	boardBaseURL := "/portal/" + url.PathEscape(cfg.TenantSlug) + "/requests"
	portalBaseURL := "/portal/" + url.PathEscape(cfg.TenantSlug)
	roadmapBaseURL := "/portal/" + url.PathEscape(cfg.TenantSlug) + "/roadmap"
	querySuffix := portalBoardQueryString(query, sort, state, roadmap, votedOnly, commentsOnly, cursor)
	detailQuerySuffix := portalBoardAppendQueryParam(querySuffix, "back", roadmapBaseURL)
	nextURL := ""
	if list.NextCursor != "" {
		nextURL = roadmapBaseURL + portalBoardQueryString(query, sort, state, roadmap, votedOnly, commentsOnly, list.NextCursor)
	}
	data := roadmapPageData{
		TenantSlug:       cfg.TenantSlug,
		TenantName:       cfg.TenantName,
		PortalURL:        portalBaseURL,
		BoardURL:         boardBaseURL,
		RoadmapURL:       roadmapBaseURL,
		Query:            query,
		Sort:             sort,
		HasQuery:         query != "",
		State:            state,
		Roadmap:          roadmap,
		HasState:         state != "",
		HasRoadmap:       roadmap != "",
		OnlyVotedByMe:    votedOnly,
		OnlyWithComments: commentsOnly,
		NoIndex:          list.NoIndex,
		RequestCount:     len(list.Requests),
		Columns:          roadmapPageColumns(cfg.TenantSlug, list, detailQuerySuffix, roadmapBaseURL),
		NextURL:          nextURL,
	}
	if err := portalRoadmapExecuteTemplate(w, data); err != nil {
		http.Error(w, "portal render failed", http.StatusInternalServerError)
		return
	}
}

func roadmapPageColumns(tenantSlug string, result pvsvc.PublicRequestList, querySuffix string, returnURL string) []roadmapPageColumnView {
	grouped := roadmapPublicColumns(result)
	columns := make([]roadmapPageColumnView, 0, len(grouped))
	for _, column := range grouped {
		view := roadmapPageColumnView{
			Name:     column.Name,
			Requests: make([]portalBoardRequestView, 0, len(column.Requests)),
		}
		for _, request := range column.Requests {
			view.Requests = append(view.Requests, boardRequestView(tenantSlug, request, querySuffix, returnURL))
		}
		columns = append(columns, view)
	}
	return columns
}

type roadmapPublicColumn struct {
	Name     string
	Requests []pvsvc.PublicRequest
}

func roadmapPublicColumns(result pvsvc.PublicRequestList) []roadmapPublicColumn {
	columns := make([]*roadmapPublicColumn, 0, len(result.Policy.RoadmapStatusMappings))
	columnsByName := make(map[string]*roadmapPublicColumn, len(result.Policy.RoadmapStatusMappings))
	addColumn := func(name string) *roadmapPublicColumn {
		column := ptrext.Of(roadmapPublicColumn{Name: name})
		columnsByName[name] = column
		columns = append(columns, column)
		return column
	}
	for _, mapping := range result.Policy.RoadmapStatusMappings {
		if !mapping.Included {
			continue
		}
		name := strings.TrimSpace(mapping.Label)
		if name == "" {
			continue
		}
		if _, exists := columnsByName[name]; exists {
			continue
		}
		addColumn(name)
	}
	for _, item := range result.Requests {
		name := strings.TrimSpace(item.Summary.RoadmapColumn)
		if name == "" {
			continue
		}
		column, exists := columnsByName[name]
		if !exists {
			column = addColumn(name)
		}
		column.Requests = append(column.Requests, item)
	}
	out := make([]roadmapPublicColumn, 0, len(columns))
	for _, column := range columns {
		out = append(out, ptrext.Indirect(column))
	}
	return out
}

func portalRoadmapExecuteTemplate(w http.ResponseWriter, data roadmapPageData) error {
	buf := ptrext.Of(bytes.Buffer{})
	if err := portalRoadmapTemplate.Execute(buf, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf.Bytes())
	return err
}
