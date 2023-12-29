FROM golang:1.21-alpine as builder

RUN apk add --no-cache make gcc musl-dev linux-headers git

WORKDIR /src/
COPY . .

ARG GOPROXY
RUN go mod download
RUN make



FROM alpine:latest

RUN apk add --no-cache ca-certificates

ADD https://git.io/GeoLite2-City.mmdb /city.mmdb
COPY --from=builder /src/build/apiserver /

EXPOSE 8080

ENTRYPOINT ["/apiserver"]
