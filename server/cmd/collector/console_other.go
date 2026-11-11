//go:build !windows

package main

// waitIfDoubleClicked only matters on Windows, where a console opened for a
// double-clicked program closes as soon as it exits.
func waitIfDoubleClicked() {}
