FROM golang:1.25-alpine AS build

RUN apk add --no-cache ca-certificates git
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/chatwoot-connector ./cmd/connector

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/chatwoot-connector /app/chatwoot-connector

ENV PORT=4100
EXPOSE 4100
USER 65532:65532
ENTRYPOINT ["/app/chatwoot-connector"]
