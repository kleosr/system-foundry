FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/foundry ./cmd/foundry \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orders-worker ./cmd/orders-worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/onboarding-worker ./cmd/onboarding-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/foundry"]
