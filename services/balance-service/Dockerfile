FROM golang:1.26.3 AS build

WORKDIR /workspace

COPY go.mod go.sum go.work go.work.sum ./
RUN go mod download

COPY gen ./gen
COPY services/balance-service ./services/balance-service

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/balance-service ./services/balance-service/cmd/server

FROM alpine:3.20

COPY --from=build /out/balance-service /usr/local/bin/balance-service

RUN apk add --no-cache ca-certificates=20260413-r0

EXPOSE 8082

ENTRYPOINT ["/usr/local/bin/balance-service"]
