# DO NOT CHANGE. This file is being managed from a central repository
# To know more simply visit https://github.com/honestbank/.github/blob/main/docs/about.md

FROM golang:1.18 as builder
WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -tags bindata -a -installsuffix cgo -o hnst ./example/simple/main.go

FROM gcr.io/distroless/static-debian10

WORKDIR /app
# Copy the Pre-built binary file from the previous stage
COPY --from=builder --chown=nonroot:nonroot /app/hnst .

ARG VERSION
ENV APP__VERSION="${VERSION}"
USER nonroot

# Command to run the executable
CMD ["./hnst"]
