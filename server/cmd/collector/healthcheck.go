package main

import (
	"errors"
	"net"
	"net/http"
	"time"
)

// Probe the local public listener without loading credentials or opening the database.
func healthcheck(address string) error {
	if address == "" {
		address = "127.0.0.1:8090"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return errors.New("healthcheck failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("healthcheck unhealthy")
	}
	return nil
}
