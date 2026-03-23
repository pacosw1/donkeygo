# ── Build stage ────────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Replace "myapp" with your binary name and "cmd/server" with your main package path
ARG APP_NAME=myapp
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server

# ── Runtime stage ──────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata wget

# Non-root user for security
RUN adduser -D -u 1000 appuser

WORKDIR /app

COPY --from=builder /server .

# Copy APNs key if present (optional — for push notifications)
COPY AuthKey.p8* ./

USER appuser

ENV PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget --spider -q http://localhost:8080/health || exit 1

CMD ["./server"]
