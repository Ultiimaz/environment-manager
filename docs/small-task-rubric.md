# Small-task rubric for hermes-engineer

A ticket is "small enough" when Engineer can plausibly implement, test, and verify it inside a single Discord turn (≤ one focused work session, ~30-90 min of Engineer's time). Tickets that don't fit get rejected or split before dispatch.

## The 4 fits

A ticket is small if **all four** are true:

1. **Single scope.** One backend route, OR one UI component, OR one config change, OR one bug fix. Not "and".
2. **One acceptance check.** "Does endpoint X return Y for input Z?" — a single verifiable behaviour. Not "and the dashboard also updates and the worker also processes…".
3. **No unknowns up-front.** The ticket names the file, the function/endpoint, and the expected behaviour. Engineer isn't asked to first "figure out the architecture".
4. **Testable in isolation.** Engineer can verify it locally or via an HTTP probe without spinning up the rest of the system.

If any of these fail, **split before dispatch**.

## Examples — green (dispatch as-is)

- "Add `GET /api/v1/health` returning `{status:'ok'}`; cover with a contract test."
- "Move the 'Add ticket' button on `web/index.html` to the bottom of the lane on mobile (<768px)."
- "Fix off-by-one in `pagination_offset()` at `app/api/list.py:42` — tests show page 2 skips item 11."
- "Add `tags` field to `Ticket` ORM model, migration that adds the column with default '{}', and one test."

## Examples — red (split before dispatching)

- "Build the calendar app with auth, scheduling, email reminders, and admin panel" — 4+ scopes, split into 4+ tickets.
- "Rewrite the auth middleware" — no clear scope or acceptance; ask for the specific change.
- "Make the dashboard mobile-friendly" — vague; split per-component or per-breakpoint.
- "Add CI for the whole repo" — split into "add lint job", "add unit-test job", "add integration-test job".

## Manager heuristic for auto-refusal

When a ticket comes in, refuse before dispatch if **any** of these fire:

- Title contains ` and ` separating distinct nouns/scopes (`"X and Y"`).
- Description estimates `>2h`, `>1 session`, or `>1 day`.
- Acceptance criteria has more than 3 checkboxes that aren't trivially related.
- The user_story spans more than one persona's interaction (e.g. "as user AND as admin").

Refusal pattern: file the oversized ticket as a parent in `backlog`, then file N child tickets in `todo` each meeting the 4 fits, each linked as `relates_to` the parent. Reply to user with the link tree. Parent moves to `done` only when all children are done.

## Verifying after dispatch

When Engineer reports "done" on a ticket, confirm:
- The acceptance check actually passes (run the test_plan).
- No related ticket regressed (grep recent Sentry for new issues).
- The test_plan describes how the user can independently re-verify.

Only then transition `doing` → `done`.
