//go:build !linux

package image

import "syscall"

func applyRasterLimits(int) error { return ErrUnavailable }

func rlimitSupported() error { return ErrUnavailable }

func workerProcAttr() *syscall.SysProcAttr { return nil }
