FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/mana-example ./examples/full

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/mana-example /app/mana-example
COPY examples/full/client.html /app/client.html
EXPOSE 8080
ENTRYPOINT ["/app/mana-example"]
