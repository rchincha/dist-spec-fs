FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/saor .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/saor /usr/local/bin/saor
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/saor", "--root", "/data", "--port", "8080"]
