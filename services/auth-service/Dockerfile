FROM golang:1.22.12 AS build

WORKDIR /workspace

COPY go.mod go.sum go.work go.work.sum ./
RUN go mod download

COPY gen ./gen
COPY services/auth-service ./services/auth-service

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/auth-service ./services/auth-service/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/auth-service /usr/local/bin/auth-service

EXPOSE 8081 9091

ENTRYPOINT ["/usr/local/bin/auth-service"]
