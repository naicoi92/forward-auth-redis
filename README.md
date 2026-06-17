# forward-auth-redis

A lightweight **forward auth** service written in Go that authenticates users via **TOTP** and stores sessions in **Redis**. Designed to sit behind **Caddy** `forward_auth`, it supports **master-write / replica-read** and includes an embedded login UI using **htmx v2**.

## Features

- TOTP-based login (username + 6-digit authenticator code).
- Session stored in Redis with 15-minute sliding TTL, renewed on each successful request.
- Stateless JWT cookie (HS256) with **no `exp` claim** — session lifetime is the source of truth.
- Redis master/replica support: writes go to master, reads go to replica, with automatic fallback to master when no replica is configured.
- Login brute-force and OTP-replay protection via Redis.
- Open-redirect protection for `return_to`.
- Embedded login page based on `simple-login-3.html` with automatic light/dark theme and htmx v2 form submission.
- Graceful degradation: the login form works without JavaScript.

## Generate JWT_SECRET

The service requires a strong `JWT_SECRET` (minimum 32 bytes). Generate one with:

```bash
openssl rand -base64 48
```

Then export it:

```bash
export JWT_SECRET="your-generated-secret"
```

## Quick start (Docker Compose)

The compose stack includes Redis master + replica, the auth service, Caddy, and a demo upstream.

```bash
cd deploy
export JWT_SECRET="$(openssl rand -base64 48)"
docker compose up -d
```

## Seed a user

```bash
cd deploy
export JWT_SECRET="your-generated-secret"
docker compose run --rm auth /seed alice
```

> Note: the seed binary is built into the auth image as `/seed`. You can also run it locally with Go:

```bash
export REDIS_MASTER_ADDR=localhost:6379
export JWT_SECRET="your-generated-secret"
go run ./cmd/seed alice
```

The command prints the TOTP secret and an `otpauth://` URI. Scan the URI with your authenticator app (or use `oathtool -b --totp <secret>` to generate test codes).

## Test the login

Open the login page in a browser:

```
http://localhost:8080/com.auth.forward/login
```

Enter the username and the current 6-digit TOTP code. On success you are redirected to `/` and the `fa_token` cookie is set.

You can also test with curl:

```bash
CODE=$(oathtool -b --totp "your-secret")
curl -i -c cookies.txt \
  -d "username=alice&code=${CODE}&return_to=/" \
  http://localhost:8080/com.auth.forward/login
```

Forward auth check:

```bash
curl -i -b cookies.txt http://localhost:8080/com.auth.forward/auth
# HTTP/1.1 200 OK
# X-Auth-User: alice
```

Protected route via Caddy:

```bash
curl -i -b cookies.txt http://localhost/app
# HTTP/1.1 200 OK
# "Hello from protected demo app"

# Without cookie:
curl -i http://localhost/app
# HTTP/1.1 302 Found
# Location: /com.auth.forward/login?return_to=%2Fapp
```

## Health check

```bash
curl http://localhost:8080/com.auth.forward/healthz
```

Returns `200 ok` if both Redis writer and reader are reachable, otherwise `503`.

## Local development (without Docker)

1. Start Redis master (and optionally a replica):
   ```bash
   redis-server --port 6379
   redis-server --port 6380 --replicaof localhost 6379
   ```
2. Copy `.env.example` to `.env` and fill in `JWT_SECRET`.
3. Seed a user: `go run ./cmd/seed alice`
4. Run the server: `go run ./cmd/server`

## Project layout

```
.
├── cmd/
│   ├── server/          # HTTP service entrypoint
│   └── seed/            # TOTP secret seed CLI
├── internal/
│   ├── config/          # Environment-based configuration
│   ├── redisx/          # Master + replica Redis clients
│   ├── store/           # TOTP, session, and login-guard stores
│   ├── auth/            # JWT, TOTP, and login/auth orchestration
│   ├── cookiex/         # Secure cookie builder
│   ├── webui/           # Embedded login HTML template (htmx v2)
│   └── httpapi/         # Chi HTTP handlers
├── deploy/
│   ├── Caddyfile        # Example Caddy forward_auth config
│   └── docker-compose.yml
├── Dockerfile
└── .env.example
```

## Security notes

- Keep `JWT_SECRET` secret and out of source control.
- Set `COOKIE_SECURE=true` in production (HTTPS only).
- The auth service should normally be reachable only inside the internal network / by the reverse proxy.
- All `return_to` redirect targets are validated as relative paths to prevent open redirects.
- Redis login-guard keys self-expire via TTL, so no manual cleanup is required.
