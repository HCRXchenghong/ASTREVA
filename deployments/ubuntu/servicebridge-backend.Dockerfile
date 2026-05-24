FROM golang:1.25-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY servicebridge/backend/go.mod servicebridge/backend/go.sum ./
RUN go mod download
COPY servicebridge/backend ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/customer-service ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/customer-service /app/customer-service
EXPOSE 8080
CMD ["/app/customer-service"]
