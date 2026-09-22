# Compile stage
FROM golang:1.26 AS build-env

ADD . /dockerdev
WORKDIR /dockerdev

RUN CGO_ENABLED=0 go build -o /heyemoji

# Final stage
FROM gcr.io/distroless/base-debian12

WORKDIR /

# Copy app executable from builder container
COPY --from=build-env /heyemoji /

CMD ["/heyemoji"]
