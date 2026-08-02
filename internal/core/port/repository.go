package port

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id string) (*domain.User, error)
	FindByUsername(ctx context.Context, username string) (*domain.User, error)
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
}

type LibraryRepository interface {
	Create(ctx context.Context, library *domain.Library) error
	FindByID(ctx context.Context, id string) (*domain.Library, error)
	FindByOwnerID(ctx context.Context, ownerID string) ([]domain.Library, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.Library, error)
	AddMember(ctx context.Context, member *domain.LibraryMember) error
	RemoveMember(ctx context.Context, libraryID, userID string) error
	UpdateMemberRole(ctx context.Context, libraryID, userID, role string) error
	GetMembers(ctx context.Context, libraryID string) ([]domain.LibraryMember, error)
	Delete(ctx context.Context, id string) error
}

type ArtistRepository interface {
	BatchCreate(ctx context.Context, artists []domain.Artist) error
	FindByID(ctx context.Context, id string) (*domain.Artist, error)
	FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Artist, error)
	FindByNameAndLibrary(ctx context.Context, name, libraryID string) (*domain.Artist, error)
	Update(ctx context.Context, artist *domain.Artist) error
}

type AlbumRepository interface {
	BatchCreate(ctx context.Context, albums []domain.Album) error
	FindByID(ctx context.Context, id string) (*domain.Album, error)
	FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Album, error)
	FindByArtistID(ctx context.Context, artistID string) ([]domain.Album, error)
	FindByNameAndArtist(ctx context.Context, name, artistID, libraryID string) (*domain.Album, error)
	Update(ctx context.Context, album *domain.Album) error
}

type TrackRepository interface {
	BatchCreate(ctx context.Context, tracks []domain.Track) error
	FindByID(ctx context.Context, id string) (*domain.Track, error)
	FindByLibraryID(ctx context.Context, libraryIDs ...string) ([]domain.Track, error)
	FindByAlbumID(ctx context.Context, albumID string) ([]domain.Track, error)
	FindByArtistID(ctx context.Context, artistID string) ([]domain.Track, error)
	FindByHash(ctx context.Context, hash string) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
	DeleteByFilePath(ctx context.Context, path, libraryID string) (string, error)
	LoadTrackArtists(ctx context.Context, trackID string) ([]*domain.TrackArtist, error)
}

type ImageRepository interface {
	Create(ctx context.Context, image *domain.Image) error
	FindByID(ctx context.Context, id string) (*domain.Image, error)
	FindByOwner(ctx context.Context, ownerType, ownerID string) (*domain.Image, error)
	Delete(ctx context.Context, id string) error
}

type PlaylistRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	FindByID(ctx context.Context, id string) (*domain.Playlist, error)
	FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Playlist, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.Playlist, error)
	Update(ctx context.Context, playlist *domain.Playlist) error
	Delete(ctx context.Context, id string) error
}

type ScanJobRepository interface {
	Create(ctx context.Context, job *domain.ScanJob) error
	FindLatestByLibraryID(ctx context.Context, libraryID string) (*domain.ScanJob, error)
	Update(ctx context.Context, job *domain.ScanJob) error
}

type DownloadJobRepository interface {
	Create(ctx context.Context, job *domain.DownloadJob) error
	FindByID(ctx context.Context, id string) (*domain.DownloadJob, error)
	FindByLibraryID(ctx context.Context, libraryID string) ([]domain.DownloadJob, error)
	Update(ctx context.Context, job *domain.DownloadJob) error
}
