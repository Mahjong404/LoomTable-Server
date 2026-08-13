package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) ResolveQuery(ctx context.Context, actorID, tableID, viewID string) (loomrecord.QueryMetadata, error) {
	if r == nil || r.db == nil {
		return loomrecord.QueryMetadata{}, domain.ErrDependencyMissing
	}
	var metadata loomrecord.QueryMetadata
	err := r.db.QueryRowContext(ctx, `
		SELECT t.id, t.primary_field_id
		FROM tables t
		JOIN bases b ON b.id = t.base_id
		JOIN workspaces w ON w.id = b.workspace_id
		WHERE t.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND w.actor_id = $2 AND w.deleted_at IS NULL
	`, tableID, actorID).Scan(&metadata.TableID, &metadata.PrimaryFieldID)
	if errors.Is(err, sql.ErrNoRows) {
		return loomrecord.QueryMetadata{}, domain.ErrNotFound
	}
	if err != nil {
		return loomrecord.QueryMetadata{}, fmt.Errorf("resolve query Table: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, type, config, deleted_at, is_primary, revision, position_index
		FROM fields
		WHERE table_id = $1
	`, tableID)
	if err != nil {
		return loomrecord.QueryMetadata{}, fmt.Errorf("resolve query Fields: %w", err)
	}
	defer rows.Close()
	metadata.Fields = make(map[string]loomrecord.FieldDefinition)
	for rows.Next() {
		var field loomrecord.FieldDefinition
		var config []byte
		if err := rows.Scan(&field.ID, &field.Type, &config, &field.DeletedAt, &field.IsPrimary, &field.Revision, &field.Position); err != nil {
			return loomrecord.QueryMetadata{}, fmt.Errorf("scan query Field: %w", err)
		}
		field.Config = append(json.RawMessage(nil), config...)
		metadata.Fields[field.ID] = field
	}
	if err := rows.Err(); err != nil {
		return loomrecord.QueryMetadata{}, fmt.Errorf("resolve query Fields: %w", err)
	}
	if viewID != "" {
		view, err := scanView(r.db.QueryRowContext(ctx, accessibleViewSQL+" AND v.table_id = $3 AND v.deleted_at IS NULL", viewID, actorID, tableID))
		if errors.Is(err, sql.ErrNoRows) {
			return loomrecord.QueryMetadata{}, domain.ErrNotFound
		}
		if err != nil {
			return loomrecord.QueryMetadata{}, fmt.Errorf("resolve query View: %w", err)
		}
		metadata.View = &view
	}
	return metadata, nil
}

func (r *Repository) QueryRecords(
	ctx context.Context,
	actorID string,
	tableID string,
	plan loomrecord.QueryPlan,
	position *loomrecord.QueryPosition,
	includeTotal bool,
) (loomrecord.StoredQueryPage, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredQueryPage{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("begin Record query: %w", err)
	}
	defer tx.Rollback()
	if err := locklessActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredQueryPage{}, err
	}

	builder := &querySQLBuilder{}
	where, err := buildRecordWhere(builder, tableID, plan)
	if err != nil {
		return loomrecord.StoredQueryPage{}, err
	}
	whereArgs := append([]any(nil), builder.args...)
	terms, err := buildSortTerms(builder, plan)
	if err != nil {
		return loomrecord.StoredQueryPage{}, err
	}

	result := loomrecord.StoredQueryPage{}
	if includeTotal {
		var total int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM records r WHERE "+where, whereArgs...).Scan(&total); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("count Records: %w", err)
		}
		result.TotalCount = &total
	}
	if position != nil {
		keyset, err := buildKeysetCondition(builder, terms, *position)
		if err != nil {
			return loomrecord.StoredQueryPage{}, err
		}
		where += " AND (" + keyset + ")"
	}

	selectTerms := make([]string, len(terms))
	orderTerms := make([]string, 0, len(terms)+1)
	for index, term := range terms {
		selectTerms[index] = term.expression
		orderTerms = append(orderTerms, term.expression+" "+term.direction)
	}
	orderTerms = append(orderTerms, "r.id ASC")
	query := `
		SELECT r.id, r.table_id, r.revision, r.values, r.created_at, r.updated_at, r.deleted_at`
	if len(selectTerms) > 0 {
		query += ", " + strings.Join(selectTerms, ", ")
	}
	query += " FROM records r WHERE " + where + " ORDER BY " + strings.Join(orderTerms, ", ")
	query += " LIMIT " + builder.add(plan.Limit+1)

	rows, err := tx.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("query Records: %w", err)
	}
	defer rows.Close()
	type rowResult struct {
		record     loomrecord.Record
		sortValues []any
	}
	loaded := make([]rowResult, 0, plan.Limit+1)
	for rows.Next() {
		var item loomrecord.Record
		var values []byte
		sortValues := make([]any, len(terms))
		destinations := []any{&item.ID, &item.TableID, &item.Revision, &values, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt}
		for index := range sortValues {
			destinations = append(destinations, &sortValues[index])
		}
		if err := rows.Scan(destinations...); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("scan queried Record: %w", err)
		}
		for index := range sortValues {
			sortValues[index] = normalizeScannedSortValue(sortValues[index], terms[index].kind)
		}
		if err := json.Unmarshal(values, &item.Values); err != nil {
			return loomrecord.StoredQueryPage{}, fmt.Errorf("decode queried Record values: %w", err)
		}
		item.Values = projectRecordValues(item.Values, plan.Projection)
		loaded = append(loaded, rowResult{record: item, sortValues: sortValues})
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("query Records: %w", err)
	}
	result.HasMore = len(loaded) > plan.Limit
	if result.HasMore {
		loaded = loaded[:plan.Limit]
		last := loaded[len(loaded)-1]
		result.NextPosition = &loomrecord.QueryPosition{SortValues: last.sortValues, RecordID: last.record.ID}
	}
	result.Items = make([]loomrecord.Record, len(loaded))
	for index, item := range loaded {
		result.Items[index] = item.record
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", tableID).Scan(&result.ChangeSequence); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("read query Change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredQueryPage{}, fmt.Errorf("commit Record query: %w", err)
	}
	return result, nil
}

type querySQLBuilder struct {
	args []any
}

func (b *querySQLBuilder) add(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}

func buildRecordWhere(builder *querySQLBuilder, tableID string, plan loomrecord.QueryPlan) (string, error) {
	parts := []string{"r.table_id = " + builder.add(tableID)}
	switch plan.Lifecycle {
	case "active":
		parts = append(parts, "r.deleted_at IS NULL")
	case "deleted":
		parts = append(parts, "r.deleted_at IS NOT NULL")
	case "all":
	default:
		return "", &domain.BadRequestError{Message: "invalid Record lifecycle"}
	}
	if plan.Filter != nil {
		predicate, err := compileFilterSQL(builder, *plan.Filter, plan.Fields)
		if err != nil {
			return "", err
		}
		parts = append(parts, predicate)
	}
	if plan.Search != "" {
		parts = append(parts, "strpos(r.search_text, "+builder.add(plan.Search)+") > 0")
	}
	return strings.Join(parts, " AND "), nil
}

func compileFilterSQL(builder *querySQLBuilder, node domain.FilterNode, fields map[string]loomrecord.FieldDefinition) (string, error) {
	if node.Kind == "group" {
		children := make([]string, 0, len(node.Children))
		for _, child := range node.Children {
			compiled, err := compileFilterSQL(builder, child, fields)
			if err != nil {
				return "", err
			}
			children = append(children, "("+compiled+")")
		}
		joiner := " AND "
		if node.Operator == "or" {
			joiner = " OR "
		}
		return strings.Join(children, joiner), nil
	}
	field, ok := fields[node.FieldID]
	if !ok {
		return "", fmt.Errorf("compile Filter for missing Field %s", node.FieldID)
	}
	fieldParameter := "(" + builder.add(field.ID) + ")::text"
	textValue := "r.query_values ->> " + fieldParameter
	jsonValue := "r.query_values -> " + fieldParameter
	empty := filterEmptyExpression(field.Type, textValue, jsonValue)
	switch node.Operator {
	case "isEmpty":
		return empty, nil
	case "isNotEmpty":
		return "NOT (" + empty + ")", nil
	}

	var value any
	if err := json.Unmarshal(node.Value, &value); err != nil {
		return "", fmt.Errorf("decode Filter value: %w", err)
	}
	parameter := builder.add(value)
	expression := textValue
	switch field.Type {
	case "number":
		expression = "(" + textValue + ")::double precision"
	case "checkbox":
		expression = "(" + textValue + ")::boolean"
	case "multiSelect":
		switch node.Operator {
		case "includes":
			return "COALESCE((" + jsonValue + ") ? " + parameter + ", FALSE)", nil
		case "excludes":
			return "NOT COALESCE((" + jsonValue + ") ? " + parameter + ", FALSE)", nil
		}
	}
	switch node.Operator {
	case "is":
		return expression + " IS NOT DISTINCT FROM " + parameter, nil
	case "isNot":
		return expression + " IS DISTINCT FROM " + parameter, nil
	case "contains":
		return "COALESCE(strpos(" + expression + ", " + parameter + ") > 0, FALSE)", nil
	case "notContains":
		return "NOT COALESCE(strpos(" + expression + ", " + parameter + ") > 0, FALSE)", nil
	case "startsWith":
		return "COALESCE(starts_with(" + expression + ", " + parameter + "), FALSE)", nil
	case "endsWith":
		return "COALESCE(right(" + expression + ", char_length(" + parameter + ")) = " + parameter + ", FALSE)", nil
	case "greaterThan":
		return expression + " > " + parameter, nil
	case "greaterOrEqual":
		return expression + " >= " + parameter, nil
	case "lessThan":
		return expression + " < " + parameter, nil
	case "lessOrEqual":
		return expression + " <= " + parameter, nil
	default:
		return "", fmt.Errorf("compile unsupported Filter operator %s", node.Operator)
	}
}

func filterEmptyExpression(fieldType, textValue, jsonValue string) string {
	switch fieldType {
	case "text", "longText":
		return "(" + textValue + " IS NULL OR " + textValue + " = '')"
	case "multiSelect":
		return "(" + jsonValue + " IS NULL OR " + jsonValue + " = 'null'::jsonb OR " + jsonValue + " = '[]'::jsonb)"
	default:
		return "(" + jsonValue + " IS NULL OR " + jsonValue + " = 'null'::jsonb)"
	}
}

type sqlSortTerm struct {
	expression string
	direction  string
	kind       string
}

func buildSortTerms(builder *querySQLBuilder, plan loomrecord.QueryPlan) ([]sqlSortTerm, error) {
	if len(plan.Sort) == 0 {
		return []sqlSortTerm{{expression: "r.created_at", direction: "ASC", kind: "timestamp"}}, nil
	}
	terms := make([]sqlSortTerm, 0, len(plan.Sort)*4)
	for _, spec := range plan.Sort {
		field, ok := plan.Fields[spec.FieldID]
		if !ok {
			return nil, fmt.Errorf("sort Field %s is unavailable", spec.FieldID)
		}
		base := sortValueExpression(builder, field)
		nullValue, nonNullValue := 1, 0
		if spec.Nulls == "first" {
			nullValue, nonNullValue = 0, 1
		}
		terms = append(terms, sqlSortTerm{
			expression: fmt.Sprintf("CASE WHEN %s IS NULL THEN %d ELSE %d END", base, nullValue, nonNullValue),
			direction:  "ASC",
			kind:       "integer",
		})
		direction := strings.ToUpper(spec.Direction)
		if field.Type != "select" {
			terms = append(terms, sqlSortTerm{expression: base, direction: direction, kind: sortValueKind(field.Type)})
			continue
		}
		active, err := orderedOptionIDs(field.Config, "options")
		if err != nil {
			return nil, err
		}
		activeSetParts := make([]string, 0, len(active))
		for _, optionID := range active {
			activeSetParts = append(activeSetParts, builder.add(optionID))
		}
		lifecycle := "1"
		if len(activeSetParts) > 0 {
			lifecycle = "CASE WHEN " + base + " IN (" + strings.Join(activeSetParts, ",") + ") THEN 0 ELSE 1 END"
		}
		terms = append(terms, sqlSortTerm{expression: lifecycle, direction: "ASC", kind: "integer"})
		rankParts := make([]string, 0, len(active))
		for index, optionID := range active {
			rankParts = append(rankParts, "WHEN "+builder.add(optionID)+" THEN "+fmt.Sprintf("%d", index))
		}
		rank := "0"
		if len(rankParts) > 0 {
			rank = "CASE " + base + " " + strings.Join(rankParts, " ") + " ELSE 0 END"
		}
		terms = append(terms, sqlSortTerm{expression: rank, direction: direction, kind: "integer"})
		terms = append(terms, sqlSortTerm{expression: base, direction: direction, kind: "text"})
	}
	return terms, nil
}

func sortValueExpression(builder *querySQLBuilder, field loomrecord.FieldDefinition) string {
	base := "r.query_values ->> (" + builder.add(field.ID) + ")::text"
	switch field.Type {
	case "number":
		return "(" + base + ")::double precision"
	case "checkbox":
		return "(" + base + ")::boolean"
	default:
		return base
	}
}

func sortValueKind(fieldType string) string {
	switch fieldType {
	case "number":
		return "number"
	case "checkbox":
		return "boolean"
	default:
		return "text"
	}
}

func orderedOptionIDs(raw json.RawMessage, property string) ([]string, error) {
	var config map[string][]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("decode Select Field config: %w", err)
	}
	options := config[property]
	ids := make([]string, len(options))
	for index, option := range options {
		ids[index] = option.ID
	}
	return ids, nil
}

func buildKeysetCondition(builder *querySQLBuilder, terms []sqlSortTerm, position loomrecord.QueryPosition) (string, error) {
	if len(position.SortValues) != len(terms) || position.RecordID == "" {
		return "", &domain.InvalidCursorError{}
	}
	allTerms := append(append([]sqlSortTerm(nil), terms...), sqlSortTerm{expression: "r.id", direction: "ASC", kind: "text"})
	values := append(append([]any(nil), position.SortValues...), position.RecordID)
	for index := range values {
		coerced, ok := coerceCursorValue(values[index], allTerms[index].kind)
		if !ok {
			return "", &domain.InvalidCursorError{}
		}
		values[index] = coerced
	}
	branches := make([]string, 0, len(allTerms))
	for current := range allTerms {
		parts := make([]string, 0, current+1)
		for previous := 0; previous < current; previous++ {
			parts = append(parts, allTerms[previous].expression+" IS NOT DISTINCT FROM "+builder.add(values[previous]))
		}
		operator := ">"
		if strings.HasPrefix(allTerms[current].direction, "DESC") {
			operator = "<"
		}
		parts = append(parts, allTerms[current].expression+" "+operator+" "+builder.add(values[current]))
		branches = append(branches, "("+strings.Join(parts, " AND ")+")")
	}
	return strings.Join(branches, " OR "), nil
}

func coerceCursorValue(value any, kind string) (any, bool) {
	if value == nil {
		return nil, true
	}
	switch kind {
	case "integer":
		switch current := value.(type) {
		case int64:
			return current, true
		case float64:
			if current == float64(int64(current)) {
				return int64(current), true
			}
		}
	case "number":
		_, ok := value.(float64)
		return value, ok
	case "boolean":
		_, ok := value.(bool)
		return value, ok
	case "text", "timestamp":
		switch current := value.(type) {
		case string:
			return current, true
		case []byte:
			return string(current), true
		}
	}
	return nil, false
}

func normalizeScannedSortValue(value any, kind string) any {
	if raw, ok := value.([]byte); ok && (kind == "text" || kind == "timestamp") {
		return string(raw)
	}
	switch current := value.(type) {
	case int:
		return int64(current)
	case int32:
		return int64(current)
	}
	return value
}

func projectRecordValues(values map[string]any, projection []string) map[string]any {
	result := make(map[string]any, len(projection))
	for _, fieldID := range projection {
		if value, exists := values[fieldID]; exists {
			result[fieldID] = value
		}
	}
	return result
}

func locklessActiveTable(ctx context.Context, tx *sql.Tx, actorID, tableID string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM tables t
			JOIN bases b ON b.id = t.base_id
			JOIN workspaces w ON w.id = b.workspace_id
			WHERE t.id = $1 AND t.deleted_at IS NULL AND b.deleted_at IS NULL
			  AND w.actor_id = $2 AND w.deleted_at IS NULL
		)
	`, tableID, actorID).Scan(&exists); err != nil {
		return fmt.Errorf("check active Table: %w", err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	return nil
}
