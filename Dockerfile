# Build Stage
FROM docker.io/library/golang:1.25.6-alpine3.23 AS builder
RUN apk add --no-cache build-base
WORKDIR /bot
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -o /x3

# Final Stage
FROM alpine:3.23
RUN apk add --no-cache exiftool libgcc
COPY --from=builder /x3 /x3
CMD ["/x3"]
