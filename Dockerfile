FROM golang:1.24-alpine AS builder

ENV CGO_ENABLED=0

WORKDIR /src

COPY . .

RUN go build -o /app -ldflags "-s -w" .

FROM alpine:3.18 AS final

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app /app


