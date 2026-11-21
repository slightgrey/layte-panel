// Mirrors the Go JSON types in internal/api and internal/actions.

export interface User {
  id: number;
  username: string;
  role: string;
}

export interface MeResponse {
  user: User;
  csrf_token: string;
}

export interface LoginResponse {
  totp_required: boolean;
  csrf_token: string;
}

export interface SectionError {
  code: string;
  message: string;
}

/** Each dashboard section has either data or an error. */
export interface Section<T> {
  data?: T;
  error?: SectionError;
}

export interface Disk {
  path: string;
  total: number;
  free: number;
  used: number;
}

export interface SystemInfo {
  hostname: string;
  os: string;
  os_version_id: string;
  kernel: string;
  uptime_seconds: number;
  cpus: number;
  load: [number, number, number];
  memory: { total: number; available: number; used: number; swap_total: number; swap_used: number };
  disks: Disk[];
}

export interface Service {
  name: string;
  description: string;
  installed: boolean;
  active: string;
  sub: string;
  enabled: string;
}

export interface Version {
  name: string;
  installed: boolean;
  version?: string;
  error?: string;
}

export interface Package {
  name: string;
  version: string;
  from: string;
  security: boolean;
}

export interface Updates {
  total: number;
  security: number;
  packages: Package[];
  checked_at: string;
}

export interface Site {
  id: number;
  domain: string;
  aliases: string[];
  system_user: string;
  root_path: string;
  php_version: string;
  managed: boolean;
  source: "created" | "imported";
  wordpress: boolean;
  ssl: boolean;
  db_name: string;
  db_user: string;
  status: "creating" | "active" | "failed" | "deleting";
  nginx_conf: string;
  fpm_pool: string;
  cert_path: string;
  created_at: string;
  updated_at: string;
}

export interface SitesResponse {
  sites: Site[];
}

export interface ScanResponse extends SitesResponse {
  found: number;
  added: number;
  removed: number;
}

export interface Vhost {
  path: string;
  content: string;
  managed: boolean;
}

export interface Certificate {
  name: string;
  domains: string[];
  issuer: string;
  not_before: string;
  not_after: string;
  managed: boolean;
  path: string;
  days_left: number;
  state: "ok" | "expiring" | "expired";
  site_id?: number;
  site_domain?: string;
}

export interface CertificatesResponse {
  certificates: Certificate[];
  email: string;
}

export interface DatabaseRow {
  name: string;
  size: number;
  system: boolean;
  managed: boolean;
  user?: string;
  site_id?: number;
  site_domain?: string;
  wordpress?: boolean;
}

export interface DatabasesResponse {
  engine: string;
  databases: DatabaseRow[];
}

export interface SecretsResponse {
  secrets: Record<string, string>;
}

export interface CronJob {
  id: number;
  site_id: number;
  site_domain: string;
  schedule: string;
  command: string;
  enabled: boolean;
  created_at: string;
}

export interface CronResponse {
  jobs: CronJob[];
  wp_cron: Record<string, string>; // site id → suggested WP-Cron command
}

export interface LogSource {
  path: string;
  label: string;
  domain?: string;
  site_id?: number;
}

export interface LogSourcesResponse {
  sources: LogSource[];
}

export interface FileEntry {
  name: string;
  type: "dir" | "file" | "symlink" | "other";
  size: number;
  mode: string;
  mtime: string;
}

export interface FileList {
  entries: FileEntry[];
  read_only: boolean;
  root: string;
}

export interface FileContent {
  content: string;
  size: number;
  read_only: boolean;
}

export interface UFWRule {
  num: number;
  to: string;
  action: string;
  from: string;
  v6: boolean;
  text: string;
  locked: boolean;
}

export interface FailedLogin {
  ip: string;
  count: number;
  last: string;
  users: string[];
}

export interface SecurityResponse {
  overview: {
    ufw: { installed: boolean; active: boolean; default_incoming: string; rules: UFWRule[] };
    ssh_ports: string[];
    reboot_required: boolean;
    failed_logins: FailedLogin[];
    failed_total: number;
  };
  updates: Updates;
  updates_error?: SectionError;
}

export interface APIKey {
  id: number;
  name: string;
  prefix: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
  revoked_at?: string;
}

export interface KeysResponse {
  keys: APIKey[];
  scopes: { name: string; description: string }[];
}

export interface JobEvent {
  seq: number;
  time: string;
  type: "step" | "log" | "done";
  step?: string;
  status?: "running" | "done" | "failed" | "succeeded";
  message?: string;
}

export interface JobInfo {
  id: string;
  kind: string;
  site_id?: number;
  status: "running" | "succeeded" | "failed";
  error?: string;
  events: JobEvent[];
  has_secret: boolean;
  created_at: string;
  finished_at?: string;
}

export interface CreateSiteRequest {
  domain: string;
  aliases: string[];
  username?: string;
  php_version: string;
  create_db: boolean;
  wordpress?: { title: string; admin_user: string; admin_email: string };
}

export interface DeleteSiteRequest {
  confirm_domain: string;
  remove_files: boolean;
  drop_db: boolean;
}

export interface JobStarted {
  job_id: string;
  site_id: number;
}

export interface Dashboard {
  system: Section<SystemInfo>;
  services: Section<Service[]>;
  versions: Section<Version[]>;
  updates: Section<Updates>;
}
