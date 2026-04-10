---
name: golang-context-patterns
description: Context usage patterns for Go including cancellation, timeouts, deadlines, and database transactions. Use when handling HTTP requests, database operations, or implementing cancellation and timeout logic.
---

# Go Context Patterns

Context usage patterns for Go following 2025-2026 best practices.

## Context Basics

**Context carries:**
- Cancellation signals
- Deadlines and timeouts
- Request-scoped values

**Golden rule:** Always pass context as first parameter.

```go
func DoWork(ctx context.Context, data string) error {
	// Pass ctx to all downstream operations
}
```

## HTTP Handler Context

**Extract from request:**
```go
func (s *Server) HandleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()  // Get request context
	
	// Context is cancelled if:
	// - Client disconnects
	// - Server timeout reached
	
	result, err := s.service.ProcessData(ctx, data)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Client disconnected
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(result)
}
```

## Database Transaction Context Pattern

**Critical pattern for TARSy:**

```go
func (s *SessionService) CreateSession(ctx context.Context, req CreateSessionRequest) (*ent.AlertSession, error) {
	writeCtx, cancel := context.WithTimeoutCause(
		context.Background(), 5*time.Second,
		fmt.Errorf("create session %s: db write timed out", req.SessionID),
	)
	defer cancel()
	
	tx, err := s.client.Tx(writeCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	
	session, err := tx.AlertSession.Create().
		SetID(req.SessionID).
		Save(writeCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}
	
	return session, nil
}
```

**Why background context for database operations:**
- HTTP request context might be cancelled if client disconnects
- Database writes should complete even if client disconnects
- Use separate timeout to prevent hanging forever

## Context Timeout Patterns

**WithTimeoutCause for operations with deadline:**
```go
func FetchData(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, 10*time.Second,
		fmt.Errorf("fetch %s timed out", url),
	)
	defer cancel()
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	return io.ReadAll(resp.Body)
}
```

**WithDeadlineCause for specific time:**
```go
func ProcessByDeadline(ctx context.Context, deadline time.Time) error {
	ctx, cancel := context.WithDeadlineCause(ctx, deadline,
		fmt.Errorf("processing missed deadline %s", deadline),
	)
	defer cancel()
	
	return doWork(ctx)
}
```

## Context Cancellation

**WithCancelCause for manual cancellation:**
```go
func ProcessWithCancel(ctx context.Context) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	
	go func() {
		if err := backgroundWork(ctx); err != nil {
			cancel(fmt.Errorf("background work failed: %w", err))
		}
	}()
	
	return mainWork(ctx)
}
```

**Checking for cancellation:**
```go
func LongOperation(ctx context.Context) error {
	for i := range 1000 {
		select {
		case <-ctx.Done():
			return context.Cause(ctx) // Returns the cause error, or ctx.Err() if no cause was set
		default:
		}
		
		processItem(i)
	}
	return nil
}
```

## Database Query Context

**Using context for queries:**
```go
func (s *SessionService) GetSession(ctx context.Context, id string) (*ent.AlertSession, error) {
	// Ent automatically uses ctx for query timeout/cancellation
	session, err := s.client.AlertSession.
		Query().
		Where(alertsession.IDEQ(id)).
		Only(ctx)  // Pass context here
	if err != nil {
		return nil, err
	}
	return session, nil
}
```

**Query with timeout:**
```go
func (s *SessionService) ListSessions(ctx context.Context, limit int) ([]*ent.AlertSession, error) {
	queryCtx, cancel := context.WithTimeoutCause(ctx, 3*time.Second,
		fmt.Errorf("list sessions query timed out"),
	)
	defer cancel()
	
	sessions, err := s.client.AlertSession.
		Query().
		Limit(limit).
		All(queryCtx)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}
```

## Goroutine Context Propagation

**Pass context to goroutines:**
```go
func ProcessConcurrently(ctx context.Context, items []string) error {
	errChan := make(chan error, len(items))
	
	for _, item := range items {
		go func() {
			errChan <- processItem(ctx, item)
		}()
	}
	
	for range items {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}
```

**Cancelling goroutines on error:**
```go
func ProcessWithEarlyExit(ctx context.Context, items []string) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	
	errChan := make(chan error, len(items))
	
	for _, item := range items {
		go func() {
			err := processItem(ctx, item)
			if err != nil {
				cancel(err)
			}
			errChan <- err
		}()
	}
	
	for range items {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}
```

## Context AfterFunc

**Run cleanup when context is cancelled:**
```go
func WatchResource(ctx context.Context, res *Resource) {
	stop := context.AfterFunc(ctx, func() {
		res.Release()
	})
	defer stop() // Prevent AfterFunc from firing if we return normally
	
	// ... use resource ...
}
```

## Context Best Practices

**DO:**
- Always pass context as first parameter
- Use `context.Background()` as root context
- Use `WithTimeoutCause`/`WithCancelCause`/`WithDeadlineCause` — always attach a cause
- Use `context.Cause(ctx)` to retrieve the cause on cancellation
- Use background context with timeout for database writes that should complete
- Check `ctx.Done()` in long-running loops
- Propagate context through call chains

**DON'T:**
- Store context in structs (pass as parameter instead)
- Pass `nil` context (use `context.TODO()` if unsure)
- Use context for optional function parameters
- Create new background context when you have a valid parent context
- Ignore context cancellation errors

## TARSy-Specific Patterns

**HTTP → Service → Database:**
```go
// HTTP Handler
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	// Use request context for coordination
	ctx := r.Context()
	
	// Service uses background context for database
	session, err := h.sessionService.CreateSession(ctx, req)
	// ...
}

// Service Layer
func (s *SessionService) CreateSession(httpCtx context.Context, req CreateSessionRequest) (*ent.AlertSession, error) {
	writeCtx, cancel := context.WithTimeoutCause(
		context.Background(), 5*time.Second,
		fmt.Errorf("create session: db write timed out"),
	)
	defer cancel()
	
	tx, err := s.client.Tx(writeCtx)
	// ...
}
```

**Background job context:**
```go
func (s *SessionService) CleanupOldSessions(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeoutCause(
		context.Background(), 30*time.Second,
		fmt.Errorf("session cleanup timed out"),
	)
	defer cancel()
	
	count, err := s.SoftDeleteOldSessions(cleanupCtx, 90)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}
	
	log.Printf("Cleaned up %d sessions", count)
	return nil
}
```

## Transaction Context Guidelines

**Standard pattern for TARSy services:**

```go
func (s *Service) WriteOperation(httpCtx context.Context, data Data) error {
	// 1. Create background context with timeout+cause for reliability and diagnostics
	writeCtx, cancel := context.WithTimeoutCause(
		context.Background(), 5*time.Second,
		fmt.Errorf("write operation: db timed out"),
	)
	defer cancel()
	
	// 2. Begin transaction with write context
	tx, err := s.client.Tx(writeCtx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	
	// 3. Use writeCtx for all operations
	result, err := tx.Entity.Create().
		SetData(data).
		Save(writeCtx)
	if err != nil {
		return err
	}
	
	// 4. Commit with same context
	if err := tx.Commit(); err != nil {
		return err
	}
	
	return nil
}
```

## Quick Reference

**Context creation:**
```go
context.Background()                       // Root context
context.TODO()                             // When unsure which to use
context.WithCancelCause(parent)            // Manual cancellation with reason
context.WithTimeoutCause(parent, d, err)   // Timeout with reason
context.WithDeadlineCause(parent, t, err)  // Deadline with reason
context.AfterFunc(ctx, fn)                 // Run fn when ctx is cancelled
```

**Context checking:**
```go
<-ctx.Done()                      // Wait for cancellation
context.Cause(ctx)                // Get the cause error, or ctx.Err() if no cause was set
errors.Is(err, context.Canceled)  // Check if cancelled
errors.Is(err, context.DeadlineExceeded)  // Check if timed out
```

**Common timeouts:**
- HTTP requests: 10-30 seconds
- Database queries: 3-5 seconds
- Database writes: 5-10 seconds
- Background jobs: 30-60 seconds
