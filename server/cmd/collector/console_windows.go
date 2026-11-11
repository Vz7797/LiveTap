package main

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// waitIfDoubleClicked keeps the console open after a failure when the program
// was started from Explorer: Windows then closes the window as soon as the
// process exits, so the message above would never be seen. A console shared
// with a shell (or no console at all, as under a service) is left alone.
func waitIfDoubleClicked() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return
	}
	var ids [2]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	if n != 1 {
		return
	}
	fmt.Fprint(os.Stderr, "按回车键退出……")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
