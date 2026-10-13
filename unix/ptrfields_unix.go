// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos) && !(linux && wasm)

package unix

import "unsafe"

// The pointers in the kernel's structs. (linux/wasm's are 32-bit words, its
// Go pointers 64: syscall_linux_wasm.go.)

func (iov *Iovec) setBase(b *byte) { iov.Base = b }

func (iov *Iovec) base() unsafe.Pointer { return unsafe.Pointer(iov.Base) }
