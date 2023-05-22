FROM golang:1.20-alpine as builder

WORKDIR /build

COPY . .

RUN go mod download && go build -o app main.go

FROM scratch

COPY --from=builder /build/app .

EXPOSE 8080

CMD [ "./app","-c", "/etc/config.json" ]




