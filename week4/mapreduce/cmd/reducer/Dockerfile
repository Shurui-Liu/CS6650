FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/reducer ./cmd/reducer

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/reducer /app/reducer
EXPOSE 8080
ENTRYPOINT ["/app/reducer"]
