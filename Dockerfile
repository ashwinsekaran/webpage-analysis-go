FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/webpage-analysis-go ./main.go

FROM alpine:3.20
WORKDIR /app

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /out/webpage-analysis-go /app/webpage-analysis-go
COPY templates /app/templates
COPY static /app/static

ENV HTTP_LISTEN_ADDRESS=:8080
EXPOSE 8080

USER appuser
ENTRYPOINT ["/app/webpage-analysis-go"]
