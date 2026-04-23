package pdf

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"

	"github.com/blacktop/go-termimg"
)

// RenderKitty converts PNG bytes into a kitty-graphics escape sequence sized
// to fit inside a (widthCells × heightCells) terminal cell region. The image
// is scaled with aspect-fit; unused space within the target rectangle is left
// blank (the caller is responsible for clearing/positioning).
//
// Kitty-only: mreview does not support sixel or iTerm2 fallbacks.
func RenderKitty(pngBytes []byte, widthCells, heightCells int) (string, error) {
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("pdf: empty png bytes")
	}
	if widthCells < 1 || heightCells < 1 {
		return "", fmt.Errorf("pdf: target cells must be positive (got %dx%d)", widthCells, heightCells)
	}
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return "", fmt.Errorf("pdf: decode png: %w", err)
	}
	escape, err := termimg.New(img).
		Protocol(termimg.Kitty).
		Width(widthCells).
		Height(heightCells).
		Scale(termimg.ScaleFit).
		Render()
	if err != nil {
		return "", fmt.Errorf("pdf: kitty render: %w", err)
	}
	return escape, nil
}

// NoRegionPlaceholder is the body text shown in the PDF pane when the cursor
// block has no SyncTeX mapping (e.g. a block that lives outside any page,
// such as an outer `\section` header with only whitespace inside).
const NoRegionPlaceholder = "[no region — block outside PDF]"
