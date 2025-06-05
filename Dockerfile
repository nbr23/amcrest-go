FROM --platform=${BUILDOS}/${BUILDARCH} golang:alpine AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /build

COPY go.mod go.sum main.go ./
RUN apk add gcc musl-dev
RUN GOOS=linux GOARCH=arm64 go build -trimpath -o amcrest-go-linux-arm64 main.go
RUN GOOS=linux GOARCH=amd64 go build -trimpath -o amcrest-go-linux-amd64 main.go

FROM --platform=${TARGETOS}/${TARGETARCH} alpine:latest
ARG TARGETARCH
ARG TARGETOS

RUN apk add --no-cache tzdata

COPY --from=builder /build/amcrest-go-${TARGETOS}-${TARGETARCH} /app/amcrest-go

CMD ["/app/amcrest-go"]