package download

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	name string
}

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) Match(url string) bool {
	return url == f.name
}
func (f *fakeSource) Resolve(ctx context.Context, url string) (*port.SourceInfo, error) {
	return nil, nil
}
func (f *fakeSource) Fetch(ctx context.Context, job *domain.DownloadJob) error {
	return nil
}

func TestNewManagerRegistersDirectSource(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)
	assert.Len(t, m.sources, 1)
	assert.Equal(t, "direct", m.sources[0].Name())
}

func TestManagerMatchSource(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)
	m.Register(&fakeSource{name: "fake"})

	assert.Equal(t, "fake", m.MatchSource("fake").Name())
	assert.Equal(t, "direct", m.MatchSource("https://example.com/x.mp3").Name())
	assert.Nil(t, m.MatchSource("unknown-scheme://x"))
}

func TestManagerCreateJobNoSource(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)
	job, err := m.CreateJob(context.Background(), "not-a-url", "lib-1")
	require.Nil(t, job)
	require.Error(t, err)

	var noSource *ErrNoSource
	require.ErrorAs(t, err, &noSource)
	assert.Equal(t, "not-a-url", noSource.URL)
	assert.Contains(t, err.Error(), "no download source supports URL")
}

func TestManagerCreateJobPersists(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO download_jobs`)).
		WithArgs(
			sqlmock.AnyArg(), "https://example.com/x.mp3", "direct", "lib-1",
			"", "pending", 0.0, "", "", "", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	job, err := m.CreateJob(context.Background(), "https://example.com/x.mp3", "lib-1")
	require.NoError(t, err)
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "pending", job.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerCreateJobCreateError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO download_jobs`)).
		WillReturnError(sql.ErrConnDone)

	_, err = m.CreateJob(context.Background(), "https://example.com/x.mp3", "lib-1")
	require.Error(t, err)
}

func TestManagerCancelUnknownJob(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	m := NewManager(db)
	m.Cancel("never-started") // must not panic
}

func TestErrNoSourceError(t *testing.T) {
	err := &ErrNoSource{URL: "http://x"}
	assert.Equal(t, "no download source supports URL: http://x", err.Error())
}
