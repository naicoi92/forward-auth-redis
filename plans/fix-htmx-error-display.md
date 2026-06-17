# Plan: Sửa lỗi HTMX không hiển thị error fragment (invalid csrf token / invalid credentials)

## Context (Bối cảnh)

**Vấn đề:** Khi đăng nhập thất bại, server trả `invalid csrf token` / `invalid credentials` nhưng HTMX **không in lỗi ra `#form-msg`**.

**Nguyên nhân gốc rễ:** Code hiện tại đang dùng **htmx v2** (`htmx.org@2.0.9` trong `internal/webui/assets/login.html`). Ở **v2, response 4xx/5xx KHÔNG được swap mặc định**. Server trả `403` (CSRF) / `422` (sai TOTP) / `429` (rate limit) → HTMX v2 bỏ qua swap → `#form-msg` rỗng → CSS `.form-msg { display: none }` giấu element → không thấy gì.

**Quyết định của user (đã chốt):** Dự án **phải dùng htmx v4** (đã ghi ở `PLAN.md` mục phiên bản, dòng 129 & decisions dòng 259). Tôi lần trước đã sai khi giữ v2 + đề xuất workaround. Lần này **nâng cấp đúng lên v4**.

**Tại sao v4 sửa luôn bug:** Trong **htmx 4.0, mọi response (kể cả 4xx/5xx) đều được swap mặc định** (chỉ `204`/`304` là không swap). Vậy chỉ cần dùng đúng v4 là error fragment `<span class="form-error">…</span>` tự được swap vào `#form-msg` và hiện ra — **không cần bất kỳ JS workaround nào**.

> ⚠️ **Lưu ý trạng thái phiên bản:** Trên npm/jsDelivr, tag `latest` = `2.0.10` (stable); **v4 hiện là `4.0.0-beta4`** (thuộc tag `next`), chưa ra stable. Plan này dùng `4.0.0-beta4` đúng yêu cầu của user. Khi v4 stable ra mắt, chỉ cần bump version + tính lại SRI hash (đã để sẵn bước riêng).

## Approach (Cách tiếp cận)

1. **Bump htmx v2.0.9 → v4** trong `login.html`: đổi URL CDN + tính lại **SRI hash** (pin version + integrity, theo đúng convention SRI đã có trong `PLAN.md`).
2. **Đổi tên attribute v4 breaking change:** `hx-disabled-elt="#submit-btn"` → `hx-disable="#submit-btn"` (xem migration "Renames and Removals": `hx-disabled-elt` giờ là `hx-disable`).
3. **KHÔNG** thêm `htmx:beforeSwap` workaround — v4 swap 4xx mặc định nên không cần. Toàn bộ login flow (`hx-post` / `hx-target` / `hx-swap` / `HX-Redirect` / `HX-Request`) đều tương thích v4, đã kiểm chứng:
   - Header `HX-Request` vẫn được gửi ở v4 → `login.go` check `r.Header.Get("HX-Request") == "true"` vẫn đúng.
   - `HX-Redirect` — v4 ghi "Unchanged" → redirect khi đăng nhập OK vẫn hoạt động.
   - Class `.htmx-request` (dùng cho CSS spinner) vẫn có ở v4 → spinner/fade CSS giữ nguyên.
   - `hx-post`/`hx-target`/`hx-swap` nằm trên chính `<form>` (trigger element) nên **không** dính rule "explicit inheritance" mới của v4.

4. **Sửa status code sai TOTP cho đúng ngữ nghĩa HTTP** (theo feedback user): trong `login.go`, nhánh "invalid credentials" đang trả `422 Unprocessable Entity` — 422 dành cho lỗi validation entity (VD: field sai định dạng). Thất bại đăng nhập là lỗi xác thực → mã chuẩn là **`401 Unauthorized`**. Đổi `http.StatusUnprocessableEntity` → `http.StatusUnauthorized`. Các mã khác **giữ nguyên** (đã đúng): CSRF fail `403`, rate-limit `429`, ParseForm fail `400`. Lưu ý: v4 swap mọi code nên `401` vẫn được hiển thị bình thường; ta **không** kèm header `WWW-Authenticate` (tránh trigger popup HTTP Basic auth trên trình duyệt).

## Files to modify

- `internal/webui/assets/login.html`:
  - Đổi `<script src="…htmx.org@2.0.9/dist/htmx.min.js" integrity="sha384-ESlCao…">` → `…htmx.org@4.0.0-beta4/dist/htmx.min.js` + SRI mới `sha384-aWZK1NtOs/aWb/+YZdTM8q2JkWEshlMc9mgZ189numT9bwFhyAyYEoO4nO/2dTXt`.
  - Đổi `hx-disabled-elt="#submit-btn"` → `hx-disable="#submit-btn"`.
- `internal/httpapi/login.go`:
  - Trong `loginSubmit`, nhánh `err != nil` (sau khi check `ErrTooManyAttempts`): đổi `status := http.StatusUnprocessableEntity` → `status := http.StatusUnauthorized`.

Các file khác **không đổi**: `error_fragment.html`, `template.go`, `login.js` (JS hiện chỉ có toggle password, không gọi htmx API → không cần migrate JS), CSP trong `router.go` (`script-src 'self' https://cdn.jsdelivr.net` vẫn đúng với jsDelivr + SRI).

## Reuse (code có sẵn, không cần tạo mới)

- Error fragment: `internal/webui/assets/error_fragment.html` (`<span class="form-error">{{.Error}}</span>`).
- Handler render fragment cho htmx: `renderLoginError` → `ExecuteErrorFragment` trong `internal/httpapi/login.go:93-100` (đã đúng, v4 sẽ swap nó).
- CSS `.form-msg:not(:empty) { display: block }` trong `login.html` đã đúng.

## Steps

- [ ] 1. `login.html`: cập nhật thẻ `<script>` htmx — URL `@4.0.0-beta4` + `integrity="sha384-aWZK1NtOs/aWb/+YZdTM8q2JkWEshlMc9mgZ189numT9bwFhyAyYEoO4nO/2dTXt"` (giữ `crossorigin="anonymous"`).
- [ ] 2. `login.html`: rename `hx-disabled-elt="#submit-btn"` → `hx-disable="#submit-btn"` trên `<form>`.
- [ ] 3. `login.go`: đổi status code nhánh "invalid credentials" từ `422` → `401` (`http.StatusUnauthorized`); giữ nguyên `403` (CSRF) và `429` (rate-limit).
- [ ] 4. (Tuỳ chọn) Chạy tool check migration: `npx htmx.org@next upgrade-check -- .` (cần Python 3) để xác nhận không còn code v2 nào sót.
- [ ] 5. `go build ./...` rồi chạy server; test thủ công (xem Verification).

## Verification

1. Mở `/login`, DevTools → Network: tải `htmx.min.js@4.0.0-beta4`, SRI pass (không lỗi console "integrity").
2. Nhập **sai mã TOTP** → response `401` + body `<span class="form-error">invalid credentials</span>` → **`#form-msg` hiện text đỏ** (v4 swap mọi code mặc định).
3. (Tuỳ chọn) Xoá cookie `fa_csrf` rồi submit → response `403` + `invalid csrf token` hiện trong `#form-msg`.
4. Nhập **đúng mã** → response `200` + header `HX-Redirect` → trình duyệt redirect về `return_to`.
5. Trong khi request đang chạy, nút Sign in vẫn hiện spinner (`htmx-request` class) → xác nhận `hx-disable` (v4) vô hiệu hoá nút đúng.

## Ngoài phạm vi đợt này (follow-up)

- **Bump lên v4 stable** khi phát hành chính thức (hiện `4.0.0-beta4`): chỉ cần đổi version trong URL + tính lại SRI (`curl … | openssl dgst -sha384 -binary | openssl base64 -A`).
- **CSRF retry loop** (edge case): khi CSRF fail thật, `renderLoginError` set cookie mới nhưng hidden field `csrf_token` vẫn giữ token cũ → submit lại tiếp tục fail. User đã quyết định **không xử lý** đợt này (luồng same-site/`SameSite=Strict` hiếm khi dính).

## Decisions Log

- **Rejected:** Giữ htmx v2 + thêm listener `htmx:beforeSwap` để ép swap response 4xx. **Why:** User đã yêu cầu dùng htmx v4 (xem `PLAN.md` dòng 129/259). Trong v4, 4xx/5xx được swap mặc định nên workaround là thừa và đi ngược yêu cầu phiên bản. Hơn nữa `htmx:beforeSwap` ở v4 đã đổi tên thành `htmx:before:swap` (dùng `detail.ctx` thay vì `detail.xhr`) — viết code v2-era là kỹ thuật sai.
- **Rejected:** Dùng extension `htmx-2-compat` để giữ hành vi v2. **Why:** Trái với mục tiêu dùng v4; chính hành vi mới của v4 (swap 4xx) mới là thứ sửa bug.
- **Rejected:** Giữ `422 Unprocessable Entity` cho "invalid credentials". **Why:** Theo feedback user ("điều chỉnh http response code cho đúng"): 422 dành cho lỗi validation entity (field sai định dạng), còn đăng nhập thất bại là lỗi xác thực → mã chuẩn là `401 Unauthorized`. CSRF `403` và rate-limit `429` đã đúng nên giữ nguyên.
