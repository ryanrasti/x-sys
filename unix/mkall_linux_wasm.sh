#!/usr/bin/env bash
# Copyright 2026 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

# linux-workers: the generated files for linux/wasm, LKL's wasm32 processes
# (as Go's syscall's: go/src/syscall/mkall_linux_wasm.sh, which explains the
# stand-in C compiler). Then mkmerge's work, for one more architecture:
# what zerrors_linux.go, ztypes_linux.go and zsyscall_linux.go already have
# is dropped (linux/mkwasm.go).
#
# usage: CLANG=clang HOSTCC=cc SYSROOT_INCLUDE=dir ./mkall_linux_wasm.sh
set -euo pipefail
cd "$(dirname "$0")"
: "${CLANG:?clang}" "${SYSROOT_INCLUDE:?the include directory of the processes}"
shim=$(mktemp -d)
# (and mkerrors.sh's cgo leaves an object behind)
trap 'rm -rf "$shim" _cgo_*.o' EXIT
cat > "$shim/gcc" <<SHIM
#!/bin/sh
case " \$* " in *" -o _errors "*) exec "${HOSTCC:-cc}" -DSIGUNUSED=31 "\$@" ;; esac
# (the preprocessor sees riscv32, whose ABI ours is; ARM: the code generator)
exec "$CLANG" --target=armv7-unknown-linux-musleabi -fsigned-char -U__ARM_EABI__ -U__arm__ -D__riscv -D__riscv_xlen=32 -D__daddr_t=int -D"__ino_t=unsigned long" -nostdinc -isystem "$SYSROOT_INCLUDE" -I"$shim/include" -include mkwasm_compat.h "\$@"
SHIM
chmod +x "$shim/gcc"
# (the generators run here: GOOS, GOARCH are this machine's; the target's are
# GOOS_TARGET, GOARCH_TARGET, but for mkerrors.sh and cgo)
export PATH="$shim:$PATH" CC="$shim/gcc" GOLANG_SYS_BUILD=docker
out=$(mktemp -d)
mkdir -p "$shim/include/abi"
# what linux/types.go wants that these headers lack: glibc's cpu_set_t
# internals (the ABI's: 32-bit words), and enumerators of newer kernels
# than these headers' (architecture-independent: zerrors_linux.go and
# ztypes_linux.go have them, and the merge below drops these)
cat > "$shim/include/mkwasm_compat.h" <<'COMPAT'
#include <sys/stat.h> /* glibc's fcntl.h has struct stat; musl's doesn't */
typedef unsigned long __cpu_mask;
#define __CPU_SETSIZE 1024
#define __NCPUBITS (8 * sizeof(__cpu_mask))
enum {
	ETHTOOL_A_RINGS_HDS_THRESH = 0x7fff0000, ETHTOOL_A_RINGS_HDS_THRESH_MAX,
	ETHTOOL_A_TSINFO_HWTSTAMP_PROVIDER, ETHTOOL_MSG_TSCONFIG_GET,
	ETHTOOL_MSG_TSCONFIG_GET_REPLY, ETHTOOL_MSG_TSCONFIG_SET,
	ETHTOOL_MSG_TSCONFIG_SET_REPLY, NL80211_MAX_SUPP_SELECTORS,
	RTNLGRP_IPV4_MCADDR, RTNLGRP_IPV6_ACADDR, RTNLGRP_IPV6_MCADDR,
};
COMPAT
go run linux/mkwasm.go abi "$CC" > "$shim/include/abi/abi.h"

GOARCH_TARGET=wasm GOOS=linux go run linux/mksysnum.go "$SYSROOT_INCLUDE/bits/syscall.h" > "$out/zsysnum_linux_wasm.go"
# GOARCH=riscv: cgo's 32-bit view of the C types (see syscall's)
GOOS=linux GOARCH=riscv ./mkerrors.sh | sed 's/^\/\/go:build riscv && linux$/\/\/go:build wasm \&\& linux/' > "$out/zerrors_linux_wasm.go"
GOOS=linux GOARCH=riscv go tool cgo -godefs -objdir="$out/cgo" -- linux/types.go | GOOS_TARGET=linux GOARCH_TARGET=wasm go run mkpost.go > "$out/ztypes_linux_wasm.go"
# (the calls linux/wasm has its own of: out of the common file)
go run linux/mkwasm.go split zsyscall_linux.go zsyscall_linux_nowasm.go munmap wait4 Adjtimex ClockAdjtime Settimeofday
for f in zerrors ztypes; do
	go run linux/mkwasm.go merge "$out/${f}_linux_wasm.go" zerrors_linux.go ztypes_linux.go zsyscall_linux.go
done
# the system calls (syscall_linux.go's that linux/wasm has its own of,
# in syscall_linux_wasm.go, aside: munmap, and those of 32-bit time)
grep -Ev '^//sys(nb)?	(munmap|wait4|Adjtimex|ClockAdjtime|Settimeofday)\(' syscall_linux.go > "$shim/syscall_linux.go"
cp syscall_linux_wasm.go "$shim/"
(cd "$shim" && GOOS_TARGET=linux GOARCH_TARGET=wasm go run "$OLDPWD/mksyscall.go" -l32 -tags linux,wasm syscall_linux.go syscall_linux_wasm.go) > "$out/zsyscall_linux_wasm.go"
go run linux/mkwasm.go merge "$out/zsyscall_linux_wasm.go" zsyscall_linux.go zsyscall_linux_nowasm.go
# pointers and uintptr in the ABI's structs: 32-bit words (GOARCH=wasm's are 64)
perl -pi -e 's/^(\t+\w+\s+)(\*\w+|uintptr)$/${1}uint32 \/\/ a pointer/' "$out/ztypes_linux_wasm.go"
# timeval: the kernel's (still 32-bit where it remains), as syscall's
perl -0pi -e 's/type Timeval struct \{\n\tSec\s+int64\n\tUsec\s+int64\n\}/type Timeval struct {\n\tSec  int32\n\tUsec int32\n}/' "$out/ztypes_linux_wasm.go"
grep -q 'Usec int32' "$out/ztypes_linux_wasm.go"
gofmt -w "$out"/z*_linux_wasm.go
cp "$out"/z*_linux_wasm.go .
