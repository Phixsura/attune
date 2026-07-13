# Public Feedback Platform Gap Analysis and Roadmap

| | |
|---|---|
| **Started** | 2026-07-13 |
| **Status** | Reference |
| **Scope** | Public board discovery, moderation automation, notifications, access control, distribution, and analytics |
| **Related** | [#221](https://github.com/Phixsura/attune/issues/221), [Public Voting Board MVP](../proposals/2026/07/2026-07-13-public-voting-board-mvp.md), [Public Visibility and Moderation Policy](../proposals/2026/07/2026-07-10-public-visibility-moderation-policy.md), [End-User Feedback Submission Portal](../proposals/2026/07/2026-07-11-end-user-feedback-submission-portal.md) |

> Evidence strength: this note uses official vendor docs and the Attune repository state as capability evidence.
> It compares product patterns and operating models, not market share or benchmark numbers.

## 1. Current Attune baseline

Attune now has the core public-board loop in place:

- a tenant-branded submit-only portal;
- a public request board and request detail surface;
- vote and unvote actions backed by the canonical vote ledger;
- public comments with moderation-aware visibility;
- a signed, tenant-scoped visitor identity cookie for anonymous participation;
- tenant-scoped public portal rate limiting;
- server-rendered search, cursor pagination, top/recent sorting, state/roadmap filters, and quick filters for viewer-voted and comment-rich requests on the public board plus the public request and roadmap list endpoints;
- the public board list and roadmap list intentionally answer different questions: `/portal/{tenant}/requests` uses requests included in the portal board, while `/portal/{tenant}/roadmap` uses requests explicitly included in the public roadmap, so the same `roadmap=Now` label can surface different audiences on each surface;
- similar-request suggestions on public request detail pages so visitors can spot likely duplicates before posting again;
- a Console preview that deep-links to both the live board and the submission page;
- a public-safe request projection that hides internal fields;
- roadmap bridging from request state into the public board;
- Console moderation saved views and surface filters for snapping between common triage states;
- integration coverage for tenant isolation and vote idempotency.

That is a strong MVP. The remaining work is the product layer above it: discovery, engagement loops, moderation automation, audience control, and distribution.

## 2. What top-tier products converge on

The strongest feedback platforms converge on the same shape:

- public and submit-only portals are separate modes, not one blurred experience;
- visitors can search, filter, and sort requests instead of scanning a flat list;
- duplicate and similar requests are surfaced early so operators can merge them;
- comments and status changes trigger notifications so voters know the loop closed;
- public boards can be public, private, or custom-access with SSO or domain/segment rules;
- custom domains and embeddable widgets make the board reachable inside the product;
- roadmap and changelog updates keep the board from becoming a dead-end;
- analytics, exports, and segmentation help operators understand who is asking for what.

That pattern is visible across Aha!, Canny, Featurebase, Productboard, UserVoice, Pendo, Nolt, Sleekplan, Frill, FeedBear, and related products.

## 3. Gap matrix

| Capability | World-class pattern | Attune today | Priority |
|---|---|---|---|
| Search, filters, and sorting | Boards usually support search plus status/tag/board filters and multiple sort modes. | Attune now has search, cursor pagination, top/recent sorting, state/roadmap filters, quick filters for viewer-voted and comment-rich requests, and Console moderation saved views for common triage states; it still lacks tag filters and cross-user saved-view sharing. | P0 |
| Duplicate and similarity handling | Top tools detect likely duplicates and support merge or split workflows. | Attune now surfaces similar requests on public detail pages, but there is still no merge action or duplicate consolidation flow on the public board. | P0 |
| Follow and notifications | Voters and commenters get status-update emails, changelog notices, or follower notifications. | No follow/subscription model exists for public requests yet. | P0 |
| Access control and identity | Public, private, and custom-access boards are common; SSO, email domain, and segment rules are standard in mature products. | Attune has tenant-scoped visitor identity and policy-based visibility, but not custom-access boards, SSO, or segment-gated audiences for the public board. | P1 |
| Custom domain and embed surfaces | Mature products let teams host on a custom domain and embed the portal or widget in-product. | The board is reachable by link and previewed from Console, but it is not yet embeddable or custom-domained. | P1 |
| Public roadmap and closed-loop updates | Roadmap/changelog/release updates are integrated with the feedback object so users see progress. | The public board is connected to roadmap state, but there is no user-facing release/update publication loop yet. | P1 |
| Analytics and contributor insight | Products expose leaderboards, reports, segmentation, and export. | Internal prioritization exists, but the public board does not yet offer visitor-facing analytics or rich operator reporting on the board itself. | P2 |
| Multi-portal and localization | Teams often run multiple portals for different audiences and languages. | The current experience is tenant-centric, with no dedicated multi-portal or localized public-board strategy. | P2 |
| Automation and integrations | Slack, Jira, Intercom, HubSpot, Zendesk, and similar integrations are common. | No public-board activity webhooks or external feedback-loop integrations are available yet. | P2 |
| AI-assisted triage | Some products automatically detect spam, suggest merges, and classify ideas. | Moderation is policy-driven, but there is no AI triage or auto-merge layer on top of the board. | P2 |

## 4. Recommended roadmap

### P0: Make the board easy to use at scale

Goal: help users find the right request quickly and help operators collapse noisy duplicates.

Deliverables:

- filter by status, roadmap column, vote state, comment state, and tags;
- sort by top, recent, and trending;
- duplicate/similarity suggestions in the moderation queue and on public detail pages;
- merge workflow for obviously redundant requests;

Exit criteria:

- a user can locate a request without browsing the entire board;
- the board supports search and at least two meaningful sort modes;
- a moderator can turn two obvious duplicates into one canonical request without losing votes or comments;
- the board still feels public-safe and curated.

### P1: Close the loop

Goal: make public feedback feel acknowledged, not merely collected.

Deliverables:

- follow/subscribe on requests;
- email notifications for status updates and major comment activity;
- public release/update posts tied back to requests;
- configurable notification preferences;
- a visible "what happened next" trail on request detail pages.

Exit criteria:

- a voter learns when a request changes state;
- a customer can see that feedback was heard and not just stored;
- the board becomes a living communication surface.

### P1: Expand access control and distribution

Goal: let teams place the board where their users already are and control who can see it.

Deliverables:

- custom domain support;
- embeddable portal or widget mode;
- custom-access boards for segments or email domains;
- SSO-based access for authenticated customers;
- tenant-level choices for public vs restricted visibility.

Exit criteria:

- the board can live inside a product help center or app shell;
- a customer does not have to jump across brands or domains to participate;
- access policy is expressive enough for enterprise customers.

### P2: Add operator intelligence

Goal: help product and support teams understand signal, not just volume.

Deliverables:

- contributor analytics and leaderboards;
- board-level trends and request velocity reporting;
- export and API access for board activity;
- multi-portal and multi-language support;
- external integrations for Slack, Jira, Intercom, Zendesk, and HubSpot;
- AI-assisted spam/duplicate/similarity triage.

Exit criteria:

- operators can answer "what is changing, who is asking, and what should we do next?";
- the board scales beyond a single public page into a product feedback system.

## 5. What we should explicitly not do yet

- Do not build a support inbox or CRM inside the board.
- Do not fork the canonical request model into a second public-only system.
- Do not add nested comment threads, reactions, or attachments before retrieval and notification are solved.
- Do not start with a full roadmap editor if the board cannot yet search, notify, or de-duplicate well.
- Do not dilute the current moderation model by exposing internal notes or scoring signals.

## 6. Risks and tradeoffs

- Anonymous participation needs careful token handling, CSRF protection, and abuse controls.
- Search and filters can leak internal structure if the public projection is not tightly allowlisted.
- Duplicate merge is powerful but dangerous if it can silently erase provenance.
- Notifications can become spam if subscription defaults are too aggressive.
- Custom access adds a lot of policy surface, so it should be introduced only once the public board is stable.

## 7. References

- [Attune public voting board MVP](../proposals/2026/07/2026-07-13-public-voting-board-mvp.md)
- [Attune public visibility and moderation policy](../proposals/2026/07/2026-07-10-public-visibility-moderation-policy.md)
- [Attune end-user feedback submission portal](../proposals/2026/07/2026-07-11-end-user-feedback-submission-portal.md)
- [Aha! public ideas portal](https://support.aha.io/aha-roadmaps/support-articles/ideas/public-ideas-portal~7444636331870503394)
- [Aha! submit-only ideas portal](https://support.aha.io/aha-roadmaps/support-articles/ideas/submit-only-ideas-portal~7444636482802978917)
- [Canny public boards](https://help.canny.io/en/articles/3832293-public-boards)
- [Canny board settings](https://help.canny.io/en/articles/4968514-board-settings)
- [Canny widget](https://help.canny.io/en/articles/1058407-the-canny-widget)
- [Canny changelog](https://help.canny.io/en/articles/3006399-changelog)
- [Featurebase collect and manage feedback](https://help.featurebase.app/articles/6728409-collect-and-manage-feedback)
- [Featurebase moderation](https://help.featurebase.app/articles/6982593-post-and-comment-moderation)
- [Featurebase segmentation](https://help.featurebase.app/articles/9188570-how-to-segment-your-users)
- [Featurebase changelog emails](https://help.featurebase.app/articles/7999387-changelog-notification-emails)
- [Productboard portals](https://support.productboard.com/hc/en-us/articles/360056315454-Getting-started-with-portals)
- [Productboard custom domain](https://support.productboard.com/hc/en-us/articles/360058173433-Customize-your-Portal-s-domain)
- [Productboard portal card updates](https://support.productboard.com/hc/en-us/articles/360058173353-Close-the-feedback-loop-with-Portal-card-updates)
- [UserVoice forum settings](https://help.uservoice.com/hc/en-us/articles/360035473053-Setting-up-a-Forum)
- [UserVoice moderation](https://help.uservoice.com/hc/en-us/articles/360035481633-Moderate-Ideas-and-Comments)
- [UserVoice statuses](https://help.uservoice.com/hc/en-us/articles/360034982174-Customize-Public-and-Internal-Status)
- [Pendo moderate requests](https://support.pendo.io/hc/en-us/articles/360032949332-Moderate-requests)
- [Pendo similar requests](https://support.pendo.io/hc/en-us/articles/360032949632-Deal-with-similar-requests)
- [Nolt help center](https://nolt.io/help)
- [Nolt widget](https://nolt.io/help/widget)
- [Nolt SCIM](https://nolt.io/help/scim)
- [Sleekplan feedback and roadmap](https://sleekplan.com/feedback)
- [Frill homepage](https://frill.co/)
- [FeedBear feedback board](https://feedback.feedbear.com/)
