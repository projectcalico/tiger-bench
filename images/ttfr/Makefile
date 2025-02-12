all: image

REGISTRY?=quay.io
VERSION?=latest
ORG?=tigeradev
NAME?=ttfr

.PHONY: image
image: binary
	docker build -t $(REGISTRY)/$(ORG)/$(NAME):$(VERSION) .

.PHONY: binary
binary: pingo.go test
	CGO_ENABLED=0 go build -o bin/pingo .

.PHONY: test
test:
	go test -v .

push: image
	docker push $(REGISTRY)/$(ORG)/$(NAME):$(VERSION)

clean:
	-docker rmi $(REGISTRY)/$(ORG)/$(NAME):$(VERSION)
	-rm bin/pingo
