# Используем официальный образ Go для сборки
FROM golang:1.26.2-alpine AS builder

# Устанавливаем рабочую директорию внутри контейнера
WORKDIR /app

# Копируем файлы с модулями для скачивания зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем остальной исходный код
COPY . .

# Собираем статически связанный бинарный файл
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/...

# Используем минимальный образ для запуска
FROM alpine:latest

# Устанавливаем необходимые сертификаты для HTTPS запросов
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарный файл из этапа сборки
COPY --from=builder /app/main .

# документация, просто инфа для разработчика, необязательно
EXPOSE 8000

# Команда для запуска
CMD ["./main"]