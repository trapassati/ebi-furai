# syntax=docker/dockerfile:1
# Ebi Furai: a single static binary (pure Go, no CGO) on a distroless image.
# Builds for the platform asked (docker buildx: linux/amd64, linux/arm64)
# while compiling on the machine's own, so it is fast everywhere.
#
# web/static/app.css is pre-compiled with Tailwind CSS and kept in the
# repository: the Tailwind CLI is only needed when the styles change.

# ---- 1. GSAP from npm (free to use, not open source: not in the repository) ----
FROM --platform=$BUILDPLATFORM alpine:latest AS vendor
ARG GSAP_VERSION=3.15.1
ARG GSAP_SHA256=3d1bea374ab1b703cc5c4105358eec63c4465efdc573931dc116e34a0a190c1b
RUN wget -qO /tmp/gsap.tgz "https://registry.npmjs.org/gsap/-/gsap-${GSAP_VERSION}.tgz" \
 && tar -xzf /tmp/gsap.tgz -C /tmp package/dist/gsap.min.js \
 && echo "${GSAP_SHA256}  /tmp/package/dist/gsap.min.js" | sha256sum -c - \
 && mv /tmp/package/dist/gsap.min.js "/gsap-${GSAP_VERSION}.min.js"

# ---- 2. Go build, always with the latest stable Go ----
FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
# Build with the Go of the image, never download another toolchain;
# baseline x86-64 so it runs on old CPUs too.
ENV GOTOOLCHAIN=local GOAMD64=v1 CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=vendor /gsap-*.min.js web/static/vendor/
# go.sum is completed here when missing (verified against sum.golang.org).
RUN go mod tidy \
 && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ebi-furai ./cmd/ebi-furai \
 && sh scripts/collect-licenses.sh /out/licenses \
 && cp LICENSE NOTICE /out/licenses/ \
 && mkdir -p /out/data

# ---- 3. Runtime: distroless, non-root (uid 65532) ----
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/ebi-furai /ebi-furai
COPY --from=build /out/licenses /licenses
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV DATA_DIR=/data LISTEN_ADDR=:9099 EBI_FURAI_INSTALL=docker
EXPOSE 9099
VOLUME /data
USER nonroot:nonroot
HEALTHCHECK --interval=60s --timeout=5s --start-period=20s --retries=3 CMD ["/ebi-furai", "healthcheck"]
ENTRYPOINT ["/ebi-furai"]
CMD ["serve"]
