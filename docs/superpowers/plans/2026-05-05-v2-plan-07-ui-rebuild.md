# env-manager v2, Plan 7 — UI rebuild

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the React frontend around the v2 API surface. Five sidebar destinations: **Home**, **Projects**, **Builds**, **Services**, **Settings**. Delete the legacy pages (Repositories, Compose, Containers, Volumes, Network, Git, Dashboard) and the half of `api.ts` that talked to them. Add token-based auth so the UI can call mutating endpoints behind Plan 6a's Bearer middleware.

**Architecture:** The frontend stack is already on Tailwind v4 / React 19 / TanStack Query v5 / React Router v7 — no framework bumps needed. Token storage is `localStorage["envm_token"]`, injected as `Authorization: Bearer <token>` on every fetch. A Settings page lets the operator paste the token. Existing Projects + ProjectDetail pages survive with light changes; Dashboard becomes Home.

**Tech Stack:** Existing — no new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "UI rebuild → Information architecture", "UI rebuild → Pages", "UI rebuild → Frontend deletes".

**Out of scope (deferred):**
- Per-env runtime log streaming (UI live-tails) — needs `/ws/runtime-logs` endpoint
- POST /envs/{id}/restart UI button — endpoint not in 6b
- Cobra-style polish; the existing AppLayout is fine
- Full design polish (focus on functional, not pretty)

---

## File structure after this plan

**Deleted:**

```
frontend/src/pages/{Repositories,Compose,Containers,ContainerDetail,Volumes,Network,Git,Dashboard}.tsx
frontend/src/components/{containers,volumes,network,git}/   (entire dirs if present)
```

**New:**

```
frontend/src/pages/Home.tsx
frontend/src/pages/Builds.tsx
frontend/src/pages/Services.tsx
```

**Modified:**

```
frontend/src/App.tsx                          — slim route table
frontend/src/components/layout/sidebar.tsx    — 5 destinations
frontend/src/services/api.ts                  — slimmed; add v2 endpoints + token + Bearer header
frontend/src/pages/Settings.tsx               — token input + LE email + version
```

**Files unchanged:** Projects.tsx, ProjectDetail.tsx, layout/header.tsx, layout/app-layout.tsx, components/ui/*, components/projects/*.

---

## Locked details

| Thing | Value |
|---|---|
| localStorage token key | `envm_token` |
| Sidebar items (in order) | Home (`/`), Projects (`/projects`), Builds (`/builds`), Services (`/services`), Settings (`/settings`) |
| Token form on Settings | textbox + Save button + masked display when stored |
| Auth header injection | All `fetch` calls in api.ts read localStorage and append `Authorization: Bearer <token>` if present |
| Home page content | Service status cards (postgres + redis) + recent builds (latest 10 across projects) |
| Builds page content | Cross-project recent builds — table with project, env, status, started, duration |
| Services page content | Two cards (postgres + redis): container name, image, exists/running flags |
| Settings page content | Token input (write-only field), LE email status (set / unset), version, GitHub PAT stub (display "configured / not configured") |

---

## Tasks

### Task 1: Branch + delete legacy pages + slim App.tsx + update sidebar

One commit covering the structural cleanup. Behaviour: app still builds + runs but the legacy routes are gone (404 in the browser). Future tasks add the new pages.

**Files:**
- Delete: 8 legacy page files
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/layout/sidebar.tsx`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status && git rev-parse HEAD
```

Expected: HEAD at `fbf056c` (Plan 6b merge) or later.

```bash
git checkout -b feat/v2-plan-07-ui-rebuild
```

- [ ] **Step 2: Delete legacy pages**

```bash
rm frontend/src/pages/Repositories.tsx \
   frontend/src/pages/Compose.tsx \
   frontend/src/pages/Containers.tsx \
   frontend/src/pages/ContainerDetail.tsx \
   frontend/src/pages/Volumes.tsx \
   frontend/src/pages/Network.tsx \
   frontend/src/pages/Git.tsx \
   frontend/src/pages/Dashboard.tsx
```

If `frontend/src/components/{containers,volumes,network,git}/` dirs exist, also remove them:

```bash
rm -rf frontend/src/components/containers/ \
       frontend/src/components/volumes/ \
       frontend/src/components/network/ \
       frontend/src/components/git/ 2>&1 | head -5
```

(2>&1 | head guards against the dir-not-existing case without using > nul.)

- [ ] **Step 3: Update App.tsx**

Replace `frontend/src/App.tsx` contents with:

```tsx
import { Routes, Route } from 'react-router-dom'
import { AppLayout } from './components/layout'
import Home from './pages/Home'
import Projects from './pages/Projects'
import ProjectDetail from './pages/ProjectDetail'
import Builds from './pages/Builds'
import Services from './pages/Services'
import Settings from './pages/Settings'

function App() {
  return (
    <Routes>
      <Route path="/" element={<AppLayout />}>
        <Route index element={<Home />} />
        <Route path="projects" element={<Projects />} />
        <Route path="projects/:id" element={<ProjectDetail />} />
        <Route path="builds" element={<Builds />} />
        <Route path="services" element={<Services />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}

export default App
```

(Home/Builds/Services pages don't exist yet — TypeScript will complain. That's OK for this task; Tasks 3-5 add them. To make the build pass intermediately, create minimal stub files:)

Create `frontend/src/pages/Home.tsx`:

```tsx
export default function Home() {
  return <div className="p-6">Home (Plan 7 stub — populated in Task 3)</div>
}
```

Create `frontend/src/pages/Builds.tsx`:

```tsx
export default function Builds() {
  return <div className="p-6">Builds (Plan 7 stub — populated in Task 4)</div>
}
```

Create `frontend/src/pages/Services.tsx`:

```tsx
export default function Services() {
  return <div className="p-6">Services (Plan 7 stub — populated in Task 5)</div>
}
```

- [ ] **Step 4: Update sidebar.tsx — 5 destinations only**

Replace `frontend/src/components/layout/sidebar.tsx`'s `navItems` block:

```tsx
import {
  Home,
  Rocket,
  Hammer,
  Database,
  Settings,
  ChevronLeft,
  ChevronRight,
} from "lucide-react"

// ... (rest of imports preserved)

const navItems: NavItem[] = [
  { title: "Home", href: "/", icon: Home },
  { title: "Projects", href: "/projects", icon: Rocket },
  { title: "Builds", href: "/builds", icon: Hammer },
  { title: "Services", href: "/services", icon: Database },
  { title: "Settings", href: "/settings", icon: Settings },
]

// ... (rest of file preserved)
```

Make sure to also remove any imports of legacy lucide icons (`LayoutDashboard`, `Box`, `HardDrive`, `Layers`, `Network`, `GitBranch`) if they're now unused.

- [ ] **Step 5: Build to verify TypeScript compiles**

```bash
cd frontend && pnpm build
```

Expected: clean build. If TS errors come from any other file referring to deleted pages/components, fix them inline (most likely `App.tsx` import block or `sidebar.tsx` icon imports).

- [ ] **Step 6: Commit**

```bash
git add -A frontend/
git commit -m "feat(ui): delete legacy pages, slim sidebar to 5 v2 destinations

Removes Repositories, Compose, Containers, Volumes, Network, Git,
Dashboard pages and their component dirs. Sidebar now: Home /
Projects / Builds / Services / Settings — matching the v2 design
spec's information architecture. Home/Builds/Services are stubs
in this commit; Tasks 3-5 add real content."
```

---

### Task 2: Slim `api.ts` + token storage + Bearer header injection

**Files:**
- Modify: `frontend/src/services/api.ts`

Slim `api.ts` to keep only the v2-relevant endpoints. Add token storage helpers and Bearer injection in the existing `fetchApi` wrapper.

- [ ] **Step 1: Replace `frontend/src/services/api.ts` content**

Replace the entire file with:

```typescript
// API client for env-manager v2.
//
// Read-only endpoints work without authentication on LAN.
// Mutating endpoints (POST/PUT/DELETE) require the admin token stored in
// localStorage["envm_token"] — set via the Settings page.

const API_BASE = '/api/v1'

// --- Token storage ---------------------------------------------------------

const TOKEN_KEY = 'envm_token'

export function getStoredToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

export function setStoredToken(token: string): void {
  try {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  } catch {
    // localStorage unavailable (private browsing) — silently noop
  }
}

// --- Fetch wrappers --------------------------------------------------------

async function fetchApi<T>(url: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((options?.headers as Record<string, string>) || {}),
  }
  const token = getStoredToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  const response = await fetch(`${API_BASE}${url}`, { ...options, headers })
  if (!response.ok) {
    const text = await response.text().catch(() => '')
    throw new Error(`HTTP ${response.status}: ${text || response.statusText}`)
  }
  // Some endpoints (DELETE) return 204 No Content; tolerate empty bodies.
  const ct = response.headers.get('content-type') || ''
  if (response.status === 204 || !ct.includes('application/json')) {
    return undefined as unknown as T
  }
  return response.json() as Promise<T>
}

// --- Types -----------------------------------------------------------------

export interface Project {
  id: string
  name: string
  repo_url: string
  default_branch: string
  status: string
  external_domain?: string
}

export interface Environment {
  id: string
  project_id: string
  branch: string
  branch_slug: string
  kind: string
  url: string
  status: string
  last_build_id?: string
  last_deployed_sha?: string
}

export interface ProjectDetail {
  project: Project
  environments: Environment[]
}

export interface Build {
  id: string
  env_id: string
  triggered_by: string
  sha: string
  started_at: string
  finished_at?: string
  status: string
  log_path: string
}

export interface ServiceStatus {
  container: string
  image: string
  running: boolean
  exists: boolean
}

export interface Settings {
  letsencrypt_email_set: boolean
  credential_store_set: boolean
  version: string
}

// --- Endpoints -------------------------------------------------------------

// Projects
export function listProjects(): Promise<Project[]> {
  return fetchApi<Project[]>('/projects')
}

export function getProject(id: string): Promise<ProjectDetail> {
  return fetchApi<ProjectDetail>(`/projects/${id}`)
}

export function createProject(repoUrl: string, token?: string): Promise<unknown> {
  return fetchApi('/projects', {
    method: 'POST',
    body: JSON.stringify({ repo_url: repoUrl, token: token || '' }),
  })
}

export function deleteProject(id: string): Promise<unknown> {
  return fetchApi(`/projects/${id}`, { method: 'DELETE' })
}

// Secrets
export function listSecretKeys(projectId: string): Promise<{ keys: string[] }> {
  return fetchApi(`/projects/${projectId}/secrets`)
}

export function setSecrets(projectId: string, kvs: Record<string, string>): Promise<unknown> {
  return fetchApi(`/projects/${projectId}/secrets`, {
    method: 'PUT',
    body: JSON.stringify(kvs),
  })
}

export function deleteSecret(projectId: string, key: string): Promise<unknown> {
  return fetchApi(`/projects/${projectId}/secrets/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

// Builds
export function triggerBuild(envId: string): Promise<{ data: { build_id: string; env_id: string } }> {
  return fetchApi(`/envs/${envId}/build`, { method: 'POST' })
}

export function listBuildsForEnv(envId: string): Promise<Build[]> {
  return fetchApi<Build[]>(`/envs/${envId}/builds`)
}

// Envs
export function destroyEnv(envId: string): Promise<unknown> {
  return fetchApi(`/envs/${envId}/destroy`, { method: 'POST' })
}

// Services + Settings
export function getPostgresStatus(): Promise<ServiceStatus> {
  return fetchApi<ServiceStatus>('/services/postgres')
}

export function getRedisStatus(): Promise<ServiceStatus> {
  return fetchApi<ServiceStatus>('/services/redis')
}

export function getSettings(): Promise<Settings> {
  return fetchApi<Settings>('/settings')
}

// Health
export function getHealth(): Promise<unknown> {
  return fetchApi('/health')
}
```

This drops all the legacy `getContainers/getVolumes/getNetwork/git*/compose*` exports — anything elsewhere in `frontend/` that imports them will fail to build. Task 1's deletes already removed the consumers, but `Projects.tsx` / `ProjectDetail.tsx` may have imported via `api.ts` for project + secret operations — those should still work (the new `api.ts` keeps them).

- [ ] **Step 2: Build to find dangling imports**

```bash
cd frontend && pnpm build
```

If errors, the diagnostic will list which files import deleted exports. For each, either:
- Use the new equivalent (e.g. `getProjects` → `listProjects`)
- Remove the import + the usage (likely a UI affordance for a deleted page)

If `Projects.tsx` or `ProjectDetail.tsx` use a removed function name (e.g. `getProjects`), grep for it: `cd frontend && grep -rn "getProjects\|getProject" src/`. The renames map: `getProjects` → `listProjects`, `getProject` → `getProject` (kept), `addProject` → `createProject`. Apply mechanically.

- [ ] **Step 3: Commit**

```bash
git add -A frontend/
git commit -m "feat(ui): slim api.ts; add token storage + Bearer header injection

api.ts now exposes only v2 endpoints (projects, secrets, builds,
envs, services, settings). Mutating calls auto-inject
Authorization: Bearer <token> from localStorage[\"envm_token\"]
when present. Token is set via the Settings page (Task 6).
Dangling consumers of deleted legacy exports are updated inline
to use the new names."
```

---

### Task 3: Home page

**Files:**
- Modify: `frontend/src/pages/Home.tsx`

- [ ] **Step 1: Implement Home**

Replace `frontend/src/pages/Home.tsx` content:

```tsx
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getPostgresStatus, getRedisStatus, getSettings } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const projects = useQuery({ queryKey: ['projects'], queryFn: listProjects })
  const postgres = useQuery({ queryKey: ['services', 'postgres'], queryFn: getPostgresStatus })
  const redis = useQuery({ queryKey: ['services', 'redis'], queryFn: getRedisStatus })

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold">env-manager</h1>
        <p className="text-sm text-muted-foreground">
          {settings.data ? `version ${settings.data.version}` : 'loading…'}
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              paas-postgres
              {postgres.data && (
                <Badge variant={postgres.data.running ? 'default' : 'destructive'}>
                  {postgres.data.running ? 'running' : 'stopped'}
                </Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm text-muted-foreground">
              {postgres.data?.image || 'postgres:16'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between">
              paas-redis
              {redis.data && (
                <Badge variant={redis.data.running ? 'default' : 'destructive'}>
                  {redis.data.running ? 'running' : 'stopped'}
                </Badge>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm text-muted-foreground">
              {redis.data?.image || 'redis:7'}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Projects ({projects.data?.length ?? 0})</CardTitle>
        </CardHeader>
        <CardContent>
          {projects.isLoading && <div className="text-sm text-muted-foreground">loading…</div>}
          {projects.data && projects.data.length === 0 && (
            <div className="text-sm text-muted-foreground">
              No projects yet. <Link to="/projects" className="underline">Onboard one</Link>.
            </div>
          )}
          {projects.data && projects.data.length > 0 && (
            <ul className="space-y-2">
              {projects.data.map((p) => (
                <li key={p.id} className="flex items-center justify-between">
                  <Link to={`/projects/${p.id}`} className="font-medium hover:underline">
                    {p.name}
                  </Link>
                  <span className="text-xs text-muted-foreground">{p.default_branch}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
```

If `Card`/`Badge` aren't already imported in your shadcn primitives, check `frontend/src/components/ui/`. If `card.tsx` / `badge.tsx` don't exist, generate them via shadcn CLI or hand-write minimal versions.

- [ ] **Step 2: Build + commit**

```bash
cd frontend && pnpm build
```

```bash
git add frontend/src/pages/Home.tsx
git commit -m "feat(ui): Home page with service status + projects list"
```

---

### Task 4: Builds page

**Files:**
- Modify: `frontend/src/pages/Builds.tsx`

- [ ] **Step 1: Implement Builds**

Builds is a cross-project view, but the API only provides per-env build lists. For now, walk all projects' envs and aggregate. (Inefficient but functional; future work could add a cross-project endpoint.)

Replace `frontend/src/pages/Builds.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { listProjects, getProject, listBuildsForEnv, type Build } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

interface EnrichedBuild extends Build {
  project_id: string
  project_name: string
  branch: string
}

async function fetchAllBuilds(): Promise<EnrichedBuild[]> {
  const projects = await listProjects()
  const all: EnrichedBuild[] = []
  for (const p of projects) {
    const detail = await getProject(p.id)
    for (const env of detail.environments) {
      try {
        const builds = await listBuildsForEnv(env.id)
        for (const b of builds) {
          all.push({ ...b, project_id: p.id, project_name: p.name, branch: env.branch })
        }
      } catch {
        // Skip envs whose build list errors; continue aggregating.
      }
    }
  }
  // Most-recent first; already sorted by API but resort across envs.
  all.sort((a, b) => (a.started_at < b.started_at ? 1 : -1))
  return all
}

function statusVariant(status: string): 'default' | 'destructive' | 'secondary' {
  switch (status) {
    case 'success':
      return 'default'
    case 'failed':
    case 'cancelled':
      return 'destructive'
    default:
      return 'secondary'
  }
}

export default function Builds() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['builds', 'all'],
    queryFn: fetchAllBuilds,
    refetchInterval: 10000,
  })

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Builds</h1>

      <Card>
        <CardHeader>
          <CardTitle>Recent builds</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <div className="text-sm text-muted-foreground">loading…</div>}
          {error && <div className="text-sm text-destructive">{(error as Error).message}</div>}
          {data && data.length === 0 && (
            <div className="text-sm text-muted-foreground">No builds yet.</div>
          )}
          {data && data.length > 0 && (
            <table className="w-full text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr className="border-b">
                  <th className="text-left py-2">Project</th>
                  <th className="text-left py-2">Branch</th>
                  <th className="text-left py-2">SHA</th>
                  <th className="text-left py-2">Status</th>
                  <th className="text-left py-2">Triggered</th>
                  <th className="text-left py-2">Started</th>
                </tr>
              </thead>
              <tbody>
                {data.slice(0, 50).map((b) => (
                  <tr key={`${b.env_id}-${b.id}`} className="border-b last:border-0">
                    <td className="py-2">
                      <Link to={`/projects/${b.project_id}`} className="hover:underline font-medium">
                        {b.project_name}
                      </Link>
                    </td>
                    <td className="py-2 text-muted-foreground">{b.branch}</td>
                    <td className="py-2 font-mono text-xs">{b.sha?.slice(0, 7) || '—'}</td>
                    <td className="py-2">
                      <Badge variant={statusVariant(b.status)}>{b.status}</Badge>
                    </td>
                    <td className="py-2 text-xs text-muted-foreground">{b.triggered_by}</td>
                    <td className="py-2 text-xs text-muted-foreground">{b.started_at}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
```

- [ ] **Step 2: Build + commit**

```bash
cd frontend && pnpm build
git add frontend/src/pages/Builds.tsx
git commit -m "feat(ui): Builds page — cross-project recent builds table

Aggregates builds across all projects' envs by walking
listProjects + getProject + listBuildsForEnv. Refetches every
10s to surface in-flight builds. Future polish could add a
dedicated /api/v1/builds endpoint for efficiency."
```

---

### Task 5: Services page

**Files:**
- Modify: `frontend/src/pages/Services.tsx`

- [ ] **Step 1: Implement Services**

Replace `frontend/src/pages/Services.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query'
import { getPostgresStatus, getRedisStatus, type ServiceStatus } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

function ServiceCard({ data, fallback }: { data?: ServiceStatus; fallback: ServiceStatus }) {
  const s = data || fallback
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between">
          {s.container}
          <Badge variant={s.running ? 'default' : 'destructive'}>
            {s.running ? 'running' : s.exists ? 'stopped' : 'absent'}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1 text-sm">
        <div>
          <span className="text-muted-foreground">Image: </span>
          <span className="font-mono">{s.image}</span>
        </div>
        <div>
          <span className="text-muted-foreground">Network: </span>
          <span className="font-mono">paas-net</span>
        </div>
      </CardContent>
    </Card>
  )
}

export default function Services() {
  const postgres = useQuery({
    queryKey: ['services', 'postgres'],
    queryFn: getPostgresStatus,
    refetchInterval: 15000,
  })
  const redis = useQuery({
    queryKey: ['services', 'redis'],
    queryFn: getRedisStatus,
    refetchInterval: 15000,
  })

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Services</h1>
      <p className="text-sm text-muted-foreground">
        Shared service-plane singletons used by every project that declares <code>services.postgres: true</code> or <code>services.redis: true</code> in <code>.dev/config.yaml</code>.
      </p>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <ServiceCard
          data={postgres.data}
          fallback={{ container: 'paas-postgres', image: 'postgres:16', running: false, exists: false }}
        />
        <ServiceCard
          data={redis.data}
          fallback={{ container: 'paas-redis', image: 'redis:7', running: false, exists: false }}
        />
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Build + commit**

```bash
cd frontend && pnpm build
git add frontend/src/pages/Services.tsx
git commit -m "feat(ui): Services page with paas-postgres + paas-redis cards"
```

---

### Task 6: Settings page (token input + LE email + version)

**Files:**
- Modify: `frontend/src/pages/Settings.tsx`

- [ ] **Step 1: Replace Settings.tsx**

Replace `frontend/src/pages/Settings.tsx`:

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getSettings, getStoredToken, setStoredToken } from '@/services/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'

function maskToken(t: string): string {
  if (!t) return '<not set>'
  if (t.length <= 12) return '<short>'
  return t.slice(0, 5) + 'xxxx...' + t.slice(-4)
}

export default function Settings() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const [tokenInput, setTokenInput] = useState('')
  const [savedToken, setSavedToken] = useState(getStoredToken())

  const save = () => {
    setStoredToken(tokenInput)
    setSavedToken(tokenInput)
    setTokenInput('')
  }

  const clear = () => {
    setStoredToken('')
    setSavedToken('')
  }

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Settings</h1>

      <Card>
        <CardHeader>
          <CardTitle>Admin token</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="text-sm text-muted-foreground">
            The token is generated by the server on first boot — look for{' '}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">==&gt; env-manager admin token</code>{' '}
            in the server log. Stored in your browser's localStorage; sent as{' '}
            <code className="text-xs">Authorization: Bearer</code> on mutating requests.
          </div>
          <div>
            <span className="text-sm text-muted-foreground">Currently stored: </span>
            <code className="text-xs font-mono">{maskToken(savedToken)}</code>
          </div>
          <div className="flex gap-2">
            <Input
              type="password"
              placeholder="envm_..."
              value={tokenInput}
              onChange={(e) => setTokenInput(e.target.value)}
              className="max-w-md"
            />
            <Button onClick={save} disabled={!tokenInput}>
              Save
            </Button>
            <Button variant="outline" onClick={clear} disabled={!savedToken}>
              Clear
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Server configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">Version</span>
            <code className="font-mono">{settings.data?.version || '—'}</code>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-muted-foreground">LETSENCRYPT_EMAIL</span>
            <Badge variant={settings.data?.letsencrypt_email_set ? 'default' : 'secondary'}>
              {settings.data?.letsencrypt_email_set ? 'configured' : 'unset'}
            </Badge>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-muted-foreground">Credential store</span>
            <Badge variant={settings.data?.credential_store_set ? 'default' : 'destructive'}>
              {settings.data?.credential_store_set ? 'ready' : 'unavailable'}
            </Badge>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
```

If `Input` component is missing in `components/ui/`, use a plain `<input className="..."/>` instead.

- [ ] **Step 2: Build + commit**

```bash
cd frontend && pnpm build
git add frontend/src/pages/Settings.tsx
git commit -m "feat(ui): Settings page with token input + server config display

Token saved to localStorage[\"envm_token\"]; cleared via Clear
button. Server config card shows version, LETSENCRYPT_EMAIL
presence, credential-store status."
```

---

### Task 7: Final sanity + plan/checklist + push + PR

- [ ] **Step 1: Build + lint + Go tests still green**

```bash
cd frontend && pnpm build && pnpm lint
cd backend && go test ./... -count=1 && go vet ./... && go build ./...
```

Expected: clean.

- [ ] **Step 2: Update rollout checklist**

Replace the Plan 7 placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` with:

```markdown
## Plan 7 — UI rebuild

After merge + redeploy:
- [ ] `cd frontend && pnpm build` — clean
- [ ] env-manager UI loads at the LAN address; sidebar shows: Home / Projects / Builds / Services / Settings (5 items)
- [ ] Home page shows version, paas-postgres + paas-redis status, projects list
- [ ] Projects page lists existing projects
- [ ] Builds page shows builds across all projects (refreshes every 10s)
- [ ] Services page shows postgres + redis cards with running/stopped/absent badges
- [ ] Settings page lets operator paste admin token; token persists in localStorage; mask shows when saved
- [ ] Settings page shows version, LETSENCRYPT_EMAIL presence, credential-store presence
- [ ] After saving a token, attempting to delete a project via UI succeeds (uses Bearer header)
- [ ] Without a token saved, mutating actions fail visibly with 401
- [ ] Legacy routes (/repos, /containers, /volumes, /network, /compose) return 404 in browser
```

- [ ] **Step 3: Commit + push + PR**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-07-ui-rebuild.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 07 (ui rebuild)"
git push -u origin feat/v2-plan-07-ui-rebuild
gh pr create --title "v2 plan 07: UI rebuild — 5 sidebar destinations + token auth" --body "Deletes legacy pages (Repositories, Compose, Containers, Volumes, Network, Git, Dashboard); adds Home, Builds, Services pages; updates Settings with token input + server config display. Mutating fetches inject Authorization: Bearer from localStorage[\"envm_token\"] so the UI works behind Plan 6a's middleware.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
```

---

## Acceptance criteria

- [ ] 8 legacy pages deleted; 3 new pages added (Home, Builds, Services)
- [ ] Sidebar has exactly 5 items in the spec'd order
- [ ] App.tsx route table matches the 5 destinations + ProjectDetail
- [ ] api.ts only exposes v2 endpoints; injects Bearer from localStorage
- [ ] Settings page accepts and stores token; shows server config
- [ ] `cd frontend && pnpm build` clean
- [ ] `cd backend && go test ./...` still green (no backend changes expected)
- [ ] Branch is 8 commits ahead (7 task + 1 docs)
- [ ] PR opened

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager`. Frontend commands run from `frontend/`.
- **PNPM not NPM** — the global rule.
- **Never use `> nul`/`> NUL`/`> /dev/null`** — destructive on this Windows host. Use `2>&1 | head -N` if you need to swallow output.
- **shadcn `Card`/`Badge`/`Button`/`Input` may already exist in `frontend/src/components/ui/`** — check before generating new ones. The existing legacy UI used them.
- **`@/` alias** points at `frontend/src/` — already configured in vite.config.ts.
- **Don't bump Tailwind / React / TanStack Query** — already on v4 / 19 / v5.
- **Don't add tests for the React components** — manual verification per rollout checklist; testing-library setup not in scope for this plan.
- **If a shadcn component is missing**, hand-write a minimal version in `components/ui/` with the same shape (className-driven). Don't run shadcn CLI in autonomous mode (interactive).
