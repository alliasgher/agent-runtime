.PHONY: backend frontend dev install

# Run Go backend
backend:
	cd backend && go run ./cmd/server

# Run Next.js frontend in dev mode
frontend:
	cd frontend && npm run dev

# Install all dependencies
install:
	cd backend && go mod tidy
	cd frontend && npm install

# Build everything
build:
	cd backend && go build -o bin/server ./cmd/server
	cd frontend && npx next build
