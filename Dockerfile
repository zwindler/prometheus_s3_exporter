###############################################################################
# Stage 1 (to create a "build" image, ~850MB)                                 #
###############################################################################
FROM golang:1.23 as builder

# smoke test to verify if golang is available
RUN go version

ARG PROJECT_VERSION

COPY . /go/src/github.com/zwindler/prometheus_s3_exporter/
WORKDIR /go/src/github.com/zwindler/prometheus_s3_exporter/

RUN set -Eeux && \
    go mod download && \
    go mod verify

RUN GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-w -s -X 'main.Version=${PROJECT_VERSION}'" \
    -o bin/prometheus_s3_exporter main.go

###############################################################################
# Stage 2 (to create a downsized "container executable", ~5MB)                #
###############################################################################

FROM scratch
WORKDIR /app/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/github.com/zwindler/prometheus_s3_exporter/bin/prometheus_s3_exporter /app/prometheus_s3_exporter
EXPOSE 9340
ENTRYPOINT ["/app/prometheus_s3_exporter"]