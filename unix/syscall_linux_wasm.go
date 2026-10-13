// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build wasm && linux

// linux-workers: LKL's wasm32 processes (as Go's syscall's, which explains
// them: go/src/syscall/syscall_linux_wasm.go). The asm-generic system calls
// for 32 bits, with riscv32's choices: no 32-bit time calls (the _time64
// ones, under the names the generic code knows), stat only as statx, the
// *64 calls for 64-bit sizes and offsets (two words, low first: -l32).
// GOARCH=wasm's pointers and uintptr are 64-bit, the ABI's 32.

package unix

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

//sys	EpollWait(epfd int, events []EpollEvent, msec int) (n int, err error) = SYS_EPOLL_PWAIT
//sys	Fadvise(fd int, offset int64, length int64, advice int) (err error) = SYS_FADVISE64_64
//sys	Fchown(fd int, uid int, gid int) (err error)
//sys	fstatfs64(fd int, size int, buf *Statfs_t) (err error) = SYS_FSTATFS64
//sys	Ftruncate(fd int, length int64) (err error) = SYS_FTRUNCATE64
//sysnb	Getegid() (egid int)
//sysnb	Geteuid() (euid int)
//sysnb	Getgid() (gid int)
//sysnb	Getuid() (uid int)
//sys	Listen(s int, n int) (err error)
//sys	pread(fd int, p []byte, offset int64) (n int, err error) = SYS_PREAD64
//sys	pwrite(fd int, p []byte, offset int64) (n int, err error) = SYS_PWRITE64
//sys	_llseek(fd int, offhi int, offlo int, result *int64, whence int) (err error) = SYS__LLSEEK
//sys	sendfile(outfd int, infd int, offset *int64, count int) (written int, err error) = SYS_SENDFILE64
//sys	setfsgid(gid int) (prev int, err error)
//sys	setfsuid(uid int) (prev int, err error)
//sys	Shutdown(fd int, how int) (err error)
//sys	Splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (n int64, err error)
//sys	statfs64(path string, size int, buf *Statfs_t) (err error) = SYS_STATFS64
//sys	SyncFileRange(fd int, off int64, n int64, flags int) (err error)
//sys	Truncate(path string, length int64) (err error) = SYS_TRUNCATE64
//sys	accept4(s int, rsa *RawSockaddrAny, addrlen *_Socklen, flags int) (fd int, err error)
//sys	bind(s int, addr unsafe.Pointer, addrlen _Socklen) (err error)
//sys	connect(s int, addr unsafe.Pointer, addrlen _Socklen) (err error)
//sysnb	getgroups(n int, list *_Gid_t) (nn int, err error)
//sysnb	setgroups(n int, list *_Gid_t) (err error)
//sys	getsockopt(s int, level int, name int, val unsafe.Pointer, vallen *_Socklen) (err error)
//sys	setsockopt(s int, level int, name int, val unsafe.Pointer, vallen uintptr) (err error)
//sysnb	socket(domain int, typ int, proto int) (fd int, err error)
//sysnb	socketpair(domain int, typ int, proto int, fd *[2]int32) (err error)
//sysnb	getpeername(fd int, rsa *RawSockaddrAny, addrlen *_Socklen) (err error)
//sysnb	getsockname(fd int, rsa *RawSockaddrAny, addrlen *_Socklen) (err error)
//sys	recvfrom(fd int, p []byte, flags int, from *RawSockaddrAny, fromlen *_Socklen) (n int, err error)
//sys	sendto(s int, buf []byte, flags int, to unsafe.Pointer, addrlen _Socklen) (err error)
//sys	recvmsg(s int, msg *Msghdr, flags int) (n int, err error)
//sys	sendmsg(s int, msg *Msghdr, flags int) (n int, err error)

// the generic code's names for the 32-bit ABI's calls (time64's: their
// structs are the kernel's __kernel_timespec, as these Timespecs are, low
// word first and the rest zero)
const (
	SYS_FCNTL                 = SYS_FCNTL64
	SYS_UTIMENSAT             = SYS_UTIMENSAT_TIME64
	SYS_PSELECT6              = SYS_PSELECT6_TIME64
	SYS_PPOLL                 = SYS_PPOLL_TIME64
	SYS_CLOCK_GETTIME         = SYS_CLOCK_GETTIME64
	SYS_CLOCK_SETTIME         = SYS_CLOCK_SETTIME64
	SYS_CLOCK_GETRES          = SYS_CLOCK_GETRES_TIME64
	SYS_CLOCK_NANOSLEEP       = SYS_CLOCK_NANOSLEEP_TIME64
	SYS_TIMERFD_GETTIME       = SYS_TIMERFD_GETTIME64
	SYS_TIMERFD_SETTIME       = SYS_TIMERFD_SETTIME64
	SYS_TIMER_GETTIME         = SYS_TIMER_GETTIME64
	SYS_TIMER_SETTIME         = SYS_TIMER_SETTIME64
	SYS_RT_SIGTIMEDWAIT       = SYS_RT_SIGTIMEDWAIT_TIME64
	SYS_RECVMMSG              = SYS_RECVMMSG_TIME64
	SYS_SEMTIMEDOP            = SYS_SEMTIMEDOP_TIME64
	SYS_MQ_TIMEDSEND          = SYS_MQ_TIMEDSEND_TIME64
	SYS_MQ_TIMEDRECEIVE       = SYS_MQ_TIMEDRECEIVE_TIME64
	SYS_IO_PGETEVENTS         = SYS_IO_PGETEVENTS_TIME64
	SYS_SCHED_RR_GET_INTERVAL = SYS_SCHED_RR_GET_INTERVAL_TIME64
	SYS_FUTEX                 = SYS_FUTEX_TIME64
)

func Fstatfs(fd int, buf *Statfs_t) error {
	return fstatfs64(fd, int(unsafe.Sizeof(*buf)), buf)
}

func Statfs(path string, buf *Statfs_t) error {
	return statfs64(path, int(unsafe.Sizeof(*buf)), buf)
}

func Seek(fd int, offset int64, whence int) (int64, error) {
	var off int64
	err := _llseek(fd, int(offset>>32), int(uint32(offset)), &off, whence)
	return off, err
}

func Select(nfd int, r *FdSet, w *FdSet, e *FdSet, timeout *Timeval) (n int, err error) {
	var ts *Timespec
	if timeout != nil {
		ts = &Timespec{Sec: int64(timeout.Sec), Nsec: timeout.Usec * 1000}
	}
	return pselect6(nfd, r, w, e, ts, nil)
}

func timespecFromStatxTimestamp(x StatxTimestamp) Timespec {
	return Timespec{
		Sec:  x.Sec,
		Nsec: int32(x.Nsec),
	}
}

func Fstatat(fd int, path string, stat *Stat_t, flags int) error {
	var r Statx_t
	// Do it the glibc way, add AT_NO_AUTOMOUNT.
	if err := Statx(fd, path, AT_NO_AUTOMOUNT|flags, STATX_BASIC_STATS, &r); err != nil {
		return err
	}

	stat.Dev = Mkdev(r.Dev_major, r.Dev_minor)
	stat.Ino = r.Ino
	stat.Mode = uint32(r.Mode)
	stat.Nlink = r.Nlink
	stat.Uid = r.Uid
	stat.Gid = r.Gid
	stat.Rdev = Mkdev(r.Rdev_major, r.Rdev_minor)
	stat.Size = int64(r.Size)
	stat.Blksize = int32(r.Blksize)
	stat.Blocks = int64(r.Blocks)
	stat.Atim = timespecFromStatxTimestamp(r.Atime)
	stat.Mtim = timespecFromStatxTimestamp(r.Mtime)
	stat.Ctim = timespecFromStatxTimestamp(r.Ctime)

	return nil
}

func Fstat(fd int, stat *Stat_t) (err error) {
	return Fstatat(fd, "", stat, AT_EMPTY_PATH)
}

func Stat(path string, stat *Stat_t) (err error) {
	return Fstatat(AT_FDCWD, path, stat, 0)
}

func Lchown(path string, uid int, gid int) (err error) {
	return Fchownat(AT_FDCWD, path, uid, gid, AT_SYMLINK_NOFOLLOW)
}

func Lstat(path string, stat *Stat_t) (err error) {
	return Fstatat(AT_FDCWD, path, stat, AT_SYMLINK_NOFOLLOW)
}

func Ustat(dev int, ubuf *Ustat_t) (err error) {
	return ENOSYS
}

func setTimespec(sec, nsec int64) Timespec {
	return Timespec{Sec: sec, Nsec: int32(nsec)}
}

func setTimeval(sec, usec int64) Timeval {
	return Timeval{Sec: int32(sec), Usec: int32(usec)}
}

func Getrlimit(resource int, rlim *Rlimit) (err error) {
	err = Prlimit(0, resource, nil, rlim)
	return
}

func futimesat(dirfd int, path string, tv *[2]Timeval) (err error) {
	if tv == nil {
		return utimensat(dirfd, path, nil, 0)
	}

	ts := []Timespec{
		NsecToTimespec(TimevalToNsec(tv[0])),
		NsecToTimespec(TimevalToNsec(tv[1])),
	}
	return utimensat(dirfd, path, (*[2]Timespec)(unsafe.Pointer(&ts[0])), 0)
}

func Time(t *Time_t) (Time_t, error) {
	var tv Timeval
	err := Gettimeofday(&tv)
	if err != nil {
		return 0, err
	}
	if t != nil {
		*t = Time_t(tv.Sec)
	}
	return Time_t(tv.Sec), nil
}

func Utime(path string, buf *Utimbuf) error {
	if buf == nil {
		return Utimes(path, nil)
	}
	tv := []Timeval{
		{Sec: int32(buf.Actime)},
		{Sec: int32(buf.Modtime)},
	}
	return Utimes(path, tv)
}

func utimes(path string, tv *[2]Timeval) (err error) {
	if tv == nil {
		return utimensat(AT_FDCWD, path, nil, 0)
	}

	ts := []Timespec{
		NsecToTimespec(TimevalToNsec(tv[0])),
		NsecToTimespec(TimevalToNsec(tv[1])),
	}
	return utimensat(AT_FDCWD, path, (*[2]Timespec)(unsafe.Pointer(&ts[0])), 0)
}

func (iov *Iovec) SetLen(length int) {
	iov.Len = uint32(length)
}

func (msghdr *Msghdr) SetControllen(length int) {
	msghdr.Controllen = uint32(length)
}

func (msghdr *Msghdr) SetIovlen(length int) {
	msghdr.Iovlen = int32(length)
}

func (cmsg *Cmsghdr) SetLen(length int) {
	cmsg.Len = uint32(length)
}

func (rsa *RawSockaddrNFCLLCP) SetServiceNameLen(length int) {
	rsa.Service_name_len = uint32(length)
}

func Pause() error {
	_, err := ppoll(nil, 0, nil, nil)
	return err
}

func Renameat(olddirfd int, oldpath string, newdirfd int, newpath string) (err error) {
	return Renameat2(olddirfd, oldpath, newdirfd, newpath, 0)
}

// The pointers in the kernel's structs, as the ABI's 32-bit words (as
// syscall's): each pointee escapes to the heap, whose objects don't move,
// and the callers keep them alive until the system call returns.
var escapeSink unsafe.Pointer

func escape(p unsafe.Pointer) uint32 {
	atomic.StorePointer(&escapeSink, p)
	return uint32(uintptr(p))
}

func (iov *Iovec) setBase(b *byte) { iov.Base = escape(unsafe.Pointer(b)) }

func (iov *Iovec) base() unsafe.Pointer { return unsafe.Pointer(uintptr(iov.Base)) }

func (msg *Msghdr) setName(p unsafe.Pointer) { msg.Name = escape(p) }

func (msg *Msghdr) setIov(iov *Iovec) { msg.Iov = escape(unsafe.Pointer(iov)) }

func (msg *Msghdr) setControl(b *byte) { msg.Control = escape(unsafe.Pointer(b)) }

func newSigsetArgpack(ss *Sigset_t, ssLen uintptr) *sigset_argpack {
	return &sigset_argpack{ss: escape(unsafe.Pointer(ss)), ssLen: uint32(ssLen)}
}

// What the ABI lacks, 32-bit time's calls, from others (as musl's).

// wait4, by waitid (musl's __emulate_wait4): the rusage is the kernel's for
// both
func wait4(pid int, wstatus *_C_int, options int, rusage *Rusage) (wpid int, err error) {
	idtype, id := P_PID, pid
	switch {
	case pid < -1:
		idtype, id = P_PGID, -pid
	case pid == -1:
		idtype, id = P_ALL, 0
	case pid == 0:
		idtype = P_PGID
	}
	var info [32]int32 // siginfo_t: signo, errno, code; pid, uid, status
	_, _, e := Syscall6(SYS_WAITID, uintptr(idtype), uintptr(id), uintptr(unsafe.Pointer(&info)),
		uintptr(options|WEXITED), uintptr(unsafe.Pointer(rusage)), 0)
	if e != 0 {
		return 0, errnoErr(e)
	}
	if info[3] != 0 && wstatus != nil {
		code, status := info[2], info[5]
		var sw int32
		switch code {
		case _CLD_CONTINUED:
			sw = 0xffff
		case _CLD_DUMPED:
			sw = status&0x7f | 0x80
		case _CLD_EXITED:
			sw = (status & 0xff) << 8
		case _CLD_KILLED:
			sw = status & 0x7f
		case _CLD_STOPPED, _CLD_TRAPPED:
			sw = status<<8 + 0x7f
		}
		*wstatus = _C_int(sw)
	}
	return int(info[3]), nil
}

const (
	_CLD_EXITED    = 1
	_CLD_KILLED    = 2
	_CLD_DUMPED    = 3
	_CLD_TRAPPED   = 4
	_CLD_STOPPED   = 5
	_CLD_CONTINUED = 6
)

func Gettimeofday(tv *Timeval) (err error) {
	var ts Timespec
	if err = ClockGettime(CLOCK_REALTIME, &ts); err != nil {
		return err
	}
	tv.Sec, tv.Usec = int32(ts.Sec), ts.Nsec/1000
	return nil
}

func Settimeofday(tv *Timeval) (err error) {
	ts := Timespec{Sec: int64(tv.Sec), Nsec: tv.Usec * 1000}
	_, _, e := RawSyscall(SYS_CLOCK_SETTIME, CLOCK_REALTIME, uintptr(unsafe.Pointer(&ts)), 0)
	if e != 0 {
		return errnoErr(e)
	}
	return nil
}

func Adjtimex(buf *Timex) (state int, err error) {
	return -1, ENOSYS
}

func ClockAdjtime(clockid int32, buf *Timex) (state int, err error) {
	return -1, ENOSYS
}

// mmap: the kernel can't map pages into the process's memory (as syscall's
// linux/wasm): the mapping is memory of ours, a copy of the file's.
var wasmMaps struct {
	sync.Mutex
	m map[uintptr][]byte
}

func mmap(addr uintptr, length uintptr, prot int, flags int, fd int, offset int64) (uintptr, error) {
	if length == 0 || flags&MAP_FIXED != 0 {
		return 0, EINVAL
	}
	b := make([]byte, length)
	if flags&MAP_ANONYMOUS == 0 {
		for n := 0; n < len(b); {
			m, err := pread(fd, b[n:], offset+int64(n))
			if err != nil {
				return 0, err
			}
			if m == 0 {
				break
			}
			n += m
		}
	}
	p := uintptr(unsafe.Pointer(&b[0]))
	wasmMaps.Lock()
	if wasmMaps.m == nil {
		wasmMaps.m = make(map[uintptr][]byte)
	}
	wasmMaps.m[p] = b
	wasmMaps.Unlock()
	return p, nil
}

func munmap(addr uintptr, length uintptr) error {
	wasmMaps.Lock()
	defer wasmMaps.Unlock()
	if _, ok := wasmMaps.m[addr]; !ok {
		return EINVAL
	}
	delete(wasmMaps.m, addr)
	return nil
}

// (in assembly elsewhere: syscall_linux_gc.go, syscall_unix_gc.go)

func SyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	r1, r2, _ = syscall.Syscall(trap, a1, a2, a3)
	return
}

func RawSyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	r1, r2, _ = syscall.RawSyscall(trap, a1, a2, a3)
	return
}

func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return syscall.Syscall(trap, a1, a2, a3)
}

func Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return syscall.Syscall6(trap, a1, a2, a3, a4, a5, a6)
}

func RawSyscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return syscall.RawSyscall(trap, a1, a2, a3)
}

func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return syscall.RawSyscall6(trap, a1, a2, a3, a4, a5, a6)
}
