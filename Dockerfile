FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN go build -o portfolio .

FROM alpine:latest
RUN apk add --no-cache openssh
WORKDIR /root
COPY --from=builder /build/portfolio .
RUN mkdir -p .ssh && \
    ssh-keygen -t ed25519 -f .ssh/term_info_ed25519 -N ""
EXPOSE 23234
CMD ["./portfolio"]