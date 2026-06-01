package web

import "embed"

// Assets contains the public templates and static files used by the master UI.
//
//go:embed public/templates/*.html public/static/*
var Assets embed.FS
