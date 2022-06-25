FROM golang:alpine as builder

WORKDIR /build

COPY go.mod go.sum main.go .
RUN apk add gcc musl-dev && go build -o amcrest-go main.go

FROM alpine:latest

RUN apk add --no-cache tzdata

COPY --from=builder /build/amcrest-go /app/amcrest-go

CMD ["/app/amcrest-go"]