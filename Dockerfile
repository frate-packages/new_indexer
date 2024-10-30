FROM golang:alpine as builder
WORKDIR /app
COPY go.mod go.sum ./ 
RUN apk add build-base
RUN go mod download
COPY . .
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" export CGO_ENABLED=1 && go build . 


FROM alpine
WORKDIR /app
RUN apk add sqlite-dev
COPY --from=builder /app/package-server /app 
COPY data.sql /app
CMD ["./package-server"]

