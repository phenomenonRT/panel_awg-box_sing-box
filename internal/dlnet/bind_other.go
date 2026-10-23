//go:build !linux

package dlnet

import (
	"errors"
	"syscall"
)

func bindControl(string) (func(network, address string, c syscall.RawConn) error, error) {
	return nil, errors.New("загрузка через выбранный интерфейс поддерживается только в Linux")
}
