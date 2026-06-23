# Local Firebase Auth emulator for the dev stack and the integration suite.
#
# There is no official Firebase emulator image, so we build a small one: Node (for
# firebase-tools) plus a JRE (the emulators are Java). It runs ONLY the Auth
# emulator, offline, under the demo project id "demo-qlab" — no real Firebase
# credentials are ever involved. Local-only, like the rest of docker-compose.yml.
FROM node:20-bookworm-slim

# default-jre-headless: the emulators require Java. curl: the healthcheck.
RUN apt-get update \
    && apt-get install -y --no-install-recommends default-jre-headless curl \
    && rm -rf /var/lib/apt/lists/*

# Pin firebase-tools so the emulator behaviour is reproducible.
RUN npm install -g firebase-tools@13.29.1

WORKDIR /emulator
# firebase.json carries the emulators block (host/port); copied at build time so the
# image is self-contained.
COPY firebase.json ./

EXPOSE 9099

# --project demo-qlab keeps it fully offline (the demo- prefix signals no real
# project). Bind host comes from firebase.json (0.0.0.0) so it's reachable from the
# host and sibling containers.
CMD ["firebase", "emulators:start", "--only", "auth", "--project", "demo-qlab"]
