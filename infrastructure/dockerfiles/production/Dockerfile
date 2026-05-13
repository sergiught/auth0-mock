FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/auth0-mock ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/auth0-mock /usr/local/bin/auth0-mock
EXPOSE 8080 8443
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/auth0-mock"]
