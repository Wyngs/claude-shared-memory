FROM golang:1.25.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /coordinator ./cmd/coordinator
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 coordinator
USER coordinator
COPY --from=build /coordinator /coordinator
ENV LISTEN_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/coordinator"]
