#FROM node:lts-alpine AS frontend-builder
#
#COPY ./ui /app
#
#WORKDIR /app
#
#RUN npm ci
#RUN npm run build



FROM golang:alpine AS backend-builder

WORKDIR /go/src/github.com/dimuls/eapteka

COPY . .
#COPY --from=frontend-builder /app/build ./ui/build

#RUN git submodule update --recursive --remote

RUN go install .



FROM alpine

COPY --from=backend-builder /go/bin/eapteka /usr/sbin/eapteka

ENTRYPOINT ["/usr/sbin/eapteka"]