FROM golang:1.20-alpine as builder

# ENV GO111MODULE on
# ENV GOOS linux
# ENV CGO_ENABLED 0
# ENV GOPROXY https://goproxy.cn,direct

WORKDIR /build

COPY . .

RUN go mod download && go build -o app main.go

FROM scratch

COPY --from=builder /build/app .

EXPOSE 8080

CMD [ "./app","-c", "/etc/app/config.json" ]