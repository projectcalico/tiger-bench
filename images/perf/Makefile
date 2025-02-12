all: image

REGISTRY?=quay.io
VERSION?=latest
ORG?=tigeradev
NAME?=perf

.PHONY: image
image:
	docker build -t $(REGISTRY)/$(ORG)/$(NAME):$(VERSION) .

.PHONY: push
push: image
	docker push $(REGISTRY)/$(ORG)/$(NAME):$(VERSION)

clean:
	-docker rmi $(REGISTRY)/$(ORG)/$(NAME):$(VERSION)
