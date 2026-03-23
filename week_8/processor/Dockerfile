# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o processor .

# ── Run stage ─────────────────────────────────────────────────────────────────
FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/processor .

CMD ["./processor"]