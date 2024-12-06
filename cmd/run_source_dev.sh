#!/bin/sh

go run main.go \
  --log-level="debug" \
  --listen-address=":5000" \
  --source-repo="http://slice:3000/bredtape/source.git" \
  --auth-token="nothing"
