# Enterprise UX bar (what “finished” means)

## Why it wasn’t “just built” first

Enterprise UX is a **product surface**, not a free side-effect of API parity:

1. **Different work** — tokens, density, empty states, toasts, a11y, user admin, nav gating.
2. **Depends on auth** — real user CRUD needs store + roles first.
3. **Iteration cost** — polish without working data paths ships a pretty shell that can’t operate storage.

Order used: **auth + BFF + parity actions → then shell quality**.

## Current enterprise baseline (this pass)

| Capability | Status |
|------------|--------|
| Light / dark / system | Yes |
| Inter typography + refined tokens | Yes |
| Buttons / inputs / select / labels / shadows / focus | Yes |
| Toast notifications | Yes |
| Login (local primary, optional SSO) | Polished |
| Logout / role badge | Yes |
| Collapsible sidebar + role-gated Admin | Yes |
| User management CRUD (admin) | Yes |
| Audit log UI | Yes |
| Empty states component | Yes |
| Max-width content layout | Yes |

## Still above this baseline (true enterprise bar)

- Design system library (full Radix menu/popover/tooltip/command)
- Table column prefs, density toggle, saved views
- Global command palette (⌘K)
- Onboarding / first-run wizard
- i18n
- Full WCAG audit + axe CI
- SSO admin config UI (issuer/client in UI, not only Helm)
- Notification center, multi-org

## How to push further

1. Adopt full shadcn/ui set under Tailwind 4.  
2. Component Storybook for review.  
3. Playwright visual snapshots (per theme).  
4. Product design pass on volumes list density vs comfort.  
