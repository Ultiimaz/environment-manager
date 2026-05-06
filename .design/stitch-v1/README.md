# Stitch redesign v1 — assets

Generated 2026-05-04 from the Stitch Google MCP using the **Env Manager Dark Pro** design system (asset id `assets/17875599790167059274`).

| File | Screen | Maps to React page |
|---|---|---|
| `01-topology.html` | Topology graph + nodes table | `pages/Topology.tsx` |
| `02-overview.html` | Lab-wide dashboard | `pages/Home.tsx` |
| `03-project-detail.html` | One project + envs grid | `pages/ProjectDetail.tsx` |
| `04-environment-detail.html` | Live logs + resource + builds | `pages/EnvDetail.tsx` |
| `05-builds.html` | Cross-project builds list | `pages/Builds.tsx` |
| `06-build-detail.html` | One build's log + steps + diff | new page (split out from `Builds.tsx`) |
| `07-service-detail.html` | Singleton service consumers/config | `pages/ServiceDetail.tsx` |
| `08-network.html` | Routes + Traefik/CoreDNS health | `pages/Settings.tsx` (new Network section) |

## Design tokens (already applied to `src/index.css`)

- Background `#0A0A0A`, card `#111`, border `#1F1F1F`, hover lift `#161616`
- Primary accent `#10B981` (single green; never used as solid bg fill)
- Status colors: running `#10B981`, building `#F59E0B`, failed `#EF4444`, idle `#71717A`
- Typography: Geist (body + headline) + Geist Mono (code/IDs)
- Border radius 8px (cards), 6px (buttons), full (status pills)
- No drop shadows — borders for separation

## Implementation status

- [x] Design tokens (`src/index.css`)
- [x] Sidebar refactor (`components/layout/sidebar.tsx`) — green-left-border active state, version subtitle
- [x] Header refactor (`components/layout/header.tsx`) — breadcrumb, status pill, ⌘K search hint, avatar
- [ ] `pages/Topology.tsx` — match `01-topology.html`
- [ ] `pages/Home.tsx` — match `02-overview.html`
- [ ] `pages/ProjectDetail.tsx` — match `03-project-detail.html`
- [ ] `pages/EnvDetail.tsx` — match `04-environment-detail.html`
- [ ] `pages/Builds.tsx` — match `05-builds.html`
- [ ] **NEW** `pages/BuildDetail.tsx` — match `06-build-detail.html`, split out from `Builds.tsx`
- [ ] `pages/ServiceDetail.tsx` — match `07-service-detail.html`
- [ ] `pages/Settings.tsx` — match `08-network.html` (Network as primary tab)

## How to use the HTML files

Each HTML file is a self-contained Tailwind page with `cdn.tailwindcss.com` script. Open in a browser to preview, or copy class strings into the corresponding React page.

The HTML uses the same Tailwind token names as our `index.css` (`bg-background`, `border-border`, etc.) where possible. When it inlines hex colors (`bg-[#0A0A0A]`), prefer the equivalent token (`bg-background`) when porting.
