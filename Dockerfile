FROM node:lts-alpine AS frontend-builder

COPY ./ui /app

WORKDIR /app

RUN npm ci
RUN NODE_ENV=production npm run build



FROM golang:alpine AS backend-builder

WORKDIR /go/src/github.com/dimuls/eapteka

COPY . .
COPY --from=frontend-builder /app/dist ./ui/dist

RUN go install ./cmd/eapteka
RUN go install ./cmd/eapteka-data-loader



FROM alpine

COPY --from=backend-builder /go/bin/eapteka-data-loader /usr/bin/eapteka-data-loader
COPY --from=backend-builder /go/bin/eapteka /usr/sbin/eapteka

ENTRYPOINT ["/usr/sbin/eapteka"]