# Build Stage
FROM docker.io/library/golang:1.25.6-alpine3.23 AS builder
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

# 1. Install dependencies + the Alpine-native ONNX runtime from edge repositories
RUN apk add --no-cache \
    exiftool \
    libgcc \
    libstdc++ \
    --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing/ \
    onnxruntime

# 2. Create the /app directory and set up the library
WORKDIR /app
COPY --from=builder /x3 /x3

# 3. Create a symlink or copy the library to the specific path your Go app expects
# Alpine installs it to /usr/lib/libonnxruntime.so.X.X.X
# We ensure a copy exists at /app/libonnxruntime.so to satisfy your error message
RUN cp /usr/lib/libonnxruntime.so /app/libonnxruntime.so

CMD ["/x3"]
