# syntax=docker/dockerfile:1
ARG GO_BASE=docker.m.daocloud.io/library/golang:1.26.3-bookworm
ARG ALPINE_BASE=docker.m.daocloud.io/library/alpine:3.20

FROM ${GO_BASE} AS builder
ENV GOTOOLCHAIN=local
ENV CGO_ENABLED=0
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/task030-dotenv .

FROM ${ALPINE_BASE}
WORKDIR /app
COPY --from=builder /out/task030-dotenv /app/task030-dotenv
EXPOSE 8080
ENTRYPOINT ["/app/task030-dotenv"]
CMD ["server", "--addr", ":8080"]
