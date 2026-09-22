# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /src

# The core has no third-party Go dependencies, so there is nothing to download;
# copying go.mod first still lets Docker cache the module metadata layer.
COPY go.mod ./
COPY . .

ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/sluice ./cmd/sluice

# ---- runtime stage ----
# Distroless static: no shell, no package manager, minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sluice /usr/local/bin/sluice
COPY config.example.yaml /etc/sluice/config.yaml
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/sluice"]
CMD ["serve", "--config", "/etc/sluice/config.yaml"]
