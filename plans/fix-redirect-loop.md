# Plan: Fix Redirect Loop trên Login Page

## Context (Bối cảnh)

Production `https://9r.techio.dev/com.auth.forward/login?return_to=%2F` bị **redirect loop vô tận**. User chưa login gì cả nhưng trang tự redirect liên tục.

**Location trả về:** `/com.auth.forward/login?return_to=%2Fcom.auth.forward%2Flogin%3Freturn_to%3D`
→ decoded: `/com.auth.forward/login?return_to=/com.auth.forward/login?return_to=`

### Nguyên nhân gốc rễ (2 lỗi)

**Lỗi 1 — JSON config dùng handler không tồn tại:**
JSON config trong comment của `deploy/Caddyfile` dùng `"handler": "forward_auth"`. Handler này **KHÔNG phải built-in Caddy** — Caddy docs ghi rõ "Non-standard". Phải dùng `caddy adapt` để generate JSON đúng từ Caddyfile.

**Lỗi 2 — forward_auth bắt cả auth endpoints → redirect loop:**
Caddy `forward_auth` chạy cho **mọi request** trong site block, kể cả `/com.auth.forward/login`. Khi user truy cập login page:

1. Browser → `/com.auth.forward/login` → forward_auth → subrequest `/auth` → không cookie → 302 về login
2. Browser redirect → `/com.auth.forward/login` → forward_auth lại bắt → 302 → **LOOP**

**Vấn đề kỹ thuật forward_auth:** discard body 2xx (không render login form được) + chỉ gửi GET (POST form không đi qua). → Auth endpoints **phải** đi trực tiếp, không qua forward_auth.

## Approach (Cách tiếp cận)

**Caddyfile dùng `handle` blocks** (Caddy directive chuẩn): match `/com.auth.forward/*` → serve auth service trực tiếp; phần còn lại → forward_auth + reverse_proxy. **Single domain**, không cần `COOKIE_DOMAIN`, không cần config mới.

### 1. Caddyfile — `handle` blocks, domain-based

```caddyfile
app.example.com {
 # Auth service endpoints: đi thẳng, KHÔNG qua forward_auth
 handle /com.auth.forward/* {
  reverse_proxy auth:8080
 }

 # Mọi request khác: forward_auth rồi reverse_proxy
 handle {
  forward_auth auth:8080 {
   uri /com.auth.forward/auth
   copy_headers X-Auth-User
   header_up X-Forwarded-Uri {uri}
  }
  reverse_proxy demo:5678
 }
}
```

`handle` blocks là Caddy directive chuẩn (xem [Caddy docs — handle](https://caddyserver.com/docs/caddyfile/directives/handle)). Caddy đánh giá theo thứ tự xuất hiện, mutually exclusive: match đầu tiên wins.

**Luồng (KHÔNG loop):**

1. Browser → `app.example.com/` → `handle` (catch-all) → forward_auth → không cookie → 302 → `/com.auth.forward/login?return_to=/`
2. Browser → `/com.auth.forward/login` → `handle /com.auth.forward/*` → reverse_proxy auth:8080 → **render login form** (KHÔNG qua forward_auth → không loop)
3. User submit → POST `/com.auth.forward/login` → `handle /com.auth.forward/*` → reverse_proxy auth:8080 → auth service verify → set cookie (Path=/) → redirect → `/`
4. Browser → `/` → `handle` (catch-all) → forward_auth → cookie OK → 200 → reverse_proxy demo:5678 → app content ✓

### 2. Tạo JSON config từ Caddyfile bằng `caddy adapt`

```bash
caddy adapt --config deploy/Caddyfile --adapter caddyfile > deploy/caddy.json
```

Thay comment JSON cũ (dùng `"handler": "forward_auth"` không tồn tại) bằng hướng dẫn `caddy adapt`.

### 3. Defense-in-depth trong app (`authz.go`) — optional

Phá loop ngay cả khi Caddy misconfigured (forward_auth vẫn bắt auth paths). Trong `authorize()`, nếu `X-Forwarded-Uri` bắt đầu bằng `BASE_PATH` → return 200 (cho qua):

```go
returnTo := r.Header.Get("X-Forwarded-Uri")
if strings.HasPrefix(returnTo, h.cfg.BasePath+"/") || returnTo == h.cfg.BasePath {
 w.WriteHeader(http.StatusOK)
 return
}
```

### 4. Tương lai: Traefik

Pattern tương tự — separate router cho auth paths (no middleware) + app router có forward-auth middleware:

```yaml
routers:
  auth-paths:
    rule: Host(`app.example.com`) && PathPrefix(`/com.auth.forward`)
    service: auth
    # KHÔNG có middleware forward-auth
  app:
    rule: Host(`app.example.com`)
    service: app
    middlewares: [forward-auth]
```

## Files to modify

| File | Thay đổi |
|---|---|
| `deploy/Caddyfile` | `handle /com.auth.forward/*` (reverse_proxy auth:8080) + `handle` (forward_auth + reverse_proxy). Domain-based. Thay comment JSON cũ bằng hướng dẫn `caddy adapt`. |
| `internal/httpapi/authz.go` | (Optional) Import `strings`; thêm defense-in-depth: `X-Forwarded-Uri` starts with `BASE_PATH` → return 200. |
| `deploy/docker-compose.yml` | Cập nhật Caddy config (domain). |
| `README.md` | Hướng dẫn `caddy adapt`, giải thích `handle` blocks, Traefik note. |

## Reuse (code có sẵn)

- `h.cfg.BasePath` — đã normalize trong `config.normalizeBasePath()`
- Cookie `Path=/` — đã có, gửi cho mọi path trên cùng domain
- `h.svc.SafeReturnTo()` — redirect an toàn, giữ nguyên
- `caddy adapt` — Caddy built-in command, convert Caddyfile → JSON

## Steps

- [x] **1. `deploy/Caddyfile`**: Thay block hiện tại bằng `handle /com.auth.forward/*` + `handle` (forward_auth + reverse_proxy). Domain-based. Thay comment JSON cũ bằng hướng dẫn `caddy adapt`.
- [x] **2. `internal/httpapi/authz.go`**: Import `strings`; thêm defense-in-depth check BASE_PATH → return 200.
- [x] **3. `deploy/docker-compose.yml`**: Không cần thay đổi (Caddyfile được mount qua volume).
- [x] **4. `README.md`**: Hướng dẫn `caddy adapt`, giải thích `handle` blocks, Traefik note.
- [x] **5. Test**: `go build ./...`, `go vet ./...`, `go test ./...`, LSP diagnostics pass. Caddy không có sẵn trong môi trường này nên chưa chạy `caddy adapt` trực tiếp.

## Verification (Kiểm tra)

1. **Login page không loop**: mở `https://app.example.com/com.auth.forward/login` → render form (qua `handle /com.auth.forward/*` → reverse_proxy, không forward_auth)
2. **curl login page**: `curl -i https://app.example.com/com.auth.forward/login` → `200 OK` + HTML form
3. **Protected path không cookie**: `curl -i https://app.example.com/` → `302 Location: /com.auth.forward/login?return_to=/` (redirect 1 lần, không loop)
4. **Login flow**: submit form → cookie set (Path=/) → redirect → `/` → forward_auth → cookie OK → 200 → app content
5. **docker compose local**: `docker compose up -d` → test trên `http://localhost`
6. **Generate JSON**: `caddy adapt --config deploy/Caddyfile --adapter caddyfile > deploy/caddy.json` → verify JSON đúng

## Decisions Log

- **Tự viết JSON config tay cho forward_auth:** **Rejected.** **Why:** User feedback — dùng `caddy adapt` convert Caddyfile → JSON. JSON cũ dùng `"handler": "forward_auth"` không tồn tại trong Caddy built-in.
- **Dùng 2 domain riêng (auth.example.com + app.example.com):** **Rejected.** **Why:** User feedback — "không đúng bản chất. authelia dùng 2 domain vì nó muốn vậy." Cookie cross-domain phức tạp. Single domain đơn giản hơn.
- **App `/auth` trả 200/401 cho BASE_PATH (code hack, không có Caddy handle):** **Rejected.** **Why:** forward_auth discard body 2xx → không serve login form. forward_auth chỉ GET → POST form không đi qua. Cần path-based routing trong Caddy.
- **Config Caddy bằng `:80`:** **Rejected.** **Why:** User feedback — "config bằng domain, không phải config bằng :80."
- **Không có path routing, chỉ forward_auth + reverse_proxy:** **Rejected.** **Why:** forward_auth bắt ALL requests → redirect loop trên login page. Tất cả repo forward auth (Authelia, nforwardauth, gate, traefik-simple-auth) đều tách auth service khỏi forward_auth. User đồng ý dùng `handle` blocks sau khi hiểu vấn đề kỹ thuật.
