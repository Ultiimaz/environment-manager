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

export interface CreateProjectResponse {
  project: Project
  environment: Environment
  required_secrets: string[]
}

// --- Endpoints -------------------------------------------------------------

// Projects
export function listProjects(): Promise<Project[]> {
  return fetchApi<Project[]>('/projects')
}

export function getProject(id: string): Promise<ProjectDetail> {
  return fetchApi<ProjectDetail>(`/projects/${id}`)
}

export function createProject(repoUrl: string, token?: string): Promise<CreateProjectResponse> {
  return fetchApi<CreateProjectResponse>('/projects', {
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

// getBuildLog fetches a historical build's log file as plain text. Returns
// the raw log contents; throws on 404 / non-2xx with a friendly message.
export async function getBuildLog(buildId: string): Promise<string> {
  const headers: Record<string, string> = {}
  const token = getStoredToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  const r = await fetch(`/api/v1/builds/${buildId}/log`, { headers })
  if (!r.ok) {
    const text = await r.text().catch(() => '')
    throw new Error(`HTTP ${r.status}: ${text || r.statusText}`)
  }
  return r.text()
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

// WebSocket URL helper for build logs
export function buildLogWsUrl(envId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws/envs/${envId}/build-logs`
}
