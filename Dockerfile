FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o loom-server ./cmd/loom-server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/loom-server /usr/local/bin/
RUN mkdir -p /data
EXPOSE 3000
ENTRYPOINT ["loom-server"]
CMD ["serve", "--port", "3000", "--data", "/data"]
