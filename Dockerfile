FROM alpine:edge

RUN apk update && apk upgrade -a

RUN apk add --no-cache bash go coreutils ca-certificates

RUN go install github.com/nathants/libaws@latest && \
    mv /root/go/bin/libaws /usr/local/bin && \
    rm -rf /root/go
