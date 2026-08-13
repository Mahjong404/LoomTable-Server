package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/cursor"
	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	"github.com/Mahjong404/LoomTable-Server/internal/id"
)

const (
	maxMapFeatures    = 500
	mapClusterTTL     = 5 * time.Minute
	mercatorMaxLat    = 85.0511287798066
	defaultTilePixels = 256.0
)

type MapStore interface {
	ResolveMap(context.Context, string, string) (QueryMetadata, error)
	LoadMapSnapshot(context.Context, string, QueryMetadata, QueryPlan, string, *MapViewport) (StoredMapSnapshot, error)
	QueryMapClusterRecords(context.Context, string, QueryMetadata, QueryPlan, string, []MapViewportBox, *QueryPosition, int, bool) (StoredQueryPage, error)
}

type mapClusterTokenPayload struct {
	ActorID        string           `json:"actorId"`
	ViewID         string           `json:"viewId"`
	ViewRevision   int64            `json:"viewRevision"`
	TableID        string           `json:"tableId"`
	LocationField  string           `json:"locationFieldId"`
	SchemaHash     string           `json:"schemaHash"`
	QueryHash      string           `json:"queryHash"`
	MapQueryHash   string           `json:"mapQueryHash"`
	ChangeSequence int64            `json:"changeSequence"`
	Boxes          []MapViewportBox `json:"boxes"`
	IssuedAt       int64            `json:"issuedAt"`
	ExpiresAt      int64            `json:"expiresAt"`
}

type mapClusterPageCursor struct {
	ActorID   string        `json:"actorId"`
	ViewID    string        `json:"viewId"`
	TokenHash string        `json:"tokenHash"`
	Limit     int           `json:"limit"`
	Position  QueryPosition `json:"position"`
	ExpiresAt int64         `json:"expiresAt"`
}

func (s *Service) QueryMap(ctx context.Context, actorID, viewID string, request MapQueryRequest) (MapQueryResult, error) {
	if err := validateMapQueryRequest(request); err != nil {
		return MapQueryResult{}, err
	}
	store, metadata, plan, config, err := s.resolveMapPlan(ctx, actorID, viewID)
	if err != nil {
		return MapQueryResult{}, err
	}
	snapshot, err := store.LoadMapSnapshot(ctx, actorID, metadata, plan, config.LocationFieldID, &request.Viewport)
	if err != nil {
		return MapQueryResult{}, err
	}
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return MapQueryResult{}, err
	}
	features, err := s.clusterMapRecords(signer, actorID, *metadata.View, config.LocationFieldID, plan, request, snapshot)
	if err != nil {
		return MapQueryResult{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: metadata.TableID, Sequence: snapshot.ChangeSequence})
	if err != nil {
		return MapQueryResult{}, fmt.Errorf("encode Map Change cursor: %w", err)
	}
	return MapQueryResult{
		Features: features, ViewportRenderableRecordCount: int64(len(snapshot.Records)),
		ViewRevision: metadata.View.Revision, ChangeCursor: changeCursor,
	}, nil
}

func (s *Service) SummarizeMap(ctx context.Context, actorID, viewID string) (MapSummaryResult, error) {
	store, metadata, plan, config, err := s.resolveMapPlan(ctx, actorID, viewID)
	if err != nil {
		return MapSummaryResult{}, err
	}
	snapshot, err := store.LoadMapSnapshot(ctx, actorID, metadata, plan, config.LocationFieldID, nil)
	if err != nil {
		return MapSummaryResult{}, err
	}
	summary := MapQuerySummary{MatchedRecordCount: int64(len(snapshot.Records))}
	renderable := make([]MapCoordinate, 0, len(snapshot.Records))
	for _, record := range snapshot.Records {
		if record.Position == nil {
			summary.UnlocatedRecordCount++
			continue
		}
		if record.Position.Lat < -mercatorMaxLat || record.Position.Lat > mercatorMaxLat {
			summary.UnrenderableRecordCount++
			continue
		}
		summary.RenderableRecordCount++
		renderable = append(renderable, *record.Position)
	}
	if len(renderable) > 0 {
		bounds := minimalMapBounds(renderable)
		summary.DataBounds = &bounds
	}
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return MapSummaryResult{}, err
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: metadata.TableID, Sequence: snapshot.ChangeSequence})
	if err != nil {
		return MapSummaryResult{}, fmt.Errorf("encode Map summary Change cursor: %w", err)
	}
	return MapSummaryResult{Summary: summary, ViewRevision: metadata.View.Revision, ChangeCursor: changeCursor}, nil
}

func (s *Service) QueryMapClusterRecords(ctx context.Context, actorID, viewID string, request MapClusterRecordsRequest) (QueryResult, error) {
	if !id.Valid(id.ViewPrefix, viewID) {
		return QueryResult{}, &domain.BadRequestError{Message: "/viewId has an invalid typed ID"}
	}
	if request.ClusterToken == "" {
		return QueryResult{}, domain.NewValidationError(requiredQueryIssue("/clusterToken", "clusterToken is required"))
	}
	if request.Limit == 0 {
		request.Limit = defaultQueryLimit
	}
	if request.Limit < 1 || request.Limit > maxQueryLimit {
		return QueryResult{}, domain.NewValidationError(domain.ValidationIssue{Path: "/limit", Code: "limit", Message: "limit must be from 1 to 500"})
	}
	signer, err := s.cursorSigner(ctx)
	if err != nil {
		return QueryResult{}, err
	}
	var token mapClusterTokenPayload
	if err := signer.Decode("map-cluster", request.ClusterToken, &token); err != nil || token.ActorID != actorID || token.ViewID != viewID {
		return QueryResult{}, &domain.InvalidCursorError{}
	}
	if s.now().UTC().Unix() >= token.ExpiresAt {
		return QueryResult{}, &domain.QuerySnapshotExpiredError{}
	}
	store, metadata, plan, config, err := s.resolveMapPlan(ctx, actorID, viewID)
	if err != nil {
		return QueryResult{}, err
	}
	if metadata.View.Revision != token.ViewRevision || metadata.TableID != token.TableID || config.LocationFieldID != token.LocationField || plan.SchemaHash != token.SchemaHash || plan.Fingerprint != token.QueryHash {
		return QueryResult{}, &domain.QuerySnapshotExpiredError{}
	}
	changeStore, ok := s.store.(ChangeStore)
	if !ok {
		return QueryResult{}, domain.ErrDependencyMissing
	}
	tail, err := changeStore.ChangeTail(ctx, actorID, metadata.TableID)
	if err != nil {
		return QueryResult{}, err
	}
	if tail != token.ChangeSequence {
		return QueryResult{}, &domain.QuerySnapshotExpiredError{}
	}

	var position *QueryPosition
	if request.Cursor != "" {
		var pageCursor mapClusterPageCursor
		if err := signer.Decode("map-cluster-page", request.Cursor, &pageCursor); err != nil {
			return QueryResult{}, &domain.InvalidCursorError{}
		}
		if pageCursor.ActorID != actorID || pageCursor.ViewID != viewID || pageCursor.Limit != request.Limit || pageCursor.TokenHash != tokenDigest(request.ClusterToken) {
			return QueryResult{}, &domain.InvalidCursorError{}
		}
		if s.now().UTC().Unix() >= pageCursor.ExpiresAt {
			return QueryResult{}, &domain.QuerySnapshotExpiredError{}
		}
		position = &pageCursor.Position
	}
	stored, err := store.QueryMapClusterRecords(ctx, actorID, metadata, plan, config.LocationFieldID, token.Boxes, position, request.Limit, request.Cursor == "")
	if err != nil {
		return QueryResult{}, err
	}
	if stored.ChangeSequence != token.ChangeSequence {
		return QueryResult{}, &domain.QuerySnapshotExpiredError{}
	}
	changeCursor, err := signer.Encode("change", changeCursorPayload{ActorID: actorID, TableID: metadata.TableID, Sequence: stored.ChangeSequence})
	if err != nil {
		return QueryResult{}, fmt.Errorf("encode cluster Change cursor: %w", err)
	}
	result := QueryResult{Items: stored.Items, HasMore: stored.HasMore, ChangeCursor: changeCursor, TotalCount: stored.TotalCount}
	if stored.HasMore {
		if stored.NextPosition == nil {
			return QueryResult{}, fmt.Errorf("Map cluster store omitted the next keyset position")
		}
		result.NextCursor, err = signer.Encode("map-cluster-page", mapClusterPageCursor{
			ActorID: actorID, ViewID: viewID, TokenHash: tokenDigest(request.ClusterToken), Limit: request.Limit,
			Position: *stored.NextPosition, ExpiresAt: token.ExpiresAt,
		})
		if err != nil {
			return QueryResult{}, fmt.Errorf("encode Map cluster page cursor: %w", err)
		}
	}
	return result, nil
}

func (s *Service) resolveMapPlan(ctx context.Context, actorID, viewID string) (MapStore, QueryMetadata, QueryPlan, domain.MapViewConfig, error) {
	if !id.Valid(id.ViewPrefix, viewID) {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, &domain.BadRequestError{Message: "/viewId has an invalid typed ID"}
	}
	if s == nil || s.store == nil {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, domain.ErrDependencyMissing
	}
	store, ok := s.store.(MapStore)
	if !ok {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, domain.ErrDependencyMissing
	}
	metadata, err := store.ResolveMap(ctx, actorID, viewID)
	if err != nil {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, err
	}
	if metadata.View == nil || metadata.View.Type != "map" {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, &domain.ViewConfigurationRequiredError{ViewID: viewID, InvalidFieldIDs: []string{}}
	}
	config, ok := metadata.View.Config.(domain.MapViewConfig)
	if !ok {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, &domain.ViewConfigurationRequiredError{ViewID: viewID, InvalidFieldIDs: []string{}}
	}
	location, exists := metadata.Fields[config.LocationFieldID]
	if !exists || location.DeletedAt != nil || location.Type != "location" {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, viewConfigurationError(metadata, []string{config.LocationFieldID})
	}
	plan, err := buildQueryPlan(QueryRequest{
		ProjectionPresent: true,
		Projection:        []string{metadata.PrimaryFieldID, config.LocationFieldID},
		Lifecycle:         "active",
	}, metadata)
	if err != nil {
		return nil, QueryMetadata{}, QueryPlan{}, domain.MapViewConfig{}, err
	}
	return store, metadata, plan, config, nil
}

func validateMapQueryRequest(request MapQueryRequest) error {
	issues := make([]domain.ValidationIssue, 0)
	if len(request.Viewport.Boxes) < 1 || len(request.Viewport.Boxes) > 2 {
		issues = append(issues, domain.ValidationIssue{Path: "/viewport/boxes", Code: "limit", Message: "viewport must contain one or two boxes"})
	}
	for index, box := range request.Viewport.Boxes {
		path := fmt.Sprintf("/viewport/boxes/%d", index)
		if !finiteInRange(box.West, -180, 180) || !finiteInRange(box.East, -180, 180) || box.West > box.East {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "format", Message: "west and east must describe a non-wrapping longitude range"})
		}
		if !finiteInRange(box.South, -mercatorMaxLat, mercatorMaxLat) || !finiteInRange(box.North, -mercatorMaxLat, mercatorMaxLat) || box.South > box.North {
			issues = append(issues, domain.ValidationIssue{Path: path, Code: "format", Message: "south and north must describe a renderable latitude range"})
		}
	}
	if math.IsNaN(request.Zoom) || math.IsInf(request.Zoom, 0) || request.Zoom < 0 {
		issues = append(issues, domain.ValidationIssue{Path: "/zoom", Code: "format", Message: "zoom must be a finite non-negative number"})
	}
	if request.PixelWidth < 1 {
		issues = append(issues, requiredQueryIssue("/pixelWidth", "pixelWidth must be positive"))
	}
	if request.PixelHeight < 1 {
		issues = append(issues, requiredQueryIssue("/pixelHeight", "pixelHeight must be positive"))
	}
	if len(issues) > 0 {
		return domain.NewValidationError(issues...)
	}
	return nil
}

func finiteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

type clusteredMapCell struct {
	key     string
	records []MapRecord
}

func (s *Service) clusterMapRecords(
	signer *cursor.Signer,
	actorID string,
	view domain.View,
	locationFieldID string,
	plan QueryPlan,
	request MapQueryRequest,
	snapshot StoredMapSnapshot,
) ([]any, error) {
	if len(snapshot.Records) <= maxMapFeatures {
		features := make([]any, 0, len(snapshot.Records))
		for _, record := range snapshot.Records {
			if record.Position == nil {
				continue
			}
			features = append(features, MapPoint{Kind: "point", RecordID: record.ID, Position: *record.Position, PrimaryFieldText: record.PrimaryFieldText})
		}
		return features, nil
	}
	cellSize := math.Max(32, math.Sqrt(float64(request.PixelWidth)*float64(request.PixelHeight)/maxMapFeatures))
	var cells map[string]*clusteredMapCell
	for {
		cells = groupMapRecords(snapshot.Records, request.Zoom, cellSize)
		if len(cells) <= maxMapFeatures {
			break
		}
		cellSize *= 1.5
	}
	keys := make([]string, 0, len(cells))
	for key := range cells {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	features := make([]any, 0, len(keys))
	for _, key := range keys {
		cell := cells[key]
		if len(cell.records) == 1 {
			record := cell.records[0]
			features = append(features, MapPoint{Kind: "point", RecordID: record.ID, Position: *record.Position, PrimaryFieldText: record.PrimaryFieldText})
			continue
		}
		positions := make([]MapCoordinate, len(cell.records))
		latTotal := 0.0
		xTotal, yTotal := 0.0, 0.0
		for index, record := range cell.records {
			positions[index] = *record.Position
			latTotal += record.Position.Lat
			radians := record.Position.Lng * math.Pi / 180
			xTotal += math.Cos(radians)
			yTotal += math.Sin(radians)
		}
		centerLng := math.Atan2(yTotal, xTotal) * 180 / math.Pi
		bounds := minimalMapBounds(positions)
		now := s.now().UTC()
		token, err := signer.Encode("map-cluster", mapClusterTokenPayload{
			ActorID: actorID, ViewID: view.ID, ViewRevision: view.Revision, TableID: view.TableID,
			LocationField: locationFieldID, SchemaHash: plan.SchemaHash, QueryHash: plan.Fingerprint,
			ChangeSequence: snapshot.ChangeSequence, Boxes: bounds.Boxes, MapQueryHash: mapQueryFingerprint(request),
			IssuedAt: now.Unix(), ExpiresAt: now.Add(mapClusterTTL).Unix(),
		})
		if err != nil {
			return nil, fmt.Errorf("encode Map cluster token: %w", err)
		}
		expansion := request.Zoom + 1
		if sameCoordinates(positions) {
			features = append(features, MapCluster{
				Kind: "cluster", ClusterID: clusterID(view.ID, key, snapshot.ChangeSequence),
				Position: MapCoordinate{Lat: latTotal / float64(len(cell.records)), Lng: centerLng}, Bounds: bounds,
				PointCount: len(cell.records), RecordsQueryToken: token,
			})
		} else {
			features = append(features, MapCluster{
				Kind: "cluster", ClusterID: clusterID(view.ID, key, snapshot.ChangeSequence),
				Position: MapCoordinate{Lat: latTotal / float64(len(cell.records)), Lng: centerLng}, Bounds: bounds,
				PointCount: len(cell.records), ExpansionZoom: &expansion, RecordsQueryToken: token,
			})
		}
	}
	return features, nil
}

func groupMapRecords(records []MapRecord, zoom, cellSize float64) map[string]*clusteredMapCell {
	worldSize := defaultTilePixels * math.Pow(2, math.Min(zoom, 30))
	cells := make(map[string]*clusteredMapCell)
	for _, record := range records {
		if record.Position == nil {
			continue
		}
		x, y := worldPixel(*record.Position, worldSize)
		cellX, cellY := int64(math.Floor(x/cellSize)), int64(math.Floor(y/cellSize))
		key := fmt.Sprintf("%d:%d:%g", cellX, cellY, cellSize)
		cell := cells[key]
		if cell == nil {
			cell = &clusteredMapCell{key: key}
			cells[key] = cell
		}
		cell.records = append(cell.records, record)
	}
	return cells
}

func worldPixel(position MapCoordinate, worldSize float64) (float64, float64) {
	x := (position.Lng + 180) / 360 * worldSize
	if x >= worldSize {
		x = math.Nextafter(worldSize, 0)
	}
	lat := math.Max(-mercatorMaxLat, math.Min(mercatorMaxLat, position.Lat)) * math.Pi / 180
	y := (1 - math.Asinh(math.Tan(lat))/math.Pi) / 2 * worldSize
	return x, y
}

func minimalMapBounds(points []MapCoordinate) MapViewport {
	latMin, latMax := points[0].Lat, points[0].Lat
	longitudes := make([]float64, len(points))
	for index, point := range points {
		latMin = math.Min(latMin, point.Lat)
		latMax = math.Max(latMax, point.Lat)
		longitudes[index] = point.Lng
	}
	sort.Float64s(longitudes)
	largestGap, gapIndex := -1.0, 0
	for index := range longitudes {
		next := longitudes[(index+1)%len(longitudes)]
		if index == len(longitudes)-1 {
			next += 360
		}
		if gap := next - longitudes[index]; gap > largestGap {
			largestGap, gapIndex = gap, index
		}
	}
	start := longitudes[(gapIndex+1)%len(longitudes)]
	span := 360 - largestGap
	end := start + span
	if end <= 180 {
		return MapViewport{Boxes: []MapViewportBox{{West: start, South: latMin, East: end, North: latMax}}}
	}
	return MapViewport{Boxes: []MapViewportBox{
		{West: start, South: latMin, East: 180, North: latMax},
		{West: -180, South: latMin, East: end - 360, North: latMax},
	}}
}

func sameCoordinates(points []MapCoordinate) bool {
	for _, point := range points[1:] {
		if point != points[0] {
			return false
		}
	}
	return true
}

func clusterID(viewID, key string, sequence int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", viewID, key, sequence)))
	return "clu_" + hex.EncodeToString(sum[:12])
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func mapQueryFingerprint(request MapQueryRequest) string {
	encoded, _ := json.Marshal(request)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
