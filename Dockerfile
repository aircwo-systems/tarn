FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /openstack ./cmd/openstack

FROM alpine:3.19

RUN apk add --no-cache docker-cli ca-certificates

COPY --from=builder /openstack /usr/local/bin/openstack

EXPOSE 4566

ENTRYPOINT ["openstack"]
CMD ["start"]
