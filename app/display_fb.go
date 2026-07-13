//go:build linux && !sim

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// FB drives the Kobo's e-ink framebuffer directly. The app draws into a
// logical portrait grayscale canvas; Blit converts to the panel's native
// pixel format/rotation, then an MXCFB_SEND_UPDATE ioctl tells the EPDC
// which region to refresh and with which waveform.

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602

	updateModePartial = 0x0
	updateModeFull    = 0x1
	wfModeDU          = 1
	wfModeGC16        = 2
	wfModeAuto        = 257
	tempUseAmbient    = 0x1000

	// _IOW('F', 0x2E, struct) — size encoded in the request number.
	mxcfbSendUpdateV1 = 0x4044462E // 68-byte v1_ntx struct
	mxcfbSendUpdateV2 = 0x4048462E // 72-byte v2 struct (NXP Mark 7+)

	// MediaTek "hwtcon" driver (Libra Colour & other Mark 13+ Kobos):
	// same 'F' 0x2E command but a 36-byte hwtcon_update_data struct.
	// Waveform values match mxcfb for DU/GC16/AUTO.
	hwtconSendUpdate  = 0x4024462E
	hwtconWfA2        = 6
	hwtconFlagForceA2 = 0x10 // HWTCON_FLAG_FORCE_A2_OUTPUT — pen updates
)

type FB struct {
	fd       int
	mem      []byte
	fbW      int // native
	fbH      int
	bpp      int
	stride   int
	rot      int // logical->native rotation (0..3)
	canvas   *image.RGBA
	marker   uint32
	useV1    bool
	isHwtcon bool // MediaTek display driver (Libra Colour etc.)
}

func OpenFB(cfg Config) (*FB, error) {
	fd, err := syscall.Open("/dev/fb0", syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/fb0: %w", err)
	}
	var vinfo [40]uint32
	if err := ioctl(fd, fbioGetVScreenInfo, unsafe.Pointer(&vinfo[0])); err != nil {
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %w", err)
	}
	var finfo [17]uint32 // 68 bytes covers up to line_length (offset 44)
	if err := ioctl(fd, fbioGetFScreenInfo, unsafe.Pointer(&finfo[0])); err != nil {
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %w", err)
	}
	// fb id string lives in the first 16 bytes of the fixed info.
	idBytes := (*[16]byte)(unsafe.Pointer(&finfo[0]))[:]
	fbID := string(idBytes)
	if i := indexByte(fbID, 0); i >= 0 {
		fbID = fbID[:i]
	}
	f := &FB{
		fd:       fd,
		fbW:      int(vinfo[0]),
		fbH:      int(vinfo[1]),
		bpp:      int(vinfo[6]),
		stride:   int(finfo[11]), // line_length at byte offset 44
		isHwtcon: fbID == "hwtcon",
	}
	smemLen := int(finfo[5]) // smem_len at byte offset 20
	f.mem, err = syscall.Mmap(fd, 0, smemLen, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap fb: %w", err)
	}
	// Rotation: app is portrait; if the native fb is landscape, rotate.
	f.rot = 0
	if f.fbW > f.fbH {
		f.rot = 3
	}
	if cfg.Rot >= 0 {
		f.rot = cfg.Rot
	}
	lw, lh := f.fbW, f.fbH
	if f.rot%2 == 1 {
		lw, lh = f.fbH, f.fbW
	}
	f.canvas = image.NewRGBA(image.Rect(0, 0, lw, lh))
	log.Printf("fb: id=%q hwtcon=%v native %dx%d bpp=%d stride=%d kernel_rotate=%d app_rot=%d logical=%dx%d smem=%d",
		fbID, f.isHwtcon, f.fbW, f.fbH, f.bpp, f.stride, vinfo[34], f.rot, lw, lh, smemLen)
	return f, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func (f *FB) Bounds() image.Rectangle { return f.canvas.Bounds() }
func (f *FB) Canvas() *image.RGBA     { return f.canvas }
func (f *FB) Close() {
	syscall.Munmap(f.mem)
	syscall.Close(f.fd)
}

// toNative maps a logical point to native fb coords.
func (f *FB) toNative(lx, ly int) (int, int) {
	switch f.rot {
	case 1:
		return f.fbW - 1 - ly, lx
	case 2:
		return f.fbW - 1 - lx, f.fbH - 1 - ly
	case 3:
		return ly, f.fbH - 1 - lx
	default:
		return lx, ly
	}
}

func (f *FB) blit(r image.Rectangle) image.Rectangle {
	r = r.Intersect(f.canvas.Bounds())
	if r.Empty() {
		return r
	}
	nMin := image.Pt(1 << 30, 1 << 30)
	nMax := image.Pt(-1, -1)
	for ly := r.Min.Y; ly < r.Max.Y; ly++ {
		si := f.canvas.PixOffset(r.Min.X, ly)
		for lx := r.Min.X; lx < r.Max.X; lx++ {
			cr, cg, cb := f.canvas.Pix[si], f.canvas.Pix[si+1], f.canvas.Pix[si+2]
			si += 4
			nx, ny := f.toNative(lx, ly)
			if nx < nMin.X {
				nMin.X = nx
			}
			if ny < nMin.Y {
				nMin.Y = ny
			}
			if nx > nMax.X {
				nMax.X = nx
			}
			if ny > nMax.Y {
				nMax.Y = ny
			}
			off := ny*f.stride + nx*(f.bpp/8)
			switch f.bpp {
			case 8:
				f.mem[off] = uint8((299*int(cr) + 587*int(cg) + 114*int(cb)) / 1000)
			case 16:
				v := uint16(cr>>3)<<11 | uint16(cg>>2)<<5 | uint16(cb>>3)
				binary.LittleEndian.PutUint16(f.mem[off:], v)
			case 32:
				// Libra Colour panel format is RGBA (per FBInk detection).
				f.mem[off] = cr
				f.mem[off+1] = cg
				f.mem[off+2] = cb
				f.mem[off+3] = 0xff
			}
		}
	}
	return image.Rect(nMin.X, nMin.Y, nMax.X+1, nMax.Y+1)
}

func (f *FB) Refresh(r image.Rectangle, mode RefreshMode) {
	nr := f.blit(r)
	if nr.Empty() {
		return
	}
	wf, um, flags := uint32(wfModeAuto), uint32(updateModePartial), uint32(0)
	switch mode {
	case RefreshFast:
		wf = wfModeDU
	case RefreshFull:
		wf, um = wfModeGC16, updateModeFull
	case RefreshPen:
		if f.isHwtcon {
			wf, flags = hwtconWfA2, hwtconFlagForceA2 // the driver's pen mode
		} else {
			wf = wfModeDU
		}
	}
	f.marker++
	f.sendUpdate(nr, wf, um, flags)
	if f.marker <= 5 {
		log.Printf("eink: update #%d sent: region=%v wf=%d mode=%d v1=%v", f.marker, nr, wf, um, f.useV1)
	}
}

// sendV2 issues the 72-byte Mark-7+ update struct: region, waveform,
// update_mode, marker, temp, flags, dither_mode, quant_bit,
// alt_buffer_data{phys,w,h,rect}.
func (f *FB) sendV2(nr image.Rectangle, wf, um uint32, temp int32) error {
	var buf [18]uint32
	buf[0], buf[1], buf[2], buf[3] = uint32(nr.Min.Y), uint32(nr.Min.X), uint32(nr.Dx()), uint32(nr.Dy())
	buf[4], buf[5], buf[6] = wf, um, f.marker
	buf[7] = uint32(temp)
	return ioctl(f.fd, mxcfbSendUpdateV2, unsafe.Pointer(&buf[0]))
}

// sendV1 issues the older 68-byte NTX update struct.
func (f *FB) sendV1(nr image.Rectangle, wf, um uint32, temp int32) error {
	var buf [17]uint32
	buf[0], buf[1], buf[2], buf[3] = uint32(nr.Min.Y), uint32(nr.Min.X), uint32(nr.Dx()), uint32(nr.Dy())
	buf[4], buf[5], buf[6] = wf, um, f.marker
	buf[7] = uint32(temp)
	return ioctl(f.fd, mxcfbSendUpdateV1, unsafe.Pointer(&buf[0]))
}

// sendHwtcon issues the MediaTek 36-byte hwtcon_update_data:
// region, waveform_mode, update_mode, update_marker, flags, dither_mode.
func (f *FB) sendHwtcon(nr image.Rectangle, wf, um, flags uint32) error {
	var buf [9]uint32
	buf[0], buf[1], buf[2], buf[3] = uint32(nr.Min.Y), uint32(nr.Min.X), uint32(nr.Dx()), uint32(nr.Dy())
	buf[4], buf[5], buf[6] = wf, um, f.marker
	buf[7], buf[8] = flags, 0 // flags, dither_mode
	return ioctl(f.fd, hwtconSendUpdate, unsafe.Pointer(&buf[0]))
}

func (f *FB) sendUpdate(nr image.Rectangle, wf, um, flags uint32) {
	if f.isHwtcon {
		if err := f.sendHwtcon(nr, wf, um, flags); err != nil {
			log.Printf("eink: hwtcon update failed: %v", err)
		}
		return
	}
	if !f.useV1 {
		err := f.sendV2(nr, wf, um, tempUseAmbient)
		if err == nil {
			return
		}
		// Only demote to the v1 struct if the kernel rejected the shape of
		// the request — other errors are not evidence that v1 is right.
		if errno, ok := errnoOf(err); ok && (errno == syscall.ENOTTY || errno == syscall.EINVAL) {
			log.Printf("eink: v2 update rejected (%v), falling back to v1", err)
			f.useV1 = true
		} else {
			log.Printf("eink: v2 update failed: %v", err)
			return
		}
	}
	if err := f.sendV1(nr, wf, um, tempUseAmbient); err != nil {
		log.Printf("eink: v1 update failed too: %v", err)
	}
}

// ioctl retries on EINTR (EPDC updates spend ~100ms in the kernel and
// runtime signals interrupt them), but with a deadline so a persistent
// interrupt storm can never wedge the app.
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if errno == 0 {
			return nil
		}
		if errno != syscall.EINTR {
			return os.NewSyscallError("ioctl", errno)
		}
		if time.Now().After(deadline) {
			return os.NewSyscallError("ioctl (EINTR storm)", errno)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func errnoOf(err error) (syscall.Errno, bool) {
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		if errno, ok := sysErr.Err.(syscall.Errno); ok {
			return errno, true
		}
	}
	return 0, false
}
