# ---- build stage ----
FROM golang:1.23-alpine AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux
COPY . .
RUN go mod download
RUN go build -o /out/ctl ./cmd/ctl

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/ctl ./bin/ctl
COPY migrations ./migrations
EXPOSE 8080
ENTRYPOINT ["./bin/ctl"]
CMD ["serve"]
