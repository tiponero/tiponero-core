package static

import "embed"

//go:embed css/output.css css/fonts.css fonts/* js/htmx.min.js js/alpine.min.js images/logo.svg images/login-lockup.svg images/favicon/*
var Assets embed.FS
