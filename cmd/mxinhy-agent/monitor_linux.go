package main

import "syscall"

func rootDiskUsage() (total, used uint64) {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil {
		return 0, 0
	}
	total = s.Blocks * uint64(s.Bsize)
	used = (s.Blocks - s.Bfree) * uint64(s.Bsize)
	return
}
