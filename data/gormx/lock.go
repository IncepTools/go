package gormx

import (
	"gorm.io/gorm/clause"
)

// lockForUpdate is clause.Locking{Strength: "UPDATE"} behind a named type, so
// ForUpdate reads clearly at the call site and the clause import stays here.
type lockForUpdate struct{}

// Name implements clause.Interface.
func (lockForUpdate) Name() string { return "FOR" }

// Build implements clause.Interface.
func (lockForUpdate) Build(b clause.Builder) {
	clause.Locking{Strength: clause.LockingStrengthUpdate}.Build(b)
}

// MergeClause implements clause.Interface.
func (lockForUpdate) MergeClause(c *clause.Clause) {
	clause.Locking{Strength: clause.LockingStrengthUpdate}.MergeClause(c)
}
