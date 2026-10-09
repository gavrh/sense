FROM golang:1.27 AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download all

COPY . .
RUN CGO_ENABLED=0 go build -o /out/sense ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/sense /sense
EXPOSE 2727
USER nonroot:nonroot
ENTRYPOINT ["/sense"]
