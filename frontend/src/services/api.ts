import type {
  Container,
  Volume,
  ComposeProject,
  NetworkConfig,
  NetworkStatus,
  GitStatus,
  GitCommit,
  SyncResult,
  BackupInfo,
  ApiResponse,
  ContainerConfig,
  ContainerStats,
  Repository,
  CloneRequest,
  FileInfo,
  GitHubStatus,
  GitHubRepo,
  Project,
  ProjectDetail,
  CreateProjectRequest,
  CreateProjectResponse,
  TriggerBuildResponse,
} from '../types';

const API_BASE = '/api/v1';

async function fetchRaw<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${url}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });
  if (!response.ok) {
    let msg = `HTTP ${response.status}`;
    try {
      const body = await response.json();
      if (body.message) msg = body.message;
      else if (body.error) msg = body.error;
    } catch {/* ignore */}
    throw new Error(msg);
  }
  return response.json();
}

async function fetchApi<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${url}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  const data: ApiResponse<T> = await response.json();

  if (!data.success) {
    throw new Error(data.error?.message || 'API request failed');
  }

  return data.data as T;
}

// Containers
export async function getContainers(): Promise<Container[]> {
  return fetchApi<Container[]>('/containers');
}

export async function getContainer(id: string): Promise<Container> {
  return fetchApi<Container>(`/containers/${id}`);
}

export async function createContainer(name: string, config: ContainerConfig): Promise<{ id: string; subdomain: string }> {
  return fetchApi('/containers', {
    method: 'POST',
    body: JSON.stringify({ name, config }),
  });
}

export async function startContainer(id: string): Promise<void> {
  await fetchApi(`/containers/${id}/start`, { method: 'POST' });
}

export async function stopContainer(id: string): Promise<void> {
  await fetchApi(`/containers/${id}/stop`, { method: 'POST' });
}

export async function restartContainer(id: string): Promise<void> {
  await fetchApi(`/containers/${id}/restart`, { method: 'POST' });
}

export async function deleteContainer(id: string): Promise<void> {
  await fetchApi(`/containers/${id}`, { method: 'DELETE' });
}

// Container Stats
export async function getContainerStats(id: string): Promise<ContainerStats> {
  return fetchApi<ContainerStats>(`/containers/${id}/stats`);
}

export async function getContainerStatsHistory(id: string, limit?: number): Promise<ContainerStats[]> {
  const params = limit ? `?limit=${limit}` : '';
  return fetchApi<ContainerStats[]>(`/containers/${id}/stats/history${params}`);
}

export async function getAllContainerStats(): Promise<ContainerStats[]> {
  return fetchApi<ContainerStats[]>('/stats');
}

// Container Exec
export async function execContainer(id: string, command: string[]): Promise<{ output: string; exit_code: number }> {
  return fetchApi(`/containers/${id}/exec`, {
    method: 'POST',
    body: JSON.stringify({ cmd: command }),
  });
}

// Volumes
export async function getVolumes(): Promise<Volume[]> {
  return fetchApi<Volume[]>('/volumes');
}

export async function createVolume(name: string, driver?: string): Promise<Volume> {
  return fetchApi('/volumes', {
    method: 'POST',
    body: JSON.stringify({ name, driver }),
  });
}

export async function deleteVolume(name: string): Promise<void> {
  await fetchApi(`/volumes/${name}`, { method: 'DELETE' });
}

export async function backupVolume(name: string): Promise<void> {
  await fetchApi(`/volumes/${name}/backup`, { method: 'POST' });
}

export async function getVolumeBackups(name: string): Promise<BackupInfo[]> {
  return fetchApi<BackupInfo[]>(`/volumes/${name}/backups`);
}

export async function restoreVolume(name: string, timestamp: string): Promise<void> {
  await fetchApi(`/volumes/${name}/restore/${timestamp}`, { method: 'POST' });
}

// Compose
export async function getComposeProjects(): Promise<ComposeProject[]> {
  return fetchApi<ComposeProject[]>('/compose');
}

export async function createComposeProject(
  projectName: string,
  composeYaml: string,
  subdomains?: Record<string, { subdomain: string; port: number }>
): Promise<ComposeProject> {
  return fetchApi('/compose', {
    method: 'POST',
    body: JSON.stringify({
      project_name: projectName,
      compose_yaml: composeYaml,
      subdomains,
    }),
  });
}

export async function getComposeProject(name: string): Promise<{ project: ComposeProject; compose_yaml: string }> {
  return fetchApi(`/compose/${name}`);
}

export async function updateComposeProject(name: string, composeYaml: string): Promise<void> {
  await fetchApi(`/compose/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ compose_yaml: composeYaml }),
  });
}

export async function deleteComposeProject(name: string): Promise<void> {
  await fetchApi(`/compose/${name}`, { method: 'DELETE' });
}

export async function composeUp(name: string): Promise<void> {
  await fetchApi(`/compose/${name}/up`, { method: 'POST' });
}

export async function composeDown(name: string): Promise<void> {
  await fetchApi(`/compose/${name}/down`, { method: 'POST' });
}

// Link a compose project to a cloned repo so pushes rebuild it automatically.
export async function linkComposeRepo(
  projectName: string,
  repoID: string,
  composePath?: string
): Promise<ComposeProject> {
  return fetchApi(`/compose/${projectName}/link`, {
    method: 'POST',
    body: JSON.stringify({ repo_id: repoID, compose_path: composePath }),
  });
}

export async function unlinkComposeRepo(projectName: string): Promise<ComposeProject> {
  return fetchApi(`/compose/${projectName}/link`, { method: 'DELETE' });
}

// Network
export async function getNetworkConfig(): Promise<NetworkConfig> {
  return fetchApi<NetworkConfig>('/network');
}

export async function updateNetworkConfig(config: Partial<NetworkConfig>): Promise<NetworkConfig> {
  return fetchApi('/network', {
    method: 'PUT',
    body: JSON.stringify(config),
  });
}

export async function getNetworkStatus(): Promise<NetworkStatus> {
  return fetchApi<NetworkStatus>('/network/status');
}

// Network Routes
export async function getNetworkRoutes(): Promise<Array<{
  subdomain: string;
  project_name: string;
  service_name: string;
  port: number;
  created_at: string;
}>> {
  return fetchApi('/network/routes');
}

export async function checkSubdomainAvailability(subdomain: string): Promise<{ available: boolean }> {
  return fetchApi(`/network/routes/${subdomain}/check`);
}

// Git
export async function getGitStatus(): Promise<GitStatus> {
  return fetchApi<GitStatus>('/git/status');
}

export async function syncGit(): Promise<SyncResult> {
  return fetchApi<SyncResult>('/git/sync', { method: 'POST' });
}

export async function getGitHistory(): Promise<GitCommit[]> {
  return fetchApi<GitCommit[]>('/git/history');
}

// Repositories
export async function getRepositories(): Promise<Repository[]> {
  const response = await fetch(`${API_BASE}/repos`);
  if (!response.ok) {
    throw new Error('Failed to fetch repositories');
  }
  return response.json();
}

export async function cloneRepository(req: CloneRequest): Promise<Repository> {
  const response = await fetch(`${API_BASE}/repos`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!response.ok) {
    const error = await response.text();
    throw new Error(error || 'Failed to clone repository');
  }
  return response.json();
}

export async function getRepository(id: string): Promise<Repository> {
  const response = await fetch(`${API_BASE}/repos/${id}`);
  if (!response.ok) {
    throw new Error('Repository not found');
  }
  return response.json();
}

// GitHub integration (single stored PAT, reused for every GitHub clone/pull)
export async function getGitHubStatus(): Promise<GitHubStatus> {
  return fetchApi<GitHubStatus>('/github/status');
}

export async function setGitHubToken(token: string): Promise<GitHubStatus> {
  return fetchApi<GitHubStatus>('/github/token', {
    method: 'POST',
    body: JSON.stringify({ token }),
  });
}

export async function disconnectGitHub(): Promise<void> {
  await fetchApi('/github/token', { method: 'DELETE' });
}

export async function listGitHubRepos(): Promise<GitHubRepo[]> {
  return fetchApi<GitHubRepo[]>('/github/repos');
}

export async function pullRepository(id: string): Promise<Repository> {
  const response = await fetch(`${API_BASE}/repos/${id}/pull`, {
    method: 'POST',
  });
  if (!response.ok) {
    const error = await response.text();
    throw new Error(error || 'Failed to pull repository');
  }
  return response.json();
}

export async function deleteRepository(id: string): Promise<void> {
  const response = await fetch(`${API_BASE}/repos/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error('Failed to delete repository');
  }
}

export async function getRepositoryFiles(id: string, path?: string): Promise<FileInfo[]> {
  const params = path ? `?path=${encodeURIComponent(path)}` : '';
  const response = await fetch(`${API_BASE}/repos/${id}/files${params}`);
  if (!response.ok) {
    throw new Error('Failed to fetch files');
  }
  return response.json();
}

export async function getRepositoryComposeFiles(id: string): Promise<string[]> {
  const response = await fetch(`${API_BASE}/repos/${id}/compose`);
  if (!response.ok) {
    throw new Error('Failed to fetch compose files');
  }
  return response.json();
}

export async function getRepositoryFileContent(id: string, path: string): Promise<string> {
  const response = await fetch(`${API_BASE}/repos/${id}/content?path=${encodeURIComponent(path)}`);
  if (!response.ok) {
    throw new Error('Failed to fetch file content');
  }
  return response.text();
}

// Projects
export async function getProjects(): Promise<Project[]> {
  return fetchRaw<Project[]>('/projects');
}

export async function getProject(id: string): Promise<ProjectDetail> {
  return fetchRaw<ProjectDetail>(`/projects/${id}`);
}

export async function createProject(req: CreateProjectRequest): Promise<CreateProjectResponse> {
  return fetchRaw<CreateProjectResponse>('/projects', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

// Builds
export async function triggerBuild(envId: string): Promise<TriggerBuildResponse> {
  return fetchApi<TriggerBuildResponse>(`/envs/${envId}/build`, {
    method: 'POST',
  });
}

// WebSocket URL helper for build logs
export function buildLogWsUrl(envId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}/ws/envs/${envId}/build-logs`;
}
