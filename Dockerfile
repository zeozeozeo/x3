FROM docker.io/library/golang:1.23.3-alpine3.20

RUN apk add build-base

WORKDIR /bot

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /x3

CMD ["/x3"]
