FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN adduser -D -H appuser
WORKDIR /home/appuser
COPY --from=build /out/server /usr/local/bin/server
USER appuser
EXPOSE 4982
ENV PORT=4982 APP_ENV=production
ENTRYPOINT ["server"]
