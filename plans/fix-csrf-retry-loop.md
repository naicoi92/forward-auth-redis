# Plan: Sửa lỗi CSRF retry loop (invalid csrf token sau khi nhập sai mã)

## Context (Bối cảnh)

**Triệu chứng:** User nhập sai mã TOTP lần đầu → form hiển thị lỗi "invalid credentials" (đúng). Nhưng khi nhập lại mã và submit lần 2 → báo lỗi **`invalid csrf token`** (status 403), không đăng nhập được nữa.

**Nguyên nhân gốc rễ:** Trong `internal/httpapi/login.go`, hàm `renderLoginError` gọi **`h.cookie.SetCSRF(w)`** ở ngay đầu — tạo ra CSRF token cookie **mới (T2)** và ghi đè cookie cũ (T1). Tuy nhiên nhánh htmx chỉ trả về một **HTML fragment** (`<span class="form-error">…</span>`) được swap vào `#form-msg`, **không thay thế toàn bộ `<form>`**. Vì vậy hidden field `csrf_token` trong DOM vẫn giữ **token cũ (T1)**, trong khi cookie đã đổi sang **T2**.

**Luồng tái hiện bug:**

1. `GET /login` → `loginForm` sinh token **T1**: set cookie `fa_csrf=T1` + hidden field `csrf_token=T1`. ✅ khớp.
2. User nhập sai TOTP → submit (htmx) → `POST /login`:
   - `validateCSRF`: T1 (form) == T1 (cookie) → **OK**.
   - `svc.Login` fail (sai TOTP).
   - `renderLoginError` → **`SetCSRF` sinh token T2, set cookie `fa_csrf=T2`** ⚠️ + trả fragment "invalid credentials" swap vào `#form-msg`.
   - Lúc này: cookie = **T2**, nhưng hidden field trong DOM vẫn = **T1**.
3. User sửa mã, submit lại (cùng form cũ):
   - `validateCSRF`: T1 (form) vs T2 (cookie) → **KHÔNG khớp** → fail → "invalid csrf token" (403).

→ Đây chính là edge case "CSRF retry loop" đã được `plans/fix-htmx-error-display.md` dự đoán ở mục "Ngoài phạm vi" nhưng lúc đó user chọn **chưa xử lý**.

**Quyết định:** **Bỏ CSRF hoàn toàn.** CSRF double-submit cookie đang là nguồn bug phức tạp mà trong context app này không mang lại giá trị bảo mật đáng kể (xem Decisions Log). Việc này loại bỏ triệt để root cause và đơn giản hóa code.

## Approach (Cách tiếp cận)

**Bỏ toàn bộ cơ chế CSRF** khỏi cả 4 tầng: handler, cookie builder, template data, và HTML form.

**Vì sao bỏ CSRF là an toàn trong context này:**

- Đây là **forward auth service** dùng **TOTP** (mỗi user có secret riêng). Vector tấn công CSRF điển hình trên login form là "login CSRF" — attacker cố đăng nhập nạn nhân vào tài khoản của chính attacker. Ở đây attacker **không thể** đăng nhập thay victim vì cần TOTP code do Authenticator app của victim sinh ra.
- Login form không tạo state nhạy cảm trên server (không phải form đổi mật khẩu, chuyển tiền…). TOTP code là one-time, replay guard đã chặn dùng lại.
- Cookie auth (`fa_token`) dùng `SameSite=Lax` → đã chặn CSRF cross-site trên các request state-changing (POST).
- CSRF cookie double-submit hiện tại là **nguồn duy nhất** gây bug retry loop → bỏ nó = loại bỏ root cause + đơn giản hóa code, thay vì vá từng nhánh lỗi.

**Phạm vi xóa** (4 file):

| File | Xóa gì |
|---|---|
| `internal/httpapi/login.go` | `validateCSRF` method; nhánh check CSRF + `csrf` var trong `loginSubmit`; block `SetCSRF` + `CSRF` field trong `loginForm` và `renderLoginError` |
| `internal/cookiex/cookie.go` | `SetCSRF`, `ReadCSRF`, `ClearCSRF`, `csrfCookieName`, `randomToken`, import `randutil` |
| `internal/webui/template.go` | field `CSRF string` trong struct `LoginData` |
| `internal/webui/assets/login.html` | hidden field `csrf_token` (`{{if .CSRF}}…{{end}}`) |

> Lưu ý: `randomToken()` trong `cookie.go` (dùng `randutil.Hex`) **chỉ** phục vụ CSRF — session tạo ID trực tiếp qua `randutil.Hex(32)` (`internal/store/session.go:139`). Vậy `randomToken` và import `randutil` trong `cookiex` cũng xóa theo.

## Files to modify

- **`internal/httpapi/login.go`**:
  - `loginForm`: xóa `csrf, err := h.cookie.SetCSRF(w)` + nhánh `if err != nil { http.Error... 500; return }`; xóa field `CSRF: csrf` trong `LoginData` (set `BasePath`, `Error: ""`, `ReturnTo`).
  - `loginSubmit`: xóa `csrf := r.PostFormValue("csrf_token")` + toàn bộ block `if !h.validateCSRF(r, csrf) { ... }`.
  - `renderLoginError`: xóa block `csrf, err := h.cookie.SetCSRF(w)` + nhánh lỗi 500 ở đầu; xóa `CSRF: csrf` trong `LoginData`.
  - Xóa hàm `validateCSRF` hoàn toàn.
  - Kiểm tra import `crypto/subtle` — nếu chỉ `validateCSRF` dùng `subtle.ConstantTimeCompare` thì xóa import này (chỉ còn `errors` cho `errors.Is`).
- **`internal/cookiex/cookie.go`**:
  - Xóa `const csrfCookieName`, 3 method `SetCSRF`/`ReadCSRF`/`ClearCSRF`, hàm `randomToken`.
  - Xóa import `"github.com/naicoi92/forward-auth-redis/internal/randutil"`.
  - Giữ `Set`/`Clear`/`Read` (auth cookie) nguyên.
- **`internal/webui/template.go`**:
  - Xóa field `CSRF string` trong struct `LoginData` (giữ `BasePath`, `Error`, `ReturnTo`).
- **`internal/webui/assets/login.html`**:
  - Xóa block 3 dòng `{{if .CSRF}} <input type="hidden" name="csrf_token" value="{{.CSRF}}" /> {{end}}`.

## Reuse (code có sẵn)

- `h.cookie.Set` / `h.cookie.Read` / `h.cookie.Clear` (auth cookie) — `internal/cookiex/cookie.go`: giữ nguyên, không liên quan CSRF.
- `h.svc.Login` — `internal/auth/service.go`: logic xác thực TOTP + brute-force + replay guard không đổi.
- `h.templates.ExecuteErrorFragment` / `ExecuteLogin` — `internal/webui/template.go`: giữ nguyên (chỉ `LoginData` mất field CSRF).
- `randutil.Hex` — `internal/randutil/randutil.go`: vẫn dùng cho session ID (`store/session.go`); chỉ xóa usage trong `cookiex`.

## Steps

- [ ] 1. `internal/webui/assets/login.html`: xóa block hidden field `csrf_token` (`{{if .CSRF}}…{{end}}`).
- [ ] 2. `internal/webui/template.go`: xóa field `CSRF string` khỏi struct `LoginData`.
- [ ] 3. `internal/cookiex/cookie.go`: xóa `csrfCookieName`, `SetCSRF`, `ReadCSRF`, `ClearCSRF`, `randomToken`, và import `randutil`.
- [ ] 4. `internal/httpapi/login.go`:
  - `loginForm`: xóa block `SetCSRF` + nhánh lỗi; xóa `CSRF: csrf` trong `LoginData`.
  - `loginSubmit`: xóa `csrf` var + block check `validateCSRF`.
  - `renderLoginError`: xóa block `SetCSRF` + nhánh lỗi 500; xóa `CSRF: csrf`.
  - Xóa hàm `validateCSRF`.
  - Dọn import `crypto/subtle` nếu không còn dùng.
- [ ] 5. `go build ./...` — xác nhận compile sạch (không còn tham chiếu CSRF).
- [ ] 6. Test thủ công (xem Verification).

## Verification

1. `go build ./...` pass — không còn lỗi compile, không còn tham chiếu `csrf`/`CSRF` nào trong code.
2. Mở `GET {BASE_PATH}/login` → DevTools → Application > Cookies: **không còn** cookie `fa_csrf`. HTML view-source: **không còn** hidden field `csrf_token`.
3. **Nhập sai mã TOTP** → submit → response 401, fragment "invalid credentials" swap vào `#form-msg`. ✅
4. **Nhập đúng mã TOTP** (cùng form, không reload) → submit → response 200 + `HX-Redirect` → đăng nhập thành công. **Bug đã hết** — không còn "invalid csrf token".
5. **Lặp lại nhiều lần sai liên tiếp** → mỗi lần vẫn chỉ báo "invalid credentials" (401), KHÔNG bao giờ 403.
6. **Rate-limit (429)**: nhập sai 6 lần → 429 "too many attempts"; sau khi hết window submit lại → 401 hoặc login OK (không 403).
7. **Non-htmx (curl)**: `curl -i -c c.txt {BASE_PATH}/login` → `curl -i -b c.txt -d 'username=alice&code=wrong&return_to=/' {BASE_PATH}/login` → 401 (không cần `csrf_token`, không 403).

## Decisions Log

- **Rejected:** Giữ CSRF + sửa `renderLoginError` không rotate token (bỏ `SetCSRF`, giữ token ổn định) + redirect về `GET /login` khi CSRF fail. **Why:** Reviewer feedback: "đây là lỗi logic, không phải tính năng redirect" — approach redirect là band-aid che đậy lỗi logic chứ không phải fix đúng; đồng thời reviewer gợi ý "nếu cảm thấy không code được tốt thì bỏ csrf". CSRF double-submit trong context forward-auth + TOTP không mang lại giá trị bảo mật đáng kể (login CSRF không khả thi vì cần TOTP của victim, cookie `SameSite=Lax` đã chặn cross-site POST) nhưng lại là nguồn duy nhất gây bug → bỏ hẳn sạch hơn là vá từng nhánh.

- **Rejected:** Fix root cause bằng cách giữ CSRF nhưng đồng bộ token đúng (sinh 1 lần khi `GET /login`, không rotate khi re-render error). **Why:** Dù đúng về mặt logic, nó vẫn giữ nguyên lớp phức tạp (double-submit cookie + `validateCSRF` + hidden field + cookie Path sync) cho một cơ chế mà app này không thực sự cần. Bỏ CSRF triệt để giảm code, loại bỏ vĩnh viễn class bug retry-loop, và không mất lớp bảo mật nào có ý nghĩa ở đây.
