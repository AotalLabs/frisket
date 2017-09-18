FROM instrumentisto/glide

RUN mkdir -p /go/src/github.com/aotallabs/frisket/
WORKDIR /go/src/github.com/aotallabs/frisket/
COPY *.go glide.yaml /go/src/github.com/aotallabs/frisket/
RUN glide install
