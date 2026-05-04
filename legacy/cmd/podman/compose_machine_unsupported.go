//go:build !(amd64 || arm64)

package main

import (
	"errors"
	"net/url"
)

func getMachineConn(connection string, parsedConnection *url.URL) (string, error) {
	return "", errors.New("tainer machine not supported on this architecture")
}
