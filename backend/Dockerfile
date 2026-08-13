FROM golang:1.25-bookworm AS builder
WORKDIR /app
# Build context is this backend/ directory (Render rootDir: backend).
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o bin/server ./cmd/server/

FROM debian:bookworm-slim
# python3 is required by the run_python tool, which shells out to it.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates python3 && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/bin/server .
EXPOSE 8080
CMD ["./server"]
