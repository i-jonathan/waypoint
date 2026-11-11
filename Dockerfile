# Builds the web UI and the binary from source, then ships only the binary.
#   docker build -t waypoint .

FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json web/
RUN cd web && npm ci
COPY web web
# vite writes the UI into internal/http/dist, which the Go binary embeds.
COPY internal/http/dist internal/http/dist
RUN cd web && npm run build

FROM golang:1.27-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/http/dist internal/http/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X waypoint/internal/cli.Version=${VERSION}" \
    -o /out/waypoint ./cmd/waypoint

FROM alpine:3.21
RUN adduser -D -u 1000 waypoint
COPY --from=go /out/waypoint /usr/local/bin/waypoint
USER waypoint
ENV HOME=/home/waypoint WAYPOINT_BIND=0.0.0.0
EXPOSE 4747
ENTRYPOINT ["waypoint"]
CMD ["serve", "--no-open"]
