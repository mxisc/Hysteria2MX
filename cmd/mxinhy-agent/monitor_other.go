//go:build !linux

package main

func rootDiskUsage() (uint64, uint64) { return 0, 0 }
