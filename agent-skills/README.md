# `agent-skills/` — attune agent skills

Version-controlled, tool-neutral **Agent Skills** that capture attune's
recurring engineering workflows so any AI assistant follows the same house
rules. Keeping them in-repo means a workflow is reviewed, versioned, and shared
the same way as code — not re-typed into each tool's config.

## Format — Anthropic Agent-Skills (`SKILL.md`)

Each skill is a folder containing a `SKILL.md` with YAML frontmatter and a
Markdown body:

```markdown
---
name: attune-preflight
description: One line — what it does and when to use it. (Always loaded.)
---

# Body: the actual steps. (Loaded on demand.)
```

This follows **progressive disclosure**: the frontmatter `name` + `description`
are short and always available so a tool can decide *whether* the skill is
relevant; the body carries the real procedure and is read only when the skill
fires. The format is the same one used by the design/output skills already
vendored in this directory — it is not attune-specific, which is the point: any
[`AGENTS.md`](https://agents.md/)-aware tool can consume these.

## The `/attune:*` namespace (n8n pattern)

attune's own skills are namespaced under `attune-*` / `/attune:*` so they don't
collide with general-purpose skills:

| Folder | Command | Purpose |
|---|---|---|
| `attune-proposal/` | `/attune:proposal` | Scaffold a CLAUDE.md §10 design proposal for an issue (Proposed → Accepted before code). |
| `attune-create-pr/` | `/attune:create-pr` | Open a PR with a Conventional-Commit title, the `[Unreleased]` changelog entry, `Closes #N`, the linked proposal, and the Co-Authored-By trailer. |
| `attune-preflight/` | `/attune:preflight` | Run `make ci-check` (the local CI gate) and read the failures; never claim green without citing the output. |

These three encode attune's **proposal → code → preflight → PR → changelog**
loop. They cite the real contract in [`CLAUDE.md`](../../CLAUDE.md) (§1 quality
gates, §2 changelog, §3 SemVer, §4 Conventional Commits, §10 proposals) and the
real `make` targets — no invented commands.

## How a tool discovers and loads them

- **Claude Code / `AGENTS.md`-aware tools.** Point the tool at this directory.
  A common pattern is to symlink the folders into the tool's own skills path
  (e.g. `ln -s ../../agent-skills/attune-proposal .claude/skills/attune-proposal`)
  or to reference `agent-skills/` directly if the tool scans it. The tool
  reads each `SKILL.md` frontmatter to index the skills, then loads a body when
  its command (`/attune:proposal`, …) is invoked or its description matches the
  task.
- **Any other agent.** Because each skill is a self-contained Markdown file with
  a documented `name`/`description`, an agent can read it directly as context —
  no runtime or plugin required.

The skills are documentation-as-contract: they don't run anything themselves;
they tell the agent which real attune commands to run and which rules to honor.
