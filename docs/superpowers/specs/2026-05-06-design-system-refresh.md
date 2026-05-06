# env-manager design system refresh

**Date:** 2026-05-06
**Status:** awaiting user approval

## Goal

Replace the current shadcn-default look with a restrained Vercel/Railway-style dark UI: airier rounded surfaces, pill badges with subtle tinted backgrounds, tighter typography rhythm, and a single neutral accent. The ask was "more professional, less vibecoded" — locked direction is **option B** from the brainstorming comparison (visual companion `direction.html`).

## Scope

**Restyle**, not rebuild. Two layers:

1. **Tokens** — rewrite the CSS custom properties in `frontend/src/index.css`. All Tailwind class consumers auto-adopt.
2. **Card + Badge primitives** — these are the most visible "vibecoded" tells today. The other shadcn components (Button, Input, Tooltip, Dialog…) stay; they just inherit the new tokens.

**Out of scope:**
- Rebuilding shadcn from scratch — overkill for a single-operator dashboard.
- Light mode toggle — dark stays default; light theme tokens get sensible defaults but no explicit toggle UI.
- Page-layout reflows — the v2 information architecture (5 sidebar destinations + 7 routes) stays. Visual treatment changes only.
- Animation / motion design — current behaviour preserved, no new transitions.

## Locked decisions

### Color palette (dark mode)

| Token | Old (HSL) | New (HSL) | New (hex approx) | Notes |
|---|---|---|---|---|
| `--background` | `220 20% 7%` | `0 0% 4%` | `#0a0a0a` | Off-black, not pure black — pure-black plays badly with shadows |
| `--foreground` | `210 20% 95%` | `0 0% 96%` | `#f5f5f5` | Subtler than icy blue-tinted white |
| `--card` | `220 20% 10%` | `0 0% 6%` | `#0f0f0f` | One step lighter than background |
| `--border` | `220 14% 18%` | `0 0% 14%` | `#242424` | Hairline-ish; ~50% darker than current |
| `--muted` | `220 14% 15%` | `0 0% 11%` | `#1c1c1c` | Subtle inset surfaces |
| `--muted-foreground` | `215 20% 55%` | `0 0% 56%` | `#8e8e8e` | Secondary text |
| `--primary` | `217 91% 60%` | `217 91% 60%` | `#3b82f6` | Keep blue accent — already used for links |
| `--primary-foreground` | `210 20% 98%` | `0 0% 100%` | `#ffffff` | |
| `--ring` | `217 91% 60%` | `217 91% 60%` | `#3b82f6` | Keep |
| `--destructive` | `0 72% 51%` | `0 72% 51%` | `#dc2626` | Keep — semantic, no aesthetic change |
| `--success` | `142 71% 45%` | `142 71% 45%` | `#22c55e` | Keep |
| `--warning` | `38 92% 50%` | `38 92% 50%` | `#f59e0b` | Keep |
| `--input` | `220 14% 18%` | `0 0% 14%` | `#242424` | Track border |

The shift is **chromatic neutralization**: the current tokens are a slightly-blue-tinted dark theme (`220 20%` hue/sat). The new tokens are pure neutral grays. Accent colors (blue/red/green/amber) stay identical so semantic meaning doesn't change — only the chrome desaturates.

### Typography

- Body font: `'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif`
  - Inter is loaded from Google Fonts via a stylesheet `<link>` in `index.html` (not a font file install — keeps the FE bundle lean).
- Mono font: `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace`
  - Used for build SHAs, env IDs, monospace data in the Builds + Settings pages.
- Numeric data: `font-variant-numeric: tabular-nums` — applied to status timestamps + counts so columns line up.
- Scale: stick with Tailwind's defaults (`text-xs` 12px, `text-sm` 14px, `text-base` 16px, `text-lg` 18px, `text-2xl` 24px). No new sizes.

### Spacing + radius

- `--radius: 0.5rem` (8px) — same value but applied to FEWER things. Cards keep it; badges go to `9999px` (full pill); buttons keep it.
- Page padding standard: `p-6` for top-level pages (current default).
- Card padding: `p-4` not `p-6` — less precious, more data-dense.
- Vertical rhythm between sections: `space-y-4` (16px), down from `space-y-6` (24px).

## Component changes

### `Card` primitive (new shape)

The current shadcn Card has two visible problems:

1. **CardHeader + CardContent split** introduces a structural divider where the design wants flow. Most pages render `<Card><CardHeader><CardTitle>…</CardTitle></CardHeader><CardContent>…</CardContent></Card>` — a lot of structural noise for "border + padding + title."
2. **Default `p-6` padding + 24px vertical rhythm between subcomponents** makes the UI feel chunky.

Replacement primitive: a NEW `<Section>` component at `frontend/src/components/ui/section.tsx`. The existing shadcn `Card`/`CardHeader`/`CardContent` files stay in place (some legacy paths may still import them; deleting risks breakage outside the v2 page surface). v2 pages migrate to `Section`. The shape:

```tsx
<section className="rounded-lg border border-border bg-card p-4 space-y-3">
  {title && (
    <div className="flex items-center justify-between">
      <h3 className="text-sm font-medium text-foreground">{title}</h3>
      {action}
    </div>
  )}
  {children}
</section>
```

Title is plain `<h3>` not `CardTitle`. No `CardHeader`/`CardContent` walls. Optional `action` slot for the existing pattern of "title on the left, button on the right."

Existing shadcn `Card`/`CardHeader`/`CardContent` files in `frontend/src/components/ui/` stay (some pages import them); we add the new `Section` and migrate the v2 pages (Home, Builds, Services, Settings, ProjectDetail) to use it.

### `Badge` primitive

Current shadcn Badge uses `default | destructive | secondary` with solid backgrounds. The B-vibe wants subtle tinted pills:

| Variant | Background | Border | Text |
|---|---|---|---|
| `success` | `bg-emerald-950/40` | `border-emerald-900/60` | `text-emerald-400` |
| `failed` | `bg-red-950/40` | `border-red-900/60` | `text-red-400` |
| `pending` | `bg-amber-950/40` | `border-amber-900/60` | `text-amber-400` |
| `default` | `bg-muted` | `border-border` | `text-muted-foreground` |

All variants share `rounded-full px-2 py-0.5 text-xs font-medium border`. The `<Badge>` component gains the new variant prop accepting these names. Migration mapping for existing call sites:

| Old variant (shadcn default) | New variant |
|---|---|
| `default` (used for "running" / "success" / "configured") | `success` |
| `destructive` (used for "failed" / "stopped" / "unavailable") | `failed` |
| `secondary` (used for "pending" / "ready" lower-emphasis states) | `default` |

The new `Badge` keeps backward-compat for the old prop names by mapping `default → success`, `destructive → failed`, `secondary → default` internally. Pages can adopt the new names incrementally without breaking.

### Header chrome cleanup

`frontend/src/components/layout/header.tsx` currently renders three decorative items in the top-right: `Refresh`, a notifications bell with hard-coded "3" count, and a fake "main" branch tag. The `3` and `main` are static and don't reflect real state — pure decoration that reads as vibecoded. Plan:

- **Delete** the bell + count.
- **Delete** the branch tag.
- **Keep** the Refresh button (functional — calls `queryClient.invalidateQueries()`).

The `Toggle menu` mobile button stays.

### Sidebar chrome cleanup

Sidebar footer says "Environment Manager v1.0" — stale string. Either pull the version from `/api/v1/settings` (we already fetch it on Home) or just remove. Plan: remove (Settings page already shows version).

## Pages affected

- **Home (`/`)** — service status + projects list both move from `Card` to `Section`. Service status simplifies: single line "paas-postgres · running · postgres:16" instead of card-with-badge.
- **Projects (`/projects`)** — project cards adopt new Section style. Empty state copy stays.
- **ProjectDetail (`/projects/:id`)** — env cards become rows in a single Section. The Logs panel + Build button stay where they are.
- **Builds (`/builds`)** — table style adopts the B-mockup look (already close): row-per-build, pill badges in subtle tints, monospace SHAs. Logs panel becomes Section.
- **Services (`/services`)** — two cards become two compact Sections side-by-side.
- **Settings (`/settings`)** — Admin token + Server config sections both convert.

No new pages, no new routes, no logic changes.

## Implementation outline

Single PR (~7 commits):

1. Update `index.css` token values (HSL changes only — no class changes yet).
2. Add Inter + JetBrains Mono `<link>` tags in `index.html`. Update body `font-family`.
3. Add `Section` primitive in `frontend/src/components/ui/section.tsx` + simple test render.
4. Rewrite `Badge` variants to subtle tinted pills.
5. Migrate Home + Services + Settings pages to `Section`.
6. Migrate ProjectDetail + Builds pages.
7. Header + sidebar chrome cleanup.

## Acceptance criteria

- Pages render with the new tokens — no class-name changes break layout
- Build clean: `cd frontend && pnpm build`
- Lint stays at the existing 5-warning baseline (no NEW warnings introduced)
- Visual smoke test via Playwright: navigate Home → Projects → Builds → Services → Settings and screenshot each
- Old shadcn Card/Badge components remain importable but unused on v2 pages

## Out of scope (deferred)

- Light theme polish (default tokens get reasonable values but no explicit toggle UI)
- Custom keyboard shortcuts / command palette
- Per-page animations or page-transition motion
- New page additions (Plan 7's 5 destinations remain the surface)
- Frontend lint warning baseline cleanup
