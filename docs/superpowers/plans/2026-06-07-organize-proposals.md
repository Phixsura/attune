# Organize Proposals by Year/Month — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize `docs/proposals/` from a flat directory into `YYYY/MM/` subdirectories so the folder stays browsable as proposal count grows.

**Architecture:** Pure filesystem move (`git mv`) + reference fixups. No code changes, no tests. Verification via `git log --follow` and grep for stale paths.

**Tech Stack:** git, bash, grep

---

### Task 1: Create branch

**Files:** None (git operation only)

- [ ] **Step 1: Create the feature branch from main**

```bash
git checkout -b docs/organize-proposals-by-year-month main
```

Expected: `Switched to a new branch 'docs/organize-proposals-by-year-month'`

---

### Task 2: Move existing proposals into `2026/06/`

**Files:**
- Create directory: `docs/proposals/2026/06/`
- Move: all 12 `.md` files from `docs/proposals/` into `docs/proposals/2026/06/`

- [ ] **Step 1: Create the target directory**

```bash
mkdir -p docs/proposals/2026/06
```

- [ ] **Step 2: git mv all proposal files**

```bash
git mv docs/proposals/2026-06-04-standalone-dockerfile.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-docker-compose.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-lint-slog.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-llmclient-otelhttp-transport.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-observability-overlay.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-precommit-hook.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-release-workflow.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-05-rename-to-attune.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-06-feature-organization.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-06-inbound-adapter-framework.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-06-lint-slog-strict.md docs/proposals/2026/06/
git mv docs/proposals/2026-06-06-protobuf-idl-contract.md docs/proposals/2026/06/
```

- [ ] **Step 3: Verify history follows**

```bash
git log --follow --oneline -3 docs/proposals/2026/06/2026-06-04-standalone-dockerfile.md
```

Expected: shows commits from when the file was at the old path.

- [ ] **Step 4: Verify root is empty of proposals**

```bash
find docs/proposals -maxdepth 1 -name '*.md'
```

Expected: no output (no `.md` files at root level yet — README comes later).

---

### Task 3: Fix cross-references

**Files:**
- Modify: `CLAUDE.md` (2 locations: §5 line ~103, §10 line ~185)
- Modify: `console/README.md` (line ~39)
- Modify: `console/.dependency-cruiser.cjs` (line ~4)
- Modify: `docs/superpowers/plans/2026-06-05-observability-overlay.md` (multiple lines)
- Modify: `docs/proposals/2026/06/2026-06-06-feature-organization.md` (line ~391)

- [ ] **Step 1: Update CLAUDE.md §5 (package layering reference)**

Old:
```
docs/proposals/2026-06-06-feature-organization.md
```
New:
```
docs/proposals/2026/06/2026-06-06-feature-organization.md
```

- [ ] **Step 2: Update CLAUDE.md §10 (location/naming rule)**

Old:
```
- **Location / naming:** `docs/proposals/YYYY-MM-DD-<slug>.md` — the date prefix
  is when the proposal was *started*, so `ls docs/proposals` reads as a timeline.
```
New:
```
- **Location / naming:** `docs/proposals/YYYY/MM/YYYY-MM-DD-<slug>.md` — the date
  prefix is when the proposal was *started*; year/month directories keep the tree
  browsable as it grows.
```

- [ ] **Step 3: Update console/README.md**

Old:
```
docs/proposals/2026-06-06-feature-organization.md
```
New:
```
docs/proposals/2026/06/2026-06-06-feature-organization.md
```

- [ ] **Step 4: Update console/.dependency-cruiser.cjs**

Old:
```
// docs/proposals/2026-06-06-feature-organization.md §4-B for the full
```
New:
```
// docs/proposals/2026/06/2026-06-06-feature-organization.md §4-B for the full
```

- [ ] **Step 5: Update docs/superpowers/plans/2026-06-05-observability-overlay.md**

Replace all occurrences of:
```
docs/proposals/2026-06-05-observability-overlay.md
```
With:
```
docs/proposals/2026/06/2026-06-05-observability-overlay.md
```

- [ ] **Step 6: Update docs/proposals/2026/06/2026-06-06-feature-organization.md**

Old (line ~391):
```
docs/proposals/2026-06-06-inbound-adapter-framework.md
```
New:
```
docs/proposals/2026/06/2026-06-06-inbound-adapter-framework.md
```

- [ ] **Step 7: Verify no stale references remain**

```bash
grep -r "docs/proposals/2026-06-0" . --include="*.md" --include="*.cjs"
```

Expected: no output.

---

### Task 4: Write the proposal document for this issue

**Files:**
- Create: `docs/proposals/2026/06/2026-06-07-organize-proposals.md`

- [ ] **Step 1: Create the proposal**

The proposal should follow §10 format with header table (Issue: #72, Status: Implemented, Started: 2026-06-07) and standard sections. Content mirrors the issue description — this is a filesystem-organization chore, not a design decision requiring alternatives analysis. Keep it concise.

- [ ] **Step 2: Stage the proposal**

```bash
git add docs/proposals/2026/06/2026-06-07-organize-proposals.md
```

---

### Task 5: Commit and push

**Files:** All changes from Tasks 2–4

- [ ] **Step 1: Stage all changes**

```bash
git add -A docs/proposals/
git add CLAUDE.md console/README.md console/.dependency-cruiser.cjs
git add docs/superpowers/plans/2026-06-05-observability-overlay.md
git add docs/superpowers/plans/2026-06-07-organize-proposals.md
```

- [ ] **Step 2: Commit**

```bash
git commit -m "docs(proposals): organize into year/month directories (#72)"
```

- [ ] **Step 3: Push and create PR**

```bash
git push -u origin docs/organize-proposals-by-year-month
gh pr create --title "docs(proposals): organize into year/month directories" \
  --body "Closes #72. See docs/proposals/2026/06/2026-06-07-organize-proposals.md."
```

---

## Verification Checklist

After all tasks complete, confirm:

1. `git log --follow docs/proposals/2026/06/2026-06-04-standalone-dockerfile.md` — history preserved
2. `grep -r "docs/proposals/2026-06-0" . --include="*.md" --include="*.cjs"` — no stale refs
3. `find docs/proposals -maxdepth 1 -name '*.md'` — empty (no root-level proposals)
4. `ls docs/proposals/2026/06/` — all 13 files (12 moved + 1 new proposal)
