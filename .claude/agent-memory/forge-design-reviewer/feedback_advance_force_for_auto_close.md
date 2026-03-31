---
name: Advance requires Force+SkipTo for auto-close
description: Auto-close from background processes must use Advance with Force=true and SkipTo=StageDone since tickets can be at any arbitrary stage
type: feedback
---

When auto-closing tickets from background processes (like commit journal watchers), `ticket.Advance()` must be called with `AdvanceOptions{SkipTo: StageDone, Force: true, Reason: "..."}`. Without `SkipTo`, Advance only moves one stage forward. Without `Force`, gate checks (review approval, acceptance criteria, etc.) will block the transition.

**Why:** Discovered during design review of commit-journal-background-e2be. The Advance function enforces pipeline gates and single-step progression by default. A ticket at "triage" cannot reach "done" without Force+SkipTo.
**How to apply:** Any future design that auto-transitions tickets to done from non-interactive contexts must use Force+SkipTo pattern.
