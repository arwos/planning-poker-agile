/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// web contains the Angular production bundle produced by `pnpm build`.
//
//go:embed all:web
var web embed.FS

func (s *Server) frontend() http.Handler {
	assets, err := fs.Sub(web, "web")
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested == "room" || strings.HasPrefix(requested, "room/") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		}
		if requested == "." || requested == "" {
			s.serveIndex(w, assets)
			return
		}
		if _, statErr := fs.Stat(assets, requested); statErr == nil {
			if requested != "index.html" {
				w.Header().Set("Cache-Control", "public, max-age=7776000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(requested), ".") {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w, assets)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, assets fs.FS) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}
