ARG TARGET_PLATFORM=linux
ARG TARGET_ARCH=amd64
FROM golang:alpine as builder
ARG TARGET_PLATFORM
ARG TARGET_ARCH

WORKDIR /build

COPY go.mod go.sum main.go ./
RUN apk add gcc musl-dev
RUN GOOS=${TARGET_PLATFORM} GOARCH=${TARGET_ARCH} go build -trimpath -o amcrest-go-${TARGET_ARCH} main.go

FROM --platform=${TARGET_PLATFORM}/${TARGET_ARCH} alpine:latest
ARG TARGET_PLATFORM
ARG TARGET_ARCH

RUN apk add --no-cache tzdata

COPY --from=builder /build/amcrest-go-${TARGET_ARCH} /app/amcrest-go

CMD ["/app/amcrest-go"]