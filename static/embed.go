package static

import "embed"

//go:embed css/output.css js/htmx.min.js js/alpine.min.js images/logo.png images/isotype.png  images/favicon/*
var Assets embed.FS
