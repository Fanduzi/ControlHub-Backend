# Agent Instructions

- Token limits: **4,000 per task; 30,000 per session**. When approaching a limit, summarize and start fresh. Report any breach.
- After each significant step, report what changed, what is verified, and what remains. Completion reports must disclose skipped work, skipped tests, and uncertainty.
- Use the model for judgment: classification, drafting, summarization, and extraction. Use deterministic code for routing, retries, and deterministic transforms; if code can answer, code answers.

## Read before work

- **File changes, code review, or commit/merge preparation:** Read [CODING_STANDARDS.md](CODING_STANDARDS.md), including verification gates for documentation-only changes.
- **Issue intake/publication or security-sensitive publication:** Read [issue-tracker.md](docs/agents/issue-tracker.md).
- **Triage or label changes:** Read [triage-labels.md](docs/agents/triage-labels.md).
- **Feature/design work, resolved product language, or accepted trade-offs:** Read [domain.md](docs/agents/domain.md), relevant [CONTEXT.md](CONTEXT.md) entries, and applicable [decision records](docs/decisions/).
- **Setup, running the project, or command lookup:** Read [README.md](README.md) and [Makefile](Makefile).
