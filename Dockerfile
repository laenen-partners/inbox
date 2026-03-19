FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /inboxui ./cmd/inboxui/

FROM alpine:3.21

COPY --from=builder /inboxui /inboxui

ENTRYPOINT ["/inboxui"]
