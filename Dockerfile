FROM golang:1.23-alpine AS builder

RUN apk add --no-cache \
    git                \
    make               \
    ca-certificates    \
    tzdata

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux \
    go build                 \
    -ldflags "-X tf-mirror/internal/common.BuildVersion=${VERSION} \
    -X tf-mirror/internal/common.Commit=${COMMIT}                  \
    -X tf-mirror/internal/common.BuildTime=${BUILD_TIME} -w -s"    \
    -a -installsuffix cgo \
    -o bin/tf-mirror ./cmd/tf-mirror

FROM alpine:3.22.1 AS app

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL maintainer="Aleksei Demidov" \
    org.opencontainers.image.title="Terraform Registry Mirror" \
    org.opencontainers.image.description="A Terraform registry mirror for caching provider packages" \
    org.opencontainers.image.version="${VERSION}" \
    org.opencontainers.image.revision="${COMMIT}" \
    org.opencontainers.image.created="${BUILD_TIME}" \
    org.opencontainers.image.source="https://github.com/kashtan404/tf-mirror" \
    org.opencontainers.image.documentation="https://github.com/kashtan404/tf-mirror/blob/main/README.md" \
    org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache  \
    ca-certificates     \
    tzdata              \
    wget                \
    curl             && \
    update-ca-certificates

RUN addgroup -g 1001 -S terraform && \
    adduser -D -S -s /bin/sh -u 1001 -G terraform terraform

RUN mkdir -p /data && \
    chown terraform:terraform /data && \
    chmod 755 /data

COPY --from=builder /app/bin/tf-mirror /usr/local/bin/

RUN chmod +x /usr/local/bin/tf-mirror

USER terraform

WORKDIR /home/terraform

VOLUME ["/data"]

EXPOSE 8080 8443

ENTRYPOINT ["/usr/local/bin/tf-mirror"]
