FROM golang:1.26-alpine
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o collector ./cmd/event-collector-price
CMD ["./collector"]
