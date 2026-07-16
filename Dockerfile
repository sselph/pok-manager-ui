# Stage 1: Build the React frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build the Go backend
FROM golang:1.25-alpine AS backend-builder
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o pok-manager .

# Stage 3: Final runtime container
FROM alpine:3.19
# Install docker CLI, compose v2 plugin, and su-exec for privilege dropping
RUN apk add --no-cache docker-cli docker-cli-compose su-exec

WORKDIR /app

# Copy built manager binary and frontend assets
COPY --from=backend-builder /app/backend/pok-manager /app/
COPY --from=frontend-builder /app/frontend/dist /app/frontend/dist

# Copy default configurations and entrypoint script
COPY defaults/ /app/defaults/
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 8084

# Run the entrypoint script which handles UID/GID switching
ENTRYPOINT ["/app/entrypoint.sh", "-base-dir", "/app/workspace", "-port", "8084"]
