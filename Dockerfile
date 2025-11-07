ARG TZ=Europe/Berlin

FROM node:lts-alpine AS build-frontend-stage
WORKDIR /app
ADD ./frontend .
RUN npm install && npm run build

FROM golang:1.24-alpine AS build-backend-stage
RUN apk update && apk add --no-cache git ca-certificates tzdata && update-ca-certificates
WORKDIR /app
ADD ./backend .
# copy frontend
COPY --from=build-frontend-stage /app/dist /app/frontend/
# pull in and verify dependencies
RUN go mod download && go mod verify
# production build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o main .

FROM scratch
ARG TZ
# COPY --from=build-backend-stage /etc/passwd /etc/
# COPY --from=build-backend-stage /etc/group /etc/
COPY --from=build-backend-stage /usr/share/zoneinfo /usr/share/zoneinfo
# copy ca certificates -> otherwise go panics when trying to make https requests
COPY --from=build-backend-stage /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app
COPY --from=build-backend-stage /app/main .
ENV TZ=$TZ
EXPOSE 3000
CMD ["./main"] 
