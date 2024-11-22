FROM golang:1.23.3-alpine3.20

WORKDIR /bot

RUN go mod download
RUN go mod verify

RUN GOOS=linux go build -o /x3

CMD ["/x3"]
