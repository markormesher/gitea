// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package db

import (
	"context"
	"errors"
	"fmt"
)

// ResourceIndex represents a resource index which could be used as issue/release and others
// We can create different tables i.e. issue_index, release_index, etc.
type ResourceIndex struct {
	GroupID  int64 `xorm:"pk"`
	MaxIndex int64 `xorm:"index"`
}

var (
	// ErrResouceOutdated represents an error when request resource outdated
	ErrResouceOutdated = errors.New("resource outdated")
	// ErrGetResourceIndexFailed represents an error when resource index retries 3 times
	ErrGetResourceIndexFailed = errors.New("get resource index failed")
)

const (
	// MaxDupIndexAttempts max retry times to create index
	MaxDupIndexAttempts = 3
)

// SyncMaxResourceIndex sync the max index with the resource
func SyncMaxResourceIndex(ctx context.Context, tableName string, groupID, maxIndex int64) (err error) {
	e := GetEngine(ctx)

	// try to update the max_index and acquire the write-lock for the record
	res, err := e.Exec(fmt.Sprintf("UPDATE %s SET max_index=? WHERE group_id=? AND max_index<?", tableName), maxIndex, groupID, maxIndex)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// if nothing is updated, the record might not exist or might be larger, it's safe to try to insert it again and then check whether the record exists
		_, errIns := e.Exec(fmt.Sprintf("INSERT INTO %s (group_id, max_index) VALUES (?, ?)", tableName), groupID, maxIndex)
		var savedIdx int64
		has, err := e.SQL(fmt.Sprintf("SELECT max_index FROM %s WHERE group_id=?", tableName), groupID).Get(&savedIdx)
		if err != nil {
			return err
		}
		// if the record still doesn't exist, there must be some errors (insert error)
		if !has {
			if errIns == nil {
				return errors.New("impossible error when SyncMaxResourceIndex, insert succeeded but no record is saved")
			}
			return errIns
		}
	}
	return nil
}

// GetNextResourceIndex generates a resource index, it must run in the same transaction where the resource is created
func GetNextResourceIndex(ctx context.Context, tableName string, groupID int64) (int64, error) {
	e := GetEngine(ctx)

	// try to update the max_index to next value, and acquire the write-lock for the record
	res, err := e.Exec(fmt.Sprintf("UPDATE %s SET max_index=max_index+1 WHERE group_id=?", tableName), groupID)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		// this slow path is only for the first time of creating a resource index
		_, errIns := e.Exec(fmt.Sprintf("INSERT INTO %s (group_id, max_index) VALUES (?, 0)", tableName), groupID)
		res, err = e.Exec(fmt.Sprintf("UPDATE %s SET max_index=max_index+1 WHERE group_id=?", tableName), groupID)
		if err != nil {
			return 0, err
		}
		affected, err = res.RowsAffected()
		if err != nil {
			return 0, err
		}
		// if the update still can not update any records, the record must not exist and there must be some errors (insert error)
		if affected == 0 {
			if errIns == nil {
				return 0, errors.New("impossible error when GetNextResourceIndex, insert and update both succeeded but no record is updated")
			}
			return 0, errIns
		}
	}

	// now, the new index is in database (protected by the transaction and write-lock)
	var newIdx int64
	has, err := e.SQL(fmt.Sprintf("SELECT max_index FROM %s WHERE group_id=?", tableName), groupID).Get(&newIdx)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, errors.New("impossible error when GetNextResourceIndex, upsert succeeded but no record can be selected")
	}
	return newIdx, nil
}

// DeleteResourceIndex delete resource index
func DeleteResourceIndex(ctx context.Context, tableName string, groupID int64) error {
	_, err := Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE group_id=?", tableName), groupID)
	return err
}
