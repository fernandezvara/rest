package rest

import (
	stdjson "encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) HTTPErrorStatus {
	t.Helper()

	var e HTTPErrorStatus
	if err := stdjson.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not a valid error payload: %v (%q)", err, rec.Body.String())
	}
	return e
}

func TestResponseSuccess(t *testing.T) {
	rec := httptest.NewRecorder()

	status := Response(rec, map[string]string{"a": "b"}, nil, http.StatusCreated, "/things/1")

	if status != http.StatusCreated || rec.Code != http.StatusCreated {
		t.Fatalf("status = %d / %d, want 201", status, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if got := rec.Header().Get("Location"); got != "/things/1" {
		t.Errorf("Location = %q", got)
	}

	var body map[string]string
	if err := stdjson.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["a"] != "b" {
		t.Errorf("body = %q, err = %v", rec.Body.String(), err)
	}
}

func TestResponseSuccessWithoutBodyOrLocation(t *testing.T) {
	rec := httptest.NewRecorder()

	status := Response(rec, nil, nil, http.StatusNoContent, "")

	if status != http.StatusNoContent {
		t.Fatalf("status = %d", status)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", rec.Body.String())
	}
	if _, ok := rec.Header()["Location"]; ok {
		t.Error("Location header must not be set")
	}
}

func TestResponseErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"conflict", ErrConflict, http.StatusConflict, ""},
		{"bad request", ErrBadRequest, http.StatusBadRequest, ""},
		{"unauthorized", ErrUnauthorized, http.StatusUnauthorized, ""},
		{"unprocessable", ErrUnprocessableEntity, http.StatusUnprocessableEntity, ""},
		{"not found", ErrNotFound, http.StatusNotFound, ""},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			status := Response(rec, "ignored", tt.err, http.StatusOK, "")

			if status != tt.status || rec.Code != tt.status {
				t.Fatalf("status = %d / %d, want %d", status, rec.Code, tt.status)
			}
			e := decodeError(t, rec)
			if e.Code != tt.status || e.Message != http.StatusText(tt.status) || e.Reason != tt.reason {
				t.Errorf("payload = %+v", e)
			}
		})
	}
}

func TestResponseErr(t *testing.T) {
	rec := httptest.NewRecorder()
	if status := ResponseErr(rec, ErrNotFound); status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}

	rec = httptest.NewRecorder()
	if status := ResponseErr(rec, nil); status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestGenericErrorHelpers(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	tests := []struct {
		name   string
		call   func(w http.ResponseWriter)
		status int
		reason string
	}{
		{"NotFound", func(w http.ResponseWriter) { NotFound(w, r) }, http.StatusNotFound, ""},
		{"NotAllowed", func(w http.ResponseWriter) { NotAllowed(w, r) }, http.StatusMethodNotAllowed, ""},
		{"BadRequest", func(w http.ResponseWriter) { BadRequest(w, r, "bad") }, http.StatusBadRequest, "bad"},
		{"Forbidden", func(w http.ResponseWriter) { Forbidden(w, r, "nope") }, http.StatusForbidden, "nope"},
		{"Unauthorized", func(w http.ResponseWriter) { Unauthorized(w, r) }, http.StatusUnauthorized, ""},
		{"UnprocessableEntity", func(w http.ResponseWriter) { UnprocessableEntity(w, r) }, http.StatusUnprocessableEntity, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.call(rec)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			e := decodeError(t, rec)
			if e.Code != tt.status || e.Message != http.StatusText(tt.status) || e.Reason != tt.reason {
				t.Errorf("payload = %+v", e)
			}
		})
	}
}

func TestBadRequestValidation(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	rec := httptest.NewRecorder()
	BadRequestValidation(rec, r, "invalid", map[string]string{"name": "required"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	e := decodeError(t, rec)
	if e.Reason != "invalid" || e.ValidationErrors["name"] != "required" {
		t.Errorf("payload = %+v", e)
	}

	// empty reason and errors are omitted from the payload
	rec = httptest.NewRecorder()
	BadRequestValidation(rec, r, "", nil)
	var raw map[string]interface{}
	if err := stdjson.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["reason"]; ok {
		t.Error("reason must be omitted when empty")
	}
	if _, ok := raw["validation_errors"]; ok {
		t.Error("validation_errors must be omitted when empty")
	}
}
