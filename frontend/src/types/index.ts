export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports?: string[];
  subdomain?: string;
  is_managed: boolean;
  desired_state?: string;
  created_at: string;
}

export interface ContainerConfig {
  image: string;
  command?: string[];
  entrypoint?: string[];
  working_dir?: string;
  env?: Record<string, string>;
  ports?: PortMapping[];
  volumes?: VolumeMount[];
  resources?: ResourceLimits;
  restart?: string;
  labels?: Record<string, string>;
}

export interface PortMapping {
  host: number;
  container: number;
  protocol?: string;
}

export interface VolumeMount {
  name?: string;
  host_path?: string;
  container_path: string;
  read_only?: boolean;
}

export interface ResourceLimits {
  memory?: string;
  cpu?: string;
}

export interface Volume {
  name: string;
  driver: string;
  mountpoint: string;
  labels?: Record<string, string>;
  is_managed: boolean;
  size_bytes?: number;
}

export interface BackupInfo {
  volume_name: string;
  timestamp: string;
  filename: string;
  size_bytes: number;
}

export interface ComposeProject {
  project_name: string;
  desired_state: string;
  services?: ComposeServiceStatus[];
  is_managed: boolean;
  repo_id?: string;
  repo_compose_path?: string;
}

export interface ComposeServiceStatus {
  name: string;
  container_id?: string;
  state: string;
  subdomain?: string;
}

export interface NetworkConfig {
  base_domain: string;
  network_name: string;
  subnet: string;
  traefik: TraefikConfig;
  coredns: CoreDNSConfig;
}

export interface TraefikConfig {
  dashboard_enabled: boolean;
  https_enabled: boolean;
}

export interface CoreDNSConfig {
  upstream_dns: string;
}

export interface NetworkStatus {
  base_domain: string;
  network_name: string;
  subnet: string;
  traefik_status: string;
  coredns_status: string;
  traefik_url?: string;
}

export interface GitStatus {
  clean: boolean;
  changed_files: string[];
}

export interface GitCommit {
  hash: string;
  message: string;
  author: string;
  date: string;
}

export interface ContainerStats {
  container_id: string;
  container_name: string;
  timestamp: string;
  cpu_percent: number;
  memory_usage: number;
  memory_limit: number;
  memory_percent: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  block_read_bytes: number;
  block_write_bytes: number;
  pids: number;
}

export type ContainerState = "running" | "paused" | "restarting" | "stopped" | "created" | "exited" | "dead"

export interface SyncResult {
  success: boolean;
  pulled_changes: boolean;
  containers_added?: string[];
  containers_updated?: string[];
  containers_removed?: string[];
  errors?: string[];
}

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
  };
}

export interface Repository {
  id: string;
  name: string;
  url: string;
  branch: string;
  commit_sha?: string;
  local_path: string;
  has_token: boolean;
  cloned_at: string;
  last_pulled: string;
  compose_files: string[];
}

export interface CloneRequest {
  url: string;
  branch?: string;
  token?: string;
}

export interface GitHubStatus {
  connected: boolean;
  login?: string;
  avatar_url?: string;
}

export interface GitHubRepo {
  id: number;
  name: string;
  full_name: string;
  private: boolean;
  clone_url: string;
  html_url: string;
  description: string;
  default_branch: string;
  updated_at: string;
}

export interface FileInfo {
  name: string;
  is_dir: boolean;
  size: number;
}

export interface ServiceSubdomain {
  subdomain: string;
  port: number;
}

export interface SubdomainEntry {
  subdomain: string;
  project_name: string;
  service_name: string;
  port: number;
  created_at: string;
}

export interface Project {
  id: string
  name: string
  repo_url: string
  local_path: string
  default_branch: string
  external_domain?: string
  database?: { engine: string; version: string }
  public_branches?: string[]
  expose?: { service: string; port: number }
  status: 'active' | 'archived' | 'stale'
  created_at: string
  migrated_from_compose?: string
}

export interface Environment {
  id: string
  project_id: string
  branch: string
  branch_slug: string
  kind: 'prod' | 'preview' | 'legacy'
  url: string
  compose_file: string
  status: 'pending' | 'building' | 'running' | 'failed' | 'destroying'
  last_build_id?: string
  last_deployed_sha?: string
  created_at: string
}

export interface Build {
  id: string
  env_id: string
  triggered_by: 'webhook' | 'manual' | 'branch-create' | 'clone'
  sha: string
  started_at: string
  finished_at?: string
  status: 'running' | 'success' | 'failed' | 'cancelled'
  log_path: string
}

export interface ProjectDetail {
  project: Project
  environments: Environment[]
}

export interface CreateProjectResponse {
  project: Project
  environment: Environment
  required_secrets: string[]
}

export interface CreateProjectRequest {
  repo_url: string
  token?: string
}

export interface TriggerBuildResponse {
  build_id: string
  env_id: string
}
