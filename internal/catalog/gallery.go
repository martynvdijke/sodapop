package catalog

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/drinkimage"
	"sodapop/internal/media"
)

// maxAttributionRunes is a display-only caption cap. DrinkImage has no uploader
// column, so attribution is free text supplied by the uploader (or a handle).
const maxAttributionRunes = 120

// ErrForbidden is returned when a non-moderator attempts a moderation action.
var ErrForbidden = errors.New("forbidden")

// CanModerateGallery reports whether a user role may promote, clear or delete
// gallery images. Admins and moderators share the destructive powers; plain
// users can only upload.
func CanModerateGallery(role string) bool {
	return role == "admin" || role == "moderator"
}

// ErrInvalidImage wraps unsupported or corrupt image uploads.
var ErrInvalidImage = errors.New("invalid image")

// DrinkImage is a community gallery entry for one drink. MediaID is derived
// from URL ("/media/{id}") and is useful for thumbnail URLs in templates.
type DrinkImage struct {
	ID          int
	MediaID     string
	URL         string
	ThumbURL    string
	IsPrimary   bool
	Attribution string
	CreatedAt   string
}

// Gallery lists one drink's images newest first. It never returns totals and
// returns ErrNotFound for an unknown or merged drink.
func (s *Service) Gallery(ctx context.Context, drinkID int) ([]DrinkImage, error) {
	if err := activeDrink(ctx, s.Client.Drink, drinkID); err != nil {
		return nil, err
	}
	rows, err := s.Client.DrinkImage.Query().
		Where(drinkimage.DrinkID(drinkID)).
		Order(ent.Desc(drinkimage.FieldCreatedAt), ent.Desc(drinkimage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DrinkImage, 0, len(rows))
	for _, row := range rows {
		mediaID := mediaIDFromURL(row.URL)
		thumbURL := row.URL
		if mediaID != "" {
			thumbURL = "/media/" + mediaID + "/thumb"
		}
		out = append(out, DrinkImage{
			ID:          row.ID,
			MediaID:     mediaID,
			URL:         row.URL,
			ThumbURL:    thumbURL,
			IsPrimary:   row.IsPrimary,
			Attribution: row.Attribution,
			CreatedAt:   row.CreatedAt,
		})
	}
	return out, nil
}

// AddImage stores the upload through mediaSvc with kind "drink" and attaches a
// DrinkImage row. New images are never promoted automatically; a moderator must
// choose a primary image explicitly. suppliedAttribution is trimmed and capped;
// an empty value falls back to the uploader's handle.
func (s *Service) AddImage(ctx context.Context, mediaSvc *media.Service, userID string, drinkID int, r io.Reader, filename, declaredMIME, suppliedAttribution string) (*ent.DrinkImage, error) {
	if mediaSvc == nil {
		return nil, errors.New("media service is not configured")
	}
	if err := activeDrink(ctx, s.Client.Drink, drinkID); err != nil {
		return nil, err
	}

	attribution := strings.TrimSpace(suppliedAttribution)
	if attribution == "" {
		if u, err := s.Client.User.Get(ctx, userID); err == nil {
			attribution = strings.TrimSpace(u.Handle)
		}
	}
	attribution = truncateRunes(attribution, maxAttributionRunes)

	stored, err := mediaSvc.Store(ctx, userID, string(mediaKindDrink), r, filename, declaredMIME)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrTooLarge):
			return nil, err
		case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrUnsupportedImage),
			errors.Is(err, media.ErrInvalidKind):
			return nil, ErrInvalidImage
		default:
			return nil, err
		}
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		s.discardStoredMedia(ctx, stored)
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Re-check under the transaction so a concurrent merge cannot attach an
	// image to a redirect row.
	if err := activeDrink(ctx, tx.Drink, drinkID); err != nil {
		// Roll back before cleanup: discardStoredMedia writes on another
		// connection and must not wait on this transaction's write lock.
		_ = tx.Rollback()
		s.discardStoredMedia(ctx, stored)
		return nil, err
	}
	row, err := tx.DrinkImage.Create().
		SetDrinkID(drinkID).
		SetURL("/media/" + stored.ID).
		SetIsPrimary(false).
		SetAttribution(attribution).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		s.discardStoredMedia(ctx, stored)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		s.discardStoredMedia(ctx, stored)
		return nil, err
	}
	return row, nil
}

// discardStoredMedia best-effort removes the media row and files created by
// AddImage when the attach transaction cannot commit. The upload would
// otherwise become unreachable but still occupy storage. Cleanup failures are
// ignored: the original error is the one worth reporting.
func (s *Service) discardStoredMedia(ctx context.Context, stored *ent.Media) {
	if stored == nil {
		return
	}
	paths := []string{stored.Path, stored.ThumbPath, stored.PosterPath}
	cleanupCtx := context.WithoutCancel(ctx)
	_ = s.Client.Media.DeleteOneID(stored.ID).Exec(cleanupCtx)
	s.removeMediaFiles(paths)
}

// SetPrimary makes imageID the single primary image for its drink. Moderators
// and admins only. The clear-then-set pair runs in one transaction so the
// partial unique index on (drink_id) WHERE is_primary = 1 can never see two
// primaries, even if two concurrent requests race.
func (s *Service) SetPrimary(ctx context.Context, moderator bool, imageID int) error {
	if !moderator {
		return ErrForbidden
	}
	if imageID <= 0 {
		return ErrNotFound
	}
	img, err := s.Client.DrinkImage.Get(ctx, imageID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if err := activeDrink(ctx, s.Client.Drink, img.DrinkID); err != nil {
		return err
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Start with a write so SQLite takes the write lock up front; a deferred
	// read-then-write transaction would fail fast with SQLITE_BUSY_SNAPSHOT
	// under concurrent moderation.
	if err := tx.DrinkImage.Update().
		Where(drinkimage.DrinkID(img.DrinkID), drinkimage.IsPrimary(true)).
		SetIsPrimary(false).
		Exec(ctx); err != nil {
		return err
	}
	if err := tx.DrinkImage.UpdateOneID(imageID).
		SetIsPrimary(true).
		Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearPrimary removes the primary flag from a drink's image, if any.
// Moderators and admins only. It leaves the image row in place.
func (s *Service) ClearPrimary(ctx context.Context, moderator bool, drinkID int) error {
	if !moderator {
		return ErrForbidden
	}
	if err := activeDrink(ctx, s.Client.Drink, drinkID); err != nil {
		return err
	}
	_, err := s.Client.DrinkImage.Update().
		Where(drinkimage.DrinkID(drinkID), drinkimage.IsPrimary(true)).
		SetIsPrimary(false).
		Save(ctx)
	return err
}

// DeleteImage permanently removes an image row (DrinkImage has no soft-delete
// column) and its media row. Moderators and admins only. The residual media
// files are removed best-effort after the transaction commits; a failed unlink
// leaves an orphan file on disk but the media URL still 404s because the row is
// gone. The drink itself is never touched.
func (s *Service) DeleteImage(ctx context.Context, moderator bool, imageID int) error {
	if !moderator {
		return ErrForbidden
	}
	if imageID <= 0 {
		return ErrNotFound
	}
	img, err := s.Client.DrinkImage.Get(ctx, imageID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}

	mediaID := mediaIDFromURL(img.URL)
	var paths []string
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.DrinkImage.DeleteOneID(imageID).Exec(ctx); err != nil {
		return err
	}
	if mediaID != "" {
		if stored, err := tx.Media.Get(ctx, mediaID); err == nil {
			paths = append(paths, stored.Path, stored.ThumbPath, stored.PosterPath)
			if err := tx.Media.DeleteOneID(stored.ID).Exec(ctx); err != nil {
				return err
			}
		} else if !ent.IsNotFound(err) {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.removeMediaFiles(paths)
	return nil
}

// removeMediaFiles unlinks stored files after their media row is gone. Paths
// are re-checked against the media root so a malformed row cannot delete
// arbitrary files. All errors are ignored: cleanup must not undo a takedown.
func (s *Service) removeMediaFiles(paths []string) {
	root := "media"
	if s.Cfg != nil && s.Cfg.DataDir != "" {
		root = filepath.Join(s.Cfg.DataDir, "media")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for _, rel := range paths {
		if strings.TrimSpace(rel) == "" || strings.ContainsRune(rel, 0) {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) {
			continue
		}
		full := filepath.Join(absRoot, clean)
		within, err := filepath.Rel(absRoot, full)
		if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
			continue
		}
		_ = os.Remove(full)
	}
}

// activeDrink proves a drink exists and has not been merged away.
func activeDrink(ctx context.Context, q *ent.DrinkClient, drinkID int) error {
	if drinkID <= 0 {
		return ErrNotFound
	}
	d, err := q.Get(ctx, drinkID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if d.Status == drink.StatusMerged {
		return ErrNotFound
	}
	return nil
}

func mediaIDFromURL(url string) string {
	const prefix = "/media/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	id := strings.TrimPrefix(url, prefix)
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// mediaKindDrink mirrors the generated Media kind without importing ent/media
// into every caller; media.Store accepts the string form.
const mediaKindDrink = "drink"

func truncateRunes(s string, limit int) string {
	if limit < 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
