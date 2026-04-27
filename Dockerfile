# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# BUILD_TARGET controls which binary is compiled.
# Accepted values: cmd  |  cmd/gateway  |  cmd/live
ARG BUILD_TARGET=cmd

WORKDIR /app

# Copy the local replace dependency first so go mod download can resolve it
COPY soft-serve/ ../soft-serve/

COPY backend-api/go.mod backend-api/go.sum ./
RUN go mod download

COPY backend-api/ .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/service ./${BUILD_TARGET}

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/service /bin/service

ENTRYPOINT ["/bin/service"]
