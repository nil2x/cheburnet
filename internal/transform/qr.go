package transform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nil2x/cheburnet/internal/config"
	"github.com/skip2/go-qrcode"
)

// The higher the level, the greater the chance to recognize compressed image
// at the cost of decreasing maximum content size that can be encoded into an image.
type QRLevel = qrcode.RecoveryLevel

const (
	QRLevelLow     = qrcode.Low
	QRLevelMedium  = qrcode.Medium
	QRLevelHigh    = qrcode.High
	QRLevelHighest = qrcode.Highest
)

// Maximum number of bytes that can be encoded at each QRLevel.
//
// Note that it doesn't mean number of characters, as in UTF-8 encoding
// one character may occupy more than 1 byte.
var QRMaxLen = map[QRLevel]int{
	QRLevelLow:     2953,
	QRLevelMedium:  2331,
	QRLevelHigh:    1663,
	QRLevelHighest: 1273,
}

// EncodeQR encodes content into QR Code as a PNG image.
//
// size specifies width and height in pixels, level specifies recovery level.
//
// Returns PNG image.
func EncodeQR(content string, size int, level QRLevel) ([]byte, error) {
	if len(content) > QRMaxLen[level] {
		return nil, fmt.Errorf("too large content: len=%v, max=%v", len(content), QRMaxLen[level])
	}

	qr, err := qrcode.New(content, level)

	if err != nil {
		return nil, err
	}

	data, err := qr.PNG(size)

	if err != nil {
		return nil, err
	}

	return data, nil
}

// DecodeQR decodes QR Code image using ZBar program.
//
// file specifies path to the image, zbar specifies path to ZBar executable.
//
// ZBar is able to recognize multiple QR codes in a single image, so DecodeQR
// returns array of results instead of a single result.
//
// If the image have no QR codes or none can be recognized, an error is returned.
// If the image have multiple QR codes and only some can be recognized, partial result is returned.
func DecodeQR(file string, zbar string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, zbar, file)

	buf := bytes.Buffer{}
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()

	if err != nil {
		if strings.Contains(output, "scanned 0 barcode symbols") {
			return nil, errors.New("qr code is not detected")
		} else if len(output) > 0 {
			return nil, fmt.Errorf("%v: %v", err, output)
		}

		return nil, err
	}

	lines := strings.Split(output, "\n")
	content := []string{}

	for _, line := range lines {
		s, found := strings.CutPrefix(line, "QR-Code:")

		if found {
			content = append(content, s)
		}
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("unexpected output: %v", output)
	}

	return content, nil
}

// SaveQR writes QR Code image data into file.
//
// ext specifies file extension, dir specifies writing directory.
// Both are optional. If dir is empty, then default temporary directory is used.
//
// Returns path to the created file.
func SaveQR(data []byte, ext string, dir string) (string, error) {
	pattern := "qr-*"

	if len(ext) > 0 {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}

		pattern += ext
	}

	f, err := os.CreateTemp(dir, pattern)

	if err != nil {
		return "", err
	}

	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return "", err
	}

	if err := f.Sync(); err != nil {
		return "", err
	}

	return f.Name(), nil
}

// MergeQR places multiple QR Code PNG images on a single PNG image.
//
// size specifies width and height in pixels of each of input image.
// All input images should have same size.
//
// Returns PNG image.
func MergeQR(data [][]byte, size int) ([]byte, error) {
	n := len(data)

	if n == 0 {
		return []byte{}, nil
	}

	side := int(math.Ceil(math.Sqrt(float64(n))))
	cols := side
	rows := int(math.Ceil(float64(n) / float64(cols)))

	width := cols * size
	height := rows * size

	rect := image.Rect(0, 0, width, height)
	merged := image.NewNRGBA(rect)

	draw.Draw(merged, merged.Bounds(), image.White, image.Point{}, draw.Src)

	for i, b := range data {
		img, _, err := image.Decode(bytes.NewReader(b))

		if err != nil {
			return nil, fmt.Errorf("image decode: %v", err)
		}

		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			return nil, fmt.Errorf("image size: %vx%v", img.Bounds().Dx(), img.Bounds().Dy())
		}

		rowN := i / cols
		colN := i % cols

		offsetX := colN * size
		offsetY := rowN * size

		point := image.Point{offsetX, offsetY}

		draw.Draw(merged, img.Bounds().Add(point), img, img.Bounds().Min, draw.Src)
	}

	var buf bytes.Buffer

	if err := png.Encode(&buf, merged); err != nil {
		return nil, fmt.Errorf("image encode: %v", err)
	}

	return buf.Bytes(), nil
}

// ValidateQR checks that the given configuration is usable.
func ValidateQR(cfg config.QR) error {
	if cfg.ImageSize == 0 {
		return errors.New("invalid image size")
	}

	if cfg.ImageLevel < int(QRLevelLow) || cfg.ImageLevel > int(QRLevelHighest) {
		return errors.New("invalid image level")
	}

	if len(cfg.ZBarPath) == 0 {
		return nil
	}

	content := "test"
	data, err := EncodeQR(content, cfg.ImageSize, QRLevel(cfg.ImageLevel))

	if err != nil {
		return err
	}

	file, err := SaveQR(data, "png", cfg.SaveDir)

	if err != nil {
		return err
	}

	defer os.Remove(file)

	decoded, err := DecodeQR(file, cfg.ZBarPath)

	if err != nil {
		return err
	}

	if len(decoded) != 1 {
		return errors.New("unexpected decoded data size")
	}

	if content != decoded[0] {
		return errors.New("encoded and decoded content mismatch")
	}

	return nil
}
