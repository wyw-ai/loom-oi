package main

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	gdi32             = windows.NewLazySystemDLL("gdi32.dll")
	querySession      = windows.NewLazySystemDLL("wtsapi32.dll").NewProc("WTSQuerySessionInformationW")
	setThreadDPI      = user32.NewProc("SetThreadDpiAwarenessContext")
	openInputDesktop  = user32.NewProc("OpenInputDesktop")
	closeDesktop      = user32.NewProc("CloseDesktop")
	getThreadDesktop  = user32.NewProc("GetThreadDesktop")
	getUserObjectInfo = user32.NewProc("GetUserObjectInformationW")
	getDC             = user32.NewProc("GetDC")
	releaseDC         = user32.NewProc("ReleaseDC")
	getMetric         = user32.NewProc("GetSystemMetrics")
	createDC          = gdi32.NewProc("CreateCompatibleDC")
	deleteDC          = gdi32.NewProc("DeleteDC")
	createBitmap      = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject      = gdi32.NewProc("SelectObject")
	deleteObject      = gdi32.NewProc("DeleteObject")
	bitBlt            = gdi32.NewProc("BitBlt")
	getDIBits         = gdi32.NewProc("GetDIBits")
)

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

func desktopName(desktop uintptr) (string, error) {
	var buf [256]uint16
	var length uint32
	ok, _, err := getUserObjectInfo.Call(desktop, 2 /* UOI_NAME */, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Sizeof(buf)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 {
		return "", fmt.Errorf("reading desktop name: %w", err)
	}
	return windows.UTF16ToString(buf[:]), nil
}

// GDI calls run on one desktop and use physical pixel coordinates, including
// negative origins when monitors extend to the left or above the primary.
func captureDesktop() (*image.RGBA, error) {
	var state *uint32
	var size uint32
	ok, _, err := querySession.Call(0, 0xffffffff /* WTS_CURRENT_SESSION */, 8 /* WTSConnectState */, uintptr(unsafe.Pointer(&state)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, fmt.Errorf("reading Windows session state: %w", err)
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(state)))
	if size < 4 || state == nil || *state != windows.WTSActive {
		return nil, errors.New("Windows session is not active")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	desk, _, err := openInputDesktop.Call(0, 0, 1 /* DESKTOP_READOBJECTS */)
	if desk == 0 {
		return nil, fmt.Errorf("opening Windows input desktop: %w", err)
	}
	defer closeDesktop.Call(desk)
	inputName, err := desktopName(desk)
	if err != nil {
		return nil, err
	}
	threadDesk, _, err := getThreadDesktop.Call(uintptr(windows.GetCurrentThreadId()))
	if threadDesk == 0 {
		return nil, fmt.Errorf("reading worker desktop: %w", err)
	}
	threadName, err := desktopName(threadDesk)
	if err != nil {
		return nil, err
	}
	if inputName != threadName {
		return nil, errors.New("worker desktop is not the active input desktop")
	}

	previousDPI, _, err := setThreadDPI.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2 (-4)
	if previousDPI == 0 {
		return nil, fmt.Errorf("setting thread DPI awareness: %w", err)
	}
	defer setThreadDPI.Call(previousDPI)
	x, _, _ := getMetric.Call(76) // SM_XVIRTUALSCREEN
	y, _, _ := getMetric.Call(77)
	w, _, _ := getMetric.Call(78)
	h, _, _ := getMetric.Call(79)
	if int32(w) <= 0 || int32(h) <= 0 || uint64(w)*uint64(h) > 64<<20 {
		return nil, fmt.Errorf("invalid or oversized virtual desktop %dx%d", int32(w), int32(h))
	}
	screen, _, err := getDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("getting screen DC: %w", err)
	}
	defer releaseDC.Call(0, screen)
	memory, _, err := createDC.Call(screen)
	if memory == 0 {
		return nil, fmt.Errorf("creating memory DC: %w", err)
	}
	defer deleteDC.Call(memory)
	bitmap, _, err := createBitmap.Call(screen, w, h)
	if bitmap == 0 {
		return nil, fmt.Errorf("creating capture bitmap: %w", err)
	}
	defer deleteObject.Call(bitmap)
	previous, _, err := selectObject.Call(memory, bitmap)
	if previous == 0 {
		return nil, fmt.Errorf("selecting capture bitmap: %w", err)
	}
	ok, _, copyErr := bitBlt.Call(memory, 0, 0, w, h, screen, x, y, 0x40cc0020 /* SRCCOPY | CAPTUREBLT */)
	// GetDIBits requires the bitmap to be deselected.
	selectObject.Call(memory, previous)
	if ok == 0 {
		return nil, fmt.Errorf("copying desktop: %w", copyErr)
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	header := bitmapInfoHeader{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	rows, _, err := getDIBits.Call(screen, bitmap, 0, h, uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&header)), 0)
	if rows != h {
		return nil, fmt.Errorf("reading desktop pixels (%d/%d rows): %w", rows, h, err)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
		img.Pix[i+3] = 255
	}
	return img, nil
}
