FROM golang:1.26-bookworm AS build

RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    && ARCH=$(dpkg --print-architecture) \
    && if [ "$ARCH" = "amd64" ]; then ARCH="x64"; fi \
    && curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.19/tailwindcss-linux-${ARCH}" -o /usr/local/bin/tailwindcss \
    && chmod +x /usr/local/bin/tailwindcss \
    && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN templ generate
RUN tailwindcss -i static/css/input.css -o static/css/output.css --minify
RUN CGO_ENABLED=1 go build -o /tiponero ./cmd/tiponero

FROM gcr.io/distroless/base-debian12

COPY --from=build /tiponero /tiponero

EXPOSE 8080
ENTRYPOINT ["/tiponero"]
