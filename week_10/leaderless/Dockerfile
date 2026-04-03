FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY *.go .
RUN go build -o leaderless .

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/leaderless .
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["./leaderless"]
