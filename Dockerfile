FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /picklock-mcp ./cmd/picklock-mcp

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /picklock-mcp /usr/local/bin/picklock-mcp
ENTRYPOINT ["picklock-mcp"]
