FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY . .
RUN GOARCH=arm64 CGO_ENABLED=0 GOOS=linux go build -o redis-external-dns cmd/controller/main.go

FROM alpine:3.18
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/redis-external-dns /usr/local/bin/
ENTRYPOINT ["redis-external-dns"] 