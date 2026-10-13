// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && !wasm

package unix

// The common types with pointers, whose size is the C ABI's but on
// linux/wasm (64-bit pointers in Go, 32 in its ABI): ztypes_linux_wasm.go
// has its own. (Moved out of ztypes_linux.go.)

type SockFprog struct {
	Len    uint16
	Filter *SockFilter
}

type sigset_argpack struct {
	ss    *Sigset_t
	ssLen uintptr
}

type BlkpgIoctlArg struct {
	Op      int32
	Flags   int32
	Datalen int32
	Data    *byte
}

type MtdOobBuf struct {
	Start  uint32
	Length uint32
	Ptr    *uint8
}
