# Domain Documentation

## Layout

ControlHub Backend uses one shared domain context:

- `CONTEXT.md` defines current domain terms and their boundaries.
- `docs/decisions/` holds accepted architecture decisions that are difficult to
  reverse or surprising without their rationale.

## Consumer Rules

Before feature or design work, read the relevant entries in `CONTEXT.md` and
the applicable decision records. Add or refine a term when product language is
resolved. Create a decision record only after a real trade-off is accepted; do
not use ADRs as a general work log.
