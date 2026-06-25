---
name: updating-hopbox-docs
description: Use when changing hopbox behavior — adding/removing a hopboxd or CLI flag, a workspace field, an SSH username grammar element, a provider, an API/RPC, or a user-visible default — before considering the change complete. Symptoms you are about to skip docs — "it's just a flag", "I'll doc it later", user didn't mention docs.
---

# Updating hopbox docs

## Core principle

hopbox ships a VitePress docs site under `docs/`. **It is part of the product.** A
behavior change that lands without its doc change is half-done — the same as
landing code with no test. The user not mentioning docs does not exempt the change.

## When this triggers

A change is **significant** (docs required) if a user could observe it:

| You changed… | Update |
|---|---|
| a `hopboxd` flag | `docs/reference/hopboxd.md` |
| a `hopbox` CLI command/flag | `docs/reference/cli.md` |
| SSH username grammar / front door | `docs/guide/ssh.md` |
| a workspace field, lifetime, persistence | `docs/guide/` (relevant page) + reference |
| auth / identity behavior | `docs/guide/auth.md` |
| deploy / ingress behavior | `docs/guide/deploy.md` |
| a new concept | `docs/guide/what-is-hopbox.md` (or a new guide page + sidebar) |

**Not significant** (skip docs): internal refactors, test-only changes, comments,
private helpers — nothing a user can see or invoke.

## Checklist (per significant change)

1. `grep -rn "<flag-or-term>" docs/` — find every page that already mentions the
   area. An existing mention that is now wrong is a doc bug; fix it.
2. Update the reference page (flags/commands) — these are exhaustive; a missing
   flag is immediately visible.
3. Update or add the guide prose explaining *why/how*, with a runnable example.
4. Add new pages to the VitePress sidebar (`docs/.vitepress/config.*`) or they are unreachable.
5. Build the docs if tooling exists (`npm run docs:build` in `docs/`) to catch dead links / bad frontmatter.

## Red flags — STOP, you are skipping docs

- "It's just one flag / field" → reference pages must be exhaustive. Add it.
- "I'll update docs in a follow-up" → the change is not complete until docs land.
- "The user only asked for the code" → docs are part of the change, not a favor.
- "Tests pass, so I'm done" → tests verify behavior; docs expose it. Both required.

Skipping docs because they weren't requested is the same error as skipping tests
because they weren't requested. Don't.
