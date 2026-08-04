FROM --platform=$BUILDPLATFORM node:22.12-alpine AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN apk add --no-cache git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=frontend /build/web/dist/. ./internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o icloud-hme .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/icloud-hme .
EXPOSE 8081
ENTRYPOINT ["/app/icloud-hme"]
