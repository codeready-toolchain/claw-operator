---
name: golang-error-handling
description: Error handling patterns for Go including error wrapping, custom errors, sentinel errors, and error checking best practices. Use when implementing error handling, defining error types, or working with error flows.
---

# Go Error Handling

Modern error handling patterns for Go following 2025-2026 best practices.

## Basic Error Handling

**Always check errors:**
```go
result, err := doSomething()
if err != nil {
	return fmt.Errorf("operation failed: %w", err)
}
```

**Never ignore errors (unless explicitly documented):**
```go
// Bad
doSomething()

// Good - explicit discard when safe
_ = file.Close()

// Good - handle in defer with function
defer func() {
	if err := file.Close(); err != nil {
		log.Printf("failed to close file: %v", err)
	}
}()
```

## Error Wrapping

**Use `%w` to wrap errors:**
```go
func GetUser(id string) (*User, error) {
	user, err := db.Query(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get user %s: %w", id, err)
	}
	return user, nil
}
```

**Unwrap errors with `errors.Is()` and `errors.As()`:**
```go
err := GetUser("123")
if errors.Is(err, sql.ErrNoRows) {
	// Handle not found
}

var validationErr *ValidationError
if errors.As(err, &validationErr) {
	// Handle validation error
	fmt.Printf("field: %s, message: %s", validationErr.Field, validationErr.Message)
}
```

## Sentinel Errors

**Define package-level error values:**
```go
package services

import "errors"

var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrConflict      = errors.New("resource conflict")
	ErrUnauthorized  = errors.New("unauthorized")
)
```

**Check with `errors.Is()`:**
```go
session, err := service.GetSession(ctx, id)
if errors.Is(err, services.ErrNotFound) {
	return http.StatusNotFound, "session not found"
}
if err != nil {
	return http.StatusInternalServerError, "internal error"
}
```

## Custom Error Types

**For errors needing additional context:**
```go
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s %s", e.Field, e.Message)
}

func NewValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// Usage
if req.SessionID == "" {
	return nil, NewValidationError("session_id", "required")
}
```

**Error type with wrapped cause:**
```go
type DatabaseError struct {
	Operation string
	Cause     error
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("database error during %s: %v", e.Operation, e.Cause)
}

func (e *DatabaseError) Unwrap() error {
	return e.Cause
}

// Usage
if err := saveData(); err != nil {
	return &DatabaseError{
		Operation: "save",
		Cause:     err,
	}
}
```

## Error Checking Patterns

**Check and return early:**
```go
func ProcessData(data string) error {
	if data == "" {
		return ErrInvalidInput
	}
	
	result, err := transform(data)
	if err != nil {
		return fmt.Errorf("transform failed: %w", err)
	}
	
	if err := save(result); err != nil {
		return fmt.Errorf("save failed: %w", err)
	}
	
	return nil
}
```

**Multiple return values:**
```go
// Idiomatic Go
func GetUser(id string) (*User, error) {
	// Return nil, err on error
	// Return user, nil on success
}

// Not idiomatic
func GetUser(id string) (*User, bool, error) {
	// Avoid bool flags
}
```

## Error Context

**Add context when wrapping:**
```go
func CreateSession(ctx context.Context, req Request) (*Session, error) {
	// Add operation context
	session, err := buildSession(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build session: %w", err)
	}
	
	// Add identifier context
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session %s: %w", session.ID, err)
	}
	
	return session, nil
}
```

**Progressive error context:**
```go
// Low-level function
func parseConfig(data []byte) (*Config, error) {
	// Returns: "invalid JSON syntax"
}

// Mid-level function
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	
	cfg, err := parseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse config from %s: %w", path, err)
	}
	return cfg, nil
}

// High-level function
func initializeApp() error {
	cfg, err := loadConfig("/etc/app/config.json")
	if err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}
	// Result: "failed to initialize app: parse config from /etc/app/config.json: invalid JSON syntax"
}
```

## Error Handling in Transactions

**Rollback on error, handle commit failure:**
```go
func (s *Service) Transaction(ctx context.Context) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	// Always rollback - it's a no-op after commit succeeds
	defer func() { _ = tx.Rollback() }()
	
	if err := tx.Entity.Create().Save(ctx); err != nil {
		return fmt.Errorf("failed to create entity: %w", err)
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	
	return nil
}
```

## Multiple Error Handling

**Using errors.Join() (Go 1.20+):**
```go
func ValidateRequest(req Request) error {
	var errs []error
	
	if req.Name == "" {
		errs = append(errs, NewValidationError("name", "required"))
	}
	if req.Email == "" {
		errs = append(errs, NewValidationError("email", "required"))
	}
	if req.Age < 0 {
		errs = append(errs, NewValidationError("age", "must be positive"))
	}
	
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Check individual errors
err := ValidateRequest(req)
if err != nil {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		// Handle first validation error
	}
}
```

## Error Logging

**Log at the right level:**
```go
func ProcessRequest(ctx context.Context, req Request) error {
	session, err := s.GetSession(ctx, req.SessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Expected error - log at lower level or not at all
			return fmt.Errorf("session not found: %w", err)
		}
		// Unexpected error - log at error level
		log.Printf("ERROR: failed to get session: %v", err)
		return fmt.Errorf("internal error: %w", err)
	}
	
	// ...
	return nil
}
```

**Don't log and return:**
```go
// Bad - error gets logged multiple times
func GetUser(id string) (*User, error) {
	user, err := db.Query(id)
	if err != nil {
		log.Printf("ERROR: failed to query user: %v", err)
		return nil, err  // Caller might also log this
	}
	return user, nil
}

// Good - log at top level or return for caller to handle
func GetUser(id string) (*User, error) {
	user, err := db.Query(id)
	if err != nil {
		return nil, fmt.Errorf("query user %s: %w", id, err)
	}
	return user, nil
}

// In handler/main
func Handler(w http.ResponseWriter, r *http.Request) {
	user, err := service.GetUser(id)
	if err != nil {
		log.Printf("ERROR: %v", err)  // Log once at top level
		http.Error(w, "internal error", 500)
		return
	}
}
```

## Error Conversion for APIs

**Convert internal errors to HTTP responses:**
```go
func ErrorToHTTPStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	
	// Check specific error types
	if errors.Is(err, services.ErrNotFound) {
		return http.StatusNotFound, "resource not found"
	}
	if errors.Is(err, services.ErrAlreadyExists) {
		return http.StatusConflict, "resource already exists"
	}
	if errors.Is(err, services.ErrInvalidInput) {
		return http.StatusBadRequest, "invalid input"
	}
	
	// Check for validation errors
	var validationErr *services.ValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest, fmt.Sprintf("invalid %s: %s", 
			validationErr.Field, validationErr.Message)
	}
	
	// Default to internal error
	return http.StatusInternalServerError, "internal server error"
}

// Usage in handler
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.CreateSession(r.Context(), req)
	if err != nil {
		status, message := ErrorToHTTPStatus(err)
		http.Error(w, message, status)
		return
	}
	
	json.NewEncoder(w).Encode(session)
}
```

## Panic and Recover

**Use recover only at boundaries:**
```go
func handler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v\n%s", r, debug.Stack())
			http.Error(w, "internal server error", 500)
		}
	}()
	
	// Handler logic
}
```

**Don't use panic for normal errors:**
```go
// Bad
func MustGetConfig() *Config {
	cfg, err := loadConfig()
	if err != nil {
		panic(err)  // Don't panic
	}
	return cfg
}

// Good
func GetConfig() (*Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}
```

## TARSy-Specific Patterns

**Service layer error definitions:**
```go
// pkg/services/errors.go
package services

var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s %s", e.Field, e.Message)
}

func NewValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
```

**Service method error handling:**
```go
func (s *SessionService) CreateSession(ctx context.Context, req CreateSessionRequest) (*ent.AlertSession, error) {
	// Validate input
	if req.SessionID == "" {
		return nil, NewValidationError("session_id", "required")
	}
	
	// Database operation
	session, err := s.client.AlertSession.Create().
		SetID(req.SessionID).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("session %s: %w", req.SessionID, ErrAlreadyExists)
		}
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	
	return session, nil
}
```

## Quick Reference

**Error creation:**
```go
errors.New("message")                    // Simple error
fmt.Errorf("format: %w", err)           // Wrapped error
&CustomError{Field: "value"}            // Custom error type
```

**Error checking:**
```go
errors.Is(err, ErrNotFound)             // Check sentinel error
errors.As(err, &validationErr)          // Check error type
err != nil                               // Check if error occurred
```

**Best practices:**
- Always check errors (except documented safe ignores)
- Wrap errors with context using `%w`
- Define sentinel errors for common cases
- Use custom types for errors needing data
- Check with `errors.Is()` and `errors.As()`
- Add context progressively up the call stack
- Log errors once at appropriate level
- Convert internal errors for API responses
