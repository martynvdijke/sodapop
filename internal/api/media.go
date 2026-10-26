package api

import (
	"io"
	"net/http"
	"os"
	"strconv"

	"sodapop/internal/auth"
)

// mediaFull streams the original upload for one media row. Access is granted by
// the media service so the API and the web layer share exactly the same
// visibility rules: owners, public check-ins, accepted friends, public avatars
// and catalog images, and active venue media. Anything else is a 404.
func (s *Server) mediaFull(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	return s.serveAPIMedia(w, r, tok, false)
}

// mediaThumb streams the 480px thumbnail, falling back to the original when a
// row has none (videos).
func (s *Server) mediaThumb(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	return s.serveAPIMedia(w, r, tok, true)
}

func (s *Server) serveAPIMedia(w http.ResponseWriter, r *http.Request, tok *auth.APIToken, thumb bool) error {
	id := r.PathValue("id")
	ok, err := s.Media.CanView(r.Context(), tok.User.ID, id)
	if err != nil || !ok {
		// Never distinguish "missing" from "not visible", and never let an
		// earlier cached 200 survive a takedown.
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return nil
	}

	var rc io.ReadCloser
	var mime string
	if thumb {
		f, row, err := s.Media.OpenThumb(r.Context(), id)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return nil
		}
		rc, mime = f, row.Mime
	} else {
		f, row, err := s.Media.Open(r.Context(), id)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return nil
		}
		rc, mime = f, row.Mime
	}
	defer rc.Close()

	if mime != "" {
		w.Header().Set("Content-Type", mime)
	}
	if size, ok := mediaLength(rc); ok {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	// Photos are personal content; a private browser cache is fine but shared
	// caches must not retain them.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
	return nil
}

// mediaLength reports the size of a stored file so the client can render a
// progress bar. Only regular files opened by the media service qualify.
func mediaLength(rc io.ReadCloser) (int64, bool) {
	file, ok := rc.(*os.File)
	if !ok {
		return 0, false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), true
}
