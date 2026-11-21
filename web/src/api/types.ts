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

export interface Dashboard {
  system: Section<SystemInfo>;
  services: Section<Service[]>;
  versions: Section<Version[]>;
  updates: Section<Updates>;
}
