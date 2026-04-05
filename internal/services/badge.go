package services

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tiponero/tiponero-core/internal/database"

	qrcode "github.com/skip2/go-qrcode"
)

type BadgeParams struct {
	Widget    *database.Widget
	Stats     *database.WidgetStats
	WidgetURL string
}

func GenerateBadgeSVG(p BadgeParams) ([]byte, error) {
	qr, err := qrcode.New(p.WidgetURL, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}
	qr.DisableBorder = true

	bitmap := qr.Bitmap()
	qrSVG := renderQRSVG(bitmap, 120)

	var svg strings.Builder
	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="400" height="170" viewBox="0 0 400 170">`)
	svg.WriteString(`<defs><style>`)
	switch p.Widget.Theme {
	case database.ThemeLight:
		svg.WriteString(`.bg{fill:#F9FAFB}.qr-bg{fill:#f5f5f5}.qr-fg{fill:#1a1a1a}.t-stat{fill:#4a4a4a}.t-muted{fill:#999999}`)
	case database.ThemeDark:
		svg.WriteString(`.bg{fill:#201F1F}.qr-bg{fill:#e5e2e1}.qr-fg{fill:#0e0e0e}.t-stat{fill:#e3bfb1}.t-muted{fill:#938f8e}`)
	default:
		svg.WriteString(`.bg{fill:#201F1F}.qr-bg{fill:#e5e2e1}.qr-fg{fill:#0e0e0e}.t-stat{fill:#e3bfb1}.t-muted{fill:#938f8e}`)
		svg.WriteString(`@media(prefers-color-scheme:light){.bg{fill:#F9FAFB}.qr-bg{fill:#f5f5f5}.qr-fg{fill:#1a1a1a}.t-stat{fill:#4a4a4a}.t-muted{fill:#999999}}`)
	}
	svg.WriteString(`</style></defs>`)
	svg.WriteString(`<rect width="400" height="170" rx="6" class="bg"/>`)

	svg.WriteString(`<g transform="translate(260, 16)">`)
	svg.WriteString(`<rect x="-4" y="-4" width="128" height="128" rx="4" class="qr-bg"/>`)
	svg.WriteString(`<g class="qr-fg">`)
	svg.WriteString(qrSVG)
	svg.WriteString(`</g>`)
	svg.WriteString(`</g>`)

	fmt.Fprintf(&svg,
		`<text x="20" y="42" fill="%s" font-family="'Space Grotesk','Inter',system-ui,sans-serif" font-size="18" font-weight="700" letter-spacing="-0.02em">%s</text>`,
		escapeXML(p.Widget.PrimaryColor), escapeXML(p.Widget.ButtonText),
	)

	if p.Widget.ShowStats && p.Stats != nil {
		xmr := formatXMRBadge(p.Stats.TotalAmount)
		fmt.Fprintf(&svg,
			`<text x="20" y="76" class="t-stat" font-family="'JetBrains Mono','Inter',monospace" font-size="13" font-weight="500">%s XMR received</text>`,
			xmr,
		)
		fmt.Fprintf(&svg,
			`<text x="20" y="100" class="t-stat" font-family="'Inter','Space Grotesk',system-ui,sans-serif" font-size="13">%d transaction%s</text>`,
			p.Stats.TotalTransactions, pluralS(p.Stats.TotalTransactions),
		)
	} else {
		svg.WriteString(
			`<text x="20" y="80" class="t-stat" font-family="'Inter','Space Grotesk',system-ui,sans-serif" font-size="13">Donate with Monero</text>`,
		)
	}

	svg.WriteString(
		`<text x="20" y="150" class="t-muted" font-family="'Inter','Space Grotesk',system-ui,sans-serif" font-size="11">Powered by Tiponero</text>`,
	)

	svg.WriteString(`</svg>`)

	return []byte(svg.String()), nil
}

func renderQRSVG(bitmap [][]bool, size int) string {
	modules := len(bitmap)
	if modules == 0 {
		return ""
	}

	cellSize := float64(size) / float64(modules)

	var svg strings.Builder
	for y, row := range bitmap {
		for x, set := range row {
			if set {
				fmt.Fprintf(&svg,
					`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
					float64(x)*cellSize, float64(y)*cellSize, cellSize+0.5, cellSize+0.5,
				)
			}
		}
	}
	return svg.String()
}

func formatXMRBadge(piconero int64) string {
	return strconv.FormatFloat(float64(piconero)/1e12, 'f', -1, 64)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
