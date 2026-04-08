# syntax=docker/dockerfile:1.7

ARG BUILDPLATFORM=linux/amd64
FROM --platform=${BUILDPLATFORM} golang:1.22.12 AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY controllers ./controllers
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/data-protection-operator .

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /out/data-protection-operator /data-protection-operator

USER 65532:65532
ENTRYPOINT ["/data-protection-operator"]
