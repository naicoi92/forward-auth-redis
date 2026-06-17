# Plan: Làm lại giao diện lỗi đăng nhập theo design `simple-login-4.html`

## Context (Bối cảnh)

**Nguồn tham chiếu:** File `simple-login-4.html` (tiêu đề "Floating soft login — **with error state**") trong project Open Design *"Tôi muốn một trang đăng nhập đơn giản,"* (`fcab6fd1-...`). Bản thiết kế mới nhất tập trung đúng vào **trạng thái lỗi**, và vốn dùng form **username + password**.

**Trạng thái hiện tại của app (đã qua các plan trước):**

- `internal/webui/assets/login.html` đã dùng **htmx v4.0.0-beta4**, `hx-disable="#submit-btn"` → lỗi đăng nhập (401/403/429) **đã swap & hiện** được trong `#form-msg`.
- Ô thứ hai hiện là **"Authentication code"**: `type="password" name="code" pattern="[0-9]{6}" inputmode="numeric" maxlength="6" placeholder="123456" autocomplete="one-time-code"` → lộ rõ đây là TOTP.
- `internal/webui/assets/error_fragment.html` = `<span class="form-error">{{.Error}}</span>` — **chỉ là text đỏ trơn**, chưa có khung cảnh báo.
- Backend `login.go` đọc `r.PostFormValue("code")` → `svc.Login(ctx, username, code, returnTo)` → `auth.VerifyTOTP(secret, code)` (verify mã 6 số).
- `#form-msg` dùng `.form-msg:not(:empty){display:block}` để ẩn/hiện. Dark mode dùng `@media (prefers-color-scheme: dark)` (không có nút toggle).

**Hai vấn đề cần giải quyết:**

1. **UI lỗi** chưa đẹp: lỗi chỉ là dòng text đỏ — thiếu icon, nền/border cảnh báo, không phản hồi trên ô input.
2. **Ô nhập lộ tính chất TOTP** (label "Authentication code", ràng buộc 6 số numeric). User muốn đổi thành **ô Password thật sự** (gửi `password`, validate như password) — đồng thời **backend vẫn verify giá trị đó như TOTP code** (che giấu bản chất TOTP; khớp luôn với design `simple-login-4.html` vốn là username/password).

**Mục tiêu:** (a) Đưa UI lỗi từ design vào app, (b) **đổi ô nhập thành Password** ở cả frontend lẫn backend, **giữ nguyên kiến trúc htmx** và logic `VerifyTOTP` ở service layer.

## Phân tích design `simple-login-4.html` vs app

| Thành phần (design) | Có trong app? | Quyết định |
|---|---|---|
| Alert box `.login-error`: icon + `--error-bg` + border đỏ + padding + bo góc + `role="alert"` | ❌ (chỉ text đỏ) | **Làm theo design** |
| `@keyframes shake` (rung nhẹ khi lỗi) | ❌ | **Làm theo design** |
| `--error-bg` (biến màu nền lỗi, light + dark) | ❌ | **Thêm vào** (`:root` + `@media dark`) |
| Ô input `aria-invalid="true"` → viền đỏ + nền nhạt | ❌ | **Làm theo design** (qua JS htmx listener) — ✅ user đã chốt |
| Ô **Password** (`name="password"`, `placeholder="••••••••"`, `autocomplete="current-password"`, label "Password") | ❌ (app đang là TOTP code) | **Làm theo design** — ✅ user đã chốt đổi sang password |
| **Nút theme toggle** (data-theme manual) | ❌ (app dùng media query) | **Không mang** — ✅ user chọn giữ auto theo hệ thống |
| Client-side validation demo (min-length…) | ❌ | **Không mang** (ô password che giấu TOTP, validate server-side) |
| JS `attemptCount` demo chặn submit | ❌ | **Không mang** (dùng htmx) |

## Approach (Cách tiếp cận)

**A. Đổi ô nhập "Authentication code" → "Password" (frontend + backend)**

1. **`login.html`** — đổi ô thứ hai:
   - Label `Authentication code` → `Password`.
   - `name="code"` → `name="password"`; giữ `id="password"` (JS `login.js` đang reference `#password` → không phải đổi JS).
   - `autocomplete="one-time-code"` → `autocomplete="current-password"`.
   - **Bỏ** `inputmode="numeric"`, `pattern="[0-9]{6}"`, `maxlength="6"`.
   - `placeholder="123456"` → `placeholder="••••••••"`.
   - Header subtitle: `Enter your username and 6-digit authentication code.` → `Enter your username and password.` (giữ h1 "Sign in").
2. **`login.go`** — `loginSubmit`:
   - `code := r.PostFormValue("code")` → `password := r.PostFormValue("password")`.
   - Truyền `password` vào `svc.Login(r.Context(), username, password, returnTo)` — **logic verify TOTP ở service giữ nguyên** (`VerifyTOTP(secret, code)`), chỉ là giá trị "password" được verify như TOTP code. Đổi comment từ "verifies the TOTP code" → "verifies the submitted password as a TOTP code".
   - **Không đổi** `svc.Login` signature, `totp.go`, `store/` — verify logic y nguyên.

**B. Làm UI lỗi theo design (giữ `#form-msg` làm alert box container, ẩn khi rỗng qua `:not(:empty)` — khớp `hx-swap="innerHTML"` hiện có, không cần `.visible` class)**

1. **CSS** (`login.html`):
   - Thêm `--error-bg` vào `:root` (light) và `@media (prefers-color-scheme: dark)`.
   - Restyle `.form-msg`: `display:none` mặc định, `:not(:empty)` → `flex` (icon + text), nền `--error-bg`, border `color-mix(var(--error) 30%)`, padding `12px 14px`, bo `12px`, icon 18px, `align-items:flex-start`.
   - Thêm `@keyframes shake`.
   - Thêm `.field input[aria-invalid="true"]` (viền đỏ + nền nhạt) + `:focus` variant.
2. **Markup** (`login.html`):
   - `#form-msg`: `<div id="form-msg" class="form-msg" role="alert">{{if .Error}}<svg>icon</svg><span>{{.Error}}</span>{{end}}</div>` — dùng `{{if .Error}}` để cả đường render-server (non-htmx / load đầu) lẫn htmx swap đều ra **cùng nội dung** (icon + text).
3. **Error fragment** (`error_fragment.html`): `<svg>icon</svg><span>{{.Error}}</span>` — đúng nội dung bên trong alert box.
4. **JS** (`login.js`) — thêm vào (giữ nguyên logic toggle password cũ):
   - Listener `htmx:afterSwap` trên `#form-msg`: replay animation shake (force reflow) + set `aria-invalid="true"` cho `#username` và `#password`.
   - Listener `input` trên 2 ô: xoá `aria-invalid` khi user gõ lại.
5. **KHÔNG đổi**: `template.go` (signature không đổi, chỉ nội dung template đổi), router/CSP (inline SVG là markup, `style-src 'unsafe-inline'` + `script-src 'self'` cho login.js — đều OK; không thêm nguồn nào).

## Files to modify

- `internal/webui/assets/login.html` — (a) đổi ô code→password + subtitle; (b) CSS (thêm `--error-bg`, restyle `.form-msg` thành alert box, thêm shake + field invalid state); (c) markup `#form-msg`.
- `internal/webui/assets/error_fragment.html` — icon SVG + `<span>{{.Error}}</span>`.
- `internal/webui/assets/login.js` — thêm htmx afterSwap listener (shake + aria-invalid) + input clear.
- `internal/httpapi/login.go` — `loginSubmit`: đọc `r.PostFormValue("password")` thay vì `"code"`, truyền vào `svc.Login`; cập nhật comment.

## Reuse (code có sẵn)

- Luồng htmx lỗi đã đúng: `renderLoginError` → `ExecuteErrorFragment` (`login.go`), swap vào `#form-msg`.
- Verify TOTP ở service: `svc.Login` → `auth.VerifyTOTP(secret, code)` (`internal/auth/service.go:55`, `totp.go:10`) — **giữ nguyên**, chỉ đổi nguồn giá trị `code` từ field `password`.
- Biến màu `--error` đã có trong `login.html` (light + dark).
- Pattern `.form-msg:not(:empty)` đã có — tận dụng để ẩn/hiện, không cần `.visible` class như design.

## Verification

1. Mở `/login` lần đầu (không lỗi): **không thấy** khung cảnh báo; ô thứ hai là **"Password"** với placeholder `••••••••`, gõ hiển thị dạng chấm, **không** bị chặn 6 số.
2. Nhập username + **mã TOTP 6 số vào ô Password** → sai → response `401` + fragment `<svg/><span>invalid credentials</span>` → **khung cảnh báo hiện** (icon "!" + nền đỏ nhạt + border + **rung nhẹ**), `#password`/`#username` chuyển viền đỏ.
3. DevTools → Network → Payload: field gửi đi tên là **`password`** (không còn `code`).
4. Gõ lại vào ô → viền đỏ biến mất; box lỗi vẫn hiện tới khi submit lại.
5. Xoá cookie `fa_csrf` rồi submit → `403` + "invalid csrf token" trong cùng alert box.
6. Sai liên tục → `429` + "too many attempts" → box hiển thị đúng.
7. Nhập **đúng mã TOTP vào ô Password** → `200` + `HX-Redirect` → redirect về `return_to`.
8. (Non-htmx / tắt JS) submit sai → trang reload, `#form-msg` render-server vẫn ra **cùng alert box** nhờ `{{if .Error}}`.

## Ngoài phạm vi (follow-up)

- Nút **theme toggle** (nếu user muốn sau): thêm `data-theme` + JS localStorage + nút tròn góc phải.
- Bump htmx lên v4 stable khi phát hành chính thức (đổi version + tính lại SRI).

## Decisions Log

- **Rejected:** Giữ ô "Authentication code" 6 số numeric. **Why:** User feedback — không muốn lộ tính chất TOTP; muốn ô **Password** (gửi `password`, validate như password), backend verify giá trị đó như TOTP code. Việc này còn khớp hơn với design `simple-login-4.html` (vốn là username/password).
- **Rejected:** Thêm client-side validation min-length cho password (như demo trong design). **Why:** Ô password thực ra che giấu TOTP; validate server-side qua `VerifyTOTP` là đủ. Thêm min-length là logic demo thừa.
- **Rejected:** Đổi logic verify ở service layer (`svc.Login`/`totp.go`). **Why:** User chỉ yêu cầu "backend verify password input thành/như totp code" — tức vẫn là `VerifyTOTP`, chỉ đổi **nguồn giá trị** (đọc field `password`). Không cần sửa thuật toán TOTP.
- **Rejected:** Nút theme toggle từ design. **Why:** User chọn giữ dark/light auto theo hệ thống (`prefers-color-scheme`), không mở rộng scope ra ngoài phần lỗi.
