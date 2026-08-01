package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embeddedAssets embed.FS

const fallbackIndexHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>iCloud HME</title>
  </head>
  <body>
    <p>Frontend assets are not built. Run the frontend production build first.</p>
  </body>
</html>
`

// Assets returns the frontend distribution filesystem rooted at dist/.
func Assets() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}

// FallbackIndexHTML returns a minimal page for source-only Go builds without web/dist.
func FallbackIndexHTML() []byte {
	return []byte(fallbackIndexHTML)
}
