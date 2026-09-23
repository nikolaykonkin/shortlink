# Стадия 1: сборка статического бинарника
FROM golang:1.26-alpine AS builder

WORKDIR /app

# слой с зависимостями кэшируется отдельно от исходников — go mod download
# перезапускается только когда меняются go.mod/go.sum, а не при каждой правке кода
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 — бинарник статический, poэтому в рантайм-образе не нужны
# системные .so-библиотеки, с которыми он был бы иначе слинкован
RUN CGO_ENABLED=0 go build -o /app/shortlink ./cmd/api

# Стадия 2: минимальный образ для запуска
#
# alpine, а не scratch: приложению не нужен shell и на HTTPS оно не ходит само
# (редиректы отдает сервер, а не клиент, так что ca-certificates не нужны), но scratch
# не содержит /etc/passwd — USER nonroot без явного создания пользователя там не сработает.
# alpine дает непривилегированного пользователя из коробки ценой лишних мегабайт, что
# для учебного проекта приемлемо
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/shortlink /app/shortlink
COPY migrations/ /app/migrations/
RUN chmod -R a+r /app/migrations

USER nobody

EXPOSE 8080

CMD ["/app/shortlink"]
