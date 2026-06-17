# Plan: Ẩn form-msg mặc định, Seed xuất QR code, Nới lỏng TOTP skew

## Context

Ba điều chỉnh nhỏ trên forward-auth-redis:

1. **`#form-msg` hiện rỗng khi load trang** — Template có whitespace (newline + spaces) nằm *ngoài* khối `{{if .Error}}...{{end}}`, khiến `<div id="form-msg">` chứa text-node whitespace. CSS `:empty` (Selectors Level 3) **không** match whitespace → `.form-msg:not(:empty)` luôn true → hộp lỗi (background, border) hiện ra dù không có lỗi.

2. **Seed CLI thiếu QR code** — `cmd/seed/main.go` đã sinh random secret và in secret + otpauth URI, nhưng chưa in QR code để quét bằng authenticator app.

3. **TOTP skew chưa tường minh** — `VerifyTOTP` gọi `totp.Validate` (default `Skew: 1` trong pquerna/otp) → **đã** cho phép 1 step trước. Nhưng code dùng hàm default không tường minh, không cấu hình được, và hàm `VerifyTOTPWithSkew` đã có sẵn nhưng không được dùng.

---

## Approach

### 1. Ẩn `#form-msg` mặc định

**File:** `internal/webui/assets/login.html`

Form dùng `hx-target="#form-msg"` + `hx-swap="innerHTML"` (dòng 338). htmx **cần `#form-msg` luôn tồn tại trong DOM** để swap fragment lỗi vào khi đăng nhập thất bại. Nếu bọc `{{if .Error}}` bao quanh cả `<div id="form-msg">`, khi không có lỗi (load trang lần đầu) thì div không render → htmx không có target → lỗi không hiển thị được.

→ Giữ div luôn render, chỉ loại bỏ whitespace nằm ngoài khối `{{if .Error}}...{{end}}` bên trong div:

```html
<!-- TRƯỚC (có whitespace ngoài {{if}}) -->
<div id="form-msg" class="form-msg" ...>
          {{if .Error}}<svg ...>...</svg><span>{{.Error}}</span>{{end}}
        </div>

<!-- SAU (không whitespace ngoài {{if}}) -->
<div id="form-msg" class="form-msg" ...>{{if .Error}}<svg ...>...</svg><span>{{.Error}}</span>{{end}}</div>
```

Khi `.Error` rỗng → `<div ...></div>` → `:empty` match → `display:none`. Khi có lỗi → nội dung có con `<svg>` + `<span>` → `display:flex`. Không cần đổi CSS.

**Cập nhật khi thực hiện:** Dù template đã render div rỗng, vẫn đổi CSS rule từ `:not(:empty)` sang `:has(> *)` để phòng trường hợp whitespace/text-node lọt vào div — selector này chỉ hiện khi có element con thực sự (svg/span/error fragment).

### 2. Seed: in QR code ASCII trong terminal

**File:** `cmd/seed/main.go`

- Dùng `key.Image(width, height)` (đã có trong pquerna/otp) để lấy `image.Image` QR code.
- Thêm hàm `printQR(img image.Image)` — duyệt pixel, gom 2 hàng/lệnh bằng Unicode half-blocks (`▀` `▄` `█` `␣`). Khoảng ~25 dòng code, **không thêm dependency mới**.
- Output hiện tại đã có: `Secret:` (stderr) + `URI:` (stdout). Thêm: in QR block-ASCII vào **stderr** (cùng channel với Secret — không trộn stdout pipe).
- Giữ flag `-secret` hiện có; khi không có flag thì random (đã đúng rồi).
- Khi người dùng truyền `-secret`, cũng build key và tạo QR từ URI đó.

### 3. Nới lỏng TOTP — tường minh + cấu hình được

**Files:** `internal/config/config.go`, `internal/auth/totp.go`, `internal/auth/service.go`

- Thêm field `TOTPSkew uint` vào `Config` (`env:"TOTP_SKEW" envDefault:"1"`).
- Đổi `VerifyTOTP(secret, code)` → nhận thêm tham số `skew uint`, gọi `totp.ValidateCustom` tường minh.
- `Service` truyền `s.cfg.TOTPSkew` khi verify.
- `Skew: 1` (default) = cho phép current ± 1 step = 30s trước + 30s sau → đúng yêu cầu "cho phép 1 step code trước đó".

---

## Files to modify

| File | Thay đổi |
|---|---|
| `internal/webui/assets/login.html` | Dời `{{if}}/{{end}}` sát tag; đổi CSS `.form-msg:not(:empty)` → `.form-msg:has(> *)` |
| `cmd/seed/main.go` | Thêm `printQR()` từ `key.Image()`, in QR ASCII ra stderr |
| `internal/config/config.go` | Thêm `TOTPSkew uint` field + valiđate `> 0` |
| `internal/auth/totp.go` | Đổi `VerifyTOTP` nhận `skew uint`, gọi `ValidateCustom` |
| `internal/auth/service.go` | Truyền `s.cfg.TOTPSkew` vào `VerifyTOTP` |
| `.env.example` | Thêm `TOTP_SKEW=1` |
| `README.md` | Cập nhật mô tả seed (QR code) + ghi chú `TOTP_SKEW` |

## Reuse

- `totp.ValidateCustom` + `otp.ValidateOpts` — đã có sẵn trong `internal/auth/totp.go:VerifyTOTPWithSkew`, refactor để gộp logic.
- `key.Image()` từ pquerna/otp — tạo QR image cho seed.
- `image.Image` interface — duyệt pixel cho ASCII renderer.

## Steps

- [ ] **1.** `login.html`: dời `{{if .Error}}` sát `>` và `{{end}}` sát `</div>` trong `#form-msg`
- [ ] **2.** `config.go`: thêm `TOTPSkew uint \`env:"TOTP_SKEW" envDefault:"1"\`` + validate `> 0`
- [ ] **3.** `totp.go`: gộp `VerifyTOTP` + `VerifyTOTPWithSkew` → một hàm `VerifyTOTP(secret, code string, skew uint)` dùng `ValidateCustom` tường minh
- [ ] **4.** `service.go`: đổi call site `VerifyTOTP(verifySecret, code)` → `VerifyTOTP(verifySecret, code, s.cfg.TOTPSkew)`
- [ ] **5.** `seed/main.go`: thêm `printQR(img)`, gọi sau khi in URI
- [ ] **6.** `.env.example`: thêm `TOTP_SKEW=1` (sau khối `# TOTP`)
- [ ] **7.** `README.md`: cập nhật mô tả seed (in QR) + thêm `TOTP_SKEW` vào danh sách env
- [ ] **8.** `go build ./...` + `go vet ./...` (không có test file hiện tại)

## Decisions Log

- **Rejected:** Bọc `{{if .Error}}` bao quanh cả `<div id="form-msg">` (render div chỉ khi có lỗi). **Why:** Form dùng `hx-target="#form-msg"` + `hx-swap="innerHTML"` — htmx cần `#form-msg` luôn tồn tại trong DOM để swap fragment lỗi vào. Nếu div không render khi không có lỗi, htmx không có target → lỗi đăng nhập không hiển thị. Giữ div luôn render, chỉ fix whitespace bên trong.

## Verification

1. **form-msg ẩn:**
   - Chạy server, mở `GET /com.auth.forward/login` → inspect `#form-msg` → `display: none` (không hiện hộp rỗng)
   - Nhập sai mã → hộp lỗi hiện với icon + text đỏ
2. **Seed QR:**
   - `go run ./cmd/seed testuser` → terminal in secret + QR ASCII (quét được bằng Google Authenticator)
   - `go run ./cmd/seed -secret=JBSWY3DPEHPK3PXP testuser2` → QR từ secret truyền vào
3. **TOTP skew:**
   - Seed user → đợi code hết hạn (chuyển sang code mới) → submit code cũ (1 step trước) → **đăng nhập thành công**
   - Submit code cũ hơn 2 step → **bị từ chối**
   - Đặt `TOTP_SKEW=2` → code 2 step trước cũng được chấp nhận
