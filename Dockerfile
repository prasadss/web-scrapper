# Stage 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/web-scrapper ./cmd/server

# Stage 2: Run
FROM alpine:3.19

RUN apk --no-cache add ca-certificates chromium font-noto

WORKDIR /app

COPY --from=builder /app/bin/web-scrapper .
COPY --from=builder /app/web/ ./web/

EXPOSE 8080

CMD ["./web-scrapper"]
