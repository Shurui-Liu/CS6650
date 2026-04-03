FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY *.go .
RUN go build -o leader-follower .

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/leader-follower .
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["./leader-follower"]
