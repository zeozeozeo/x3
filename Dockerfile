# Build Stage
FROM docker.io/library/golang:1.26-alpine AS builder
RUN apk add --no-cache build-base
WORKDIR /bot
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GO_BUILD_TAGS="goolm"
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -tags "$GO_BUILD_TAGS" -o /x3

# Final Stage
FROM alpine:3.23

RUN apk add --no-cache \
    exiftool \
    libgcc \
    libstdc++ \
    --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing/ \
    onnxruntime

WORKDIR /app

RUN find /usr/lib/ -name "libonnxruntime.so*" -type f -exec cp {} /app/libonnxruntime.so \;

COPY --from=builder /x3 /x3

CMD ["/x3"]
