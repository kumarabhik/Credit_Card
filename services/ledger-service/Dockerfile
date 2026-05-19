FROM golang:1.26.3 AS build

WORKDIR /workspace

COPY go.mod go.sum go.work go.work.sum ./
RUN go mod download

COPY gen ./gen
COPY services/ledger-service ./services/ledger-service

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ledger-service ./services/ledger-service/cmd/server

FROM alpine:3.20

COPY --from=build /out/ledger-service /usr/local/bin/ledger-service

RUN apk add --no-cache ca-certificates=20260413-r0

EXPOSE 8083 9093

ENTRYPOINT ["/usr/local/bin/ledger-service"]
