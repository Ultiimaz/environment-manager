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
