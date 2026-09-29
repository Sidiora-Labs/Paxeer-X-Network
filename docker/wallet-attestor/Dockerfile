FROM golang:1.25 AS build
WORKDIR /src
COPY . .
WORKDIR /src/human/wallet/attestor
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/attestor ./cmd/attestor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/attestor /usr/local/bin/attestor
USER nonroot:nonroot
EXPOSE 8443 9443
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/attestor"]
