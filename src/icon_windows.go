//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type icoImage struct {
	width      byte
	height     byte
	colorCount byte
	planes     uint16
	bitCount   uint16
	data       []byte
}

func applyWindowsIconToPERuntimeBytes(runtimeBytes []byte, iconPath string) ([]byte, error) {
	images, err := readICOFile(iconPath)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "tiny-icon-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tmpExe := filepath.Join(tmpDir, "runtime.exe")
	if err := os.WriteFile(tmpExe, runtimeBytes, 0644); err != nil {
		return nil, err
	}

	if err := updateWindowsIconResource(tmpExe, images); err != nil {
		return nil, err
	}

	return os.ReadFile(tmpExe)
}

func readICOFile(iconPath string) ([]icoImage, error) {
	if strings.ToLower(filepath.Ext(iconPath)) != ".ico" {
		return nil, fmt.Errorf("icon must be a .ico file")
	}

	bytes, err := os.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}
	if len(bytes) < 6 {
		return nil, fmt.Errorf("invalid ico file")
	}

	reserved := binary.LittleEndian.Uint16(bytes[0:2])
	iconType := binary.LittleEndian.Uint16(bytes[2:4])
	count := int(binary.LittleEndian.Uint16(bytes[4:6]))
	if reserved != 0 || iconType != 1 || count < 1 {
		return nil, fmt.Errorf("invalid ico header")
	}

	dirLen := 6 + count*16
	if len(bytes) < dirLen {
		return nil, fmt.Errorf("truncated ico directory")
	}

	images := make([]icoImage, 0, count)
	for i := 0; i < count; i++ {
		entry := bytes[6+i*16 : 6+(i+1)*16]
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if size <= 0 || offset < 0 || offset+size > len(bytes) {
			return nil, fmt.Errorf("invalid ico image entry %d", i)
		}

		imageData := make([]byte, size)
		copy(imageData, bytes[offset:offset+size])
		images = append(images, icoImage{
			width:      entry[0],
			height:     entry[1],
			colorCount: entry[2],
			planes:     binary.LittleEndian.Uint16(entry[4:6]),
			bitCount:   binary.LittleEndian.Uint16(entry[6:8]),
			data:       imageData,
		})
	}

	return images, nil
}

func updateWindowsIconResource(exePath string, images []icoImage) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	beginUpdateResource := kernel32.NewProc("BeginUpdateResourceW")
	updateResource := kernel32.NewProc("UpdateResourceW")
	endUpdateResource := kernel32.NewProc("EndUpdateResourceW")

	pathPtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}

	handle, _, callErr := beginUpdateResource.Call(uintptr(unsafe.Pointer(pathPtr)), 0)
	if handle == 0 {
		return fmt.Errorf("BeginUpdateResourceW failed: %v", callErr)
	}

	discard := uintptr(1)
	defer func() {
		if discard != 0 {
			endUpdateResource.Call(handle, discard)
		}
	}()

	const (
		rtIcon      = uintptr(3)
		rtGroupIcon = uintptr(14)
		langEnglish = uintptr(0x0409)
	)

	for i, image := range images {
		id := uintptr(i + 1)
		data := image.data
		ok, _, callErr := updateResource.Call(
			handle,
			rtIcon,
			id,
			langEnglish,
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(len(data)),
		)
		if ok == 0 {
			return fmt.Errorf("UpdateResourceW RT_ICON %d failed: %v", id, callErr)
		}
	}

	group := buildIconGroupResource(images)
	ok, _, callErr := updateResource.Call(
		handle,
		rtGroupIcon,
		uintptr(1),
		langEnglish,
		uintptr(unsafe.Pointer(&group[0])),
		uintptr(len(group)),
	)
	if ok == 0 {
		return fmt.Errorf("UpdateResourceW RT_GROUP_ICON failed: %v", callErr)
	}

	ok, _, callErr = endUpdateResource.Call(handle, 0)
	discard = 0
	if ok == 0 {
		return fmt.Errorf("EndUpdateResourceW failed: %v", callErr)
	}

	return nil
}

func buildIconGroupResource(images []icoImage) []byte {
	group := make([]byte, 6+len(images)*14)
	binary.LittleEndian.PutUint16(group[0:2], 0)
	binary.LittleEndian.PutUint16(group[2:4], 1)
	binary.LittleEndian.PutUint16(group[4:6], uint16(len(images)))

	for i, image := range images {
		entry := group[6+i*14 : 6+(i+1)*14]
		entry[0] = image.width
		entry[1] = image.height
		entry[2] = image.colorCount
		entry[3] = 0
		binary.LittleEndian.PutUint16(entry[4:6], image.planes)
		binary.LittleEndian.PutUint16(entry[6:8], image.bitCount)
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(image.data)))
		binary.LittleEndian.PutUint16(entry[12:14], uint16(i+1))
	}

	return group
}
