FROM golang:1.22.5-bookworm AS builder

ARG CGO_ENABLED=0

WORKDIR /tenant-controller
COPY ["go.mod", "go.sum", "./"]
RUN go mod download
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN go build -o bin/tenant-controller ./cmd

FROM gcr.io/distroless/static:nonroot as final

COPY --from=builder /tenant-controller/bin/ /usr/local/bin/

CMD ["/usr/local/bin/tenant-controller"]
