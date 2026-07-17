FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/dist-spec-fs .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/dist-spec-fs /usr/local/bin/dist-spec-fs
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dist-spec-fs", "--root", "/data", "--port", "8080"]
