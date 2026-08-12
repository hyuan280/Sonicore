package transcoder

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- MP4 box builders ----

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[:4], uint32(8+len(payload)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func fullBox(typ string, version byte, payload []byte) []byte {
	return box(typ, append([]byte{version, 0, 0, 0}, payload...))
}

// mdhd with a 32-bit (version 0) timescale at the standard offset.
func mdhdV0(timescale uint32) []byte {
	payload := make([]byte, 4+4+4+4+2+2)
	binary.BigEndian.PutUint32(payload[0:4], 0)   // creation
	binary.BigEndian.PutUint32(payload[4:8], 0)   // modification
	binary.BigEndian.PutUint32(payload[8:12], timescale)
	binary.BigEndian.PutUint32(payload[12:16], 1000000) // duration
	return fullBox("mdhd", 0, payload)
}

// tfdt with a 32-bit (version 0) base media decode time.
func tfdtV0(base uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, base)
	return fullBox("tfdt", 0, payload)
}

func moovWithTimescale(timescale uint32) []byte {
	mdia := box("mdia", mdhdV0(timescale))
	trak := box("trak", mdia)
	return box("moov", trak)
}

func fragment(base uint32) []byte {
	traf := box("traf", tfdtV0(base))
	moof := box("moof", traf)
	mdat := box("mdat", make([]byte, 64))
	return append(moof, mdat...)
}

func fmp4File(timescale uint32, bases ...uint32) []byte {
	data := box("ftyp", []byte("isom"))
	data = append(data, moovWithTimescale(timescale)...)
	for _, b := range bases {
		data = append(data, fragment(b)...)
	}
	return data
}

// ---- box parsing primitives ----

func TestReadBoxAt(t *testing.T) {
	data := box("mdat", make([]byte, 10))
	r := bytes.NewReader(data)

	size, typ, end, ok := readBoxAt(r, 0)
	require.True(t, ok)
	assert.Equal(t, int64(18), size)
	assert.Equal(t, "mdat", typ)
	assert.Equal(t, int64(18), end)
}

func TestReadBoxAtLargeSize(t *testing.T) {
	b := make([]byte, 16+8)
	binary.BigEndian.PutUint32(b[:4], 1) // extended size marker
	copy(b[4:8], "wide")
	binary.BigEndian.PutUint64(b[8:16], 4096)
	r := bytes.NewReader(b)

	size, _, end, ok := readBoxAt(r, 0)
	require.True(t, ok)
	assert.Equal(t, int64(4096), size)
	assert.Equal(t, int64(4096), end)
}

func TestReadBoxAtSizeZero(t *testing.T) {
	b := make([]byte, 8)
	copy(b[4:8], "free")
	r := bytes.NewReader(b)

	size, typ, end, ok := readBoxAt(r, 0)
	require.True(t, ok)
	assert.Equal(t, int64(-1), size, "size 0 means to-end-of-file")
	assert.Equal(t, "free", typ)
	assert.Equal(t, int64(-1), end)
}

func TestReadBoxAtTruncated(t *testing.T) {
	r := bytes.NewReader([]byte{0, 0, 0, 4, 'f'})
	_, _, _, ok := readBoxAt(r, 0)
	assert.False(t, ok)
}

func TestFindBox(t *testing.T) {
	inner := box("traf", tfdtV0(100))
	data := box("moof", inner)
	r := bytes.NewReader(data)

	off, size, ok := findBox(r, 8, int64(len(data)), "traf")
	require.True(t, ok)
	assert.Equal(t, int64(8), off)
	assert.Equal(t, int64(len(inner)), size)

	_, _, ok = findBox(r, 8, int64(len(data)), "nope")
	assert.False(t, ok)
}

func TestFindTimescale(t *testing.T) {
	data := moovWithTimescale(44100)
	r := bytes.NewReader(data)

	ts, ok := findTimescale(r, 0, int64(len(data)))
	require.True(t, ok)
	assert.Equal(t, uint32(44100), ts)
}

func TestReadTfdtValue(t *testing.T) {
	data := tfdtV0(12345)
	r := bytes.NewReader(data)

	v, ok := readTfdtValue(r, 0)
	require.True(t, ok)
	assert.Equal(t, uint64(12345), v)
}

func TestFindTfdt(t *testing.T) {
	data := box("moof", box("traf", tfdtV0(999)))
	r := bytes.NewReader(data)

	v, ok := findTfdt(r, 0, int64(len(data)))
	require.True(t, ok)
	assert.Equal(t, uint64(999), v)
}

// ---- index building ----

func writeTempFmp4(t *testing.T, data []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "frag*.m4s")
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	_, err = f.Write(data)
	require.NoError(t, err)
	return f
}

func TestBuildFmp4Index(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	moov := moovWithTimescale(1000)
	initSize := int64(len(ftyp) + len(moov))

	data := ftyp
	data = append(data, moov...)
	frag0 := fragment(0)
	frag10 := fragment(10000)
	data = append(data, frag0...)
	data = append(data, frag10...)

	f := writeTempFmp4(t, data)
	idx, err := buildFmp4Index(f)
	require.NoError(t, err)

	assert.Equal(t, initSize, idx.initSize)
	require.Len(t, idx.frags, 2)

	assert.Equal(t, float64(0), idx.frags[0].start)
	assert.Equal(t, initSize, idx.frags[0].offset)
	assert.Equal(t, int64(len(frag0)), idx.frags[0].size)

	assert.Equal(t, float64(10), idx.frags[1].start, "10000/1000 = 10s")
	assert.Equal(t, initSize+int64(len(frag0)), idx.frags[1].offset)
	assert.Equal(t, int64(len(frag10)), idx.frags[1].size)

	assert.Equal(t, float64(20), idx.duration, "last start + step")
}

func TestBuildFmp4IndexIgnoresPartialTail(t *testing.T) {
	data := fmp4File(1000, 0, 10000)
	// cut mid-way through the second mdat: trailing box is incomplete
	f := writeTempFmp4(t, data[:len(data)-32])

	idx, err := buildFmp4Index(f)
	require.NoError(t, err)
	require.Len(t, idx.frags, 1, "partial trailing fragment must be ignored")
	assert.Equal(t, float64(0), idx.frags[0].start)
}

func TestBuildFmp4IndexEmptyFile(t *testing.T) {
	f := writeTempFmp4(t, []byte{})
	idx, err := buildFmp4Index(f)
	require.NoError(t, err)
	assert.Empty(t, idx.frags)
	assert.Zero(t, idx.duration)
}

func TestFragForTime(t *testing.T) {
	idx := fmp4Index{frags: []fmp4Fragment{
		{start: 0},
		{start: 10},
		{start: 20},
	}}

	tests := []struct {
		t    float64
		want int
	}{
		{0, 0},
		{5, 0},
		{10, 1},
		{19.9, 1},
		{20, 2},
		{999, 2},
		{-5, 0},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, fragForTime(idx, tt.t), "t=%v", tt.t)
	}
}

func TestFragForTimeEmpty(t *testing.T) {
	assert.Equal(t, 0, fragForTime(fmp4Index{}, 5))
}

func TestExtractFragments(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	moov := moovWithTimescale(1000)
	frag0 := fragment(0)
	frag10 := fragment(10000)

	data := append(ftyp, moov...)
	data = append(data, frag0...)
	data = append(data, frag10...)

	f := writeTempFmp4(t, data)
	idx, err := buildFmp4Index(f)
	require.NoError(t, err)

	// from frag0 to frag10 inclusive
	out, err := extractFragments(f, idx, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(len(frag0)+len(frag10)), int64(len(out)))
	assert.Equal(t, data[len(ftyp)+len(moov):], out)
}
