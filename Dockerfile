FROM golang:1.16.2-alpine
RUN apk update
RUN apk add openssl ca-certificates
WORKDIR /go/src/github.com/akkeris/logtrain
COPY . .
RUN go get -u golang.org/x/sys/...
RUN go build -o logtrain github.com/akkeris/logtrain/cmd/logtrain
RUN go build -o logtail github.com/akkeris/logtrain/cmd/logtail
CMD ./logtrain