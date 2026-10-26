// Package media owns upload storage, image normalisation and media access
// checks. Files live under <DATA_DIR>/media/YYYY/MM and the media table keeps
// the relative path, so a database backup plus that directory is portable.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"sodapop/ent"
	"sodapop/ent/media"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

var (
	// ErrUnsupported is returned for content that is neither a supported image
	// nor a supported video.
	ErrUnsupported = errors.New("unsupported media type")
	// ErrUnsupportedImage marks image containers such as HEIC and AVIF that we
	// cannot decode in-process.
	ErrUnsupportedImage = errors.New("unsupported image format")
	// ErrTooLarge is returned when the upload exceeds the photo or video cap.
	ErrTooLarge = errors.New("file too large")
	// ErrNotFound hides both missing rows and missing files from callers.
	ErrNotFound = errors.New("media not found")
	// ErrInvalidPath means a stored path tried to escape the media root.
	ErrInvalidPath = errors.New("invalid media path")
	// ErrInvalidKind means the caller supplied an unknown media kind.
	ErrInvalidKind = errors.New("invalid media kind")
)

const (
	maxPhotoBytes = 12 << 20
	maxVideoBytes = 120 << 20
	maxPhotoEdge  = 1600
	thumbEdge     = 480
	jpegQuality   = 85
	// Refuse decompression bombs before image.Decode allocates a huge buffer.
	maxPixels = 100_000_000
	sniffLen  = 512
)

// Service stores and opens media rows.
type Service struct {
	Client *ent.Client
	Dir    string

	sql *sql.DB
}

// New wires the media service to the shared database and the data directory.
// Uploads go to <DataDir>/media.
func New(database *db.DB, cfg *config.Config) *Service {
	dir := "media"
	if cfg != nil && cfg.DataDir != "" {
		dir = filepath.Join(cfg.DataDir, "media")
	}
	s := &Service{Dir: dir}
	if database != nil {
		s.Client = database.Client
		s.sql = database.SQL
	}
	return s
}

// Store validates an upload, normalises it and writes the media row. Images are
// re-encoded to JPEG (EXIF stripped, longest edge 1600px, 480px thumbnail);
// videos are streamed to disk untouched and never get a poster without ffmpeg.
func (s *Service) Store(ctx context.Context, userID, kind string, r io.Reader, filename, declaredMIME string) (*ent.Media, error) {
	if r == nil || s.Client == nil {
		return nil, ErrUnsupported
	}
	mediaKind, err := normalizeKind(kind)
	if err != nil {
		return nil, err
	}

	ext := safeExt(filename)
	head, err := readHead(r)
	if err != nil {
		return nil, err
	}
	if len(head) == 0 {
		return nil, ErrUnsupported
	}

	sniffed := baseMIME(http.DetectContentType(head))
	if isHEIF(head, ext, declaredMIME, sniffed) {
		return nil, ErrUnsupportedImage
	}

	if mimeType, ok := videoMIME(sniffed, head, ext); ok {
		return s.storeVideo(ctx, userID, r, head, mimeType)
	}

	if !isSupportedImage(sniffed) {
		return nil, ErrUnsupported
	}
	// Image content never becomes a video row; the caller's hint is only used
	// for check-in/avatar/drink/venue/brand photos.
	if mediaKind == media.KindVideo {
		mediaKind = media.KindCheckin
	}
	return s.storeImage(ctx, userID, mediaKind, r, head)
}

// Open streams the main file for a media row. A row without a main path falls
// back to its thumbnail.
func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, *ent.Media, error) {
	row, err := s.lookup(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	rel := row.Path
	if strings.TrimSpace(rel) == "" {
		rel = row.ThumbPath
	}
	file, err := s.openRelative(rel)
	if err != nil {
		return nil, nil, err
	}
	return file, row, nil
}

// OpenThumb streams the thumbnail, falling back to the main file when a row has
// no thumbnail (videos).
func (s *Service) OpenThumb(ctx context.Context, id string) (io.ReadCloser, *ent.Media, error) {
	row, err := s.lookup(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	rel := row.ThumbPath
	if strings.TrimSpace(rel) == "" {
		rel = row.Path
	}
	file, err := s.openRelative(rel)
	if err != nil {
		return nil, nil, err
	}
	return file, row, nil
}

// CanView reports whether viewerID may see the media. Owners always can.
// Check-in photos follow check-in visibility, with friends-only rows visible to
// accepted friends. Catalog-shaped media (avatars and drink/brand images) is
// public. Venue media is public only while an active, non-deleted venue_media
// row and a non-merged venue still reference it, so a removed photo stops being
// anonymously fetchable.
func (s *Service) CanView(ctx context.Context, viewerID, id string) (bool, error) {
	row, err := s.lookup(ctx, id)
	if err != nil {
		return false, err
	}
	if viewerID != "" && row.UserID != nil && *row.UserID == viewerID {
		return true, nil
	}
	switch row.Kind {
	case media.KindAvatar, media.KindDrink, media.KindBrand:
		return true, nil
	}
	if s.sql == nil {
		return false, errors.New("media: database handle is not configured")
	}

	// The friends clause is skipped for anonymous viewers by the ? <> ''
	// guard before the friendship table is touched.
	var visible bool
	err = s.sql.QueryRowContext(ctx, canViewQuery, id, viewerID, viewerID, viewerID, id, id, id).Scan(&visible)
	if err != nil {
		return false, err
	}
	return visible, nil
}

const canViewQuery = `
SELECT EXISTS (
	SELECT 1 FROM checkins c
	JOIN users u ON u.id = c.user_id
	WHERE c.photo_media_id = ?
	  AND c.deleted_at IS NULL
	  AND u.deleted_at IS NULL AND u.status <> 'deleted'
	  AND (
		c.visibility = 'public'
		OR (
			c.visibility = 'friends'
			AND ? <> ''
			AND EXISTS (
				SELECT 1 FROM friendships f
				WHERE f.status = 'accepted'
				  AND (
					(f.user_a = ? AND f.user_b = c.user_id)
					OR (f.user_b = ? AND f.user_a = c.user_id)
				  )
			)
		)
	  )
)
OR EXISTS (SELECT 1 FROM users u WHERE u.avatar_media_id = ? AND u.deleted_at IS NULL)
OR EXISTS (SELECT 1 FROM brands b WHERE b.logo_media_id = ?)
OR EXISTS (SELECT 1 FROM venue_media vm JOIN venues v ON v.id = vm.venue_id WHERE vm.media_id = ? AND vm.deleted_at IS NULL AND v.merged_into_id IS NULL)`

func (s *Service) lookup(ctx context.Context, id string) (*ent.Media, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrNotFound
	}
	row, err := s.Client.Media.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return row, nil
}

func (s *Service) storeImage(ctx context.Context, userID string, kind media.Kind, r io.Reader, head []byte) (*ent.Media, error) {
	data, err := readRest(head, r, maxPhotoBytes)
	if err != nil {
		return nil, err
	}
	imgConfig, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	switch format {
	case "jpeg", "png", "gif", "webp":
	default:
		return nil, ErrUnsupported
	}
	if imgConfig.Width <= 0 || imgConfig.Height <= 0 {
		return nil, ErrUnsupported
	}
	// Refuse decompression bombs before image.Decode allocates the full buffer.
	if int64(imgConfig.Width)*int64(imgConfig.Height) > maxPixels {
		return nil, fmt.Errorf("%w: image dimensions are too large", ErrUnsupported)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return nil, ErrUnsupported
	}

	full, width, height, err := encodeJPEG(img, maxPhotoEdge)
	if err != nil {
		return nil, err
	}
	thumb, _, _, err := encodeJPEG(img, thumbEdge)
	if err != nil {
		return nil, err
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	dir := time.Now().UTC().Format("2006/01")
	rel := path.Join(dir, id+".jpg")
	thumbRel := path.Join(dir, id+".thumb.jpg")

	fullPath, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(fullPath, full); err != nil {
		return nil, err
	}
	thumbPath, err := s.resolve(thumbRel)
	if err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	if err := writeAtomic(thumbPath, thumb); err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}

	builder := s.Client.Media.Create().
		SetKind(kind).
		SetPath(pathSlash(rel)).
		SetThumbPath(pathSlash(thumbRel)).
		SetPosterPath("").
		SetMime("image/jpeg").
		SetWidth(width).
		SetHeight(height).
		SetBytes(len(full))
	if userID != "" {
		builder.SetUserID(userID)
	}
	row, err := builder.Save(ctx)
	if err != nil {
		_ = os.Remove(fullPath)
		_ = os.Remove(thumbPath)
		return nil, err
	}
	return row, nil
}

func (s *Service) storeVideo(ctx context.Context, userID string, r io.Reader, head []byte, mimeType string) (*ent.Media, error) {
	ext := ".mp4"
	switch mimeType {
	case "video/webm":
		ext = ".webm"
	case "video/quicktime":
		ext = ".mov"
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	dir := time.Now().UTC().Format("2006/01")
	rel := path.Join(dir, id+ext)
	fullPath, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}

	size, err := writeStream(fullPath, io.MultiReader(bytes.NewReader(head), r), maxVideoBytes)
	if err != nil {
		return nil, err
	}

	builder := s.Client.Media.Create().
		SetKind(media.KindVideo).
		SetPath(pathSlash(rel)).
		SetThumbPath("").
		SetPosterPath("").
		SetMime(mimeType).
		SetBytes(int(size))
	if userID != "" {
		builder.SetUserID(userID)
	}
	row, err := builder.Save(ctx)
	if err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	return row, nil
}

func (s *Service) openRelative(rel string) (io.ReadCloser, error) {
	full, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return file, nil
}

// resolve cleans a database path and proves the result stays below the media
// root. It rejects absolute paths, ".." segments and NUL bytes.
func (s *Service) resolve(rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", ErrInvalidPath
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	root, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, clean)
	within, err := filepath.Rel(root, full)
	if err != nil || within == ".." ||
		strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", ErrInvalidPath
	}
	return full, nil
}

func normalizeKind(raw string) (media.Kind, error) {
	kind := media.Kind(strings.ToLower(strings.TrimSpace(raw)))
	if kind == "" {
		return media.KindCheckin, nil
	}
	if err := media.KindValidator(kind); err != nil {
		return "", ErrInvalidKind
	}
	return kind, nil
}

func readHead(r io.Reader) ([]byte, error) {
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return head[:n], nil
}

func readRest(head []byte, r io.Reader, limit int64) ([]byte, error) {
	remaining := limit - int64(len(head)) + 1
	if remaining < 1 {
		remaining = 1
	}
	rest, err := io.ReadAll(io.LimitReader(r, remaining))
	if err != nil {
		return nil, err
	}
	if int64(len(head))+int64(len(rest)) > limit {
		return nil, ErrTooLarge
	}
	data := make([]byte, 0, len(head)+len(rest))
	data = append(data, head...)
	data = append(data, rest...)
	return data, nil
}

// encodeJPEG flattens transparency over the Sodapop cream background, scales
// the longest edge down to maxEdge and encodes a quality-85 JPEG.
func encodeJPEG(src image.Image, maxEdge int) ([]byte, int, int, error) {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, 0, 0, ErrUnsupported
	}

	dstWidth, dstHeight := width, height
	if width > maxEdge || height > maxEdge {
		scale := float64(maxEdge) / float64(width)
		if height > width {
			scale = float64(maxEdge) / float64(height)
		}
		dstWidth = maxInt(1, int(math.Round(float64(width)*scale)))
		dstHeight = maxInt(1, int(math.Round(float64(height)*scale)))
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	cream := image.NewUniform(color.RGBA{R: 0xF2, G: 0xF4, B: 0xE6, A: 0xFF})
	stddraw.Draw(dst, dst.Bounds(), cream, image.Point{}, stddraw.Src)
	if dstWidth == width && dstHeight == height {
		stddraw.Draw(dst, dst.Bounds(), src, bounds.Min, stddraw.Over)
	} else {
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), dstWidth, dstHeight, nil
}

func writeAtomic(full string, data []byte) (err error) {
	dir := filepath.Dir(full)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	_ = tmp.Chmod(0o640)
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, full)
}

func writeStream(full string, src io.Reader, limit int64) (size int64, err error) {
	dir := filepath.Dir(full)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return 0, err
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(name)
		}
	}()

	size, err = io.Copy(tmp, io.LimitReader(src, limit+1))
	if err != nil {
		return size, err
	}
	if size > limit {
		return size, ErrTooLarge
	}
	_ = tmp.Chmod(0o640)
	if err = tmp.Close(); err != nil {
		return size, err
	}
	if err = os.Rename(name, full); err != nil {
		return size, err
	}
	return size, nil
}

func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// safeExt extracts a conservative lowercase extension. The original filename is
// never used for storage; the random ID is the only path component we control.
func safeExt(filename string) string {
	name := strings.TrimSpace(filename)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" || len(ext) > 5 {
		return ""
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

func baseMIME(value string) string {
	if i := strings.IndexByte(value, ';'); i >= 0 {
		value = value[:i]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedImage(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// isHEIF catches formats that image.Decode cannot handle so callers get
// "unsupported image format" instead of a generic sniff failure.
func isHEIF(head []byte, ext, declaredMIME, sniffed string) bool {
	switch baseMIME(declaredMIME) {
	case "image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence", "image/avif":
		return true
	}
	switch ext {
	case "heic", "heif", "avif", "hif":
		return true
	}
	switch sniffed {
	case "image/heic", "image/heif", "image/avif":
		return true
	}
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return false
	}
	return isHEIFBrand(string(head[8:12]))
}

func isHEIFBrand(brand string) bool {
	switch brand {
	case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1", "msf1", "avif", "avis":
		return true
	default:
		return false
	}
}

// videoMIME classifies MP4, WebM and QuickTime. ISO base media files can sniff
// as application/octet-stream, so the ftyp box and extension are used as
// fallbacks. Arbitrary bytes with a .mp4 name are still rejected.
func videoMIME(sniffed string, head []byte, ext string) (string, bool) {
	switch sniffed {
	case "video/mp4":
		return "video/mp4", true
	case "video/webm":
		return "video/webm", true
	case "video/quicktime":
		return "video/quicktime", true
	}

	isISOBMFF := len(head) >= 12 && string(head[4:8]) == "ftyp"
	if isISOBMFF {
		brand := string(head[8:12])
		if brand == "qt  " || ext == "mov" || ext == "qt" {
			return "video/quicktime", true
		}
		return "video/mp4", true
	}
	if sniffed != "application/octet-stream" && !strings.HasPrefix(sniffed, "video/") {
		return "", false
	}
	switch ext {
	case "mp4", "m4v":
		return "video/mp4", true
	case "webm":
		return "video/webm", true
	case "mov", "qt":
		return "video/quicktime", true
	default:
		return "", false
	}
}

func pathSlash(value string) string {
	return filepath.ToSlash(value)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
