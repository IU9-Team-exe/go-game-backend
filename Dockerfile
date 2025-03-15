FROM golang:1.24

WORKDIR /TP-Game
COPY ./ ./
RUN go mod download
RUN GOOS=linux go build -o /docker-tpgame ./cmd/game/main.go

EXPOSE 8080
CMD ["/docker-tpgame"]
