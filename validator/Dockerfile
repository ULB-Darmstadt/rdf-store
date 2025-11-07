FROM node:lts-alpine
ARG USER=user
ARG UID=10001
RUN adduser --disabled-password --gecos "" --home "/nonexistent" --shell "/sbin/nologin" --no-create-home --uid "$UID" "$USER"
WORKDIR /app
ADD package.json package-lock.json server.ts validator.ts ./
RUN npm i
USER $USER:$USER
EXPOSE 8000
ENTRYPOINT ["npm", "run", "start"]
