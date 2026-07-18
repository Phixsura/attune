// SPDX-License-Identifier: Apache-2.0

package portal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	rnrepo "github.com/Phixsura/attune/internal/repo/requestnotification"
	portalsvc "github.com/Phixsura/attune/internal/service/portal"
)

const (
	portalChangelogPageSize = 10
	portalChangelogFeedSize = 20
)

var portalChangelogTemplate = template.Must(template.New("portal-changelog").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="{{if .NoIndex}}noindex,nofollow{{else}}index,follow{{end}}">
  <meta name="description" content="{{.TenantName}} public changelog and release notes.">
  <link rel="canonical" href="{{.ChangelogURL}}">
  <link rel="alternate" type="application/rss+xml" title="{{.TenantName}} changelog RSS" href="{{.FeedRSSURL}}">
  <link rel="alternate" type="application/feed+json" title="{{.TenantName}} changelog JSON" href="{{.FeedJSONURL}}">
  {{if .NextURL}}<link rel="next" href="{{.NextURL}}">{{end}}
  <title>{{.TenantName}} | Changelog</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f2ece2;
      --panel: rgba(255, 255, 255, 0.88);
      --panel-strong: rgba(255, 255, 255, 0.98);
      --text: #111318;
      --muted: #606575;
      --border: rgba(17, 19, 24, 0.1);
      --border-strong: rgba(17, 19, 24, 0.15);
      --accent: #0e766d;
      --accent-strong: #0b5f57;
      --accent-soft: rgba(14, 118, 109, 0.1);
      --shadow: 0 30px 100px -68px rgba(18, 22, 28, 0.5);
      --sans: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      --display: "Iowan Old Style", "Baskerville", "Palatino Linotype", "Book Antiqua", Georgia, serif;
    }
    html { scroll-behavior: smooth; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: var(--sans);
      background:
        radial-gradient(circle at 12% 10%, rgba(255, 255, 255, 0.95), transparent 22%),
        radial-gradient(circle at 84% 14%, rgba(14, 118, 109, 0.12), transparent 26%),
        radial-gradient(circle at 78% 78%, rgba(17, 19, 24, 0.05), transparent 28%),
        linear-gradient(180deg, #faf6ef 0%, var(--bg) 100%);
      color: var(--text);
    }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background:
        linear-gradient(rgba(255, 255, 255, 0.06), rgba(255, 255, 255, 0)),
        repeating-linear-gradient(0deg, rgba(17, 19, 24, 0.018) 0, rgba(17, 19, 24, 0.018) 1px, transparent 1px, transparent 44px),
        repeating-linear-gradient(90deg, rgba(17, 19, 24, 0.015) 0, rgba(17, 19, 24, 0.015) 1px, transparent 1px, transparent 44px);
      opacity: 0.4;
      mix-blend-mode: soft-light;
    }
    .shell {
      position: relative;
      z-index: 1;
      max-width: 1360px;
      margin: 0 auto;
      padding: 28px 20px 56px;
    }
    .page {
      display: grid;
      gap: 24px;
      grid-template-columns: minmax(0, 1.55fr) minmax(300px, 0.9fr);
      align-items: start;
    }
    .stack {
      display: grid;
      gap: 24px;
      min-width: 0;
    }
    .hero {
      position: relative;
      overflow: hidden;
      display: grid;
      padding: 34px 34px 30px;
      border: 1px solid var(--border);
      border-radius: 34px;
      background: linear-gradient(180deg, rgba(255,255,255,0.92), rgba(255,255,255,0.76));
      box-shadow: var(--shadow);
    }
    .hero::before,
    .hero::after {
      content: "";
      position: absolute;
      pointer-events: none;
      border-radius: 999px;
      filter: blur(8px);
    }
    .hero::before {
      top: -90px;
      right: -70px;
      width: 240px;
      height: 240px;
      background: radial-gradient(circle, rgba(14, 118, 109, 0.18) 0%, rgba(14, 118, 109, 0) 70%);
    }
    .hero::after {
      left: -80px;
      bottom: -120px;
      width: 280px;
      height: 280px;
      background: radial-gradient(circle, rgba(17, 19, 24, 0.08) 0%, rgba(17, 19, 24, 0) 72%);
    }
    .hero-grid {
      position: relative;
      z-index: 1;
      display: grid;
      gap: 22px;
      grid-template-columns: minmax(0, 1.3fr) minmax(260px, 0.75fr);
      align-items: end;
    }
    .eyebrow {
      margin: 0 0 10px;
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.24em;
      text-transform: uppercase;
      color: var(--accent-strong);
    }
    h1 {
      margin: 0;
      font-family: var(--display);
      font-size: clamp(3.1rem, 7vw, 5.8rem);
      line-height: 0.93;
      letter-spacing: -0.08em;
      font-weight: 700;
      text-wrap: balance;
    }
    .lede {
      margin: 14px 0 0;
      max-width: 60ch;
      color: var(--muted);
      font-size: 1.05rem;
      line-height: 1.8;
      text-wrap: pretty;
    }
    .hero-meta {
      margin-top: 20px;
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-items: center;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-height: 34px;
      padding: 0 12px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 650;
      text-decoration: none;
      white-space: nowrap;
      font-variant-numeric: tabular-nums;
    }
    .quick-chips {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 18px;
    }
    .chip {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-height: 40px;
      padding: 0 12px;
      border-radius: 999px;
      border: 1px solid rgba(14, 118, 109, 0.18);
      background: rgba(255, 255, 255, 0.86);
      color: var(--accent-strong);
      font-size: 0.9rem;
      font-weight: 650;
      text-decoration: none;
      transition: transform 180ms ease, border-color 180ms ease, box-shadow 180ms ease;
    }
    .chip:hover {
      transform: translateY(-1px);
      border-color: rgba(14, 118, 109, 0.28);
      box-shadow: 0 14px 28px -22px rgba(17, 19, 24, 0.4);
    }
    .hero-stats {
      display: grid;
      gap: 12px;
    }
    .stat {
      padding: 16px 16px 15px;
      border-radius: 22px;
      background: rgba(255, 255, 255, 0.78);
      border: 1px solid rgba(17, 19, 24, 0.08);
      box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.74);
    }
    .stat-label {
      margin: 0 0 6px;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .stat-value {
      display: block;
      min-height: 2.1rem;
      font-family: var(--display);
      font-size: 2.1rem;
      line-height: 1;
      letter-spacing: -0.06em;
      color: var(--text);
      font-variant-numeric: tabular-nums;
      text-wrap: balance;
    }
    .stat-note {
      margin: 8px 0 0;
      color: var(--muted);
      font-size: 0.92rem;
      line-height: 1.6;
      text-wrap: pretty;
    }
    .archive {
      display: grid;
      gap: 18px;
    }
    .archive-head {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      justify-content: space-between;
      align-items: end;
      margin-top: 2px;
    }
    .archive-head h2 {
      margin: 0;
      font-family: var(--display);
      font-size: clamp(1.6rem, 2.9vw, 2.3rem);
      line-height: 1.04;
      letter-spacing: -0.06em;
      text-wrap: balance;
    }
    .archive-meta {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
      line-height: 1.5;
      font-variant-numeric: tabular-nums;
    }
    .timeline {
      display: grid;
      gap: 16px;
    }
    .post {
      display: grid;
      grid-template-columns: 24px minmax(0, 1fr);
      gap: 18px;
      padding: 24px;
      border-radius: 28px;
      border: 1px solid rgba(17, 19, 24, 0.08);
      background: linear-gradient(180deg, rgba(255, 255, 255, 0.96), rgba(250, 251, 252, 0.96));
      box-shadow: var(--shadow);
    }
    .post-rail {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 10px;
      padding-top: 8px;
    }
    .post-dot {
      width: 12px;
      height: 12px;
      border-radius: 999px;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      box-shadow: 0 0 0 6px rgba(14, 118, 109, 0.1);
    }
    .post-line {
      width: 1px;
      flex: 1;
      min-height: 72px;
      background: linear-gradient(180deg, rgba(14, 118, 109, 0.2), rgba(14, 118, 109, 0));
    }
    .post-main {
      min-width: 0;
      display: grid;
      gap: 16px;
    }
    .post-top {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      justify-content: space-between;
      align-items: start;
    }
    .post-kicker {
      margin: 0 0 8px;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .post-top h3 {
      margin: 0;
      font-family: var(--display);
      font-size: clamp(1.5rem, 2.4vw, 2.1rem);
      line-height: 1.05;
      letter-spacing: -0.06em;
      max-width: 22ch;
      text-wrap: balance;
    }
    .post-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      color: var(--muted);
      font-size: 0.9rem;
    }
    .post-time {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-height: 34px;
      padding: 0 11px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent-strong);
      font-weight: 650;
      white-space: nowrap;
      font-variant-numeric: tabular-nums;
    }
    .body {
      margin: 0;
      max-width: 66ch;
      white-space: pre-wrap;
      color: var(--text);
      line-height: 1.8;
      font-size: 0.98rem;
      text-wrap: pretty;
    }
    .requests {
      display: grid;
      gap: 10px;
    }
    .requests-head {
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .request-grid {
      display: grid;
      gap: 12px;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    }
    .request-card {
      position: relative;
      display: grid;
      gap: 8px;
      padding: 15px 15px 14px 16px;
      border-radius: 20px;
      border: 1px solid rgba(17, 19, 24, 0.08);
      background: linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(249, 250, 252, 0.95));
      text-decoration: none;
      font-size: 0.9rem;
      color: inherit;
      box-shadow: 0 14px 30px -26px rgba(17, 19, 24, 0.36);
      transition: transform 180ms ease, border-color 180ms ease, box-shadow 180ms ease;
    }
    .request-card::before {
      content: "";
      position: absolute;
      inset: 14px auto 14px 0;
      width: 3px;
      border-radius: 999px;
      background: linear-gradient(180deg, var(--accent), rgba(14, 118, 109, 0.18));
    }
    .request-card:hover {
      transform: translateY(-2px);
      border-color: rgba(14, 118, 109, 0.26);
      box-shadow: 0 18px 38px -24px rgba(17, 19, 24, 0.38);
    }
    .request-title {
      font-size: 0.98rem;
      line-height: 1.45;
      font-weight: 650;
    }
    .request-summary {
      color: var(--muted);
      font-size: 0.9rem;
      line-height: 1.6;
    }
    .request-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    .request-meta span {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      min-height: 26px;
      padding: 0 8px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent-strong);
      font-size: 12px;
      font-weight: 650;
      font-variant-numeric: tabular-nums;
    }
    .empty-grid {
      display: grid;
      gap: 16px;
      grid-template-columns: minmax(0, 1.1fr) minmax(260px, 0.9fr);
    }
    .empty-card {
      padding: 22px;
      border-radius: 24px;
      border: 1px solid rgba(17, 19, 24, 0.08);
      background: linear-gradient(180deg, rgba(255, 255, 255, 0.95), rgba(250, 251, 252, 0.96));
      box-shadow: var(--shadow);
    }
    .empty-card--hero {
      display: grid;
      gap: 18px;
      align-content: start;
      min-height: 100%;
    }
    .empty-card h3 {
      margin: 0;
      font-family: var(--display);
      font-size: clamp(1.5rem, 2.5vw, 2.1rem);
      line-height: 1.08;
      letter-spacing: -0.06em;
      text-wrap: balance;
    }
    .empty-card p {
      margin: 0;
      color: var(--muted);
      line-height: 1.75;
      text-wrap: pretty;
    }
    .empty-rules {
      display: grid;
      gap: 12px;
    }
    .rule-item {
      display: flex;
      gap: 12px;
      align-items: flex-start;
      padding: 12px 0;
      border-top: 1px solid rgba(17, 19, 24, 0.08);
    }
    .rule-item:first-child {
      border-top: 0;
      padding-top: 0;
    }
    .rule-mark {
      width: 9px;
      height: 9px;
      margin-top: 0.55rem;
      border-radius: 50%;
      background: var(--accent);
      box-shadow: 0 0 0 5px rgba(14, 118, 109, 0.1);
      flex: none;
    }
    .rule-item strong {
      display: block;
      font-size: 0.94rem;
      line-height: 1.4;
    }
    .rule-item span {
      display: block;
      margin-top: 3px;
      color: var(--muted);
      font-size: 0.88rem;
      line-height: 1.55;
    }
    .rail {
      position: sticky;
      top: 18px;
      display: grid;
      gap: 16px;
    }
    .panel {
      padding: 22px;
      border-radius: 26px;
      border: 1px solid rgba(17, 19, 24, 0.08);
      background: rgba(255, 255, 255, 0.84);
      box-shadow: var(--shadow);
      backdrop-filter: blur(16px) saturate(120%);
    }
    .panel-kicker {
      margin: 0 0 8px;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .panel h4 {
      margin: 0;
      font-family: var(--display);
      font-size: 1.45rem;
      line-height: 1.08;
      letter-spacing: -0.05em;
      text-wrap: balance;
    }
    .panel p {
      margin: 12px 0 0;
      color: var(--muted);
      line-height: 1.75;
    }
    .feed-links {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 16px;
    }
    .feed-links a {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-height: 40px;
      padding: 0 12px;
      border-radius: 999px;
      border: 1px solid rgba(14, 118, 109, 0.18);
      background: rgba(255, 255, 255, 0.9);
      color: var(--accent-strong);
      text-decoration: none;
      font-size: 0.9rem;
      font-weight: 650;
      transition: transform 180ms ease, border-color 180ms ease, box-shadow 180ms ease;
    }
    .feed-links a:hover {
      transform: translateY(-1px);
      border-color: rgba(14, 118, 109, 0.28);
      box-shadow: 0 14px 28px -22px rgba(17, 19, 24, 0.4);
    }
    .link-stack {
      display: grid;
      gap: 10px;
      margin-top: 16px;
    }
    .link-stack a {
      color: var(--accent-strong);
      text-decoration: none;
      font-weight: 650;
    }
    .link-stack a:hover { text-decoration: underline; }
    .page-footer {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      justify-content: space-between;
      align-items: center;
      margin-top: 4px;
      padding: 0 2px;
    }
    .hint {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
      line-height: 1.6;
    }
    .next {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      min-height: 44px;
      padding: 0 16px;
      border-radius: 999px;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      color: white;
      font-weight: 700;
      text-decoration: none;
      box-shadow: 0 14px 30px -18px rgba(14, 118, 109, 0.68);
      transition: transform 180ms ease, box-shadow 180ms ease;
    }
    .next:hover {
      transform: translateY(-1px);
      box-shadow: 0 18px 36px -20px rgba(14, 118, 109, 0.75);
    }
    .next:focus-visible,
    .chip:focus-visible,
    .feed-links a:focus-visible,
    .request-card:focus-visible,
    .link-stack a:focus-visible {
      outline: 2px solid rgba(14, 118, 109, 0.3);
      outline-offset: 2px;
    }
    @media (max-width: 1120px) {
      .page { grid-template-columns: minmax(0, 1fr); }
      .rail { position: static; }
      .hero-grid { grid-template-columns: minmax(0, 1fr); }
      .empty-grid { grid-template-columns: 1fr; }
    }
    @media (max-width: 760px) {
      .shell { padding-inline: 16px; }
      .hero { padding: 26px 20px 22px; border-radius: 28px; }
      .hero-grid,
      .post,
      .empty-grid { gap: 14px; }
      .post { grid-template-columns: 18px minmax(0, 1fr); padding: 20px; border-radius: 24px; }
      .request-grid { grid-template-columns: 1fr; }
      .page-footer { align-items: stretch; }
      .next { width: 100%; justify-content: center; }
      .panel,
      .empty-card { padding: 20px; border-radius: 22px; }
    }
  </style>
</head>
<body>
  <main class="shell">
    <div class="page">
      <div class="stack">
        <section class="hero">
          <div class="hero-grid">
            <div>
              <p class="eyebrow">{{.TenantName}} public changelog</p>
              <h1>Changelog</h1>
              <p class="lede">Release notes for shipped work, written for the public portal and mirrored to feeds without leaking private context.</p>
              <div class="hero-meta">
                <span class="pill">Public changelog</span>
                <span class="pill">{{.PostCount}} posts</span>
                <span class="pill">{{.RequestCount}} linked requests</span>
                <span class="pill">{{if .NoIndex}}No index{{else}}Indexable{{end}}</span>
              </div>
              <div class="quick-chips">
                <a class="chip" href="{{.FeedRSSURL}}">RSS feed</a>
                <a class="chip" href="{{.FeedJSONURL}}">JSON feed</a>
                <a class="chip" href="{{.BoardURL}}">Browse requests</a>
                <a class="chip" href="{{.RoadmapURL}}">Roadmap</a>
              </div>
            </div>
            <div class="hero-stats">
              <article class="stat">
                <p class="stat-label">Published posts</p>
                <strong class="stat-value">{{.PostCount}}</strong>
                <p class="stat-note">Entries currently visible in the public archive.</p>
              </article>
              <article class="stat">
                <p class="stat-label">Linked requests</p>
                <strong class="stat-value">{{.RequestCount}}</strong>
                <p class="stat-note">Only approved shipped requests are rendered here.</p>
              </article>
              <article class="stat">
                <p class="stat-label">Latest note</p>
                <strong class="stat-value">{{if .HasItems}}{{.LatestTitle}}{{else}}Waiting for first release{{end}}</strong>
                <p class="stat-note">{{if .HasItems}}{{if .LatestLabel}}{{.LatestLabel}} · {{end}}{{.LatestSnippet}}{{else}}Publish from Console and the page plus feeds update together.{{end}}</p>
              </article>
            </div>
          </div>
        </section>

        <section class="archive" aria-label="Changelog archive">
          <div class="archive-head">
            <div>
              <p class="eyebrow">Archive</p>
              <h2>{{if .HasItems}}Recent releases{{else}}Ready for the first release note{{end}}</h2>
            </div>
            <p class="archive-meta">{{.PostCount}} posts · {{.RequestCount}} linked requests</p>
          </div>

          {{if .Items}}
          <div class="timeline">
            {{range .Items}}
            <article class="post" id="{{.Anchor}}">
              <div class="post-rail" aria-hidden="true">
                <span class="post-dot"></span>
                <span class="post-line"></span>
              </div>
              <div class="post-main">
                <div class="post-top">
                  <div>
                    <p class="post-kicker">Release note</p>
                    <h3>{{.Title}}</h3>
                  </div>
                  <div class="post-meta">
                    {{if .PublishedLabel}}<time class="post-time" datetime="{{.PublishedDateTime}}" title="{{.PublishedTitle}}">{{.PublishedLabel}}</time>{{end}}
                  </div>
                </div>
                <p class="body">{{.Body}}</p>
                {{if .Requests}}
                <section class="requests">
                  <div class="requests-head">Linked requests</div>
                  <div class="request-grid">
                    {{range .Requests}}
                    <a class="request-card" href="{{.URL}}">
                      <strong class="request-title">{{.PublicTitle}}</strong>
                      {{if .PublicSummary}}<div class="request-summary">{{.PublicSummary}}</div>{{end}}
                      <div class="request-meta">
                        {{if .PublicState}}<span>{{.PublicState}}</span>{{end}}
                        {{if .RoadmapColumn}}<span>{{.RoadmapColumn}}</span>{{end}}
                      </div>
                    </a>
                    {{end}}
                  </div>
                </section>
                {{end}}
              </div>
            </article>
            {{end}}
          </div>
          {{else}}
          <div class="empty-grid">
            <section class="empty-card empty-card--hero">
              <div>
                <p class="post-kicker">No release notes yet</p>
                <h3>When the first shipped update lands, the changelog becomes the public source of truth.</h3>
                <p>Publish a changelog post from Console and it will appear here with linked requests, RSS, and JSON feed entries.</p>
              </div>
              <div class="feed-links">
                <a href="{{.FeedRSSURL}}">RSS feed</a>
                <a href="{{.FeedJSONURL}}">JSON feed</a>
                <a href="{{.BoardURL}}">Browse requests</a>
              </div>
            </section>
            <section class="empty-card">
              <p class="panel-kicker">What appears here</p>
              <div class="empty-rules">
                <div class="rule-item">
                  <span class="rule-mark"></span>
                  <div>
                    <strong>Shipped work only</strong>
                    <span>Posts are published from requests that already reached shipped status.</span>
                  </div>
                </div>
                <div class="rule-item">
                  <span class="rule-mark"></span>
                  <div>
                    <strong>Public-safe fields</strong>
                    <span>The page renders public title, summary, state, and roadmap column only.</span>
                  </div>
                </div>
                <div class="rule-item">
                  <span class="rule-mark"></span>
                  <div>
                    <strong>Shared projection</strong>
                    <span>The HTML page and feed use the same source data, so they stay in sync.</span>
                  </div>
                </div>
              </div>
            </section>
          </div>
          {{end}}
        </section>

        <footer class="page-footer">
          <p class="hint">{{.FeedHint}}</p>
          {{if .NextURL}}<a class="next" href="{{.NextURL}}">Older posts</a>{{end}}
        </footer>
      </div>

      <aside class="rail">
        <section class="panel">
          <p class="panel-kicker">Subscribe</p>
          <h4>Follow the archive</h4>
          <p>Use the public feed endpoints to keep release notes in sync with your reader or automation.</p>
          <div class="feed-links">
            <a href="{{.FeedRSSURL}}">RSS feed</a>
            <a href="{{.FeedJSONURL}}">JSON feed</a>
          </div>
        </section>

        <section class="panel">
          <p class="panel-kicker">Public rules</p>
          <h4>Only public-safe fields</h4>
          <p>The changelog stays aligned with the portal policy and never exposes internal request details.</p>
          <div class="empty-rules">
            <div class="rule-item">
              <span class="rule-mark"></span>
              <div>
                <strong>Approved and shipped</strong>
                <span>Only requests that passed portal moderation and reached shipped status are eligible.</span>
              </div>
            </div>
            <div class="rule-item">
              <span class="rule-mark"></span>
              <div>
                <strong>Public request summary</strong>
                <span>The linked cards show the public title, summary, state, and roadmap column only.</span>
              </div>
            </div>
          </div>
        </section>

        <section class="panel">
          <p class="panel-kicker">Navigate</p>
          <h4>Open related surfaces</h4>
          <p>Move back to the public request board or roadmap without losing the current context.</p>
          <div class="link-stack">
            <a href="{{.BoardURL}}">Browse requests</a>
            <a href="{{.RoadmapURL}}">Roadmap</a>
          </div>
        </section>
      </aside>
    </div>
  </main>
</body>
</html>`))

type portalChangelogPageData struct {
	TenantName    string
	ChangelogURL  string
	FeedRSSURL    string
	FeedJSONURL   string
	BoardURL      string
	RoadmapURL    string
	FeedHint      string
	NoIndex       bool
	NextURL       string
	Items         []portalChangelogPostView
	PostCount     int
	RequestCount  int
	HasItems      bool
	LatestTitle   string
	LatestLabel   string
	LatestSnippet string
}

type portalChangelogPostView struct {
	Anchor            string
	Title             string
	Body              string
	PublishedLabel    string
	PublishedDateTime string
	PublishedTitle    string
	Requests          []portalChangelogRequestView
}

type portalChangelogRequestView struct {
	URL           string
	PublicTitle   string
	PublicSummary string
	PublicState   string
	RoadmapColumn string
}

type portalChangelogFeedRequest struct {
	URL           string `json:"url"`
	PublicTitle   string `json:"public_title"`
	PublicSummary string `json:"public_summary,omitempty"`
	PublicState   string `json:"public_state,omitempty"`
	RoadmapColumn string `json:"roadmap_column,omitempty"`
}

type portalChangelogFeedItem struct {
	ID            string                       `json:"id"`
	URL           string                       `json:"url"`
	Title         string                       `json:"title"`
	ContentText   string                       `json:"content_text"`
	DatePublished *string                      `json:"date_published,omitempty"`
	Tags          []string                     `json:"tags,omitempty"`
	Requests      []portalChangelogFeedRequest `json:"requests,omitempty"`
}

type portalChangelogJSONFeed struct {
	Version     string                    `json:"version"`
	Title       string                    `json:"title"`
	HomePageURL string                    `json:"home_page_url"`
	FeedURL     string                    `json:"feed_url"`
	Items       []portalChangelogFeedItem `json:"items"`
	NextURL     string                    `json:"next_url,omitempty"`
	NoIndex     bool                      `json:"no_index,omitempty"`
}

type portalChangelogRSSFeed struct {
	XMLName xml.Name                  `xml:"rss"`
	Version string                    `xml:"version,attr"`
	Channel portalChangelogRSSChannel `xml:"channel"`
}

type portalChangelogRSSChannel struct {
	Title       string                   `xml:"title"`
	Link        string                   `xml:"link"`
	Description string                   `xml:"description"`
	Items       []portalChangelogRSSItem `xml:"item"`
}

type portalChangelogRSSItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     *string  `xml:"pubDate,omitempty"`
	Description string   `xml:"description"`
	Category    []string `xml:"category,omitempty"`
}

func (h *Handler) ChangelogPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", publicRequestCacheControl)
	if h.submission == nil || h.notifications == nil {
		http.Error(w, "portal not configured", http.StatusNotImplemented)
		return
	}
	cfg, err := h.submission.GetSubmissionConfig(ctx, strings.TrimSpace(chi.URLParam(r, "tenant_slug")))
	if err != nil {
		portalChangelogLoadError(w, r, err)
		return
	}
	if !cfg.ChangelogEnabled {
		w.Header().Set("X-Robots-Tag", "noindex")
		http.NotFound(w, r)
		return
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	result, err := h.notifications.ListChangelog(ctx, cfg.TenantID, portalChangelogPageSize, cursor)
	if err != nil {
		portalChangelogLoadError(w, r, err)
		return
	}
	if result.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex")
	} else {
		w.Header().Del("X-Robots-Tag")
	}
	page := portalChangelogPageDataFromResult(cfg, result, cursor)
	buf := ptrext.Of(bytes.Buffer{})
	if err := portalChangelogTemplate.Execute(buf, page); err != nil {
		http.Error(w, "portal render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) ChangelogFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", publicRequestCacheControl)
	if h.submission == nil || h.notifications == nil {
		http.Error(w, "portal not configured", http.StatusNotImplemented)
		return
	}
	cfg, err := h.submission.GetSubmissionConfig(ctx, strings.TrimSpace(chi.URLParam(r, "tenant_slug")))
	if err != nil {
		portalChangelogLoadError(w, r, err)
		return
	}
	if !cfg.ChangelogEnabled {
		w.Header().Set("X-Robots-Tag", "noindex")
		http.NotFound(w, r)
		return
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	result, err := h.notifications.ListChangelog(ctx, cfg.TenantID, portalChangelogFeedSize, cursor)
	if err != nil {
		portalChangelogLoadError(w, r, err)
		return
	}
	if result.NoIndex {
		w.Header().Set("X-Robots-Tag", "noindex")
	} else {
		w.Header().Del("X-Robots-Tag")
	}
	page := portalChangelogPageDataFromResult(cfg, result, cursor)
	format := changelogFeedFormat(r)
	switch format {
	case "json":
		out := portalChangelogJSONFeedFromData(page)
		body, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			http.Error(w, "portal render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	default:
		out := portalChangelogRSSFromData(page)
		body, err := xml.MarshalIndent(out, "", "  ")
		if err != nil {
			http.Error(w, "portal render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append([]byte(xml.Header), body...))
	}
}

func portalChangelogPageDataFromResult(cfg portalsvc.SubmissionConfig, result rnrepo.ChangelogListResult, cursor string) portalChangelogPageData {
	rssURL := portalChangelogURL(cfg.TenantSlug, "rss", "")
	jsonURL := portalChangelogURL(cfg.TenantSlug, "json", "")
	data := portalChangelogPageData{
		TenantName:   cfg.TenantName,
		ChangelogURL: portalChangelogURL(cfg.TenantSlug, "", ""),
		FeedRSSURL:   rssURL,
		FeedJSONURL:  jsonURL,
		BoardURL:     "/portal/" + url.PathEscape(cfg.TenantSlug) + "/requests",
		RoadmapURL:   "/portal/" + url.PathEscape(cfg.TenantSlug) + "/roadmap",
		FeedHint:     "Subscribe with RSS or JSON to track shipped release notes.",
		NoIndex:      result.NoIndex,
	}
	if result.NextCursor != "" {
		data.NextURL = portalChangelogURL(cfg.TenantSlug, "", result.NextCursor)
	}
	data.PostCount = len(result.Items)
	for _, post := range result.Items {
		requestCount := len(post.Requests)
		data.RequestCount += requestCount
		view := portalChangelogPostView{
			Anchor:   portalChangelogAnchor(post),
			Title:    post.Title,
			Body:     post.Body,
			Requests: make([]portalChangelogRequestView, 0, requestCount),
		}
		if !result.HidePublicTimestamps && !post.PublishedAt.IsZero() {
			view.PublishedLabel = "Published " + post.PublishedAt.UTC().Format("Jan 2")
			view.PublishedDateTime = post.PublishedAt.UTC().Format(time.RFC3339)
			view.PublishedTitle = "Published " + post.PublishedAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		for _, req := range post.Requests {
			view.Requests = append(view.Requests, portalChangelogRequestView{
				URL:           "/portal/" + url.PathEscape(cfg.TenantSlug) + "/requests/" + url.PathEscape(req.PublicSlug),
				PublicTitle:   req.PublicTitle,
				PublicSummary: req.PublicSummary,
				PublicState:   req.PublicState,
				RoadmapColumn: req.RoadmapColumn,
			})
		}
		data.Items = append(data.Items, view)
	}
	data.HasItems = len(data.Items) > 0
	if data.HasItems {
		data.LatestTitle = data.Items[0].Title
		data.LatestLabel = data.Items[0].PublishedLabel
		data.LatestSnippet = portalChangelogSnippet(data.Items[0].Body, 96)
	}
	return data
}

func portalChangelogJSONFeedFromData(data portalChangelogPageData) portalChangelogJSONFeed {
	out := portalChangelogJSONFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       data.TenantName + " Changelog",
		HomePageURL: data.ChangelogURL,
		FeedURL:     data.FeedJSONURL,
		NextURL:     data.NextURL,
		NoIndex:     data.NoIndex,
		Items:       make([]portalChangelogFeedItem, 0, len(data.Items)),
	}
	for _, post := range data.Items {
		itemURL := data.ChangelogURL + "#" + post.Anchor
		item := portalChangelogFeedItem{
			ID:          itemURL,
			URL:         itemURL,
			Title:       post.Title,
			ContentText: portalChangelogFeedContentText(post),
			Requests:    make([]portalChangelogFeedRequest, 0, len(post.Requests)),
		}
		if post.PublishedDateTime != "" {
			item.DatePublished = ptrext.Of(post.PublishedDateTime)
		}
		if len(post.Requests) > 0 {
			item.Tags = make([]string, 0, len(post.Requests))
		}
		for _, req := range post.Requests {
			item.Requests = append(item.Requests, portalChangelogFeedRequest(req))
			item.Tags = append(item.Tags, req.PublicTitle)
		}
		out.Items = append(out.Items, item)
	}
	return out
}

func portalChangelogRSSFromData(data portalChangelogPageData) portalChangelogRSSFeed {
	items := make([]portalChangelogRSSItem, 0, len(data.Items))
	for _, post := range data.Items {
		itemURL := data.ChangelogURL + "#" + post.Anchor
		item := portalChangelogRSSItem{
			Title:       post.Title,
			Link:        itemURL,
			GUID:        itemURL,
			Description: portalChangelogFeedContentText(post),
			Category:    []string{},
		}
		if post.PublishedDateTime != "" {
			item.PubDate = ptrext.Of(post.PublishedDateTime)
		}
		for _, req := range post.Requests {
			item.Category = append(item.Category, req.PublicTitle)
		}
		items = append(items, item)
	}
	return portalChangelogRSSFeed{
		Version: "2.0",
		Channel: portalChangelogRSSChannel{
			Title:       data.TenantName + " Changelog",
			Link:        data.ChangelogURL,
			Description: data.TenantName + " public changelog and release notes.",
			Items:       items,
		},
	}
}

func portalChangelogFeedContentText(post portalChangelogPostView) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(post.Body))
	if len(post.Requests) == 0 {
		return strings.TrimSpace(b.String())
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString("Related requests:\n")
	for _, req := range post.Requests {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(req.PublicTitle))
		if req.PublicState != "" || req.RoadmapColumn != "" {
			b.WriteString(" (")
			parts := make([]string, 0, 2)
			if req.PublicState != "" {
				parts = append(parts, req.PublicState)
			}
			if req.RoadmapColumn != "" {
				parts = append(parts, req.RoadmapColumn)
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func portalChangelogLoadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, portalsvc.ErrNotFound), errors.Is(err, rnrepo.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, portalsvc.ErrValidation), errors.Is(err, rnrepo.ErrInvalidInput):
		http.Error(w, "invalid request", http.StatusBadRequest)
	default:
		http.Error(w, "portal unavailable", http.StatusInternalServerError)
	}
}

func portalChangelogSnippet(raw string, limit int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if cleaned == "" {
		return ""
	}
	if sentenceEnd := strings.IndexAny(cleaned, ".!?"); sentenceEnd >= 0 {
		sentence := strings.TrimSpace(cleaned[:sentenceEnd+1])
		if sentence != "" {
			runes := []rune(sentence)
			if len(runes) <= limit {
				return sentence
			}
			cleaned = sentence
		}
	}
	runes := []rune(cleaned)
	if len(runes) <= limit {
		return cleaned
	}
	if limit < 3 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func changelogFeedFormat(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "json", "feed", "jsonfeed":
		return "json"
	case "rss", "xml":
		return "rss"
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	switch {
	case strings.Contains(accept, "application/feed+json"), strings.Contains(accept, "application/json"):
		return "json"
	case strings.Contains(accept, "application/rss+xml"), strings.Contains(accept, "application/xml"), strings.Contains(accept, "text/xml"):
		return "rss"
	default:
		return "rss"
	}
}

func portalChangelogURL(tenantSlug string, format string, cursor string) string {
	base := "/portal/" + url.PathEscape(tenantSlug) + "/changelog"
	if strings.TrimSpace(format) == "rss" {
		base = base + "/feed?format=rss"
	} else if strings.TrimSpace(format) == "json" {
		base = base + "/feed?format=json"
	}
	if strings.TrimSpace(cursor) == "" {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "cursor=" + url.QueryEscape(strings.TrimSpace(cursor))
}

func portalChangelogAnchor(post rnrepo.ChangelogPost) string {
	slug := portalChangelogSlugify(post.Title)
	sum := sha256.Sum256([]byte(strings.TrimSpace(post.Title) + "\n" + strings.TrimSpace(post.Body)))
	return slug + "-" + hex.EncodeToString(sum[:4])
}

func portalChangelogSlugify(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "post"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "post"
	}
	return out
}
