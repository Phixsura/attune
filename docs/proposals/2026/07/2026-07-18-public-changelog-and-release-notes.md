<!-- markdownlint-disable MD013 -->

# Public Changelog and Release Notes

| Field | Value |
|---|---|
| Issue | [#223](https://github.com/Phixsura/attune/issues/223) |
| Status | Implemented |
| Started | 2026-07-18T13:30:42+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#215](https://github.com/Phixsura/attune/issues/215), [#222](https://github.com/Phixsura/attune/issues/222), [#224](https://github.com/Phixsura/attune/issues/224), [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md), [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md), [Close the Loop Request Notifications](./2026-07-16-close-the-loop-request-notifications.md) |

## Problem

Attune already has the building blocks needed to explain shipped work publicly:

- public request projections and a public portal;
- a roadmap surface derived from workflow state;
- request-notification infrastructure that can publish public updates and fan
  out subscriber notifications.

What it does not yet have is a dedicated public changelog experience that turns
shipped requests into release notes without introducing a second CMS, leaking
private request data, or duplicating the existing public-update pipeline.

Issue #223 asks for a public changelog and release-notes flow that:

- lets operators publish a changelog post linked to shipped requests;
- renders only public-safe request data;
- allows generated drafts to be edited before publish;
- exposes the result as both HTML and feed output;
- stays compatible with future close-the-loop notification behavior.

The change should fit the existing product architecture rather than create a
parallel one-off system.

## Goals

- Publish a public changelog page when changelog visibility is enabled.
- Expose the same content as RSS and JSON Feed.
- Let Console operators create and edit changelog drafts from shipped requests.
- Reuse the existing public-update tables and notification pipeline.
- Keep the public surface limited to public-safe request summaries only.
- Preserve the existing request board and roadmap surfaces.
- Keep the HTML and feed projections shared so they cannot drift.

## Non-goals

- Do not build a full CMS, editorial workflow, or marketing campaign manager.
- Do not add custom-domain routing, embeds, SSO, or localization here.
- Do not expose internal notes, scores, CRM data, audit history, or raw
  moderation records.
- Do not introduce a second changelog schema if the existing public-update
  tables already fit the feature.
- Do not create a separate notification system just for changelog posts.

## Proposal

### 1. Treat changelog posts as a first-class public update kind

Model a changelog entry as a `changelog_post` public update. The publish path
should keep using the existing public-update transaction so one operator action
creates the whole record set:

- a public update thread with `surface=changelog_post`;
- a published public update post with `kind=changelog_post`;
- a primary link to the shipped request that anchors the release note;
- a notification event of type `changelog.post_published`.

This keeps the changelog in the same lifecycle as other public updates while
still allowing the changelog to have its own public route and rendering rules.

The draft body should remain plain text from the service perspective so the
public page can render it safely as escaped text. The title/body can be
auto-generated from the shipped request's public title and summary, then edited
before publish.

### 2. Add a dedicated public changelog surface

Expose the changelog under the public portal family:

- `GET /portal/{tenant_slug}/changelog` for HTML;
- `GET /portal/{tenant_slug}/changelog/feed?format=rss` for RSS;
- `GET /portal/{tenant_slug}/changelog/feed?format=json` for JSON Feed.

The feed endpoint should also honor `Accept` header negotiation so clients can
ask for RSS or JSON without changing URLs. The HTML page and feeds should be
driven by the same projection code so the same post ordering, linked requests,
and visibility rules are applied everywhere.

Visibility should be policy-driven:

- if `changelog_enabled` is false, return 404;
- if public indexing is disabled, keep `X-Robots-Tag: noindex`;
- keep `Cache-Control: no-store` on the public pages and feeds.

Pagination can stay lightweight for now with an offset-style cursor because the
expected history is small and the route already serves read-only public content.
The page should show older-post navigation, while the feeds should expose the
same cursor for clients that want to page through history.

### 3. Render only public-safe request context

The public changelog must not show raw internal request data. The public
projection should include only the allowlisted request fields already used in
the portal:

- public slug;
- public title;
- public summary;
- public state;
- roadmap column when present.

Linked requests should be filtered through the same public visibility rules as
the rest of the portal. A request is eligible only when it is:

- approved by the public moderation flow;
- included in the public portal;
- shipped;
- not archived;
- not merged into another request.

If a linked request no longer qualifies for the public portal, it should be
excluded from the public changelog view rather than exposed partially.

### 4. Keep the Console publishing workflow in one place

The request-notifications composer should gain a `changelog_post` kind rather
than a separate editor. That lets operators:

- choose a shipped request;
- preview the generated changelog draft;
- edit the title and body before publish;
- publish from the same workflow they already use for public updates.

When the selected kind is `changelog_post`, the UI can auto-fill the title and
body from the shipped request's public projection if the operator leaves those
fields blank. The generated draft should be clearly editable so the operator can
tighten release-note copy before sending it live.

The Console should also link directly to the live changelog page and both feed
variants so operators can verify the public output end to end.

### 5. Preserve subscriber compatibility

Publishing a changelog post should still emit the existing request-notification
event machinery. That means later subscriber-facing follow-up can reuse the
same event stream and recipient logic without another migration.

The changelog feature should be additive to the notification system, not a
forked path that duplicates audience selection, delivery tracking, or public
update persistence.

## Alternatives considered

- Build a separate changelog table and feed model.
  - Rejected because the repository already has public-update tables that can
    express the feature without a second schema.
- Add only a feed and skip the HTML page.
  - Rejected because the issue asks for a public changelog surface, not just an
    export format.
- Add the changelog editor as a brand-new Console workflow.
  - Rejected because the existing request-notifications composer already owns
    the publish path and can be extended without adding another surface.
- Put changelog rendering inside `publicvisibility` instead of
  `requestnotification`.
  - Rejected because the changelog content and publish lifecycle already live
    in the request-notification domain model.
- Add Atom in addition to RSS and JSON Feed.
  - Rejected because RSS + JSON Feed cover the intended consumption patterns
    without maintaining a third serializer.

## Risks / tradeoffs

- Reusing the publish tables means the implementation must stay disciplined
  about which fields are rendered publicly.
- Offset-style pagination is simple, but a very large changelog archive may
  eventually want keyset pagination.
- Feed compatibility can drift if the HTML page and feed do not share the same
  projection code.
- If changelog drafts are too loosely coupled to shipped requests, operators
  may publish copy that does not match the linked release.
- When a linked request is later removed from the public portal, the historical
  changelog must remain readable without exposing the removed request.

## Implementation plan

1. Extend request-notification publish logic to support `changelog_post` and
   generate defaults from shipped requests.
2. Add a public changelog query over the existing `public_update_*` tables and
   public request projection.
3. Add `/portal/{tenant_slug}/changelog` plus RSS and JSON feed routes.
4. Extend the Console request-notifications composer with changelog draft and
   preview behavior.
5. Add navigation links and visibility gating where `changelog_enabled` is
   true.
6. Add tests for publish-kind mapping, draft generation, public filtering,
   feed/page rendering, and policy gating.

## Verification

- `go test ./internal/handlers/portal ./internal/service/requestnotification ./internal/repo/requestnotification`
- `go test ./cmd/attune ./internal/handlers/console/...`
- `pnpm vitest run src/features/request-notifications/components/request-notifications-page.test.tsx`
- `pnpm exec vite build`
- Manual browser checks for `/portal/{tenant_slug}/changelog` and
  `/portal/{tenant_slug}/changelog/feed?format=rss|json`

## References

- [Issue #223](https://github.com/Phixsura/attune/issues/223)
- [Issue #202](https://github.com/Phixsura/attune/issues/202)
- [Issue #215](https://github.com/Phixsura/attune/issues/215)
- [Issue #222](https://github.com/Phixsura/attune/issues/222)
- [Issue #224](https://github.com/Phixsura/attune/issues/224)
- [Public Visibility and Moderation Policy](./2026-07-10-public-visibility-moderation-policy.md)
- [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md)
- [Close the Loop Request Notifications](./2026-07-16-close-the-loop-request-notifications.md)
- `internal/repo/requestnotification/changelog.go`
- `internal/handlers/portal/changelog.go`
- `console/src/features/request-notifications/components/request-notifications-page.tsx`
