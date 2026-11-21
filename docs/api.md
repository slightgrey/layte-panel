# Layte Panel API

A read-only HTTP API for other apps: dashboards, monitoring, or a multi-server overview such as siteherd.
The machine-readable description is served by every panel at **`/api/v1/openapi.json`** (OpenAPI 3.1).

## Authentication
1. In the panel, open **API keys** → **New API key**.
2. Pick the scopes the app needs and an expiry date.
3. Copy the key. It looks like `lyt_` followed by 43 characters, and it is shown only once.
4. Send it on every request:

```bash
curl -H "Authorization: Bearer lyt_…" https://panel.example.com/api/v1/sites
```

Things to know about keys:
- **Storage:** only a SHA-256 hash of the key is stored, so a lost key can't be recovered. Create a new one and revoke the old one.
- **Read-only:** keys only work on `GET` endpoints. Logging in, creating or changing anything, the file manager, jobs and key management all need a logged-in admin.
- **Rate limit:** 120 requests per minute per key (`429` with `Retry-After: 60` when exceeded).
- **Attribution:** requests made with a key show up in the agent's logs as `api:<key name>`.
- **Audit:** creating and revoking keys is recorded in the audit log.

## Scopes
| Scope | Endpoints |
|---|---|
| `system:read` | `GET /dashboard`: server info, services, software versions, pending updates |
| `sites:read` | `GET /sites`, `GET /sites/{id}`, `GET /sites/{id}/vhost`, `GET /php/versions` |
| `ssl:read` | `GET /ssl`: every certificate, `days_left`, `state` (`ok` / `expiring` = under 21 days / `expired`) |
| `databases:read` | `GET /databases`: names, sizes, owning site (never passwords) |
| `cron:read` | `GET /cron` |
| `security:read` | `GET /security`: ufw rules, failed SSH logins, pending updates, reboot required |
| `logs:read` | `GET /logs`, `GET /logs/stream` (Server-Sent Events). Logs can contain IPs and URLs, so only grant this when needed. |

## Errors
Errors are always JSON:

```json
{ "error": { "code": "insufficient_scope", "message": "this API key needs the ssl:read scope" } }
```

| Status | Codes |
|---|---|
| 401 | `invalid_api_key`, `api_key_expired` |
| 403 | `insufficient_scope`, `read_only_key`, `session_required` |
| 404 | `not_found` |
| 429 | `rate_limited` |
| 502 | `agent_unavailable` (the privileged agent isn't running) and other server-side failures |

## Connecting from another server
- Put the panel behind nginx with HTTPS (see `deploy/nginx-proxy.conf.example`).
- If the panel's nginx uses an IP allowlist, add the calling server's IP.
- If the other app runs on the same server, it can call `http://127.0.0.1:8443/api/v1` directly.
- Calls are expected server-to-server, so no CORS headers are sent. Don't put keys in browser code.

## Example: certificates about to expire
```bash
curl -s -H "Authorization: Bearer $LAYTE_KEY" https://panel.example.com/api/v1/ssl \
  | jq '.certificates[] | select(.state != "ok") | {domains, days_left}'
```
