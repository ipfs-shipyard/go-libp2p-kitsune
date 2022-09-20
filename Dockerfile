FROM golang:1.19-alpine as builder
WORKDIR /app
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0
RUN apk --update add ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -o /kitsune

FROM scratch
COPY --from=builder /kitsune .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV GOLOG_LOG_LEVEL=info
ENTRYPOINT ["/kitsune"]
