package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/Mahjong404/LoomTable-Server/internal/domain"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
)

func (r *Repository) DistinctValues(
	ctx context.Context,
	actorID string,
	tableID string,
	plan loomrecord.DistinctPlan,
) (loomrecord.StoredDistinctPage, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredDistinctPage{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("begin distinct values query: %w", err)
	}
	defer tx.Rollback()
	if err := locklessActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredDistinctPage{}, err
	}

	builder := &querySQLBuilder{}
	where, err := buildRecordWhere(builder, tableID, loomrecord.QueryPlan{
		Lifecycle: "active", Filter: plan.Filter, Fields: plan.Fields,
	})
	if err != nil {
		return loomrecord.StoredDistinctPage{}, err
	}
	fieldParameter := "(" + builder.add(plan.FieldID) + ")::text"
	textValue := "r.query_values ->> " + fieldParameter
	jsonValue := "r.query_values -> " + fieldParameter
	empty := filterEmptyExpression(plan.FieldType, textValue, jsonValue)

	result := loomrecord.StoredDistinctPage{}
	if err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM records r WHERE "+where+" AND ("+empty+")",
		builder.args...,
	).Scan(&result.EmptyCount); err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("count empty Field values: %w", err)
	}

	var rows *sql.Rows
	if plan.FieldType == "multiSelect" {
		rows, err = tx.QueryContext(ctx, `
			SELECT element.value, NULL::text, count(*)
			FROM records r, jsonb_array_elements_text(`+jsonValue+`) AS element(value)
			WHERE `+where+` AND NOT (`+empty+`)
			GROUP BY element.value
		`, builder.args...)
	} else {
		rows, err = tx.QueryContext(ctx, `
			SELECT `+textValue+`, min(r.values ->> `+fieldParameter+`), count(*)
			FROM records r
			WHERE `+where+` AND NOT (`+empty+`)
			GROUP BY `+textValue+`
		`, builder.args...)
	}
	if err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("query distinct Field values: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item loomrecord.StoredDistinctValue
		var display sql.NullString
		if err := rows.Scan(&item.Value, &display, &item.Count); err != nil {
			return loomrecord.StoredDistinctPage{}, fmt.Errorf("scan distinct Field value: %w", err)
		}
		item.Display = display.String
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("query distinct Field values: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", tableID).Scan(&result.ChangeSequence); err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("read distinct values Change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredDistinctPage{}, fmt.Errorf("commit distinct values query: %w", err)
	}
	return result, nil
}

func (r *Repository) AggregateRecords(
	ctx context.Context,
	actorID string,
	tableID string,
	plan loomrecord.AggregatePlan,
) (loomrecord.StoredAggregateResult, error) {
	if r == nil || r.db == nil {
		return loomrecord.StoredAggregateResult{}, domain.ErrDependencyMissing
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return loomrecord.StoredAggregateResult{}, fmt.Errorf("begin Record aggregation: %w", err)
	}
	defer tx.Rollback()
	if err := locklessActiveTable(ctx, tx, actorID, tableID); err != nil {
		return loomrecord.StoredAggregateResult{}, err
	}

	result := loomrecord.StoredAggregateResult{Results: make(map[string]map[string]any, len(plan.Requests))}
	for _, request := range plan.Requests {
		builder := &querySQLBuilder{}
		where, err := buildRecordWhere(builder, tableID, loomrecord.QueryPlan{
			Lifecycle: "active", Filter: plan.Filter, Fields: plan.Fields,
		})
		if err != nil {
			return loomrecord.StoredAggregateResult{}, err
		}
		fieldParameter := "(" + builder.add(request.FieldID) + ")::text"
		textValue := "r.query_values ->> " + fieldParameter
		jsonValue := "r.query_values -> " + fieldParameter
		empty := filterEmptyExpression(request.FieldType, textValue, jsonValue)
		numeric := "(" + textValue + ")::double precision"

		sumExpression := "NULL::double precision"
		avgExpression := "NULL::double precision"
		minExpression := "NULL::text"
		maxExpression := "NULL::text"
		switch request.FieldType {
		case "number":
			sumExpression = "sum(" + numeric + ")"
			avgExpression = "avg(" + numeric + ")"
			minExpression = "min(" + numeric + ")::text"
			maxExpression = "max(" + numeric + ")::text"
		case "date":
			minExpression = "min(" + textValue + ")"
			maxExpression = "max(" + textValue + ")"
		}

		var count int64
		var sum, avg sql.NullFloat64
		var minimum, maximum sql.NullString
		err = tx.QueryRowContext(ctx, `
			SELECT count(*) FILTER (WHERE NOT (`+empty+`)),
			       `+sumExpression+`, `+avgExpression+`,
			       `+minExpression+`, `+maxExpression+`
			FROM records r
			WHERE `+where+`
		`, builder.args...).Scan(&count, &sum, &avg, &minimum, &maximum)
		if err != nil {
			return loomrecord.StoredAggregateResult{}, fmt.Errorf("aggregate Field %s: %w", request.FieldID, err)
		}

		values := make(map[string]any, len(request.Functions))
		for _, fn := range request.Functions {
			switch fn {
			case "count":
				values[fn] = count
			case "sum":
				values[fn] = nullableFloat(sum)
			case "avg":
				values[fn] = nullableFloat(avg)
			case "min":
				values[fn] = aggregateBoundary(request.FieldType, minimum)
			case "max":
				values[fn] = aggregateBoundary(request.FieldType, maximum)
			}
		}
		result.Results[request.FieldID] = values
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(change_sequence), 0) FROM changes WHERE table_id = $1", tableID).Scan(&result.ChangeSequence); err != nil {
		return loomrecord.StoredAggregateResult{}, fmt.Errorf("read aggregation Change tail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return loomrecord.StoredAggregateResult{}, fmt.Errorf("commit Record aggregation: %w", err)
	}
	return result, nil
}

func nullableFloat(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

func aggregateBoundary(fieldType string, value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	if fieldType == "number" {
		if number, err := strconv.ParseFloat(value.String, 64); err == nil {
			return number
		}
		return nil
	}
	return value.String
}
