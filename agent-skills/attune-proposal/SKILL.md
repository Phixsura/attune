---
name: attune-proposal
description: Scaffold a CLAUDE.md §10 design proposal for an attune issue before writing code. Use at the start of any issue work — every issue gets one proposal, committed alongside the implementing PR, and it must reach Status "Accepted" before implementation begins. Surfaced as /attune:proposal.
---

# /attune:proposal — scaffold an attune design proposal

attune's contract (CLAUDE.md §10) is **one short design proposal per issue,
written before/with the implementation and committed in the same change**. The
proposal is where assumptions get surfaced and alternatives weighed *before*
code (§6). This skill scaffolds that document so it matches the house shape.

## When to use

- You are about to start work on an issue (`#N`). Do this **first**.
- The maintainer asked to "open a proposal" / "write up the design".

## Steps

1. **Pick the path and filename.** Proposals live at
   `docs/proposals/YYYY/MM/YYYY-MM-DD-<slug>.md`. The date prefix is when the
   proposal was *started*; the `YYYY/MM/` directories keep the tree browsable.
   `<slug>` is a short kebab-case topic (e.g. `notify-dead-queue`,
   `api-key-scopes`). Confirm the date with the environment, not from memory.

2. **Read one or two recent siblings first** under the same `docs/proposals/`
   month to match depth and tone — e.g.
   `docs/proposals/2026/06/2026-06-18-notify-dead-queue.md`. Match the
   established structure; do not invent a new layout.

3. **Write the header table** (exact rows, in this order):

   ```markdown
   # <Title — what the change does, not the issue number>

   | | |
   |---|---|
   | **Issue** | #N |
   | **Status** | Proposed |
   | **Started** | YYYY-MM-DD |
   | **Related** | #X (one-line why it relates), #Y (…) |
   ```

   `Status` starts at `Proposed`. `Related` lists adjacent issues/PRs with a
   short reason each (omit the row only if there genuinely are none).

4. **Fill the ADR/RFC-lite sections, in this order** (CLAUDE.md §10):

   - **Problem** — what's wrong today, grounded in the **actual codebase**
     (cite real files/paths/line refs, like the siblings do — not hand-waving).
   - **Goals / Non-goals** — what's in scope and, explicitly, what is not.
   - **Proposal** — the concrete design: schema/proto/repo/service/handler/
     console changes, data flow, error codes. Respect the §5 layering rules.
   - **Alternatives considered** — options weighed and why each was rejected.
   - **Risks / tradeoffs** — breaking changes (call out per §3 SemVer),
     concurrency, migration/backfill, failure modes.
   - **Implementation plan** — ordered steps / phases mapped to locations.
   - **Verification** — how each claim is checked (unit/integration tests,
     `make proto` no-drift, `make ci-check`, a real-LLM run where relevant).
   - **References** — the issue, house patterns mirrored, and any
     benchmarking sources.

5. **Meet the depth bar (MEMORY).** A proposal that will be accepted shows:
   clear value, **code-verified** assumptions (you actually read the files you
   cite), benchmarking against comparable top projects where it informs the
   design, and a clean abstraction (§6.2 — abstract before the 3rd copy).

6. **Get it Accepted before any code.** Flip `Status: Proposed → Accepted` only
   once the maintainer agrees on scope. **Do not write implementation code while
   it is still `Proposed`.** Sync the resolved decisions back to the issue.

7. **Commit it with the implementation.** The implementing PR uses `Closes #N`
   and includes this proposal doc in the same change. Move
   `Status: Accepted → Implemented` as the work lands.

## Output

State the chosen path, then write the file. Then summarize the open questions /
assumptions the maintainer needs to confirm before the `Proposed → Accepted`
flip.
