#   Copyright 2018 Cruise LLC
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

PKG=github.com/cruise-automation/rbacsync
VERSION=$(shell git describe --match 'v[0-9]*' --dirty='.m' --always)
VERSION_TAG=$(VERSION:v%=%) # drop the v-prefix for docker images, per convention
REVISION=$(shell git rev-parse HEAD)$(shell if ! git diff --no-ext-diff --quiet --exit-code; then echo .m; fi)
COMMANDS=rbacsync
BINARIES=$(addprefix bin/,$(COMMANDS))
PACKAGES=$(shell go list ./... | grep -v /vendor/)
COVERPACKAGES=$(shell go list ./... | grep -v /vendor/ | grep -v /generated/ | grep -v /apis/ | tr '[:space:]' ,)

GO_LDFLAGS=-ldflags '-s -w -X $(PKG)/version.Version=$(VERSION) -X $(PKG)/version.Revision=$(REVISION) -X $(PKG)/version.Package=$(PKG) $(EXTRA_LDFLAGS)'
TESTFLAGS=-race -v
COVERFLAGS=-coverpkg=$(COVERPACKAGES) -covermode=atomic -coverprofile=coverage.out

.PHONY: all generate build test binaries clean FORCE vndr image deploy test-coverage .deps.coverage

all: binaries
FORCE: # force targets to run no matter what

generate:
	./hack/update-codegen.sh

build:
	go build ${GO_LDFLAGS} ${PACKAGES}

binaries: ${BINARIES}

bin/%: cmd/% FORCE
	go build ${GO_GCFLAGS} ${GO_BUILD_FLAGS} -o $@ ${GO_LDFLAGS} ${GO_TAGS}  ./$<

test:
	go test ${TESTFLAGS} ${PACKAGES} -logtostderr # for some reason, this flag only works on the end

test-coverage: .deps.coverage
	go test ${TESTFLAGS} ${COVERFLAGS} ${PACKAGES} -logtostderr # for some reason, this flag only works on the end
	gocov convert coverage.out | gocov-xml > coverage.xml
	go tool cover -html coverage.out -o coverage.html

.deps.coverage:
	# Installs coverage deps using go get. Should be replaced by something better.
	go get github.com/axw/gocov/...
	go get github.com/AlekSi/gocov-xml

image:
	docker build -t rbacsync:${VERSION_TAG} .

push: image
ifndef REGISTRY
	$(error REGISTRY must be set for push)
endif
	# Caller can define the registry they are pushing to.
	docker tag rbacsync:${VERSION_TAG} ${REGISTRY}rbacsync:${VERSION_TAG}
	docker push ${REGISTRY}rbacsync:${VERSION_TAG}

deploy: push
	kubectl apply -f deploy/
	kubectl set image deploy/rbacsync rbacsync=${REGISTRY}rbacsync:${VERSION_TAG}

vndr:
	vndr

clean:
	rm -rf bin/*

