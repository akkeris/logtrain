FROM golang:1.14-alpine
RUN apk update
RUN apk add openssl ca-certificates
WORKDIR /go/src/github.com/akkeris/logtrain
COPY . .
RUN sysctl fs.inotify.max_user_instances=2048
RUN go build -o logtrain github.com/akkeris/logtrain/cmd/logtrain
CMD ./logtrain