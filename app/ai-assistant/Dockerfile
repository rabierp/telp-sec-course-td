FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY ai-assistant ./ai-assistant
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/ai-assistant ./ai-assistant

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/ai-assistant /ai-assistant
EXPOSE 8080
ENTRYPOINT ["/ai-assistant"]
