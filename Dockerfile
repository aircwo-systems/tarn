ARG OPENSTACK_BUILD_UI=false

FROM golang:1.23-alpine AS go-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /openstack ./cmd/openstack

FROM oven/bun:1 AS ui-builder

ARG OPENSTACK_BUILD_UI

WORKDIR /workspace
COPY ui ./ui

RUN mkdir -p /out/ui && \
    if [ "$OPENSTACK_BUILD_UI" = "true" ]; then \
      cd /workspace/ui && bun install && bun run build && cp -R build/. /out/ui/; \
    fi

FROM alpine:3.19

ARG OPENSTACK_BUILD_UI=false

RUN apk add --no-cache docker-cli ca-certificates

COPY --from=go-builder /openstack /usr/local/bin/openstack
COPY --from=ui-builder /out/ui /opt/openstack/ui

ENV OPENSTACK_UI_DIR=/opt/openstack/ui
ENV OPENSTACK_UI_ENABLED=${OPENSTACK_BUILD_UI}

EXPOSE 4566

ENTRYPOINT ["openstack"]
CMD ["start"]
