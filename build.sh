#!/bin/sh

set -e

rm -f ./restore-percona-backup
GOOS=linux GOARCH=amd64 go build ./util/restore-percona-backup.go
docker build -t 'scpr/restore-percona-backup' .