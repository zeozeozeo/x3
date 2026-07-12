# Build Stage
FROM docker.io/library/golang:1.26-alpine AS builder
WORKDIR /bot
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GO_BUILD_TAGS="goolm"
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -tags "$GO_BUILD_TAGS" -o /x3

# Final Stage
FROM alpine

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
