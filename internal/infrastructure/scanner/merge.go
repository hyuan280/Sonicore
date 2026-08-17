package scanner

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/lib/pq"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

// resolveVersions groups tracks by (metadata_source, external_id) and sets
// version/version_label.
func (e *Engine) resolveVersions(ctx context.Context, libraryID string) error {
	rows, err := e.db.QueryContext(ctx,
		`SELECT metadata_source, external_id, array_agg(id ORDER BY
		 CASE file_format
		 WHEN 'flac' THEN 0 WHEN 'alac' THEN 1 WHEN 'wav' THEN 2
		 WHEN 'aiff' THEN 3 WHEN 'mp3' THEN 4 WHEN 'aac' THEN 5
		 WHEN 'ogg' THEN 6 WHEN 'opus' THEN 7 ELSE 8 END,
		 bit_rate DESC,
		 file_path
		 ) AS ids
		 FROM tracks WHERE library_id = $1 AND external_id != ''
		 GROUP BY metadata_source, external_id
		 ORDER BY metadata_source, external_id`, libraryID)
	if err != nil {
		return fmt.Errorf("query tracks by external id: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var externalID string
		var source string
		var ids []string
		if err := rows.Scan(&source, &externalID, pq.Array(&ids)); err != nil {
			return fmt.Errorf("scan external id group: %w", err)
		}

		if len(ids) < 2 {
			if _, err := e.db.ExecContext(ctx, `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, ids[0]); err != nil {
				log.Printf("[scan] reset version for %s: %v", ids[0], err)
			}
			if _, err := e.db.ExecContext(ctx, `DELETE FROM track_version_groups WHERE metadata_source = $1 AND external_id = $2 AND library_id = $3`, source, externalID, libraryID); err != nil {
				log.Printf("[scan] delete stale version group %s/%s: %v", source, externalID, err)
			}
			continue
		}

		for i, id := range ids {
			version := 0
			if i == 0 {
				version = 1
			} else {
				version = i + 1
			}
			label := ExtractVersionLabel(ctx, e.db, id)
			res, err := e.db.ExecContext(ctx, `UPDATE tracks SET version = $1, version_label = $2 WHERE id = $3`, version, label, id)
			if err != nil {
				log.Printf("[scan] version update error: source=%s external_id=%s ver=%d id=%s err=%v", source, externalID, version, id, err)
			} else if n, _ := res.RowsAffected(); n == 0 {
				log.Printf("[scan] version update affected 0 rows: source=%s external_id=%s ver=%d id=%s", source, externalID, version, id)
			}
			if _, err := e.db.ExecContext(ctx,
				`INSERT INTO track_version_groups (metadata_source, external_id, library_id, track_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				source, externalID, libraryID, id); err != nil {
				log.Printf("[scan] insert version group row for %s: %v", id, err)
			}
		}
	}

	// Clean up: tracks that used to be in a group but no longer have a group external id
	if _, err := e.db.ExecContext(ctx,
		`DELETE FROM track_version_groups WHERE library_id = $1 AND (metadata_source, external_id) NOT IN (SELECT DISTINCT metadata_source, external_id FROM tracks WHERE library_id = $1 AND external_id != '')`, libraryID); err != nil {
		log.Printf("[scan] delete stale version group rows for %s: %v", libraryID, err)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate external id groups: %w", err)
	}
	return nil
}

// mergeKey builds the normalized comparison key (title, album, artist) for
// the text-based duplicate merge. Unknown/empty components disqualify the
// track from merging.
func mergeKey(title, albums, artists string) (string, bool) {
	title = metadata.TrimParenSuffix(strings.TrimSpace(title))
	albums = strings.TrimSpace(albums)
	artists = strings.TrimSpace(artists)
	if title == "" || albums == "" || artists == "" {
		return "", false
	}
	// Reject placeholder components per value: albums/artists arrive
	// \x1f-separated, so "Unknown Artist\x1fJohn" must still disqualify the
	// track even though the joined string differs from "unknown artist".
	for _, c := range []string{title, albums, artists} {
		for _, part := range strings.Split(c, "\x1f") {
			low := strings.ToLower(strings.TrimSpace(part))
			if low == "unknown artist" || low == "unknown album" || low == "unknown" {
				return "", false
			}
		}
	}
	// Normalize the artist list: split on the aggregation separator (\x1f —
	// the same unit separator LoadMergeCandidates uses for albums, which
	// cannot appear in names) and normalize each name (case/punctuation so
	// "Artist A" and "artist a" collide), then sort so the order is
	// irrelevant. A single name containing "/", "、" or even a comma (e.g.
	// "Earth, Wind & Fire") stays whole — a multi-separator or comma split
	// would wrongly break it and falsely merge with separate artists.
	var artistKeys []string
	for _, p := range strings.Split(artists, "\x1f") {
		if k := metadata.NormalizeForMatch(p); k != "" {
			artistKeys = append(artistKeys, k)
		}
	}
	if len(artistKeys) == 0 {
		return "", false
	}
	sort.Strings(artistKeys)
	artistKey := strings.Join(artistKeys, "\x1f")

	// Albums arrive \x1f-separated (the unit separator cannot appear in
	// titles, unlike the old "|"); split on it and normalize each title, then
	// sort so album order is irrelevant. A title containing "|" or a comma
	// stays whole — normalizeForMatch keeps "|" — so it can never collide
	// with the joined form of two albums.
	var albumKeys []string
	for _, p := range strings.Split(albums, "\x1f") {
		if k := metadata.NormalizeForMatch(p); k != "" {
			albumKeys = append(albumKeys, k)
		}
	}
	if len(albumKeys) == 0 {
		return "", false
	}
	sort.Strings(albumKeys)
	albumKey := strings.Join(albumKeys, ",")

	key := metadata.NormalizeForMatch(title) + "\x00" + albumKey + "\x00" + artistKey
	return key, true
}

// sourcePriority returns the metadata source's registry priority (ascending,
// lower wins) for main-version selection. It mirrors the Registry's source
// ordering so re-ordering sources (e.g. raising NetEase above MusicBrainz)
// automatically changes which source becomes a group's main version.
// Unknown or disabled sources rank last.

func (e *Engine) sourcePriority(s string) int {
	if p, ok := e.sourcePrio[s]; ok {
		return p
	}
	return 100
}

// mergeExternalIDs merges several source→id maps into one (later maps win on
// the same key; empty values are dropped).

func mergeExternalIDs(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			if v != "" {
				out[k] = v
			}
		}
	}
	return out
}

// tryMergeByIdentifiedID runs right after identification: when the track's
// (source, external id) already exists in the library (primary id or
// external_ids map), the track is aligned to that version group's main
// source and the external id sets are merged, so both versions carry every
// recognizable source id. Returns whether the track was updated.

func (e *Engine) tryMergeByIdentifiedID(ctx context.Context, libraryID string, track *domain.Track) bool {
	if track == nil || track.ExternalID == "" {
		return false
	}
	source := track.MetadataSource
	if source == "" {
		source = "musicbrainz"
	}
	existing, err := e.trackRepo.FindByExternalID(ctx, libraryID, source, track.ExternalID, track.ID)
	if err != nil {
		log.Printf("[scan] merge lookup error for %s: %v", track.ID, err)
		return false
	}
	if existing == nil {
		return false
	}
	// A match via the alias table may leave existing without a primary id.
	// Aligning then would copy that empty id onto the freshly identified
	// track, clearing its valid primary identity — so only align when the
	// existing track actually carries one.
	if existing.ExternalID == "" {
		return false
	}
	mainSource := existing.MetadataSource
	if mainSource == "" {
		mainSource = "musicbrainz"
	}
	ids := mergeExternalIDs(existing.ExternalIDs, track.ExternalIDs)
	ids[source] = track.ExternalID
	ids[mainSource] = existing.ExternalID
	changed := track.MetadataSource != mainSource || track.ExternalID != existing.ExternalID ||
		!externalIDsEqual(track.ExternalIDs, ids)
	if changed {
		track.MetadataSource = mainSource
		track.ExternalID = existing.ExternalID
		track.ExternalIDs = ids
	}
	return changed
}

func externalIDsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// mergeCandidate pairs a merge-candidate track with its normalized key.

type mergeCandidate struct {
	mt  repository.MergeTrack
	key string
}

// mergeDuplicates groups tracks that represent the same song across
// different files/versions within a library:
//
// Phase 1 (text match): tracks that already belong to a version group and
// are not the group's main version (version=1) are skipped; the remaining
// tracks are sorted by (title, album, artists) and adjacent tracks with an
// identical normalized key merge into one group whose main source (lowest
// source priority) and merged external ids are written back.
//
// Phase 2 (group sync, incremental): for every existing version group the
// main version's current (source, external id) is propagated to its other
// members and every member's external_ids is completed to the group-wide
// union. Only members whose values differ are written.

func (e *Engine) mergeDuplicates(ctx context.Context, libraryID string) error {
	tracks, err := e.trackRepo.LoadMergeCandidates(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("load merge candidates: %w", err)
	}
	if len(tracks) < 2 {
		return nil
	}
	inGroup, err := e.trackRepo.TrackIDsInVersionGroups(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("load version groups: %w", err)
	}

	// Phase 1: only main versions (version=1) and ungrouped tracks compare.
	var cands []mergeCandidate
	for _, mt := range tracks {
		if inGroup[mt.ID] && mt.Version > 1 {
			continue // secondary versions never participate in comparison
		}
		key, ok := mergeKey(mt.Title, mt.Albums, mt.Artists)
		if !ok {
			continue
		}
		cands = append(cands, mergeCandidate{mt: mt, key: key})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].key != cands[j].key {
			return cands[i].key < cands[j].key
		}
		return cands[i].mt.ID < cands[j].mt.ID
	})

	// Adjacent runs with the same key form a group. Collect (id, source,
	// external id, external_ids) per group, then align every member to the
	// main source and the merged id set.
	i := 0
	for i < len(cands) {
		j := i
		for j < len(cands) && cands[j].key == cands[i].key {
			j++
		}
		if j-i >= 2 {
			e.alignGroup(ctx, cands[i:j])
		}
		i = j
	}

	// Phase 2: propagate main-version source to secondary versions and
	// complete external_ids group-wide.
	e.syncVersionGroups(ctx, libraryID)
	return nil
}

// alignGroup writes the main source and merged external ids to every member
// of a newly formed group (incremental: only differing members are written).

func (e *Engine) alignGroup(ctx context.Context, members []mergeCandidate) {
	if len(members) < 2 {
		return
	}
	// Main source = lowest source priority among members carrying an id.
	mainSource := ""
	mainID := ""
	best := 100
	for _, c := range members {
		if c.mt.ExternalID == "" {
			continue
		}
		prio := e.sourcePriority(c.mt.MetadataSource)
		if prio < best {
			best = prio
			mainSource = c.mt.MetadataSource
			mainID = c.mt.ExternalID
		}
	}
	if mainSource == "" {
		return // no member carries any external id; nothing to align
	}
	ids := make(map[string]string)
	for _, c := range members {
		ids = mergeExternalIDs(ids, c.mt.ExternalIDs)
		if c.mt.ExternalID != "" {
			ids[c.mt.MetadataSource] = c.mt.ExternalID
		}
	}
	// The main's own id for its primary source is authoritative: a later
	// member carrying a different id under the same source (the typical
	// multi-version case) must not overwrite it, or the main's primary column
	// and its external_ids alias would diverge.
	ids[mainSource] = mainID
	for _, c := range members {
		if c.mt.MetadataSource != mainSource || c.mt.ExternalID != mainID || !externalIDsEqual(c.mt.ExternalIDs, ids) {
			if err := e.trackRepo.UpdateMergeFields(ctx, c.mt.ID, mainSource, mainID, ids); err != nil {
				log.Printf("[scan] merge align error for %s: %v", c.mt.ID, err)
			}
		}
	}
}

// syncVersionGroups updates secondary versions from their main version
// (version=1) and completes external_ids to the group-wide union. Only
// members whose values differ are written. All members are loaded in one
// query (no per-member version lookup or FindByID).
func (e *Engine) syncVersionGroups(ctx context.Context, libraryID string) {
	rows, err := e.trackRepo.LoadVersionGroupMembers(ctx, libraryID)
	if err != nil {
		log.Printf("[scan] load version group members: %v", err)
		return
	}
	byKey := make(map[string][]repository.VersionGroupMember)
	var keys []string
	for _, m := range rows {
		key := m.GroupSource + "\x00" + m.GroupExtID
		if _, ok := byKey[key]; !ok {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], m)
	}

	for _, key := range keys {
		members := byKey[key]
		if len(members) < 2 {
			continue
		}
		// Find the main version (version=1) among the members.
		var mainID string
		mainSource, mainExternalID := members[0].Source, members[0].ExternalID
		for i := range members {
			if members[i].Version == 1 {
				mainID = members[i].TrackID
				mainSource, mainExternalID = members[i].Source, members[i].ExternalID
				break
			}
		}
		if mainID == "" {
			continue
		}
		// The group key in track_version_groups records the source from the
		// previous pass; the main version's CURRENT (source, external id) is
		// authoritative and propagates to the other members.
		var mainTrack *repository.VersionGroupMember
		for i := range members {
			if members[i].TrackID == mainID {
				mainTrack = &members[i]
				break
			}
		}
		if mainTrack == nil {
			continue
		}
		mainSource, mainExternalID = mainTrack.Source, mainTrack.ExternalID
		if mainSource == "" {
			// No authoritative source to propagate (mirrors alignGroup's
			// empty-main guard): defaulting it to musicbrainz would mis-attribute
			// the group's ids and persist that attribution on every member.
			continue
		}
		ids := mergeExternalIDs(mainTrack.ExternalIDs)
		if mainExternalID != "" {
			ids[mainSource] = mainExternalID
		}
		groupExternalIDs := make(map[string]string)
		for i := range members {
			m := &members[i]
			if m.TrackID == mainID {
				continue
			}
			groupExternalIDs = mergeExternalIDs(groupExternalIDs, m.ExternalIDs)
			if m.ExternalID != "" {
				groupExternalIDs[m.Source] = m.ExternalID
			}
		}
		// The main version's current id for its primary source is
		// authoritative: a stale member alias (an old id a secondary
		// version still carries) must not overwrite it, or the dirty
		// alias would be written back and self-persist across scans. A
		// cleared main id must not be written as an empty alias.
		//
		// The union is computed over ALL members before any write, so every
		// member (not just the last one processed) receives the full group-wide
		// id set in a single pass; alias lookups via external_ids @> then reach
		// all members immediately instead of converging on a later scan.
		for i := range members {
			m := &members[i]
			if m.TrackID == mainID {
				continue
			}
			merged := mergeExternalIDs(ids, groupExternalIDs)
			if mainExternalID != "" {
				merged[mainSource] = mainExternalID
			}
			// Propagate the main version's current source to this member.
			if m.Source != mainSource || m.ExternalID != mainExternalID {
				if err := e.trackRepo.UpdateMergeFields(ctx, m.TrackID, mainSource, mainExternalID, merged); err != nil {
					log.Printf("[scan] version group sync error for %s: %v", m.TrackID, err)
				}
				continue
			}
			if !externalIDsEqual(m.ExternalIDs, merged) {
				if err := e.trackRepo.UpdateMergeFields(ctx, m.TrackID, mainSource, mainExternalID, merged); err != nil {
					log.Printf("[scan] version group sync error for %s: %v", m.TrackID, err)
				}
			}
		}
		// The main version itself also carries the group-wide id union (its
		// own aliases plus every member's source ids), so alias lookups via
		// external_ids @> can reach it.
		union := mergeExternalIDs(ids, groupExternalIDs)
		if mainExternalID != "" {
			union[mainSource] = mainExternalID
		}
		if !externalIDsEqual(mainTrack.ExternalIDs, union) {
			if err := e.trackRepo.UpdateMergeFields(ctx, mainID, mainSource, mainExternalID, union); err != nil {
				log.Printf("[scan] version group sync error for %s: %v", mainID, err)
			}
		}
	}
}
