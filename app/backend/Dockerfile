FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY backend ./backend
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/backend ./backend

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/backend /backend
EXPOSE 8080
ENTRYPOINT ["/backend"]
