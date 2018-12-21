#!/usr/bin/env bash

set -eou pipefail

S3DIR=$(mktemp -d)

fakes3 server --port 4569 --root "${S3DIR}" &
FAKES3_PID="$!"

fake_sqs &
FAKESQS_PID="$!"

go test -v -race $(glide novendor)

kill -2 ${FAKES3_PID} ${FAKESQS_PID}
rm -rf "${S3DIR}"
