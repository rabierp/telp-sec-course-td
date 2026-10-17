FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY frontend ./frontend
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/frontend ./frontend

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/frontend /frontend
EXPOSE 8080
ENTRYPOINT ["/frontend"]
