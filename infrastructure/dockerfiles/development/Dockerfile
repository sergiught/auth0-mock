FROM golang:1.26-alpine

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8080 8443

CMD ["go", "run", "./cmd/api"]
