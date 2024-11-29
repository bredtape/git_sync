#!/bin/sh

go run main.go \
  --listen-address=":5000" \
  --source-repo="http://slice:3000/bredtape/source.git"
