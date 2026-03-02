FROM --platform=$BUILDPLATFORM docker.io/golang:1.26 as build

WORKDIR /app
COPY go.mod /app/go.mod
RUN go mod download

COPY . /app

ENV GOCACHE=/root/.cache/go-build

ARG TARGETOS
ARG TARGETARCH
RUN echo "Building the binary for $TARGETOS/$TARGETARCH"
RUN --mount=type=cache,target="/root/.cache/go-build" \
    GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -o ./sgateway ./cmd/

FROM scratch

COPY --from=build /app/sgateway /sgateway
ENTRYPOINT ["/sgateway"]