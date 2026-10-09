---
description: File a well-structured GitHub issue on the go-grip fork from a one-line description, using the house style established in this repo
---

# /issue

You are ORCHESTRATING the filing of one GitHub issue on the fork `Galvill/go-grip` from the short brief the user passed as arguments. All research, drafting, and filing happens in the designated `issue-filer` subagent (sonnet) — its agent definition carries the full house style. Do NOT read implicated source files, run the duplicate check, or draft the body in this session; your only in-session work is the dispatch and the final report.

## Arguments

Seed brief: `$ARGUMENTS`

Only stop to ask the user if the brief is genuinely ambiguous about *what subsystem* it targets (CLI flags, server/serving, live reload, the rendering pipeline, a specific Markdown extension, theme/page UI, vendored assets, build/Nix/CI). Otherwise dispatch immediately.

## Workflow

1. **Dispatch one `issue-filer` agent** (Agent tool, `subagent_type: "issue-filer"`) with a prompt of this shape:

   > File one GitHub issue on Galvill/go-grip from the seed brief below. Research it yourself before drafting: locate the implicated code, read the load-bearing lines, gather file:line evidence, and check how the behavior is wired end-to-end today. For a rendering issue, render the Markdown with go-grip and state what GitHub renders. Check for duplicates on the fork before filing, and check chrishrb/go-grip for an existing upstream issue or PR to link; on a near-duplicate on the fork do NOT file — return `DUPLICATE <number> <title>` instead. Follow your house style. After filing, return your contract line plus one sentence summarizing what the issue covers.
   >
   > Seed: <the user's brief, verbatim>
   > Extra context: <anything this conversation added beyond the seed — observed symptoms, constraints, refs already established. Omit the line if there is nothing.>

2. **Relay the outcome.**
   - `DUPLICATE`: tell the user which open issue already covers it, and stop.
   - Filed: print exactly

     ```
     Filed #<N> — <title> → <url>
     ```

     followed by the agent's one-sentence summary. No recap of the body — the user can click the link.

## Rules

- Filing ends at the URL — never start /process-issues or implementation afterwards.
- Issues go on the fork only. Never file on `chrishrb/go-grip`; if the user wants an upstream issue, they file it themselves.
- Don't file if the problem is solvable in the current task; issues are for work that must wait. If that's the case, say so instead of dispatching.
- If the agent fails (gh error, unverifiable refs, empty return), fix the dispatch prompt and re-dispatch. Do not fall back to researching or drafting in-session — that defeats the point of the delegation.
