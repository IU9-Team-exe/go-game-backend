FROM golang:1.24-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Выполняем билд явно из директории cmd и кладём бинарник в корень приложения
RUN go build -o backend ./cmd

# Запускаем бинарник после запуска контейнера
CMD ["./backend"]
