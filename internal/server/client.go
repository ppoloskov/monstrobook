package server

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

// clientProxyHandler forwards unmatched UI requests to an upstream web client
// server (such as the v3 Next.js React client running on localhost).
func clientProxyHandler(targetURL, basePath string) http.Handler {
	target, err := url.Parse(targetURL)
	if err != nil || target.Host == "" {
		return http.NotFoundHandler()
	}
	base := strings.TrimRight(basePath, "/")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
			if base != "" {
				p := pr.Out.URL.Path
				if p == "/" {
					p = ""
				}
				pr.Out.URL.Path = base + p
				pr.Out.URL.RawPath = ""
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("web client proxy error", "path", r.URL.Path, "error", err)
			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		},
	}
	return proxy
}

// clientHandler serves the web client from root. The client is a Nuxt SPA
// (ssr false, target static) whose router renders its own 404 page, so any
// GET that is not a file and does not look like an asset (no extension) gets
// index.html. A missing asset is a plain 404. Directories are never listed.
func clientHandler(root string) http.Handler {
	if root == "" {
		return http.NotFoundHandler()
	}
	files := os.DirFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		switch {
		case serveFile(w, r, files, name):
		case path.Ext(name) == "":
			if !serveFile(w, r, files, "index.html") {
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	})
}

// serveFile serves name when it is a regular file and reports whether it did.
func serveFile(w http.ResponseWriter, r *http.Request, files fs.FS, name string) bool {
	file, err := files.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	content, seekable := file.(io.ReadSeeker)
	if err != nil || !info.Mode().IsRegular() || !seekable {
		return false
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), content)
	return true
}
