# syntax=docker/dockerfile:1

# ---------- Build stage ----------
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies first to leverage Docker layer caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/seed ./cmd/seed

# ---------- Runtime stage ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/server /server
COPY --from=builder /bin/seed /seed

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/server"]
