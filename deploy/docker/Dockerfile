FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/1panel-agent ./cmd/1panel-agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/1panel-agent /usr/local/bin/1panel-agent
ENTRYPOINT ["1panel-agent"]
