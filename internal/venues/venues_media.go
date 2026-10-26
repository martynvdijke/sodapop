package venues

import (
	"context"
	"errors"
	"io"
	"strings"

	"sodapop/ent"
	"sodapop/ent/venuemedia"
	"sodapop/internal/db"
	"sodapop/internal/media"
)

const (
	// maxMediaPerUser is the active photo/menu cap for one uploader at a venue.
	maxMediaPerUser = 6
	// maxMediaPerVenue is the active photo/menu cap across all uploaders.
	maxMediaPerVenue = 20
	// maxMediaLabel caps a caption in runes, not bytes.
	maxMediaLabel = 80
)

var (
	// ErrInvalidKind is returned for a venue media kind other than photo|menu.
	ErrInvalidKind = errors.New("invalid venue media kind")
	// ErrInvalidLabel is returned when a label exceeds maxMediaLabel runes.
	ErrInvalidLabel = errors.New("venue media label too long")
	// ErrInvalidMedia wraps media validation failures.
	ErrInvalidMedia = errors.New("invalid venue media")
	// ErrTooLarge wraps media.ErrTooLarge.
	ErrTooLarge = errors.New("venue media too large")
	// ErrMediaLimit is returned when a venue or uploader is at its active cap.
	ErrMediaLimit = errors.New("venue media limit reached")
)

// VenueMediaView is a public venue photo or menu row. CanRemove is left false
// by the service; the web layer sets it from the viewer and uploader identity.
type VenueMediaView struct {
	ID             string
	MediaID        string
	Kind           string
	Label          string
	UploadedBy     string
	UploaderHandle string
	CreatedAt      string
	CanRemove      bool
}

// VenueMedia lists the active photos and menus for a venue, newest first.
func (s *Service) VenueMedia(ctx context.Context, venueID int) ([]VenueMediaView, error) {
	if err := activeVenue(ctx, s.Client.Venue, venueID); err != nil {
		return nil, err
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT vm.id, vm.media_id, vm.kind, vm.label, vm.uploaded_by,
		       COALESCE(u.handle, ''), vm.created_at
		FROM venue_media vm
		LEFT JOIN users u ON u.id = vm.uploaded_by
			AND u.deleted_at IS NULL AND u.status <> 'deleted'
		WHERE vm.venue_id = ? AND vm.deleted_at IS NULL
		ORDER BY vm.created_at DESC, vm.id DESC`, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []VenueMediaView{}
	for rows.Next() {
		var v VenueMediaView
		if err := rows.Scan(&v.ID, &v.MediaID, &v.Kind, &v.Label, &v.UploadedBy,
			&v.UploaderHandle, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AddVenueMedia stores an image through mediaSvc and attaches it to a venue.
// The reported caps are checked before storing so an obviously rejected upload
// never writes a file, then re-checked inside the insert transaction so two
// concurrent uploads cannot both squeeze past a cap. The transaction can still
// fail after Store wrote the file (for example on losing that race), leaving an
// orphaned media row/file; that is accepted rather than deleting user bytes.
func (s *Service) AddVenueMedia(ctx context.Context, mediaSvc *media.Service, userID string, venueID int, kind, label string, r io.Reader, filename, declaredMIME string) (*ent.VenueMedia, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = string(venuemedia.KindPhoto)
	}
	if kind != string(venuemedia.KindPhoto) && kind != string(venuemedia.KindMenu) {
		return nil, ErrInvalidKind
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > maxMediaLabel {
		return nil, ErrInvalidLabel
	}
	if err := activeVenue(ctx, s.Client.Venue, venueID); err != nil {
		return nil, err
	}
	if err := activeMediaCap(ctx, s.Client.VenueMedia, userID, venueID); err != nil {
		return nil, err
	}

	stored, err := mediaSvc.Store(ctx, userID, "venue", r, filename, declaredMIME)
	if err != nil {
		return nil, mapMediaError(err)
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := activeVenue(ctx, tx.Venue, venueID); err != nil {
		return nil, err
	}
	if err := activeMediaCap(ctx, tx.VenueMedia, userID, venueID); err != nil {
		return nil, err
	}
	row, err := tx.VenueMedia.Create().
		SetVenueID(venueID).
		SetMediaID(stored.ID).
		SetKind(venuemedia.Kind(kind)).
		SetLabel(label).
		SetUploadedBy(userID).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

// RemoveVenueMedia soft-deletes an active venue media row. The uploader or an
// admin may remove it; everyone else sees ErrNotFound so the row and its media
// stay private. The media row and file are deliberately kept.
func (s *Service) RemoveVenueMedia(ctx context.Context, userID string, admin bool, id string) error {
	row, err := s.Client.VenueMedia.Query().
		Where(venuemedia.ID(id), venuemedia.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if !admin && row.UploadedBy != userID {
		return ErrNotFound
	}
	return s.Client.VenueMedia.UpdateOneID(row.ID).
		SetDeletedAt(db.Now()).
		Exec(ctx)
}

// activeVenue proves a venue exists and has not been merged away.
func activeVenue(ctx context.Context, q *ent.VenueClient, venueID int) error {
	if venueID <= 0 {
		return ErrNotFound
	}
	v, err := q.Get(ctx, venueID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if v.MergedIntoID != nil {
		return ErrNotFound
	}
	return nil
}

// activeMediaCap enforces both active rows caps for a venue.
func activeMediaCap(ctx context.Context, q *ent.VenueMediaClient, userID string, venueID int) error {
	byUser, err := q.Query().
		Where(
			venuemedia.VenueID(venueID),
			venuemedia.UploadedBy(userID),
			venuemedia.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if byUser >= maxMediaPerUser {
		return ErrMediaLimit
	}
	total, err := q.Query().
		Where(
			venuemedia.VenueID(venueID),
			venuemedia.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if total >= maxMediaPerVenue {
		return ErrMediaLimit
	}
	return nil
}

func mapMediaError(err error) error {
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return ErrTooLarge
	case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrUnsupportedImage):
		return ErrInvalidMedia
	default:
		return err
	}
}
