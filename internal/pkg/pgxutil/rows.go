// SPDX-License-Identifier: Apache-2.0

// Package pgxutil provides PostgreSQL utility functions.
package pgxutil

import (
	"github.com/jackc/pgx/v5"
)

// CollectRows collects all rows into a slice using the provided scan function.
// Automatically closes rows when done or on error.
func CollectRows[T any](rows pgx.Rows, scan func(pgx.Rows) (T, error)) ([]T, error) {
	defer rows.Close()

	var result []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// CollectOne collects a single row using the provided scan function.
// Returns pgx.ErrNoRows if no rows are returned.
// Automatically closes rows when done or on error.
func CollectOne[T any](rows pgx.Rows, scan func(pgx.Rows) (T, error)) (T, error) {
	defer rows.Close()

	var zero T
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, err
		}
		return zero, pgx.ErrNoRows
	}

	result, err := scan(rows)
	if err != nil {
		return zero, err
	}

	if err := rows.Err(); err != nil {
		return zero, err
	}

	return result, nil
}

// ForEachRow iterates over rows and calls the provided function for each.
// Automatically closes rows when done or on error.
func ForEachRow(rows pgx.Rows, fn func(pgx.Rows) error) error {
	defer rows.Close()

	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}

	return rows.Err()
}
