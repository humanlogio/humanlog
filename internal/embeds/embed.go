package embeds

import "embed"

//go:embed assets
var Assets embed.FS

//go:embed assets/icon/systray/humanlog.icns
var HumanlogIconset []byte

//go:embed assets/icon/systray/humanlog_icon_512x512.png
var HumanlogIcon512x512 []byte
