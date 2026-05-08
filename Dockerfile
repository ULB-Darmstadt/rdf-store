ARG TZ=Europe/Berlin

FROM node:lts-alpine AS build-frontend-stage
WORKDIR /app
ADD ./frontend .
ADD .env .
RUN npm install && npm run build

FROM node:lts-alpine AS build-swagger-stage
WORKDIR /app
ADD ./swagger .
ADD .env .
RUN npm install && npm run build

FROM golang:1.24-alpine AS build-backend-stage
RUN apk update && apk add --no-cache git ca-certificates tzdata && update-ca-certificates
WORKDIR /app
COPY ./backend/go.mod ./backend/go.sum ./
# copy frontend
COPY --from=build-frontend-stage /app/dist /app/frontend/
COPY --from=build-swagger-stage /app/dist /app/swagger/
# pull in and verify dependencies
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download && go mod verify
ADD ./backend .
# production build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o rdf-store-backend .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o rdf-store-cli ./cli/main.go

FROM busybox:1.37
ARG TZ
COPY --from=build-backend-stage /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build-backend-stage /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app
COPY --from=build-backend-stage /app/rdf-store-backend .
COPY --from=build-backend-stage /app/rdf-store-cli .
ENV TZ=$TZ
ENV PATH=$PATH:/app
EXPOSE 3000
CMD ["./rdf-store-backend"] 
