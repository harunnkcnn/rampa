FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/harunnkcnn/rampa/app.Version=${VERSION}" -o /out/rampa ./cmd/rampa

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/rampa /rampa
VOLUME /data
ENV RAMPA_ADDR=0.0.0.0:8080 RAMPA_DB=/data/rampa.db
EXPOSE 8080
ENTRYPOINT ["/rampa"]
