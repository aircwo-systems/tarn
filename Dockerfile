ARG TARN_BUILD_UI=false
ARG VERSION=0.1.0-dev

FROM golang:1.26-alpine AS go-builder

ARG VERSION

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/tarnstack/tarn/internal/cli.version=${VERSION}" \
    -o /tarn ./cmd/tarn

FROM oven/bun:1 AS ui-builder

ARG TARN_BUILD_UI

WORKDIR /workspace
COPY ui ./ui

RUN mkdir -p /out/ui && \
    if [ "$TARN_BUILD_UI" = "true" ]; then \
      cd /workspace/ui && bun install && bun run build && cp -R build/. /out/ui/; \
    fi

FROM alpine:3.19

ARG TARN_BUILD_UI=false

RUN apk add --no-cache docker-cli ca-certificates

COPY --from=go-builder /tarn /usr/local/bin/tarn
COPY --from=ui-builder /out/ui /opt/tarn/ui

ENV TARN_UI_DIR=/opt/tarn/ui
ENV TARN_UI_ENABLED=${TARN_BUILD_UI}

EXPOSE 4566

ENTRYPOINT ["tarn"]
CMD ["start"]
