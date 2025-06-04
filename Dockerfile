FROM golang:alpine AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /build

COPY go.mod go.sum main.go ./
RUN apk add gcc musl-dev
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o amcrest-go-${TARGETOS}-${TARGETARCH} main.go

FROM --platform=${TARGETOS}/${TARGETARCH} alpine:latest
ARG TARGETARCH
ARG TARGETOS

RUN apk add --no-cache tzdata

COPY --from=builder /build/amcrest-go-${TARGETOS}-${TARGETARCH} /app/amcrest-go

CMD ["/app/amcrest-go"]