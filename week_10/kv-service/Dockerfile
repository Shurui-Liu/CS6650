# --- build stage ---
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY *.go .
RUN go build -o kv-service .

# --- run stage ---
FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/kv-service .
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["./kv-service"]
