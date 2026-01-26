# Build stage
FROM golang:1.24.11 AS build
WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# Run stage
FROM gcr.io/distroless/base-debian12
WORKDIR /
COPY --from=build /app/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
