# Attune vs Industry: 100 Product Gaps

> Generated 2026-06-29. Based on deep research across 26 sources (Canny,
> ProductBoard, Qualtrics, Medallia, Intercom, Zendesk, Fider, AppFollow,
> Survicate, Hotjar, etc.) with adversarial verification (18/25 claims survived).
> Cross-referenced against attune codebase at `bd1212d9`.

Importance: **C** = Critical (blocks adoption), **H** = High (competitive
disadvantage), **M** = Medium (nice-to-have for target segment), **L** = Low
(enterprise-only or niche).

---

## A · Ingest Channel Coverage (attune: webhook + email + API only)

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 1 | No Slack channel ingest | Canny Autopilot, ProductBoard, Intercom — auto-extract feedback from Slack messages | **C** |
| 2 | No Discord ingest | Canny, Fider community — Discord bot captures feature requests | **H** |
| 3 | No in-app feedback widget / JS SDK | Survicate, Hotjar, Pendo — embeddable widget with targeting rules (scroll depth, time, exit intent) | **C** |
| 4 | No mobile SDK (iOS/Android) | Survicate, Instabug, Pendo — native mobile feedback with screenshot attachment | **H** |
| 5 | No app store review ingest (Apple App Store / Google Play) | AppFollow, Appfigures, ProductBoard — native sync + sentiment per store | **H** |
| 6 | No G2/Capterra/TrustRadius review ingest | ProductBoard — auto-import from review sites every 5 min | **M** |
| 7 | No social media ingest (Twitter/X, Facebook, Reddit) | Medallia, Birdeye — 30+ social sites monitored | **M** |
| 8 | No voice/call recording analysis | Medallia Speech, Gong → ProductBoard, Grain → ProductBoard | **M** |
| 9 | No video feedback | Medallia Video (formerly LivingLens) — video sentiment + transcript | **L** |
| 10 | No NPS/CSAT/CES survey builder | Delighted, AskNicely, Survicate, Qualtrics — built-in survey distribution | **C** |
| 11 | No session replay context attachment | Hotjar, FullStory → ProductBoard — link feedback to session recordings | **M** |
| 12 | No support ticket auto-extraction | Zendesk, Intercom, Freshdesk — parse feedback signals from support conversations | **H** |
| 13 | No RSS/Atom feed ingest | Issue #165 exists but not implemented — RSS for blog comments, forums | **M** |
| 14 | No SMS/WhatsApp ingest | Podium, Birdeye — text message feedback collection | **L** |
| 15 | No IVR/phone survey ingest | Medallia, Qualtrics — post-call IVR captures | **L** |

## B · Integration Ecosystem (attune: GitHub Issues + raw webhook only)

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 16 | No Jira bidirectional sync | Canny, ProductBoard, Released, Savio — two-way status sync with Jira | **C** |
| 17 | No Linear integration | Canny — native Linear two-way sync | **H** |
| 18 | No ClickUp integration | Canny — native ClickUp sync | **M** |
| 19 | No Asana integration | Canny — native Asana sync | **M** |
| 20 | No Azure DevOps integration | Canny, ProductBoard — Azure DevOps push | **M** |
| 21 | No Slack outbound (notification + interactive) | Planned v0.6 but not shipped — Canny, ProductBoard, Intercom all have | **C** |
| 22 | No Discord outbound | Planned v0.6 but not shipped | **H** |
| 23 | No email outbound (notifications) | Planned v0.6 but not shipped | **C** |
| 24 | No Salesforce CRM integration | ProductBoard — MRR/ARR import, dynamic segmentation | **H** |
| 25 | No HubSpot CRM integration | Canny — native HubSpot customer data enrichment | **H** |
| 26 | No Zapier connector | Canny — Zapier for 5,000+ app connectivity | **C** |
| 27 | No Make (Integromat) connector | Canny — Make/Pabbly integration | **H** |
| 28 | No Segment integration | Canny, ProductBoard — user identity enrichment via Segment | **M** |
| 29 | No Amplitude/Mixpanel cohort sync | ProductBoard — product analytics cohort correlation | **H** |
| 30 | No Microsoft Teams integration | Enterprise table-stakes for internal feedback routing | **M** |
| 31 | No Zendesk bidirectional | Canny, ProductBoard — sync support tickets ↔ feedback items | **H** |
| 32 | No Intercom bidirectional | Canny, ProductBoard — conversation → feedback extraction | **H** |
| 33 | No Figma/Miro integration | ProductBoard — design collaboration context | **L** |
| 34 | GitHub Issues outbound is one-way only | Canny — bidirectional status sync with GitHub Issues | **H** |

## C · User-Facing Engagement (attune: operator-only console)

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 35 | No public voting portal | Fider, Canny, UserVoice — user-facing feature request board with upvotes | **C** |
| 36 | No public roadmap page | Canny, ProductBoard — shareable roadmap board (planned/in-progress/done) | **H** |
| 37 | No changelog / release notes page | Canny, Quackback, Featurebase — public changelog with voter notification | **H** |
| 38 | No end-user feedback submission portal | Fider, Astuto — users browse + submit ideas without API integration | **H** |
| 39 | No user account system (end-user identity) | Fider — user registration, profile, notification preferences | **M** |
| 40 | No embeddable feedback board (iframe) | Canny — embed board in your product | **M** |
| 41 | No "close the loop" voter notification | Canny, Featurebase — auto-notify voters when feature ships | **H** |
| 42 | No custom domain for public pages | Canny — custom domain for feedback portal | **M** |

## D · AI / Analytics Depth (attune: classify + cluster + draft reply)

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 43 | No predictive churn scoring | Qualtrics Predict iQ — neural-network churn prediction | **M** |
| 44 | No statistical modeling (regression/cluster analysis) | Qualtrics Stats iQ — linear/logistic/ridge regression | **L** |
| 45 | No anomaly/spike detection | SentiSum, Medallia Athena — real-time alert on feedback volume spikes | **H** |
| 46 | No theme/topic trend over time | Thematic, Chattermill — theme evolution visualization | **H** |
| 47 | No cross-theme correlation | Thematic — detect that "slow checkout" and "payment errors" co-occur | **M** |
| 48 | No root cause analysis | Medallia Athena, Qualtrics — automated RCA for negative trends | **M** |
| 49 | No generative AI summarization (across items) | Medallia Ask Athena, InMoment Active Listening — summarize 1000 items into insights | **H** |
| 50 | No LLM-powered Q&A over feedback corpus | Enterpret — "ask a question" over all feedback, get cited answer | **H** |
| 51 | No confidence scoring on enrichment results | attune classifies but doesn't report confidence — competitors show model certainty | **H** |
| 52 | No enrichment cost tracking per item | attune has LLM audit but no per-item cost attribution visible in Console | **M** |
| 53 | No A/B testing for prompts/models | Issue #160 exists — competitors offer model comparison | **H** |
| 54 | No re-enrichment campaign manager | Issue #163 exists — bulk re-classify after prompt changes | **M** |
| 55 | No classification quality drift detection | Issue #161 exists — detect when model outputs degrade | **H** |
| 56 | No custom taxonomy / user-defined categories | Thematic, Kapiche — user defines their own classification hierarchy | **H** |
| 57 | No auto-translation of feedback | Appfigures — automatic translation for cross-language review analysis | **M** |
| 58 | No intent detection | Medallia — predict what user will do next based on feedback patterns | **L** |
| 59 | No emotion detection (beyond sentiment) | Lexalytics — anger/frustration/joy granular emotion scoring | **M** |
| 60 | No competitive mention detection | Chattermill — detect when users mention competitor products | **M** |

## E · Prioritization & Product Management

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 61 | No revenue-weighted prioritization | ProductBoard — weight feature requests by customer MRR/ARR | **H** |
| 62 | No RICE/ICE/value-effort scoring framework | ProductBoard, Aha! — built-in prioritization frameworks | **H** |
| 63 | No customer segmentation (plan/tier/cohort) | ProductBoard, Canny — segment feedback by customer attributes | **H** |
| 64 | No feature request deduplication with merge | Canny — merge duplicate requests, combine voter counts | **H** |
| 65 | No impact scoring (who + how many affected) | ProductBoard User Impact Score — analytics-informed prioritization | **M** |
| 66 | No product area / initiative hierarchy | Aha! — map feedback to strategic initiatives and product areas | **M** |
| 67 | No OKR/goal alignment tracking | Aha! Ideas — link feedback themes to company objectives | **L** |
| 68 | No voting weight by customer segment | UserVoice — high-value customers' votes weigh more | **M** |

## F · Console UI / Operator Experience

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 69 | No saved views / custom filters | Issue #170 — Canny, Intercom — save and share filter combinations | **H** |
| 70 | No bulk actions on feedback list | Canny — multi-select → tag/archive/merge/assign | **H** |
| 71 | No keyboard shortcuts for triage | Issue #171 — Intercom — j/k navigation, shortcuts for tag/assign | **M** |
| 72 | No search with full-text + filters | Canny, Zendesk — combined text search + faceted filters | **H** |
| 73 | No dashboard / analytics home page | Canny, Intercom — at-a-glance metrics: volume, sentiment trend, top themes | **C** |
| 74 | No customizable dashboard widgets | Qualtrics — drag-and-drop dashboard builder | **M** |
| 75 | No dark mode | Table-stakes for developer-facing tools | **M** |
| 76 | No mobile-responsive console | Intercom — fully responsive admin on mobile | **M** |
| 77 | No real-time updates (WebSocket/SSE) | Intercom — live-updating conversation and feedback views | **H** |
| 78 | No command palette (Cmd+K) | Modern SaaS pattern — quick navigation | **M** |
| 79 | No activity timeline per feedback item | Intercom — full history of enrichment, assignment, replies | **H** |
| 80 | No inline reply composition and send | attune drafts replies but doesn't send them from Console | **H** |

## G · CSAT / Satisfaction Measurement

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 81 | No CSAT measurement framework | Intercom — Fin AI Agent CSAT with 5-point scale drill-down | **H** |
| 82 | No NPS collection + tracking | Delighted, AskNicely — NPS survey + trend over time | **H** |
| 83 | No CES (Customer Effort Score) | Survicate, Qualtrics — CES survey template | **M** |
| 84 | No AI output quality scoring | Intercom — evaluate quality of AI-generated responses | **H** |
| 85 | No closed-loop feedback verification | Intercom — verify whether drafted reply resolved the issue | **M** |

## H · Security / Compliance / Governance (beyond current)

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 86 | No SOC 2 Type II certification | Canny, ProductBoard — SOC 2 Type II certified | **H** |
| 87 | No data residency / region selection | Qualtrics, Medallia — choose data storage region | **M** |
| 88 | No IP allowlisting | Enterprise SaaS table-stakes — restrict API access by IP range | **M** |
| 89 | No retention policy automation | Issue #157 — automated data deletion after configurable period | **H** |
| 90 | No consent management for feedback collection | GDPR requires explicit consent for certain feedback channels | **M** |
| 91 | No SSO enforcement (require SSO, disable password) | Enterprise requirement — force all admins through OIDC | **M** |

## I · Deployment / Operations

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 92 | No horizontal scaling documentation | Medallia — multi-region, multi-datacenter deployment | **H** |
| 93 | No read replica support | Large-scale feedback requires read replicas for analytics | **M** |
| 94 | No backup/restore CLI commands | attune has no built-in backup management (relies on pg_dump) | **M** |
| 95 | No blue-green / canary deploy guide | Enterprise deployment requires zero-downtime upgrade path | **M** |
| 96 | No GitOps bootstrap | Issue #166 — declarative resource provisioning for IaC | **M** |
| 97 | No statement_timeout on connection pool | Issue #84 — runaway queries can lock the database | **H** |
| 98 | No connection pool tuning documentation | Operators need guidance on pgBouncer / PgPool configuration | **M** |

## J · SDK / Developer Experience

| # | Gap | Competitor benchmark | Imp |
|---|-----|---------------------|-----|
| 99 | No Python SDK | ProductBoard, Canny — Python client for data science workflows | **H** |
| 100 | No webhook receiver SDK (for outbound events) | Stripe — SDK helpers for verifying + parsing outbound webhook signatures | **M** |

---

## Summary by importance

| Level | Count | Description |
|-------|-------|-------------|
| **Critical** | 8 | Blocks competitive adoption: in-app widget, Slack, Jira, Zapier, voting portal, survey builder, email outbound, analytics dashboard |
| **High** | 46 | Clear competitive disadvantage: app store reviews, CRM sync, NPS/CSAT, theme trends, saved views, search, etc. |
| **Medium** | 36 | Nice-to-have for target segment |
| **Low** | 10 | Enterprise-only or niche |

## Top 8 Critical gaps (recommended attack order)

1. **#10 NPS/CSAT survey builder** — without this, attune can't proactively collect signal
2. **#35 Public voting portal** — Fider has this as open-source; attune's biggest OSS competitor gap
3. **#26 Zapier connector** — instant 5,000+ app connectivity, unblocks integration breadth
4. **#3 In-app feedback widget** — every competitor has embeddable collection
5. **#16 Jira bidirectional sync** — PM teams won't adopt without Jira integration
6. **#21/23 Slack + email outbound** — already planned for v0.6, ship it
7. **#73 Analytics dashboard** — Console needs an at-a-glance home page
8. **#1 Slack ingest** — most feedback lives in Slack channels

## Sources

- Canny: canny.io/integrations (primary, verified)
- ProductBoard: productboard.com/platform/integrations/ (primary, verified)
- Intercom: intercom.com/help/en/articles/8368157 (primary, verified 3-0)
- Appfigures: appfigures.com/products/app-review-monitoring (primary, verified 3-0)
- Fider: github.com/getfider/fider (primary, verified 3-0)
- Qualtrics/Medallia: enterpret.com/comparisons/qualtrics-vs-medallia (secondary, verified 3-0)
- SentiSum: sentisum.com/library/customer-feedback-management-systems (secondary, verified)
- 19 additional blog/forum sources cross-referenced
