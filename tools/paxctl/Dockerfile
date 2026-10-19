FROM docker.io/golang:1.26 as BUILD

WORKDIR /go/src/seictl

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /go/bin/seictl .

FROM gcr.io/distroless/static-debian12
COPY --from=BUILD /go/bin/seictl /usr/bin/

ENTRYPOINT ["/usr/bin/seictl"]