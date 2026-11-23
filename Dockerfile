#
# docker build . -t ast-metrics/ast-metrics:latest
# docker run -it --rm ast-metrics/ast-metrics:latest sh
# docker run -it -v .:/src  --rm ast-metrics/ast-metrics:latest ast-metrics analyze --report-html=/src/report /src
#
FROM golang:1.27-alpine AS builder

# Install packages
RUN apk --update --no-cache add \
        build-base \
        make \
        curl \
        git \
# Remove alpine cache
    && rm -rf /var/cache/apk/*

WORKDIR /usr/app

COPY . /usr/app

RUN make build
FROM alpine:latest

WORKDIR /

COPY --from=builder /usr/app/bin/ast-metrics /usr/local/bin/ast-metrics

RUN chmod +x /usr/local/bin/ast-metrics
RUN rm -rf /usr/app

CMD ["ast-metrics", "--version"]
