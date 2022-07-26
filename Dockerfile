FROM golang:1.15.6-alpine as builder
RUN apk update
RUN apk add openssl ca-certificates
WORKDIR /go/src/github.com/akkeris/logtrain
ENV GO111MODULE=on

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go get -u golang.org/x/sys/...
RUN go build -o logtrain github.com/akkeris/logtrain/cmd/logtrain
RUN go build -o logtail github.com/akkeris/logtrain/cmd/logtail

FROM alpine:latest

WORKDIR /logtrain
COPY --from=builder /go/src/github.com/akkeris/logtrain/logtrain ./logtrain
COPY --from=builder /go/src/github.com/akkeris/logtrain/logtail ./logtail

CMD ./logtrain