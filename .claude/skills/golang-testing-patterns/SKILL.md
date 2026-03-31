---
name: golang-testing-patterns
description: Modern Go testing patterns including table-driven tests, subtests, test organization, and best practices. Use when writing or refactoring tests, implementing test coverage, or when the user asks about Go testing approaches.
---

# Go Testing Patterns

Modern testing patterns for Go following 2025-2026 best practices.

## Table-Driven Tests

The idiomatic Go approach for comprehensive testing.

**Basic structure:**
```go
func TestFeature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		wantErr  bool
	}{
		{
			name:     "valid input",
			input:    "hello",
			expected: 5,
			wantErr:  false,
		},
		{
			name:     "empty input",
			input:    "",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessString(tt.input)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			
			if got != tt.expected {
				t.Errorf("got %d, want %d", got, tt.expected)
			}
		})
	}
}
```

## Using Subtests with t.Run()

**Benefits:**
- Clear failure messages showing which case failed
- Can run specific tests: `go test -run TestFeature/valid_input`
- Parallel execution support

**Pattern:**
```go
for _, tt := range tests {
	tt := tt // Required for parallel tests
	t.Run(tt.name, func(t *testing.T) {
		t.Parallel() // Run subtests in parallel
		// ... test logic
	})
}
```

## Test Organization

**File structure:**
- Tests live alongside code: `service.go` → `service_test.go`
- Integration tests in separate package: `service_integration_test.go`
- Shared test utilities: `test/util/` or `testutil/`

**Test function naming:**
```go
// Unit tests
func TestSessionService_CreateSession(t *testing.T) {}
func TestSessionService_CreateSession_ValidationError(t *testing.T) {}

// Integration tests
func TestSessionService_Integration(t *testing.T) {}
```

## Setup and Teardown

**Using t.Cleanup():**
```go
func TestWithCleanup(t *testing.T) {
	// Setup
	db := setupTestDB(t)
	t.Cleanup(func() {
		db.Close() // Always runs, even if test fails
	})
	
	// Test logic
	// ...
}
```

**Setup once for all subtests:**
```go
func TestSuite(t *testing.T) {
	// Shared setup
	db := setupTestDB(t)
	t.Cleanup(func() { db.Close() })
	
	t.Run("test1", func(t *testing.T) {
		// Uses shared db
	})
	
	t.Run("test2", func(t *testing.T) {
		// Uses shared db
	})
}
```

## Testing Database Operations

**Transaction-based isolation:**
```go
func TestDatabaseOperation(t *testing.T) {
	db := setupTestDB(t)
	
	tests := []struct {
		name string
		// ... test fields
	}{
		// ... test cases
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each subtest gets fresh data
			tx, _ := db.Begin()
			t.Cleanup(func() { tx.Rollback() })
			
			// Use tx for test operations
			// ...
		})
	}
}
```

## Error Testing Patterns

**Check error occurrence:**
```go
if tt.wantErr {
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	return
}

if err != nil {
	t.Fatalf("unexpected error: %v", err)
}
```

**Check specific error:**
```go
if !errors.Is(err, ErrNotFound) {
	t.Errorf("expected ErrNotFound, got %v", err)
}
```

**Check error message:**
```go
if err == nil || !strings.Contains(err.Error(), "required") {
	t.Errorf("expected validation error, got %v", err)
}
```

## Test Helpers

**Creating test data:**
```go
func newTestSession(t *testing.T, overrides ...func(*ent.AlertSession)) *ent.AlertSession {
	t.Helper() // Ensures stack traces point to the caller
	
	session := &ent.AlertSession{
		ID:        "test-123",
		Status:    "pending",
		StartedAt: time.Now(),
	}
	
	for _, override := range overrides {
		override(session)
	}
	
	return session
}

// Usage
session := newTestSession(t, func(s *ent.AlertSession) {
	s.Status = "completed"
})
```

## Mocking and Interfaces

**Interface-based testing:**
```go
type SessionRepository interface {
	GetByID(ctx context.Context, id string) (*Session, error)
}

// In test
type mockSessionRepo struct {
	sessions map[string]*Session
}

func (m *mockSessionRepo) GetByID(ctx context.Context, id string) (*Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}
```

## Testing Patterns for TARSy

**Service layer tests:**
```go
func TestSessionService_CreateSession(t *testing.T) {
	// Setup test database with cleanup
	client, _ := test.SetupTestDatabase(t)
	service := services.NewSessionService(client)
	
	tests := []struct {
		name    string
		req     models.CreateSessionRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid session",
			req: models.CreateSessionRequest{
				SessionID: "sess-123",
				// ... other fields
			},
			wantErr: false,
		},
		{
			name: "missing session ID",
			req: models.CreateSessionRequest{
				SessionID: "",
			},
			wantErr: true,
			errMsg:  "session_id",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			session, err := service.CreateSession(ctx, tt.req)
			
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q doesn't contain %q", err.Error(), tt.errMsg)
				}
				return
			}
			
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			
			if session.ID != tt.req.SessionID {
				t.Errorf("session ID = %q, want %q", session.ID, tt.req.SessionID)
			}
		})
	}
}
```

## Quick Reference

**When to use table-driven tests:**
- Multiple similar test cases with different inputs/outputs
- Testing edge cases and validation
- Any test with more than 2-3 cases

**When to skip table-driven tests:**
- Single, unique test case
- Complex setup that varies significantly between cases
- Integration tests with sequential dependencies

**Parallel testing:**
```go
t.Parallel() // Use for fast, independent tests
// Don't use for: database tests, shared state, rate-limited APIs
```

**Assertions:**
- Use `t.Errorf()` for non-fatal assertions
- Use `t.Fatalf()` when continuing would panic or is meaningless
- Always include both `got` and `want` in error messages
