FROM golang:1.25-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY servicebridge/backend/go.mod servicebridge/backend/go.sum ./
RUN go mod download
COPY servicebridge/backend ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/customer-service ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -g 10001 -S app \
  && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=build /out/customer-service /app/customer-service
RUN mkdir -p /uploads && chown -R app:app /app /uploads
USER 10001:10001
EXPOSE 8080
CMD ["/app/customer-service"]
