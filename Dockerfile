# syntax=docker/dockerfile:1
FROM node:24-alpine AS web-build
WORKDIR /src/shiftory-web
COPY shiftory-web/package.json shiftory-web/pnpm-lock.yaml* ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY shiftory-web/ ./
RUN pnpm build-only
FROM golang:1.26.8-alpine AS api-build
WORKDIR /src/shiftory-server
COPY shiftory-server/go.mod shiftory-server/go.sum ./
RUN go mod download
COPY shiftory-server/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/shiftory-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/shiftory-migrate ./cmd/migrate
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=api-build /out/shiftory-api /app/shiftory-api
COPY --from=api-build /out/shiftory-migrate /app/shiftory-migrate
COPY --from=web-build /src/shiftory-web/dist /app/web
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
COPY config/production.example.yaml /app/config/production.yaml
RUN mkdir -p /app/var /app/uploads
ENV SHIFTORY_SERVER_PORT=8080 SHIFTORY_SERVER_WEB_DIR=/app/web SHIFTORY_STORAGE_UPLOAD_DIR=/app/uploads SHIFTORY_JWT_PRIVATE_KEY_FILE=/app/var/jwt-private.pem SHIFTORY_JWT_PUBLIC_KEY_FILE=/app/var/jwt-public.pem
EXPOSE 8080
VOLUME ["/app/uploads", "/app/var"]
RUN chmod +x /app/docker-entrypoint.sh
ENTRYPOINT ["/app/docker-entrypoint.sh"]
