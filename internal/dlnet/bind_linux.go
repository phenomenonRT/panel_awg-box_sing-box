//go:build linux

package dlnet

import "syscall"

func bindControl(iface string) (func(network, address string, c syscall.RawConn) error, error) {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
		}); err != nil {
			return err
		}
		return serr
	}, nil
}
