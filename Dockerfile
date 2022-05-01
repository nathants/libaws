FROM alpine:edge

RUN apk update && apk upgrade -a

RUN apk add --no-cache bash go coreutils ca-certificates

RUN go install github.com/nathants/cli-aws@latest && \
    mv /root/go/bin/cli-aws /usr/local/bin && \
    rm -rf /root/go
