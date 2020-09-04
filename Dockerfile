FROM golang:alpine as build

RUN apk add git

WORKDIR /app
COPY go.mod go.sum /app/
RUN go mod download
COPY . .
RUN apk add alpine-sdk
RUN go build .

FROM alpine:latest

RUN apk --no-cache add ca-certificates
COPY --from=build /app/service-ready-exporter /bin/

ENTRYPOINT ["/bin/service-ready-exporter"]
EXPOSE     9150
