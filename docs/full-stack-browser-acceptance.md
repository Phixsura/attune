# Full-Stack Browser Acceptance

Use this runbook for product workflows where ordinary scripts can pass while
the deployed operator experience is still broken. The browser path must be
visible and mouse-driven. Automation may collect evidence, but it must not
replace the operator actions.

## When To Use It

Run this acceptance tier for changes that touch:

- production image boot, embedded Console assets, or runtime configuration;
- authenticated Console workflows that write durable state;
- provider sync, webhooks, retries, cursors, comments, or dedupe;
- background workers whose results must appear in Console;
- migrations that are required by a visible workflow.

## Run Record Template

Copy this section into the PR test plan or proposal verification notes.

```md
### Full-Stack Browser Acceptance

- Issue / PR:
- Commit:
- Image:
- Base URL:
- Compose project:
- Evidence directory:
- Browser:
- Operator:
- Started:
- Finished:

#### Mouse-Driven Path

- [ ] Console opened from the deployed image.
- [ ] Operator logged in through the visible browser.
- [ ] Workflow entry point was reached with mouse clicks.
- [ ] Forms were completed through visible browser controls.
- [ ] State-changing buttons were clicked with the mouse.
- [ ] The browser was scrolled to each relevant before/after state.
- [ ] Final UI state matched the expected durable state.

#### Evidence

- [ ] Health/readiness/startup outputs captured.
- [ ] Console HTML/assets response captured.
- [ ] Screenshots captured for every key before/after state.
- [ ] Provider mock or sandbox request log captured.
- [ ] Postgres rows captured for durable links, runs, events, or comments.
- [ ] Service logs reviewed for the same time window.
- [ ] Unrelated warnings/errors explained.
- [ ] Teardown command documented, or stack intentionally left running.

#### Verdict

- [ ] Accepted.
- [ ] Rejected.

Notes:
```

## Evidence Collector

After the mouse-driven path is complete, collect a read-only evidence bundle:

```bash
scripts/collect-full-stack-evidence.sh \
  --project attune-example \
  --compose-file .cache/example/docker-compose.yml \
  --base-url http://127.0.0.1:55011 \
  --provider-service github-mock \
  --output-dir .cache/example/evidence
```

The collector reads health endpoints, Compose state, selected Postgres tables,
provider mock logs, and service logs. It does not click the browser and does
not create application records.

Inspect the bundle before sharing it outside the development machine. Provider
mocks should log whether authorization was present, not token values.

## GitHub Issue Sync Checklist

For managed GitHub Issue sync, the visible browser path must cover:

- log into Console from the deployed image;
- create or select a Customer Request;
- create or select a GitHub connection pointing at an HTTPS mock or sandbox;
- click Test Connection and verify a provider repository read with
  authorization present;
- save a Customer Request to Issue mapping with `push` or `bidirectional`
  direction;
- click the Customer Request detail action that creates a GitHub Issue;
- verify the provider saw `POST /repos/{owner}/{repo}/issues`;
- verify the provider saw the managed comment read/write calls when comment
  sync is enabled;
- verify the Customer Request detail page shows the external URL, external
  state, sync state, and last sync timestamp;
- verify External Sync shows a succeeded `push` run with one seen record, one
  changed record, zero failures, and zero conflicts;
- verify the selected run detail shows the expected `input_metadata` source and
  no failures or conflicts.

## Rejection Rules

Reject the acceptance evidence when:

- the workflow succeeds only through API calls, database writes, or hidden
  automation;
- the provider did not receive the expected requests;
- authorization or signature status is absent where required;
- the database state and Console state disagree;
- a run is `failed`, `dead`, or `partial` without an accepted product reason;
- logs show TLS, SSRF, migration, panic, worker, permission, or provider
  errors for the workflow under validation;
- screenshots omit the state that the operator is claiming to have verified.
