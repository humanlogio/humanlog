// Package rgbterm colorizes bytes and strings using RGB colors, for a
// full range of pretty terminal strings.
//
// Beyond the traditional boring 16 colors of your terminal lie an
// extended set of 256 pretty colors waiting to be used. However, they
// are weirdly encoded; simply asking for an RGB color is much more
// convenient!
//
// It's easy to use, pick an RGB code and just use it!
//
//	var r, g, b uint8
//	// pick a color
//	r, g, b = 252, 255, 43
//	// choose a word
//	word := "=)"
//	// colorize it!
//	coloredWord := rgbterm.String(word, r, g, b)
//
//	fmt.Println("Oh!", coloredWord, "hello!")
//
// The RGB <-> HSL helpers were shamelessly taken from gorilla color, BSD 2 clauses
// licensed:
//    https://code.google.com/p/gorilla/source/browse/?r=ef489f63418265a7249b1d53bdc358b09a4a2ea0#hg%2Fcolor
package rgbterm

var (
	reset    = []byte("\033[0;00m")
	fgcolors = fgTermRGB[16:232]
	bgcolors = bgTermRGB[16:232]
)

// String colorizes the input with the terminal color that matches
// the closest the RGB color.
//
// This is simply a helper for Bytes.
func String(in string, r, g, b uint8) string {
	return string(Bytes([]byte(in), r, g, b))
}

// BgString colorizes the background of the input with the terminal color
// that matches the closest the RGB color.
//
// This is simply a helper for Bytes.
func BgString(in string, r, g, b uint8) string {
	return string(BgBytes([]byte(in), r, g, b))
}

// Bytes colorizes the input with the terminal color that matches
// the closest the RGB color.
func Bytes(in []byte, r, g, b uint8) []byte {
	return append(append(rgb(r, g, b, true), in...), reset...)
}

// BgBytes colorizes the background of the input with the terminal color
// that matches the closest the RGB color.
func BgBytes(in []byte, r, g, b uint8) []byte {
	return append(append(rgb(r, g, b, false), in...), reset...)
}

// Byte colorizes the input with the terminal color that matches
// the closest the RGB color.
func Byte(in byte, r, g, b uint8) []byte {
	return append(append(rgb(r, g, b, true), in), reset...)
}

// BgByte colorizes the background of the input with the terminal color
// that matches the closest the RGB color.
func BgByte(in byte, r, g, b uint8) []byte {
	return append(append(rgb(r, g, b, false), in), reset...)
}

func rgb(r, g, b uint8, foreground bool) []byte {
	// if all colors are equal, it might be in the grayscale range
	if r == g && g == b {
		color, ok := grayscale(r, foreground)
		if ok {
			return color
		}
	}

	// the general case approximates RGB by using the closest color.
	r6 := ((uint16(r) * 5) / 255)
	g6 := ((uint16(g) * 5) / 255)
	b6 := ((uint16(b) * 5) / 255)
	i := 36*r6 + 6*g6 + b6
	if foreground {
		return fgcolors[i]
	} else {
		return bgcolors[i]
	}
}

func grayscale(scale uint8, foreground bool) ([]byte, bool) {
	var source [256][]byte

	if foreground {
		source = fgTermRGB
	} else {
		source = bgTermRGB
	}

	switch scale {
	case 0x08:
		return source[232], true
	case 0x12:
		return source[233], true
	case 0x1c:
		return source[234], true
	case 0x26:
		return source[235], true
	case 0x30:
		return source[236], true
	case 0x3a:
		return source[237], true
	case 0x44:
		return source[238], true
	case 0x4e:
		return source[239], true
	case 0x58:
		return source[240], true
	case 0x62:
		return source[241], true
	case 0x6c:
		return source[242], true
	case 0x76:
		return source[243], true
	case 0x80:
		return source[244], true
	case 0x8a:
		return source[245], true
	case 0x94:
		return source[246], true
	case 0x9e:
		return source[247], true
	case 0xa8:
		return source[248], true
	case 0xb2:
		return source[249], true
	case 0xbc:
		return source[250], true
	case 0xc6:
		return source[251], true
	case 0xd0:
		return source[252], true
	case 0xda:
		return source[253], true
	case 0xe4:
		return source[254], true
	case 0xee:
		return source[255], true
	}
	return nil, false
}
