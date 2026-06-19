FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o portfolio .

FROM alpine:latest
# Run as a non-root user. wish generates the SSH host key at startup (or reads
# it from $SSH_HOST_KEY), so no openssh tooling is needed in the image.
RUN adduser -D -h /app app
WORKDIR /app
COPY --from=builder /build/portfolio .
RUN mkdir -p .ssh && chown -R app:app /app
USER app
EXPOSE 23234
CMD ["./portfolio"]
