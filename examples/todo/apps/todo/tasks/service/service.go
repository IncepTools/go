// Package service holds the tasks module's business rules.
//
// Note what it does NOT import: nothing from web/. It takes a context and
// plain arguments and returns a value or a typed error, so the same logic
// serves an HTTP request, a scheduled job and a test without any of them
// pretending to be the others.
package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/dto"
	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/web/pagination"
)

// TaskCompleted is published when a task is marked done.
//
// Publishing an event rather than calling the notifier directly is what lets
// a second subscriber be added later without this file changing.
type TaskCompleted struct {
	TaskID    uuid.UUID
	AccountID uuid.UUID
	Title     string
}

// Task is the stored shape.
//
// A service backed by a database would put this in entity/ and reach it
// through a repository; an in-memory map keeps the example runnable with no
// database to install.
type Task struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Title       string
	Notes       string
	Priority    string
	Tags        []string
	DueAt       *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// TaskService stores tasks in memory.
type TaskService struct {
	mu    sync.RWMutex
	tasks map[uuid.UUID]Task
	bus   *events.Bus
}

// NewTaskService builds the service.
func NewTaskService(bus *events.Bus) *TaskService {
	return &TaskService{tasks: map[uuid.UUID]Task{}, bus: bus}
}

// List returns one page of the caller's tasks.
func (s *TaskService) List(ctx context.Context, q pagination.Query, includeDone bool) ([]Task, int64, error) {
	tenant := ctxkit.TenantOf(ctx)
	if tenant.Zero() {
		return nil, 0, errs.Unauthorized("no account context").WithCode("NO_TENANT")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// The ownership filter is applied HERE, not by the caller. In a service
	// backed by a database this is the repository's job and is not optional:
	// the query that forgets it returns another customer's rows, silently.
	var mine []Task
	for _, t := range s.tasks {
		if t.AccountID != tenant.AccountID {
			continue
		}
		if !includeDone && t.CompletedAt != nil {
			continue
		}
		mine = append(mine, t)
	}
	sortTasks(mine, q)

	total := int64(len(mine))
	start := min(q.Offset(), len(mine))
	end := min(start+q.Limit, len(mine))
	return mine[start:end], total, nil
}

func sortTasks(items []Task, q pagination.Query) {
	less := func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) }
	switch q.Sort {
	case "title":
		less = func(i, j int) bool { return items[i].Title < items[j].Title }
	case "due_at":
		less = func(i, j int) bool {
			switch {
			case items[i].DueAt == nil:
				return false // undated sorts last, whichever direction
			case items[j].DueAt == nil:
				return true
			default:
				return items[i].DueAt.Before(*items[j].DueAt)
			}
		}
	}
	sort.SliceStable(items, less)
	if q.Order == pagination.Desc && q.Sort != "" && q.Sort != "created_at" {
		for l, r := 0, len(items)-1; l < r; l, r = l+1, r-1 {
			items[l], items[r] = items[r], items[l]
		}
	}
}

// Get returns one task.
func (s *TaskService) Get(ctx context.Context, id uuid.UUID) (Task, error) {
	tenant := ctxkit.TenantOf(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	// Another account's task is NotFound, not Forbidden. Forbidden would
	// confirm the id exists to somebody who should not know that.
	if !ok || t.AccountID != tenant.AccountID {
		return Task{}, errs.NotFound("task not found").WithCode("TASK_NOT_FOUND")
	}
	return t, nil
}

// Create stores a task.
func (s *TaskService) Create(ctx context.Context, req dto.CreateTask) (Task, error) {
	tenant := ctxkit.TenantOf(ctx)
	if tenant.Zero() {
		return Task{}, errs.Unauthorized("no account context").WithCode("NO_TENANT")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.AccountID == tenant.AccountID && strings.EqualFold(t.Title, req.Title) && t.CompletedAt == nil {
			return Task{}, errs.Conflict("an open task titled %q already exists", req.Title).
				WithCode("TITLE_TAKEN")
		}
	}

	t := Task{
		ID:        uuid.New(),
		AccountID: tenant.AccountID,
		Title:     req.Title,
		Notes:     req.Notes,
		Priority:  req.Priority,
		Tags:      req.Tags,
		DueAt:     req.DueAt,
		CreatedAt: time.Now().UTC(),
	}
	s.tasks[t.ID] = t
	return t, nil
}

// Update applies the fields the client actually sent.
func (s *TaskService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateTask) (Task, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Only a non-nil pointer means "the client sent this field".
	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Notes != nil {
		existing.Notes = *req.Notes
	}
	if req.Priority != nil {
		existing.Priority = *req.Priority
	}
	if req.DueAt != nil {
		existing.DueAt = req.DueAt
	}
	s.tasks[existing.ID] = existing
	return existing, nil
}

// Complete marks a task done and publishes TaskCompleted.
func (s *TaskService) Complete(ctx context.Context, id uuid.UUID) (Task, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if t.CompletedAt != nil {
		return Task{}, errs.Conflict("this task is already done").WithCode("ALREADY_COMPLETED")
	}

	now := time.Now().UTC()
	t.CompletedAt = &now

	s.mu.Lock()
	s.tasks[t.ID] = t
	s.mu.Unlock()

	// Publishing is best-effort by design: the subscribers are side effects,
	// and a failing notifier must not fail the request that completed the
	// task.
	_ = events.Publish(ctx, s.bus, TaskCompleted{
		TaskID: t.ID, AccountID: t.AccountID, Title: t.Title,
	})
	return t, nil
}

// Delete removes a task.
func (s *TaskService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

// PurgeCompleted deletes tasks finished before cutoff and returns how many.
//
// It takes a context and no request, which is the whole point of keeping the
// service free of HTTP: the scheduled job calls exactly this.
func (s *TaskService) PurgeCompleted(ctx context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, t := range s.tasks {
		if t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
			delete(s.tasks, id)
			removed++
		}
	}
	if removed > 0 {
		logx.C(ctx).Info("purged completed tasks", "count", removed)
	}
	return removed, nil
}

// ToResponse maps to the public shape.
func ToResponse(t Task) dto.TaskResponse {
	return dto.TaskResponse{
		ID: t.ID.String(), Title: t.Title, Notes: t.Notes,
		Priority: t.Priority, Tags: t.Tags, DueAt: t.DueAt,
		CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt,
	}
}

// ToResponses maps a page.
func ToResponses(items []Task) []dto.TaskResponse {
	out := make([]dto.TaskResponse, 0, len(items))
	for _, t := range items {
		out = append(out, ToResponse(t))
	}
	return out
}
