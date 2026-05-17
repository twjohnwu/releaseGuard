FROM golang:1.25-alpine AS build
WORKDIR /src
COPY main.go ./
RUN go mod init mockgitlab && CGO_ENABLED=0 go build -o /out/mockgitlab .

FROM alpine:3.20
COPY --from=build /out/mockgitlab /usr/local/bin/mockgitlab
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mockgitlab"]
