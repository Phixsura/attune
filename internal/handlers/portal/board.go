// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

var portalBoardTemplate = template.Must(template.New("portal-board").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="{{if .NoIndex}}noindex,nofollow{{else}}index,follow{{end}}">
  <title>{{.TenantName}} | Public board</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4efe7;
      --panel: rgba(255, 255, 255, 0.92);
      --panel-strong: #ffffff;
      --text: #13151a;
      --muted: #5f6472;
      --border: rgba(19, 21, 26, 0.1);
      --accent: #0f766e;
      --accent-strong: #115e59;
      --accent-2: #1f6feb;
      --success: #166534;
      --shadow: 0 28px 90px -62px rgba(19, 21, 26, 0.45);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(255, 255, 255, 0.88), transparent 26%),
        radial-gradient(circle at top right, rgba(15, 118, 110, 0.09), transparent 34%),
        linear-gradient(180deg, #f9f4ec 0%, var(--bg) 100%);
      color: var(--text);
    }
    .shell {
      max-width: 1160px;
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
      max-width: 72ch;
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
    }
    .link {
      color: var(--accent-2);
      text-decoration: none;
      font-weight: 650;
    }
    .link:hover { text-decoration: underline; }
    .grid {
      display: grid;
      gap: 16px;
    }
    .detail {
      display: grid;
      gap: 16px;
      margin-bottom: 18px;
      padding: 24px;
      border-radius: 24px;
      border: 1px solid rgba(15, 118, 110, 0.16);
      background: linear-gradient(180deg, rgba(255,255,255,0.98), rgba(247,251,250,0.98));
      box-shadow: var(--shadow);
    }
    .section-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: end;
    }
    .section-head h2 {
      margin: 0;
      font-size: 1.7rem;
      letter-spacing: -0.04em;
      line-height: 1.1;
    }
    .section-head p {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
    }
    .board-card {
      display: grid;
      gap: 12px;
      padding: 20px 20px 18px;
      border: 1px solid var(--border);
      border-radius: 22px;
      background: var(--panel-strong);
      box-shadow: var(--shadow);
    }
    .board-card.featured {
      border-color: rgba(15, 118, 110, 0.2);
      background: linear-gradient(180deg, rgba(255,255,255,0.99), rgba(243,251,249,0.98));
    }
    .board-card-header {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: start;
    }
    .board-card-header h3 {
      margin: 0;
      font-size: 1.25rem;
      line-height: 1.2;
      letter-spacing: -0.03em;
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
    .board-card-footer {
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
      padding: 11px 16px;
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
    .ghost {
      color: var(--accent-2);
      text-decoration: none;
      font-weight: 650;
    }
    .empty {
      padding: 22px 24px;
      border-radius: 20px;
      border: 1px dashed rgba(19, 21, 26, 0.15);
      background: rgba(255,255,255,0.8);
      color: var(--muted);
      line-height: 1.6;
    }
    .comment-thread {
      display: grid;
      gap: 16px;
      padding-top: 8px;
    }
    .comment-thread .section-head h3 {
      margin: 0;
      font-size: 1.35rem;
      letter-spacing: -0.04em;
      line-height: 1.1;
    }
    .comment-list {
      display: grid;
      gap: 12px;
    }
    .comment-card {
      display: grid;
      gap: 10px;
      padding: 16px 18px;
      border-radius: 18px;
      border: 1px solid rgba(19, 21, 26, 0.08);
      background: rgba(255, 255, 255, 0.96);
    }
    .comment-card[data-tone="pending"] {
      border-color: rgba(180, 83, 9, 0.24);
      background: rgba(255, 251, 235, 0.96);
    }
    .comment-card[data-tone="flagged"] {
      border-color: rgba(153, 27, 27, 0.24);
      background: rgba(254, 242, 242, 0.96);
    }
    .comment-card-head {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: start;
    }
    .comment-author {
      font-weight: 700;
      letter-spacing: -0.01em;
    }
    .comment-body {
      margin: 0;
      white-space: pre-wrap;
      line-height: 1.65;
      color: var(--text);
    }
    .comment-form {
      display: grid;
      gap: 12px;
      padding: 18px;
      border: 1px solid rgba(15, 118, 110, 0.14);
      border-radius: 20px;
      background: rgba(255, 255, 255, 0.92);
    }
    .comment-form label {
      font-size: 0.92rem;
      font-weight: 650;
      color: var(--text);
    }
    .comment-form textarea {
      width: 100%;
      min-height: 120px;
      resize: vertical;
      padding: 14px 16px;
      border-radius: 16px;
      border: 1px solid rgba(19, 21, 26, 0.12);
      background: #fff;
      color: var(--text);
      font: inherit;
      line-height: 1.6;
    }
    .comment-form textarea:focus {
      outline: 2px solid rgba(15, 118, 110, 0.22);
      border-color: rgba(15, 118, 110, 0.35);
    }
    .comment-form-footer {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      flex-wrap: wrap;
    }
    .comment-note {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
      line-height: 1.5;
    }
    .backlink {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      width: fit-content;
    }
    @media (max-width: 760px) {
      .shell { padding-inline: 16px; }
      .hero, .detail, .board-card { padding-inline: 18px; }
      .section-head, .board-card-header, .board-card-footer, .comment-card-head, .comment-form-footer { align-items: start; flex-direction: column; }
      .vote-button { width: 100%; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">{{.TenantName}}</p>
      <h1>Public board</h1>
      <p class="lede">Browse the requests already on the board, vote for the ones that matter most, and open any item for the full public summary.</p>
      <div class="meta">
        <span class="pill">{{.RequestCount}} requests</span>
        <a class="link" href="{{.SubmissionURL}}">Submit new feedback</a>
        <a class="link" href="{{.BoardURL}}">Refresh board</a>
      </div>
    </section>

    {{if .Selected}}
    <section class="detail">
      <div class="section-head">
        <div>
          <p class="eyebrow">Featured request</p>
          <h2>{{.Selected.Title}}</h2>
        </div>
        <div class="tags">
          {{if .Selected.RoadmapColumn}}<span class="tag tag-accent">{{.Selected.RoadmapColumn}}</span>{{end}}
          <span class="tag">{{.Selected.State}}</span>
        </div>
      </div>
      <p class="summary">{{.Selected.Summary}}</p>
      <div class="board-card-footer">
        <div class="vote-group">
          {{if .Selected.ShowVoteCount}}<span class="tag">{{.Selected.VoteLabel}}</span>{{end}}
          {{if .CanVote}}
            <button class="vote-button" type="button" data-vote-action data-method="{{.Selected.VoteMethod}}" data-url="{{.Selected.VoteURL}}">{{.Selected.VoteButtonLabel}}</button>
          {{end}}
        </div>
        <div class="tags">
          {{if .Selected.SubmittedByDisplay}}<span class="tag tag-muted">{{.Selected.SubmittedByDisplay}}</span>{{end}}
          {{if .Selected.ShowCommentCount}}<span class="tag tag-muted">{{.Selected.CommentLabel}}</span>{{end}}
          <a class="ghost backlink" href="{{.Selected.DetailURL}}">Open public detail</a>
        </div>
      </div>
      {{if .Selected.ShowComments}}
      <div class="comment-thread">
        <div class="section-head">
          <div>
            <p class="eyebrow">Discussion</p>
            <h3>Comments</h3>
          </div>
          <p>{{if .Selected.CanComment}}Public comments are reviewed before they appear broadly.{{else}}Public comment writing is closed on this board.{{end}}</p>
        </div>
        {{if .Selected.Comments}}
        <div class="comment-list">
          {{range .Selected.Comments}}
          <article class="comment-card" data-tone="{{.ToneClass}}">
            <div class="comment-card-head">
              <span class="comment-author">{{.AuthorLabel}}</span>
              <div class="tags">
                <span class="tag">{{.StateLabel}}</span>
                {{if .CreatedAt}}<span class="tag tag-muted">{{.CreatedAt}}</span>{{end}}
              </div>
            </div>
            <p class="comment-body">{{.Body}}</p>
          </article>
          {{end}}
        </div>
        {{else}}
        <div class="empty">No public comments are visible yet.</div>
        {{end}}
        {{if .Selected.CanComment}}
        <form class="comment-form" data-comment-form data-url="{{.Selected.CommentURL}}">
          <label for="comment-body">Add a comment</label>
          <textarea id="comment-body" name="body" maxlength="5000" placeholder="Share context, workarounds, or why this matters."></textarea>
          <div class="comment-form-footer">
            <p class="comment-note">Comments are reviewed before they appear publicly.</p>
            <button class="vote-button comment-submit" type="submit">Post comment</button>
          </div>
        </form>
        {{end}}
      </div>
      {{end}}
    </section>
    {{end}}

    {{if .Requests}}
    <section class="grid" aria-label="Public requests">
      {{range .Requests}}
      <article class="board-card{{if .IsFeatured}} featured{{end}}">
        <div class="board-card-header">
          <div>
            <h3><a class="link" href="{{.DetailURL}}">{{.Title}}</a></h3>
            <p class="summary">{{.Summary}}</p>
          </div>
          <div class="tags">
            {{if .RoadmapColumn}}<span class="tag tag-accent">{{.RoadmapColumn}}</span>{{end}}
            <span class="tag">{{.State}}</span>
          </div>
        </div>
        <div class="board-card-footer">
          <div class="vote-group">
            {{if .ShowVoteCount}}<span class="tag">{{.VoteLabel}}</span>{{end}}
            {{if $.CanVote}}
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
    </section>
    {{else if not .Selected}}
    <section class="empty">
      No public requests are visible yet. When operators publish requests, they will appear here automatically.
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
    document.addEventListener("submit", async (event) => {
      const form = event.target.closest("[data-comment-form]");
      if (!form) {
        return;
      }
      event.preventDefault();
      if (form.dataset.busy === "true") {
        return;
      }
      const textarea = form.querySelector("textarea[name='body']");
      const submit = form.querySelector("button[type='submit']");
      if (!textarea || !submit) {
        return;
      }
      if (!textarea.value.trim()) {
        alert("Comment cannot be empty.");
        return;
      }
      form.dataset.busy = "true";
      submit.disabled = true;
      try {
        const response = await fetch(form.dataset.url, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Accept": "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({ body: textarea.value })
        });
        if (!response.ok) {
          throw new Error("comment request failed");
        }
        window.location.reload();
      } catch (error) {
        form.dataset.busy = "false";
        submit.disabled = false;
        alert("Comment action failed. Please try again.");
      }
    });
  </script>
</body>
</html>`))

type portalBoardPageData struct {
	TenantSlug    string
	TenantName    string
	BoardURL      string
	SubmissionURL string
	NoIndex       bool
	CanVote       bool
	RequestCount  int
	Selected      *portalBoardRequestView
	Requests      []portalBoardRequestView
}

type portalBoardRequestView struct {
	ID                 string
	Slug               string
	Title              string
	Summary            string
	State              string
	RoadmapColumn      string
	VoteCount          int
	CommentCount       int
	ShowVoteCount      bool
	ShowCommentCount   bool
	SubmittedByDisplay string
	CreatedAt          string
	UpdatedAt          string
	VoteLabel          string
	CommentLabel       string
	VoteURL            string
	VoteMethod         string
	VoteButtonLabel    string
	DetailURL          string
	CommentURL         string
	CanVote            bool
	CanComment         bool
	ShowComments       bool
	Comments           []portalBoardCommentView
	IsFeatured         bool
}

type portalBoardCommentView struct {
	AuthorLabel string
	Body        string
	StateLabel  string
	ToneClass   string
	CreatedAt   string
}

func (h *Handler) RequestsPage(w http.ResponseWriter, r *http.Request) {
	h.renderBoardPage(w, r, "")
}

func (h *Handler) RequestPage(w http.ResponseWriter, r *http.Request) {
	h.renderBoardPage(w, r, chi.URLParam(r, "public_slug"))
}

func (h *Handler) renderBoardPage(w http.ResponseWriter, r *http.Request, selectedSlug string) {
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
	list, err := h.read.ListPublicRequests(ctx, tenantSlug, 100, "", visitorID)
	if portalBoardLoadError(w, r, err) {
		return
	}
	if list.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex")
	}
	if !list.NoIndex {
		w.Header().Del("X-Robots-Tag")
	}
	data := portalBoardPageData{
		TenantSlug:    cfg.TenantSlug,
		TenantName:    cfg.TenantName,
		BoardURL:      "/portal/" + url.PathEscape(cfg.TenantSlug) + "/requests",
		SubmissionURL: "/portal/" + url.PathEscape(cfg.TenantSlug),
		NoIndex:       list.NoIndex,
		RequestCount:  len(list.Requests),
		Requests:      portalBoardRequestViews(cfg.TenantSlug, list.Requests, selectedSlug),
	}
	if selectedSlug != "" {
		selected, loadErr := h.portalBoardSelectedView(ctx, tenantSlug, selectedSlug, visitorID, cfg.TenantSlug)
		if portalBoardLoadError(w, r, loadErr) {
			return
		}
		data.Selected = selected
	}
	data.CanVote = boardCanVote(data.Selected, data.Requests)
	if data.Selected != nil {
		data.RequestCount++
	}
	if err := portalBoardExecuteTemplate(w, data); err != nil {
		http.Error(w, "portal render failed", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) portalBoardConfigured(w http.ResponseWriter) bool {
	if h.read == nil || h.submission == nil {
		http.Error(w, "portal not configured", http.StatusNotImplemented)
		return false
	}
	return true
}

func portalBoardLoadError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, portalsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		http.NotFound(w, r)
	default:
		http.Error(w, "portal unavailable", http.StatusInternalServerError)
	}
	return true
}

func (h *Handler) portalBoardSelectedView(
	ctx context.Context,
	tenantSlug string,
	selectedSlug string,
	visitorID string,
	boardTenantSlug string,
) (*portalBoardRequestView, error) {
	detail, err := h.read.GetPublicRequest(ctx, tenantSlug, selectedSlug, visitorID)
	if err != nil {
		return nil, err
	}
	view := boardRequestView(boardTenantSlug, detail)
	view.IsFeatured = true
	return ptrext.Of(view), nil
}

func portalBoardRequestViews(tenantSlug string, requests []pvsvc.PublicRequest, selectedSlug string) []portalBoardRequestView {
	views := make([]portalBoardRequestView, 0, len(requests))
	for _, item := range requests {
		view := boardRequestView(tenantSlug, item)
		if selectedSlug != "" && view.Slug == selectedSlug {
			continue
		}
		views = append(views, view)
	}
	return views
}

func portalBoardExecuteTemplate(w http.ResponseWriter, data portalBoardPageData) error {
	buf := ptrext.Of(bytes.Buffer{})
	if err := portalBoardTemplate.Execute(buf, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf.Bytes())
	return err
}

func boardRequestView(tenantSlug string, request pvsvc.PublicRequest) portalBoardRequestView {
	voteLabel := ""
	commentLabel := ""
	if request.Policy.ShowVoteCount {
		voteLabel = fmt.Sprintf("%d votes", nonNegative(request.Votes))
	}
	if request.Policy.CommentsEnabled && request.Policy.ShowCommentCount {
		commentLabel = fmt.Sprintf("%d comments", nonNegative(request.Comments))
	}
	view := portalBoardRequestView{
		ID:                 request.Summary.ID.String(),
		Slug:               request.Summary.PublicSlug,
		Title:              request.Summary.PublicTitle,
		Summary:            request.Summary.PublicSummary,
		State:              request.Summary.PublicState,
		RoadmapColumn:      request.Summary.RoadmapColumn,
		VoteCount:          request.Votes,
		CommentCount:       request.Comments,
		ShowVoteCount:      request.Policy.ShowVoteCount,
		ShowCommentCount:   request.Policy.CommentsEnabled && request.Policy.ShowCommentCount,
		SubmittedByDisplay: request.SubmitterDisplay,
		VoteLabel:          voteLabel,
		CommentLabel:       commentLabel,
		VoteURL:            "/v1/portal/" + url.PathEscape(tenantSlug) + "/requests/" + url.PathEscape(request.Summary.PublicSlug) + "/votes",
		VoteMethod:         http.MethodPost,
		VoteButtonLabel:    "Vote",
		DetailURL:          "/portal/" + url.PathEscape(tenantSlug) + "/requests/" + url.PathEscape(request.Summary.PublicSlug),
		CommentURL:         "/v1/portal/" + url.PathEscape(tenantSlug) + "/requests/" + url.PathEscape(request.Summary.PublicSlug) + "/comments",
		CanVote:            request.Policy.VoteWriteMode != pvrepo.WriteModeDisabled,
		CanComment:         request.CanComment,
		ShowComments:       request.Policy.CommentsEnabled,
	}
	if len(request.CommentItems) > 0 {
		view.Comments = make([]portalBoardCommentView, 0, len(request.CommentItems))
		for _, comment := range request.CommentItems {
			view.Comments = append(view.Comments, boardCommentView(request.Policy, comment))
		}
	}
	if request.ViewerHasVoted {
		view.VoteMethod = http.MethodDelete
		view.VoteButtonLabel = "Remove vote"
	}
	return view
}

func boardCommentView(policy pvrepo.Policy, comment pvrepo.PublicRequestComment) portalBoardCommentView {
	author := publicSubmitterDisplay(policy, comment.SubmittedByDisplay)
	if author == "" {
		author = "Visitor"
	}
	return portalBoardCommentView{
		AuthorLabel: author,
		Body:        comment.Body,
		StateLabel:  boardCommentStateLabel(comment.State),
		ToneClass:   boardCommentTone(comment.State),
		CreatedAt:   boardCommentCreatedAt(policy, comment.CreatedAt),
	}
}

func boardCommentStateLabel(state pvrepo.ModerationState) string {
	switch state {
	case pvrepo.ModerationStateApproved:
		return "Approved"
	case pvrepo.ModerationStateRejected:
		return "Rejected"
	case pvrepo.ModerationStateHidden:
		return "Hidden"
	case pvrepo.ModerationStateSpam:
		return "Spam"
	default:
		return "Pending review"
	}
}

func boardCommentTone(state pvrepo.ModerationState) string {
	switch state {
	case pvrepo.ModerationStateApproved:
		return "approved"
	case pvrepo.ModerationStatePending:
		return "pending"
	default:
		return "flagged"
	}
}

func boardCommentCreatedAt(policy pvrepo.Policy, createdAt time.Time) string {
	if policy.HidePublicTimestamps || createdAt.IsZero() {
		return ""
	}
	return createdAt.UTC().Format("2006-01-02 15:04 UTC")
}

func boardCanVote(selected *portalBoardRequestView, requests []portalBoardRequestView) bool {
	if selected != nil {
		return selected.CanVote
	}
	if len(requests) > 0 {
		return requests[0].CanVote
	}
	return false
}
