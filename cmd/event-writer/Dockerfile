FROM golang:1.26-alpine
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o event-writer ./cmd/event-writer
CMD ["./event-writer"]
