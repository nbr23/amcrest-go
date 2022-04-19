FROM golang:alpine as builder

COPY main.go .
RUN mkdir /build && go build -o /build/amcrest-go main.go

FROM alpine:latest

RUN apk add --no-cache tzdata

COPY --from=builder /build/amcrest-go /app/amcrest-go

CMD ["/app/amcrest-go"]