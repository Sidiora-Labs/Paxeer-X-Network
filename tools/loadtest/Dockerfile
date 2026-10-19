FROM golang:1.24.5-bullseye AS build

WORKDIR /go/src/pax-load

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /go/bin/paxload ./

FROM gcr.io/distroless/base
COPY --from=build /go/bin/paxload /usr/bin/

ENTRYPOINT ["/usr/bin/paxload"]