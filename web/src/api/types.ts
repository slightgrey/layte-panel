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
  /** Ubuntu's phased rollout hasn't reached this server yet. */
  phased?: boolean;
}

export interface Updates {
  /** Ready to install (phased ones not counted). */
  total: number;
  security: number;
  /** Deferred by Ubuntu's phased rollout. */
  phased: number;
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
  adopted: boolean; // imported, then adopted: runs as system_user with a Layte pool
  /** Adopted, and Layte manages its nginx config and pool (files stay in root_path). */
  nginx_managed: boolean;
  wordpress: boolean;
  ssl: boolean;
  db_name: string;
  db_user: string;
  status: "creating" | "active" | "failed" | "deleting";
  nginx_conf: string;
  fpm_pool: string;
  cert_path: string;
  /** PHP settings (sites Layte created); empty fields mean the defaults. */
  php_settings: PHPSettings;
  /** Page cache (nginx FastCGI cache; WordPress sites Layte created). */
  page_cache: PageCache;
  /** Redirects in the site's nginx config (sites whose config Layte manages). */
  redirects: Redirect[];
  created_at: string;
  updated_at: string;
}

/** PUT /sites/{id}/php-settings. Sizes like "256M" or "1G". */
export interface PHPSettings {
  memory_limit?: string;
  /** upload_max_filesize and post_max_size (and nginx's client_max_body_size). */
  upload_max?: string;
  max_execution_time?: number;
  max_input_vars?: number;
}

/** One redirect: an exact path (with or without trailing slash) to a path or http(s) URL. */
export interface Redirect {
  from: string;
  to: string;
  code: 301 | 302;
}

/** PUT /sites/{id}/page-cache */
export interface PageCache {
  enabled?: boolean;
  /** Minutes a cached page stays fresh (0/absent = 60). */
  ttl_minutes?: number;
}

export interface SitesResponse {
  sites: Site[];
}

export interface ScanResponse extends SitesResponse {
  found: number;
  added: number;
  removed: number;
}

export interface AdoptPreview {
  domain: string;
  web_root: string;
  php_version: string;
  pool_file: string;
  pool_name: string;
  old_user: string;
  old_group: string;
  unsafe?: string[];
  blockers?: string[];
  pool_diff: string[]; // "+ line" / "- line" / "  line"
  pool_hash: string;
  log_files?: string[];
  username: string; // suggested new user
}

/** GET /sites/{id}/takeover: what letting Layte manage an adopted site's nginx config changes. */
export interface TakeoverPreview {
  domain: string;
  web_root: string;
  php_version: string;
  ssl: boolean;
  own_cert: string; // certbot live folder of the certificate kept at first ("" = none usable)
  vhost_file: string; // the admin's file, as nginx loads it
  kept_as: string; // where it goes ("" = only the sites-enabled link is removed)
  new_vhost_file: string;
  pool_file: string;
  new_pool_file: string;
  vhost_diff: string[]; // "+ line" / "- line" / "  line"
  pool_diff: string[];
  vhost_hash: string;
  blockers?: string[];
  warnings?: string[]; // custom nginx rules that won't be kept
  email: string; // Let's Encrypt email on file
}

export interface DiskUsage {
  path: string;
  total: number; // bytes
  parts?: { name: string; bytes: number }[];
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
  serial: string;
  fingerprint: string; // SHA-256
  key_type: string;
  signature: string;
  auto_renew: boolean;
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
  linked?: boolean; // existing database linked to an adopted site: never dropped or reset
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

/** GET/PUT /sites/{id}/wp-debug */
export interface WPDebugState {
  enabled: boolean;
  /** Where WordPress debug messages go ("" if unknown). */
  log_path: string;
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

/** "Behind Cloudflare": real visitor IPs for sites using Cloudflare's proxy. */
export interface CloudflareStatus {
  enabled: boolean;
  up_to_date: boolean; // the file matches this version's list of Cloudflare ranges
  ranges: number;
  list_date: string;
  conflicts?: string[]; // other nginx files that already set this up for all sites
  problem?: string; // why it can't be turned on here
}

export interface SecurityResponse {
  overview: {
    ufw: { installed: boolean; active: boolean; default_incoming: string; rules: UFWRule[] };
    ssh_ports: string[];
    reboot_required: boolean;
    failed_logins: FailedLogin[];
    failed_total: number;
    cloudflare: CloudflareStatus;
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

export interface MemDetail {
  total: number;
  available: number;
  free: number;
  buffers: number;
  cached: number;
  shmem: number;
  swap_total: number;
  swap_free: number;
  apps_used: number;
  processes_rss: number;
}

export interface ProcInfo {
  pid: number;
  user: string;
  name: string;
  command: string;
  group: string;
  rss: number;
  cpu: number; // % of the whole machine
}

export interface ProcGroup {
  name: string;
  processes: number;
  rss: number;
  cpu: number;
}

export interface ProcessesResult {
  cpus: number;
  load: [number, number, number];
  uptime_seconds: number;
  memory: MemDetail;
  groups: ProcGroup[];
  processes: ProcInfo[];
  sample_ms: number;
}

export interface DiskBreakdown {
  path: string;
  total: number;
  entries: { path: string; bytes: number }[];
  partial?: boolean;
}

export interface PlannedPkg {
  name: string;
  from?: string; // installed version ("" = new package)
  to: string;
  origin: string;
  security: boolean;
}

export interface UpgradePlan {
  upgrades: PlannedPkg[];
  new_packages: PlannedPkg[];
  removals: string[];
  kept_back: string[];
  /** Ubuntu releases these gradually; this server's turn hasn't come (installing by name still works). */
  phased: string[];
  restarts: string[];
  reboot_likely: boolean;
  hash: string;
}
