#   Copyright 2018 GM Cruise LLC

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

FROM golang:1.11-alpine as build
RUN apk --no-cache add ca-certificates make git gcc libc-dev
WORKDIR /go/src/github.com/cruise-automation/rbacsync
ADD . /go/src/github.com/cruise-automation/rbacsync

RUN make

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=build /go/src/github.com/cruise-automation/rbacsync/bin /bin

ENTRYPOINT ["/bin/rbacsync"]
