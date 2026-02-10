FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/splitter ./cmd/splitter

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/splitter /app/splitter
EXPOSE 8080
ENTRYPOINT ["/app/splitter"]
