FROM golang:1.26.2-alpine AS builder

# штука для go.mod и http сервера
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# копируем go.mod и go.sum для скачивания зависимостей
COPY go.mod go.sum ./
RUN go mod download

# остаток исходников
COPY . .

# сборка
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /notification-service ./cmd/notification

# запускаем минимальный образ
FROM alpine:latest

# ca-certificates для HTTPS запросов and tzdata для таймштампов в логах
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# копируем бинарник из билдера
COPY --from=builder /notification-service .

# порт микросервиса
EXPOSE 8005

# ехала
CMD ["./notification-service"]