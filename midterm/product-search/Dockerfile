FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN go build -o search-service .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/search-service .
EXPOSE 8080
CMD ["./search-service"]