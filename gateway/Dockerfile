# ---- build stage ----
FROM golang:1.26-alpine AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go build -o /out/ctl ./cmd/ctl && go build -o /out/acceptance ./cmd/acceptance

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/ctl ./bin/ctl
COPY --from=builder /out/acceptance ./bin/acceptance
COPY migrations ./migrations
EXPOSE 8080
ENTRYPOINT ["./bin/ctl"]
CMD ["serve"]
