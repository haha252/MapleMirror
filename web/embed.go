package web

import "embed"

// Assets contains the public and admin templates/static files used by the master UI.
//
//go:embed public/templates/*.html public/static/* admin/templates/*.html admin/static/*
var Assets embed.FS
