FROM golang:1.22.12 AS build

WORKDIR /workspace

COPY go.mod go.sum go.work go.work.sum ./
RUN go mod download

COPY gen ./gen
COPY services/balance-service ./services/balance-service

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/balance-service ./services/balance-service/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/balance-service /usr/local/bin/balance-service

ENTRYPOINT ["/usr/local/bin/balance-service"]
