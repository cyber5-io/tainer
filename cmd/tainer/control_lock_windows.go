//go:build windows

package main

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr { return nil }
