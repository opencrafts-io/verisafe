FROM golang:1.21-alpine AS builder

ENV CGO_ENABLED=0

WORKDIR /src
COPY . .

RUN go build -o /app -ldflags "-s -w" ./cmd/main.go 

FROM scratch AS final

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
