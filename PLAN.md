# Plan: Forward Auth Service với Redis Backend

## Context (Bối cảnh)

Dự án greenfield (chỉ có `README.md` placeholder + git). Mục tiêu: xây dựng một **forward auth service** đơn giản bằng **Go**, dùng **Redis làm backend**, tích hợp với **Caddy** qua directive `forward_auth`.

**Vấn đề giải quyết:** Caddy (reverse proxy) sẽ subrequest đến service này cho mọi request tới app được bảo vệ. Service xác thực cookie JWT + session trong Redis, trả `200` (cho phép) hoặc `401` (từ chối → redirect login). Session là sliding window 15 phút, gia hạn (rate-limited) trên mỗi request thành công.

**Lựa chọn công nghệ (đã chốt với user):**
- **Go + Chi router** (nhẹ, idiomatic, middleware tốt)
- **Caddy `forward_auth`** (proxy)
- **Seed CLI** cho TOTP secret (service chỉ verify)
- **JWT HS256** (HMAC shared secret)
- **Login UI**: dùng design có sẵn `simple-login-3.html` (open-design project `fcab6fd1…`), render bằng Go `html/template` + `go:embed`, form submit bằng **htmx v2** (stable), theme sáng/tối tự động theo thiết bị (`prefers-color-scheme`)

## Kiến trúc & luồng dữ liệu

```
                        ┌──────────────┐
  Browser ──cookie──▶  │    Caddy     │  forward_auth subrequest GET {BASE_PATH}/auth
                        └──────┬───────┘
                               │  (mọi request tới app bảo vệ)
                        ┌──────▼───────┐      read (GET/HGET)        ┌─────────────┐
                        │  Auth Service│ ──────────────────────────▶ │ Redis REPLICA│
                        │   (Go/Chi)   │                              └─────────────┘
                        │              │      write (SET/EXPIRE)      ┌─────────────┐
                        │ {BP}/login   │ ──────────────────────────▶ │ Redis MASTER│
                        │ {BP}/auth    │                              └─────────────┘
                        │ {BP}/logout  │                                    ▲
                        └──────────────┘                                    │ replication
                                                                  ┌─────────────┐
                                                                  │ Redis REPLICA│ (thêm replica nếu cần)
                                                                  └─────────────┘
```

### Luồng Login (HTML form + htmx) — `GET` render, `POST {BASE_PATH}/login` xử lý
- `GET {BASE_PATH}/login`: render template `login.html` (design `simple-login-3.html` đã adapt — xem mục **Login UI**). Query `?return_to=<url>` truyền vào hidden field.
- `POST {BASE_PATH}/login` (form `application/x-www-form-urlencoded`; htmx gửi kèm header `HX-Request: true`):
  1. Form fields: `username`, `code`, `return_to`. **Chuẩn hóa `return_to` bằng `safeReturnTo()`** (xem **Bảo mật & độ tin cậy**) — chỉ chấp nhận path tương đối bắt đầu `/` (không `//`, không scheme/host); sai → `/`.
  2. **Brute-force guard (master)**: `INCR login:attempts:<username>` (+ `EXPIRE = LOGIN_WINDOW` ở lần đầu). Nếu count > `MAX_LOGIN_ATTEMPTS` → trả form kèm lỗi “thử quá nhiều lần”, status `429` (không verify, không set cookie).
  3. Đọc TOTP secret (read **replica**, fallback **master**): `HGET totp:secrets <username>` → secret. Không có secret → tính fail.
  4. Verify code bằng `totp.Validate(code, secret)` (`pquerna/otp`, skew ±1 step).
  5. **Replay guard (master)**: `SET login:used:<username>:<code> 1 NX EX <OTP_REUSE_TTL>`. Nếu trả không-set (mã đã dùng trong cửa sổ) → fail — mỗi mã chỉ nhập 1 lần / `OTP_REUSE_TTL`.
  6. Nếu **fail** (bước 3/4/5) → trả lại form (htmx swap lỗi vào `#form-msg`), status `422`; **không reset** attempts (counter tự hết hạn theo `LOGIN_WINDOW`).
  7. Nếu **OK**: xóa `login:attempts:<username>` (master `DEL`). Tạo `session_id` (crypto/rand 32 bytes → hex).
  8. **Write master**: `HSET session:<id> username <u> last_renewal <unix-seconds>` + `EXPIRE session:<id> 900`. Sau đó **`WAIT <SESSION_WAIT_REPLICAS> <SESSION_WAIT_TIMEOUT>`** để đảm bảo ≥1 replica đã nhận session → tránh race replica-lag (login không phải hot path, chấp nhận chờ ≤500ms). Không có replica → bỏ WAIT.
  9. Tạo JWT (HS256): claims `{ sub: username, sid: session_id, iat }` — **KHÔNG có `exp`**; hợp lệ phụ thuộc hoàn toàn vào session trong Redis.
  10. Set cookie (HttpOnly/Secure/SameSite=Lax, Path=/, MaxAge=`COOKIE_MAX_AGE`).
  11. **Phản hồi**:
     - Có header `HX-Request: true` → trả `200` + header **`HX-Redirect: <safe return_to>`** (htmx điều hướng).
     - Không (JS tắt / curl) → trả **`302 Location: <safe return_to>`** (graceful degradation — form có `action`/`method` thật nên vẫn chạy).

### Luồng Forward Auth (`GET {BASE_PATH}/auth`) — Caddy gọi
1. Đọc cookie → nếu thiếu HOẶC parse/verify chữ ký HS256 fail → **`302` redirect về login** (xem mục **Redirect về login**).
2. Lấy `username`, `session_id` từ claims
3. **Read replica** (fallback master): `HGETALL session:<id>` → nếu rỗng (session hết hạn/đã logout) → **`302` redirect về login**.
4. **Gia hạn async + rate-limited (KHÔNG block response)**: spawn **goroutine** với `context.Background()`:
   - Nếu `now - last_renewal >= RENEW_INTERVAL` (mặc định 60s) → **write master**: `EXPIRE session:<id> 900` + `HSET session:<id> last_renewal <now>`.
   - Ngược lại bỏ qua. (Fire-and-forget; vì `RENEW_INTERVAL` 60s ≪ `SESSION_TTL` 15 phút nên không rủi ro session hết hạn giữa chừng.)
5. Set header response `X-Auth-User: <username>` → Caddy `copy_headers` forward lên upstream
6. Return `200` ngay lập tức (không đợi goroutine gia hạn xong)

### Luồng Logout (`POST {BASE_PATH}/logout`)
- **Write master**: `DEL session:<id>`, xóa cookie → `200`

### Redirect về login khi chưa đăng nhập
- Khi `GET {BASE_PATH}/auth` không có session hợp lệ → trả **`302 Location: {BASE_PATH}/login?return_to=<original>`** (Caddy `forward_auth` trả nguyên response 302 về browser → browser tự redirect).
- `original` lấy từ header `X-Forwarded-Uri` (Caddy set khi subrequest), fallback `/`. Qua **`safeReturnTo()`** (xem **Bảo mật & độ tin cậy**) — chỉ chấp nhận path tương đối, chống open redirect.

## Login UI (giao diện đăng nhập)

**Nguồn design:** file `simple-login-3.html` trong project open-design `fcab6fd1-b0c0-4447-be01-59fa58de33d0` (đã đọc nội dung). Đây là login-card dạng glassmorphism:

- **Theme sáng/tối tự động** qua `prefers-color-scheme`, không có nút toggle thủ công.
- Card `.login-card` (backdrop-blur, border-radius 24px), header “Welcome back”, field username + field password (nút ẩn/hiện), nút submit pill + spinner loading.

**Adapt cho forward-auth (TOTP):**

| Phần design | Giữ / Đổi |
|---|---|
| Toàn bộ CSS variables, layout `.stage`/`.login-card`, theme system | **Giữ nguyên** |
| Header copy | “Welcome back” → “Sign in” + gợi ý nhập mã TOTP |
| Field username | **Giữ** (`autocomplete="username"`) |
| Field password + nút eye-toggle | **Giữ** `type="password"` (che mã TOTP như password, có nút ẩn/hiện) — chỉ đổi `name="code"`, `autocomplete="one-time-code"`, `maxlength="6"`, `inputmode="numeric"`, `pattern="[0-9]{6}"`, `placeholder="123456"`. Mã TOTP được che (yêu cầu "hidden bảo mật" của user) |
| JS submit demo (setTimeout) | **Đổi** → **htmx v2 stable**: `<form hx-post="{BASE_PATH}/login" hx-target="#form-msg" hx-swap="innerHTML" hx-disabled-elt="#submit-btn">` |
| Validate client (JS) | Giữ validate 6 chữ số + server là nguồn sự thật; lỗi server swap vào `#form-msg` |
| Theme toggle JS | **Bỏ** toggle thủ công; chỉ giữ auto theme qua `prefers-color-scheme` |

**Render:** `internal/webui/assets/login.html` là Go `html/template` với `{{.Error}}` (thông báo lỗi) và `{{.ReturnTo}}` (hidden input). `<form>` có `action="{BASE_PATH}/login"` + `method="post"` thật (graceful degradation khi tắt JS). Embed bằng `//go:embed`; htmx v2 stable từ CDN kèm **SRI** (`<script src="…htmx.org@2.x" integrity="sha384-…" crossorigin="anonymous">`, pin version + tính hash khi implement).

## Thiết kế lưu trữ Redis

| Dữ liệu | Loại | Key / Field | TTL | Ghi chú |
|---|---|---|---|---|
| TOTP secret | `HASH` | key `totp:secrets`, field=`<username>`, value=`<secret>` | — (vĩnh viễn) | Seed CLI ghi |
| Session | `HASH` | key `session:<session_id>`, fields `username` (string) + `last_renewal` (**Unix giây, int64**) | 15 phút (sliding) | Login tạo (kèm `WAIT`), /auth gia hạn |
| Login attempts | `STRING` counter | key `login:attempts:<username>` | `LOGIN_WINDOW` (5m) | `INCR`; > `MAX_LOGIN_ATTEMPTS` → block. Master. |
| OTP đã dùng | `STRING` | key `login:used:<username>:<code>` | `OTP_REUSE_TTL` (5m) | `SET NX EX`; chống replay. Master. |

> **Quyết định:** TOTP secret lưu trong **một HASH duy nhất** `totp:secrets` (field=username) — khớp yêu cầu "dưới dạng hash, key=username, value=secret" và đọc hiệu quả bằng 1 lệnh `HGET`. Nếu user muốn key riêng `totp:<username>`, dễ đổi.

## Master-Write / Replica-Read

go-redis **không** tự route read/write ở chế độ standalone (chỉ cluster/sentinel mới có). Giải pháp cho project đơn giản: **2 client riêng**.

- `master *redis.Client` ← mọi lệnh ghi: `SET`, `HSET`, `EXPIRE`, `DEL`
- `replica *redis.Client` (hoặc round-robin qua list replica) ← mọi lệnh đọc: `GET`, `HGET`, `HGETALL`, `TTL`
- **Fallback read-from-master**: nếu `REDIS_REPLICA_ADDR` trống/không cấu hình → **`reader = writer`** (cùng con trỏ `*redis.Client`, dùng chung 1 connection pool — không tạo client thỡi). Lớp `store` giữ 2 trường `writer`/`reader` (cùng giá trị khi không có replica).
- **Login guard luôn dùng master**: các op brute-force/replay (`INCR`, `SET NX`, `DEL`) chạy trên **writer** để chính xác (không phải hot path).
- Lớp `store` (repository) đóng gói cả hai, route theo loại op. Helper:
  - `TOTPStore.GetSecret(username)` → read (replica hoặc master)
  - `SessionStore.Create(id, username)` → write master
  - `SessionStore.Get(id)` → read (replica hoặc master)
  - `SessionStore.Renew(id)` → write master (nhận `context.Background()` để gọi độc lập trong goroutine)

Config: `REDIS_MASTER_ADDR` + `REDIS_REPLICA_ADDR` (**tùy chọn**, 1 địa chỉ duy nhất). Nếu cần load balancing giữa nhiều replica, dùng service bên thứ 3.

## Thư viện (Reuse)

| Mục đích | Package |
|---|---|
| HTTP router + middleware | `github.com/go-chi/chi/v5` (+ `chi/v5/middleware`) |
| Redis client | `github.com/redis/go-redis/v9` |
| TOTP generate/verify | `github.com/pquerna/otp/totp` |
| JWT HS256 | `github.com/golang-jwt/jwt/v5` |
| Config từ env | `github.com/caarlos0/env/v11` (hoặc `os.Getenv` thuần) |
| Logging | `log/slog` (stdlib) |

> **Phiên bản:** Go **1.23+** (stable mới nhất). Mọi dependency dùng version stable mới nhất tại thời điểm implement và ghi rõ trong `go.mod` (`go 1.23.x`): `go-chi/chi/v5`, `redis/go-redis/v9`, `pquerna/otp`, `golang-jwt/jwt/v5`, `caarlos0/env/v11`. htmx **v4** (pin version + SRI).

## Cấu trúc thư mục

```
forward-auth-redis/
├── cmd/
│   ├── server/main.go        # auth service entrypoint
│   └── seed/main.go          # seed TOTP secret CLI (generate + in otpauth://)
├── internal/
│   ├── config/config.go      # load env -> Config struct
│   ├── redisx/client.go      # init master + replica clients
│   ├── store/
│   │   ├── totp.go           # TOTPStore (read replica)
│   │   ├── session.go        # SessionStore (create+WAIT/renew=master, get=replica)
│   │   └── loginguard.go     # rate-limit + chống replay OTP (master)
│   ├── auth/
│   │   ├── totp.go           # wrapper verify code
│   │   ├── jwt.go            # sign/parse HS256
│   │   └── service.go        # orchestrate login/authz/logout
│   ├── webui/
│   │   ├── template.go       # embed + parse html/template
│   │   └── assets/login.html # design simple-login-3.html đã adapt (TOTP + htmx)
│   ├── httpapi/
│   │   ├── router.go         # chi routes + middleware (mount dưới BASE_PATH)
│   │   ├── login.go          # GET render form / POST xử lý form (htmx)
│   │   ├── authz.go          # GET /auth  (forward auth)
│   │   └── logout.go         # POST /logout
│   └── cookiex/cookie.go     # build/clear cookie attrs
├── deploy/
│   ├── Caddyfile             # ví dụ forward_auth + copy_headers
│   └── docker-compose.yml    # redis master + replica + service + caddy + demo app
├── Dockerfile                # multi-stage Go build
├── .env.example
├── go.mod
└── README.md                 # hướng dẫn chạy end-to-end
```

## Cấu hình (env)

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Service listen |
| `BASE_PATH` | `/com.auth.forward` | Tiền tố path endpoint (reverse-DNS, tránh đè route app thật). Family: `/com.auth.forward/{login,auth,logout,healthz}`. **Normalize khi load**: ensure `/` đầu, strip `/` cuối |
| `REDIS_MASTER_ADDR` | `localhost:6379` | Redis master |
| `REDIS_REPLICA_ADDR` | *(trống)* | Địa chỉ replica **tùy chọn** (1 địa chỉ); bỏ trống → read từ master |
| `REDIS_PASSWORD` | — | Tùy chọn |
| `REDIS_DB` | `0` | DB index |
| `JWT_SECRET` | — (bắt buộc) | HMAC key ≥32 bytes, sinh qua env (xem README mục **Generate JWT_SECRET**) |
| `SESSION_TTL` | `15m` | TTL session Redis — nguồn sự thật duy nhất về thời gian sống |
| `RENEW_INTERVAL` | `60s` | Rate-limit gia hạn session |
| `COOKIE_NAME` | `fa_token` | Tên cookie |
| `COOKIE_MAX_AGE` | `0` | MaxAge cookie (0 = browser-session cookie); JWT không có TTL nên thời gian sống do session Redis quyết định |
| `COOKIE_SECURE` | `true` | HTTPS-only (prod). **Dev local HTTP → đặt `false`** (không thì browser không gửi cookie → loop login) |
| `COOKIE_DOMAIN` | — | Tùy chọn |
| `MAX_LOGIN_ATTEMPTS` | `5` | Số lần sai tối đa / username trong `LOGIN_WINDOW` |
| `LOGIN_WINDOW` | `5m` | Cửa sổ đếm brute-force (TTL key `login:attempts:<u>`) |
| `OTP_REUSE_TTL` | `5m` | TTL chống replay mã OTP (key `login:used:<u>:<code>`) |
| `SESSION_WAIT_REPLICAS` | `1` | Số replica tối thiểu phải nhận session sau `WAIT` (0 = bỏ WAIT) |
| `SESSION_WAIT_TIMEOUT` | `500` | Timeout `WAIT` (ms) sau khi tạo session |
| `TOTP_ISSUER` | `forward-auth` | Issuer trong otpauth URI |

## Files sẽ tạo/sửa

- **Tạo mới:** toàn bộ cây thư mục `cmd/`, `internal/`, `deploy/`, `Dockerfile`, `.env.example`, `go.mod`.
- **Sửa:** `README.md` (hướng dẫn đầy đủ).

## Các bước triển khai (Steps)

- [ ] **1. Khởi tạo module**: `go mod init`, thêm dependencies (chi, go-redis, otp, jwt, env), `go mod tidy`.
- [ ] **2. `internal/config`**: struct `Config` + load env (validate `JWT_SECRET` bắt buộc ≥32 bytes, parse duration). **Normalize `BASE_PATH`**: đảm bảo `/` đầu, **strip `/` cuối** (`/com.auth.forward/` → `/com.auth.forward`) để route không bị 404.
- [ ] **3. `internal/redisx`**: khởi tạo `writer` (master) + `reader` (replica, round-robin nếu nhiều). **Không có replica → `reader = writer` (cùng con trỏ `*redis.Client`, chung 1 pool)**. `Ping` cả hai khi start.
- [ ] **4. `internal/store/totp.go`**: `GetSecret(ctx, username) (string, error)` → `HGET totp:secrets` qua replica.
- [ ] **5. `internal/store/session.go`**: `Create` (HSET `username` + `last_renewal` = **Unix giây int64**, EXPIRE) + **`WAIT <n> <ms>`** sau khi tạo (chống replica-lag; bỏ qua nếu không replica), `Get` (HGETALL), `Renew` (EXPIRE + HSET last_renewal), `Delete` — route đúng master/replica. `Renew` nhận `context.Context` riêng (gọi trong goroutine).
- [ ] **5b. `internal/store/loginguard.go`**: `RecordAttempt(username)` (master `INCR`+`EXPIRE`, trả count/blocked), `ResetAttempts(username)` (master `DEL`), `MarkCodeUsed(username, code)` (master `SET NX EX`, trả alreadyUsed). Các key tự clean qua TTL.
- [ ] **6. `internal/auth/jwt.go`**: `Sign(claims)` + `Parse(token)` HS256 (golang-jwt v5). Claims chỉ `{sub, sid, iat}` — **không set `exp`** (JWT stateless về TTL, sống theo session).
- [ ] **7. `internal/auth/totp.go`**: `Verify(secret, code) bool` + helper `ValidateSkew` cho lệch đồng hồ.
- [ ] **8. `internal/auth/service.go`**: `Login(form)`, `Authorize(cookie)`, `Logout` — orchestrate store+jwt+totp. `Login`: `RecordAttempt` → verify TOTP → `MarkCodeUsed` (chống replay) → `ResetAttempts` → tạo session (`Create` + WAIT) → JWT. Thêm helper **`safeReturnTo(returnTo) string`** (chỉ path tương đối bắt đầu `/`, không `//`/scheme/host; decode rồi check; default `/`).
- [ ] **9. `internal/cookiex`**: build cookie (HttpOnly, Secure, SameSite=Lax, Path=/, MaxAge=`COOKIE_MAX_AGE`) + clear.
- [ ] **10. `internal/webui`**: port design `simple-login-3.html` → `assets/login.html`: **giữ field `type="password"`** (che mã TOTP như password + nút eye-toggle) — chỉ đổi `name="code"`, `autocomplete="one-time-code"`, `maxlength="6"`, `inputmode="numeric"`, `pattern="[0-9]{6}"`; giữ hệ theme sáng/tối tự động (`prefers-color-scheme`); thêm **htmx v2 stable** (`hx-post`, `hx-target="#form-msg"`, `hx-disabled-elt`). `<form>` có `action="{BASE_PATH}/login"`+`method="post"` thật (graceful degradation); thẻ `<script>` htmx kèm **SRI** (`integrity`+`crossorigin`). Embed `html/template` + `go:embed`; placeholder `{{.Error}}`, `{{.ReturnTo}}`.
- [ ] **11. `internal/httpapi`**: chi router mount dưới `BASE_PATH` `/com.auth.forward`: `GET {BP}/login` (render form), `POST {BP}/login` (verify → set cookie; **detect header `HX-Request`**: có → trả `HX-Redirect`, không → `302 Location` cho graceful degradation; fail → swap lỗi vào `#form-msg`, status `422`/`429`), `GET {BP}/auth`, `POST {BP}/logout`, `GET {BP}/healthz` (**ping Redis writer+reader → 200/503**); middleware logging + recover. Handler `{BP}/auth` spawn goroutine gia hạn.
- [ ] **12. `cmd/server/main.go`**: load config → init redis → wire service + webui → start HTTP server (graceful shutdown).
- [ ] **13. `cmd/seed/main.go`**: nhận `username` (+ optional secret), generate secret `totp.Generate`, `HSET totp:secrets` (master), in `otpauth://` URI để quét app.
- [ ] **14. `deploy/Caddyfile`**: `forward_auth auth:8080 { uri /com.auth.forward/auth; copy_headers X-Auth-User }` + `reverse_proxy` demo app.
- [ ] **15. `deploy/docker-compose.yml`**: image **`redis:7-alpine`** (hỗ trợ `replicaof`): master (6379) + replica (6380, `replicaof master 6379`) + auth service + caddy + 1 demo upstream (vd `hashicorp/http-echo`).
- [ ] **16. `Dockerfile`**: multi-stage (builder `golang:1.23` → scratch/distroless), expose 8080.
- [ ] **17. `.env.example` + `README.md`**: ví dụ env + mục **"Generate JWT_SECRET"** (`openssl rand -base64 48`) + hướng dẫn chạy end-to-end + test thủ công (gồm mở `/com.auth.forward/login` trên browser để xem theme sáng/tối).

## Verification (Kiểm tra end-to-end)

1. `docker compose up -d` → redis master + replica + service + caddy + demo app.
2. **Seed user**: `go run ./cmd/seed alice` → in secret + otpauth URI → add vào Authenticator app (hoặc dùng `oathtool -b --totp <secret>` sinh code test).
3. **Login qua browser (htmx)**: mở `http://localhost:8080/com.auth.forward/login` → form từ design, theme sáng/tối tự động theo OS + nút toggle. Nhập `alice` + mã TOTP 6 số → submit (htmx) → cookie `fa_token` set + redirect về `return_to` (hoặc `/`). Test bằng curl: `curl -i -c c.txt localhost:8080/com.auth.forward/login -d 'username=alice&code=<totp>&return_to=/'` → `200` + `Set-Cookie` + header `HX-Redirect: /`.
4. **Forward auth OK**: `curl -i -b c.txt localhost:8080/com.auth.forward/auth` → `200`, header `X-Auth-User: alice` (response trả về ngay, gia hạn chạy nền).
5. **Qua Caddy tới demo app**: `curl -i -b c.txt localhost/app` → `200` (Caddy cho qua). Không cookie → `302` redirect về `/com.auth.forward/login?return_to=/app`.
6. **Rate-limit gia hạn (async)**: bật `redis-cli -p 6379 MONITOR`, gọi `/com.auth.forward/auth` 10 lần trong 10s → chỉ thấy ≤1 lệnh `EXPIRE`/phút (không spam, response không bị block).
7. **Session hết hạn**: chờ 16 phút không request → `/com.auth.forward/auth` → `302` về login (session bị Redis xóa; JWT không có `exp` nên session là nguồn duy nhất). Hoặc giảm `SESSION_TTL=30s` để test nhanh.
8. **Logout**: `curl -b c.txt -XPOST localhost:8080/com.auth.forward/logout` → session `DEL`, `/com.auth.forward/auth` tiếp theo → `302`.
9. **Chống open redirect**: `curl -i 'localhost:8080/com.auth.forward/login?return_to=https://evil.com'` (và `return_to=//evil.com`) → redirect vẫn về `/`, không theo evil.com.
10. **Brute-force**: nhập sai code 6 lần liên tiếp → response `429` + bị khóa trong `LOGIN_WINDOW`.
11. **Chống replay**: đăng nhập thành công với code X, gửi lại cùng code X trong 5 phút → fail.
12. **Replica-lag / WAIT**: login xong gọi `/auth` ngay (< 100ms) → `200` (không bị loop login nhờ `WAIT`).
13. **`/healthz` thật**: `curl localhost:8080/com.auth.forward/healthz` → `200`; stop redis container → `503`.

## Bảo mật & độ tin cậy

- **Chống open redirect — `safeReturnTo()`**: `return_to` chỉ được là path tương đối bắt đầu bằng `/`; từ chối `//` (protocol-relative), `scheme://`, hostname. Decode URL trước khi check (chống `%2F%2Fevil`). Default `/`. Áp dụng cho cả `POST /login` (HX-Redirect/302) và `GET /auth` (302 về login).
- **Brute-force TOTP**: rate-limit per username — `INCR login:attempts:<u>` trên **master**, chặn khi > `MAX_LOGIN_ATTEMPTS` (5) trong `LOGIN_WINDOW` (5m) → `429`. Key tự clean qua TTL.
- **Chống replay OTP**: `SET login:used:<u>:<code> 1 NX EX <OTP_REUSE_TTL>` (5m) — mỗi mã chỉ nhập được 1 lần trong cửa sổ.
- **Replica-lag sau login**: sau khi ghi session, gọi `WAIT <SESSION_WAIT_REPLICAS> <SESSION_WAIT_TIMEOUT>` (mặc định 1 replica / 500ms) để đảm bảo replica đã nhận trước khi browser request `/auth` tiếp theo (tránh loop login). Không có replica → bỏ WAIT.
- **Định dạng `last_renewal`**: lưu **Unix timestamp giây (int64)**; `now - last_renewal` tính bằng giây.
- **reader = writer khi không có replica**: dùng chung 1 `*redis.Client` (1 pool), không lãng phí.
- **`COOKIE_SECURE`**: default `true` (prod). **Dev local qua HTTP** → đặt `COOKIE_SECURE=false` trong `.env`/README, nếu không browser không gửi cookie → loop login.
- **Normalize `BASE_PATH`**: strip `/` cuối, đảm bảo `/` đầu, tránh route 404.
- **SRI cho htmx**: thẻ `<script>` kèm `integrity="sha384-…"` + `crossorigin="anonymous"` (hash của version pin) chống CDN inject.
- **Graceful degradation**: `<form action method>` thật → vẫn submit được khi JS tắt (server trả `302` thay vì `HX-Redirect`).
- **`/healthz` thật**: ping Redis writer+reader, trả `200/503` để load balancer biết service healthy.
- **JWT secret**: ≥32 bytes, load từ env/KMS, KHÔNG hardcode. README có mục “Generate JWT_SECRET” (`openssl rand -base64 48`).
- **Cookie attrs**: `HttpOnly`, `Secure` (prod), `SameSite=Lax`, `Path=/`.
- **TOTP**: `totp.Validate` (period 30s, digits 6, skew ±1 step).
- **Network**: service chỉ nghe internal network khi deploy cùng Caddy (không expose public).
- **Redis image**: `redis:7+` để hỗ trợ `replicaof`.

## Decisions Log

- **JWT có `exp` claim / TTL 5h cho cookie:** **Rejected.** **Why:** User yêu cầu JWT không cần TTL — tính hợp lệ "hold bằng session". Session Redis (15 phút sliding) là nguồn sự thật duy nhất về thời gian sống; JWT chỉ mang danh tính (`sub`, `sid`), không tự hết hạn. Cookie dùng `COOKIE_MAX_AGE` cấu hình được (mặc định 0 = browser-session).
- **Endpoint dùng path ngắn (`/login`, `/auth`):** **Rejected.** **Why:** Dễ bị đè/chèn vào route của app thật phía sau proxy (authentik dùng path dài kiểu `/outpost.goauthentik.io/`). Đổi sang tiền tố `BASE_PATH` (mặc định `/forward-auth`) — đặt biệt hóa, tránh xung đột.
- **Gia hạn session đồng bộ (block response khi write master):** **Rejected.** **Why:** User yêu cầu write master có rate limit VÀ chạy trong goroutine, không block luồng chính. Đổi: handler đọc session (sync) → trả `200` ngay → goroutine dùng `context.Background()` kiểm tra `RENEW_INTERVAL` rồi `EXPIRE`/`HSET` nếu cần.
- **Read bắt buộc từ replica:** **Rejected.** **Why:** User yêu cầu nếu không có replica-read thì read từ master. Đổi: `REDIS_REPLICA_ADDR` tùy chọn; trường `reader` được inject = replica nếu có, ngược lại = master (1 client duy nhất).
- **Base path `/forward-auth`:** **Rejected.** **Why:** User chỉ định base path cụ thể `/com.auth.forward/` (reverse-DNS, tránh đè route app thật). Đổi `BASE_PATH` mặc định thành `/com.auth.forward`.
- **Login chỉ là JSON API:** **Rejected.** **Why:** User yêu cầu form đăng nhập dùng **htmx v4** + dùng design có sẵn từ open-design. Đổi: thêm trang HTML `GET /com.auth.forward/login` (render template), `POST` xử lý form htmx (`HX-Redirect` khi OK, swap lỗi vào `#form-msg` khi fail).
- **Tự thiết kế UI từ đầu:** **Rejected.** **Why:** User có design sẵn `simple-login-3.html` trong project open-design (`fcab6fd1…`). Dùng làm nguồn — giữ hệ theme sáng/tối tự động (`prefers-color-scheme`), tích hợp htmx v2 stable.
- **Field TOTP hiển thị dạng số rõ (không che):** **Rejected.** **Why:** User muốn giữ field dạng **password form** (che mã) vì đây là "hidden bảo mật". Đổi: giữ `type="password"` + nút eye-toggle như design gốc, chỉ đổi `name`/`autocomplete`/`pattern` cho TOTP (`autocomplete="one-time-code"`, chính xác 6 chữ số).
- **`return_to` không validate (open redirect):** **Rejected.** **Why:** Reviewer chỉ ra attacker dùng `?return_to=https://evil.com` lừa redirect. Đổi: thêm `safeReturnTo()` — chỉ path tương đối bắt đầu `/`, decode rồi check.
- **Không xử lý replica-lag sau login:** **Rejected.** **Why:** Vừa login xong, `/auth` đọc replica có thể chưa nhận session → loop login. Đổi: gọi `WAIT` sau khi tạo session (login chấp nhận chờ ≤500ms).
- **Định dạng `last_renewal` mơ hồ:** **Rejected.** **Why:** Serialize `time.Now()` tùy tiện gây parse sai khi tính `now - last_renewal`. Đổi: lưu **Unix giây int64**.
- **Tạo 2 client trỏ master khi không có replica:** **Rejected.** **Why:** Lãng phí 2 connection pool. Đổi: `reader = writer` (cùng `*redis.Client`).
- **Không giới hạn brute-force / replay TOTP:** **Rejected.** **Why:** TOTP 6 chữ số có thể bị thử nhanh; mã cũ có thể replay. Đổi: `LoginGuard` (rate-limit per username + `SET NX EX` chống replay).
- **htmx CDN không SRI / form phụ thuộc JS:** **Rejected.** **Why:** Rủi ro CDN inject; JS tắt thì form hỏng. Đổi: thêm SRI (`integrity`+`crossorigin`) + `<form action method>` thật (server trả `302` khi không phải htmx). Dùng htmx **v2 stable** thay vì v4 beta.
- **`/healthz` chỉ trả 200 tĩnh:** **Rejected.** **Why:** Load balancer không biết Redis chết. Đổi: ping Redis writer+reader → `200/503`.
