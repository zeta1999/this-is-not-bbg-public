// Package views provides TUI panel renderers for market data.
package views

import (
	"strings"

	"github.com/skip2/go-qrcode"
)

// RenderQRBlock returns a terminal-renderable QR code for the given
// payload, using Unicode half-block characters so each text row
// encodes two bit-rows. Returns the QR plus a hint line; on error
// returns the error text so the caller can drop it in an overlay.
//
// We use go-qrcode.Medium (~15% recovery) which is a good fit for
// terminal scanners: dense enough to fit a typical pairing payload
// in a 21-29 module grid, robust enough to survive a phone camera
// tilt across a 256-pixel-tall terminal cell.
func RenderQRBlock(payload string) string {
	q, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return "QR encode error: " + err.Error()
	}
	q.DisableBorder = false
	bitmap := q.Bitmap()
	return halfBlockBitmap(bitmap)
}

// halfBlockBitmap turns a row-major bitmap (true = dark module) into
// a Unicode half-block string. Each output row covers two bitmap
// rows: top half on/off and bottom half on/off picks the right of
// "█▀▄ " (full / upper / lower / blank). White background = print
// the dark cells with a visible glyph; the terminal's default
// white-on-black inverts naturally so the QR scans without
// inverting the colour scheme.
func halfBlockBitmap(bitmap [][]bool) string {
	if len(bitmap) == 0 {
		return ""
	}
	var b strings.Builder
	for y := 0; y < len(bitmap); y += 2 {
		row := bitmap[y]
		var nextRow []bool
		if y+1 < len(bitmap) {
			nextRow = bitmap[y+1]
		}
		for x := 0; x < len(row); x++ {
			top := row[x]
			bot := false
			if x < len(nextRow) {
				bot = nextRow[x]
			}
			switch {
			case top && bot:
				b.WriteRune('█')
			case top && !bot:
				b.WriteRune('▀')
			case !top && bot:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
