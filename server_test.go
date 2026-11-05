package rushline

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmHoldHandler(t *testing.T) {
	t.Run("memory confirmation and retry succeed", func(t *testing.T) {
		clock := MockClock{
			StaticTime: now,
		}
		event := setupEvent(t, now, 1)
		expectedHold := setupHold(t, event, now)
		expectedHold.status = HoldStatusConfirmed
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body SuccessResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		if err != nil {
			t.Fatalf("failed to decode success response: %v", err)
		}
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		if status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd := event.getAvailability()
		storedHold, exists := event.getHoldByID("1")
		if body.Hold.ID != expectedHold.id {
			t.Errorf("expected body hold id to be %v, but got %v", expectedHold.id, body.Hold.ID)
		}
		if body.Hold.Quantity != expectedHold.quantity {
			t.Errorf("expected body hold quantity to be %v, but got %v", expectedHold.quantity, body.Hold.Quantity)
		}
		if !body.Hold.Deadline.Equal(expectedHold.deadline) {
			t.Errorf("expected body hold deadline to be %v, but got %v", expectedHold.deadline, body.Hold.Deadline)
		}
		if body.Hold.Status != "confirmed" {
			t.Errorf("expected body hold status to be 'confirmed', but got '%v'", body.Hold.Status)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}

		clock.StaticTime = now.Add(2 * time.Minute)
		req = httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var retryBody SuccessResponse
		err = json.NewDecoder(rr.Body).Decode(&retryBody)
		if err != nil {
			t.Fatalf("failed to decode success response: %v", err)
		}
		status = rr.Code
		contentTypeHeader = rr.Header().Get("Content-Type")
		if status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("Expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd = event.getAvailability()
		storedHold, exists = event.getHoldByID("1")
		if retryBody.Hold.ID != body.Hold.ID {
			t.Errorf("expected body hold id to be %v, but got %v", body.Hold.ID, retryBody.Hold.ID)
		}
		if retryBody.Hold.Quantity != body.Hold.Quantity {
			t.Errorf("expected body hold quantity to be %v, but got %v", body.Hold.Quantity, retryBody.Hold.Quantity)
		}
		if !retryBody.Hold.Deadline.Equal(body.Hold.Deadline) {
			t.Errorf("expected body hold deadline to be %v, but got %v", body.Hold.Deadline, retryBody.Hold.Deadline)
		}
		if retryBody.Hold.Status != "confirmed" {
			t.Errorf("expected body hold status to be 'confirmed', but got '%v'", retryBody.Hold.Status)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
	})

	t.Run("repository confirmation and retry succeed", func(t *testing.T) {
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		clock := MockClock{
			StaticTime: now,
		}
		event := &Event{
			id:                 "1",
			capacity:           2,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		}
		hold := Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline,
		}
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))
		defer cancel()
		repo, _ := setupTestcontainer(t, ctx)
		_, err := repo.CreateEvent(ctx, event)
		require.NoError(t, err)
		_, err = repo.CreateHold(ctx, hold, event.id)
		require.NoError(t, err)
		server, err := initServer(&clock, repo)
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body SuccessResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		require.NoError(t, err)
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "application/json; charset=utf-8", contentTypeHeader)

		assert.Equal(t, hold.id, body.Hold.ID)
		assert.Equal(t, "confirmed", body.Hold.Status)
		assert.Equal(t, hold.quantity, body.Hold.Quantity)
		assert.Equal(t, hold.deadline, body.Hold.Deadline)

		storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, storedHold.id)
		assert.Equal(t, HoldStatusConfirmed, storedHold.status)
		assert.Equal(t, hold.quantity, storedHold.quantity)
		assert.Equal(t, hold.deadline, storedHold.deadline)

		clock.StaticTime = now.Add(3 * time.Hour)
		req = httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var retryBody SuccessResponse
		err = json.NewDecoder(rr.Body).Decode(&retryBody)
		require.NoError(t, err)
		status = rr.Code
		contentTypeHeader = rr.Header().Get("Content-Type")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "application/json; charset=utf-8", contentTypeHeader)

		assert.Equal(t, hold.id, retryBody.Hold.ID)
		assert.Equal(t, "confirmed", retryBody.Hold.Status)
		assert.Equal(t, hold.quantity, retryBody.Hold.Quantity)
		assert.Equal(t, hold.deadline, retryBody.Hold.Deadline)

		storedHold, err = repo.ReadHold(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, storedHold.id)
		assert.Equal(t, HoldStatusConfirmed, storedHold.status)
		assert.Equal(t, hold.quantity, storedHold.quantity)
		assert.Equal(t, hold.deadline, storedHold.deadline)

		operation, err := repo.ReadOperation(ctx, "1", storedHold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, storedHold, operation)

		var operationCount int
		err = repo.pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM operations").Scan(&operationCount)
		assert.NoError(t, err)
		assert.Equal(t, 1, operationCount)
	})

	t.Run("unexpected error", func(t *testing.T) {
		clock := MockClock{
			StaticTime: now,
		}
		event := setupEvent(t, now, 1)
		expectedHold := setupHold(t, event, now)
		expectedHold.status = HoldStatusInvalid
		event.holds[expectedHold.id] = expectedHold
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body ErrorResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		if err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		if status != http.StatusInternalServerError {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusInternalServerError)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("Expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd := event.getAvailability()
		storedHold, exists := event.getHoldByID("1")
		if body.Code != "internal_error" {
			t.Errorf("expected internal_error code, but got '%v'", body.Code)
		}
		if body.Message != "Internal Server Error" {
			t.Errorf("expected error message to be '%v', but got '%v'", "Internal Server Error", body.Message)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
	})

	t.Run("non-POST request", func(t *testing.T) {
		clock := MockClock{
			StaticTime: now,
		}
		event := setupEvent(t, now, 1)
		expectedHold := setupHold(t, event, now)
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodGet, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body ErrorResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		if err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		allowHeader := rr.Header().Get("Allow")
		if status != http.StatusMethodNotAllowed {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusMethodNotAllowed)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("Expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}
		if allowHeader != "POST" {
			t.Errorf("Expected 'Allow: POST', but was 'Allow: %v'", allowHeader)
		}

		availabilityEnd := event.getAvailability()
		storedHold, exists := event.getHoldByID("1")
		if body.Code != "method_not_allowed" {
			t.Errorf("expected method_not_allowed code, but got '%v'", body.Code)
		}
		if body.Message != "method not allowed" {
			t.Errorf("expected error message to be '%v', but got '%v'", "method not allowed", body.Message)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
	})

	t.Run("active hold over deadline", func(t *testing.T) {
		clock := MockClock{
			StaticTime: now.Add(2 * time.Minute),
		}
		event := setupEvent(t, now, 1)
		expectedHold := setupHold(t, event, now)
		expectedHold.status = HoldStatusExpired
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body ErrorResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		if err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		if status != http.StatusConflict {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusConflict)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd := event.getAvailability()
		storedHold, exists := event.getHoldByID("1")
		if body.Code != "after_confirmation_deadline" {
			t.Errorf("expected 'after_confirmation_deadline' code, but got '%v'", body.Code)
		}
		if body.Message != ErrAfterConfirmationDeadline.Error() {
			t.Errorf("expected error message to be '%v', but got '%v'", ErrAfterConfirmationDeadline.Error(), body.Message)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart+expectedHold.quantity != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart+expectedHold.quantity, availabilityEnd)
		}
	})

	t.Run("operation key conflict", func(t *testing.T) {
		clock := MockClock{
			StaticTime: now,
		}
		event := setupEvent(t, now, 2)
		hold := setupHold(t, event, now)
		confirmedHold, err := event.confirmHold(hold.id, now, "1")
		if err != nil {
			t.Fatalf("failed to confirm hold, %v", err.Error())
		}
		expectedHold, err := event.createHold("2", 1, now, time.Minute)
		if err != nil {
			t.Fatalf("failed to create hold, %v", err.Error())
		}
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
		req := httptest.NewRequest(http.MethodPost, "/events/1/holds/2/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var body ErrorResponse
		err = json.NewDecoder(rr.Body).Decode(&body)
		if err != nil {
			t.Fatalf("failed to decode error response, %v", err)
		}
		status := rr.Code
		contentTypeHeader := rr.Header().Get("Content-Type")
		if status != http.StatusConflict {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusConflict)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd := event.getAvailability()
		storedHoldOne, exists := event.getHoldByID("1")
		if !exists {
			t.Error("stored hold '1' not found")
		}
		storedHoldTwo, exists := event.getHoldByID("2")
		if !exists {
			t.Error("stored hold '2' not found")
		}
		if body.Code != "operation_key_conflict" {
			t.Errorf("expected 'operation_key_conflict' code, but got '%v'", body.Code)
		}
		if body.Message != ErrOperationKeyConflict.Error() {
			t.Errorf("expected error message to be '%v', but got '%v'", ErrOperationKeyConflict.Error(), body.Message)
		}
		if storedHoldOne != confirmedHold {
			t.Errorf("expected stored hold be %v, but it was %v", confirmedHold, storedHoldOne)
		}
		if storedHoldTwo != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHoldTwo)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}

		clock.StaticTime = now.Add(2 * time.Minute)
		req = httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
		req.Header.Set("operation-key", "1")
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		var retryBody SuccessResponse
		err = json.NewDecoder(rr.Body).Decode(&retryBody)
		if err != nil {
			t.Fatalf("failed to decode success response: %v", err)
		}
		status = rr.Code
		contentTypeHeader = rr.Header().Get("Content-Type")
		if status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}
		if contentTypeHeader != "application/json; charset=utf-8" {
			t.Errorf("Expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
		}

		availabilityEnd = event.getAvailability()
		storedHold, exists := event.getHoldByID("1")
		if retryBody.Hold.ID != confirmedHold.id {
			t.Errorf("expected body hold id to be %v, but got %v", confirmedHold.id, retryBody.Hold.ID)
		}
		if retryBody.Hold.Quantity != confirmedHold.quantity {
			t.Errorf("expected body hold quantity to be %v, but got %v", confirmedHold.quantity, retryBody.Hold.Quantity)
		}
		if !retryBody.Hold.Deadline.Equal(confirmedHold.deadline) {
			t.Errorf("expected body hold deadline to be %v, but got %v", confirmedHold.deadline, retryBody.Hold.Deadline)
		}
		if retryBody.Hold.Status != "confirmed" {
			t.Errorf("expected body hold status to be 'confirmed', but got '%v'", retryBody.Hold.Status)
		}
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != confirmedHold {
			t.Errorf("expected stored hold be %v, but it was %v", confirmedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
	})

	errorTests := []struct {
		name          string
		eventId       string
		holdId        string
		status        HoldStatus
		operationKey  string
		missingKey    bool
		expectStatus  int
		expectCode    string
		expectMessage string
	}{
		{"operation key missing header", "1", "1", HoldStatusActive, "", true, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"operation key empty header", "1", "1", HoldStatusActive, "", false, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"operation key missing header, event not found", "0", "1", HoldStatusActive, "", true, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"operation key empty header, event not found", "0", "1", HoldStatusActive, "", false, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"operation key missing header, hold not found", "1", "0", HoldStatusActive, "", true, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"operation key empty header, hold not found", "1", "0", HoldStatusActive, "", false, 400, "operation_key_required", ErrOperationKeyRequired.Error()},
		{"event not found", "2", "1", HoldStatusActive, "1", false, 404, "event_not_found", "event not found"},
		{"hold not found", "1", "0", HoldStatusActive, "1", false, 404, "hold_not_found", ErrHoldNotFound.Error()},
		{"confirmed hold", "1", "1", HoldStatusConfirmed, "1", false, 409, "hold_already_confirmed", ErrHoldAlreadyConfirmed.Error()},
		{"cancelled hold", "1", "1", HoldStatusCancelled, "1", false, 409, "hold_cancelled", ErrHoldAlreadyCancelled.Error()},
		{"expired hold", "1", "1", HoldStatusExpired, "1", false, 409, "hold_expired", ErrHoldConfirmExpired.Error()},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			clock := MockClock{
				StaticTime: now,
			}
			event := setupEvent(t, now, 1)
			expectedHold := setupHold(t, event, now)
			if tt.status == HoldStatusConfirmed {
				var err error
				expectedHold, err = event.confirmHold(tt.holdId, now, "2")
				if err != nil {
					t.Fatalf("failed to confirm hold, %v", err.Error())
				}
			} else {
				expectedHold.status = tt.status
			}
			event.holds[expectedHold.id] = expectedHold
			availabilityStart := event.getAvailability()
			memory := Memory{
				events: make(map[string]*Event),
			}
			memory.events[event.id] = event
			server, err := initServer(&clock, memory)
			if err != nil {
				t.Fatalf("failed to init server, %v", err.Error())
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)
			req := httptest.NewRequest(http.MethodPost, "/events/"+tt.eventId+"/holds/"+tt.holdId+"/confirm", nil)
			if !tt.missingKey {
				req.Header.Set("operation-key", tt.operationKey)
			}
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			var body ErrorResponse
			decoder := json.NewDecoder(rr.Body)
			err = decoder.Decode(&body)
			if err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}
			err = decoder.Decode(&body)
			if err != io.EOF {
				t.Errorf("expected one JSON response, but also got %v", err)
			}
			status := rr.Code
			contentTypeHeader := rr.Header().Get("Content-Type")
			if status != tt.expectStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectStatus)
			}
			if contentTypeHeader != "application/json; charset=utf-8" {
				t.Errorf("Expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
			}

			availabilityEnd := event.getAvailability()
			storedHold, exists := event.getHoldByID("1")
			if body.Code != tt.expectCode {
				t.Errorf("expected '%v' code, but got '%v'", tt.expectCode, body.Code)
			}
			if body.Message != tt.expectMessage {
				t.Errorf("expected error message to be '%v', but got '%v'", tt.expectMessage, body.Message)
			}
			if !exists {
				t.Error("stored hold not found")
			}
			if storedHold != expectedHold {
				t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
			}
			if availabilityStart != availabilityEnd {
				t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
			}
		})
	}
}

func TestConfirmHoldHandlerConcurrent(t *testing.T) {
	t.Run("confirm two concurrent memory hold requests", func(t *testing.T) {
		responseChan := make(chan *httptest.ResponseRecorder)
		clock := MockClock{
			StaticTime: now,
		}
		event := setupEvent(t, now, 1)
		expectedHold := setupHold(t, event, now)
		expectedHold.status = HoldStatusConfirmed
		availabilityStart := event.getAvailability()
		memory := Memory{
			events: make(map[string]*Event),
		}
		memory.events[event.id] = event
		server, err := initServer(&clock, memory)
		if err != nil {
			t.Fatalf("failed to init server, %v", err.Error())
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)

		for range 2 {
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
				req.Header.Set("operation-key", "1")
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				responseChan <- rr
			}()
		}

		for range 2 {
			rr := <-responseChan
			var body SuccessResponse
			err = json.NewDecoder(rr.Body).Decode(&body)
			if err != nil {
				t.Fatalf("failed to decode success response: %v", err)
			}
			status := rr.Code
			contentTypeHeader := rr.Header().Get("Content-Type")
			if status != http.StatusOK {
				t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
			}
			if contentTypeHeader != "application/json; charset=utf-8" {
				t.Errorf("expected 'Content-Type: application/json; charset=utf-8', but was 'Content-Type: %v'", contentTypeHeader)
			}
			if body.Hold.ID != expectedHold.id {
				t.Errorf("expected body hold id to be %v, but got %v", expectedHold.id, body.Hold.ID)
			}
			if body.Hold.Quantity != expectedHold.quantity {
				t.Errorf("expected body hold quantity to be %v, but got %v", expectedHold.quantity, body.Hold.Quantity)
			}
			if !body.Hold.Deadline.Equal(expectedHold.deadline) {
				t.Errorf("expected body hold deadline to be %v, but got %v", expectedHold.deadline, body.Hold.Deadline)
			}
			if body.Hold.Status != "confirmed" {
				t.Errorf("expected body hold status to be 'confirmed', but got '%v'", body.Hold.Status)
			}
		}

		storedHold, exists := event.getHoldByID("1")
		availabilityEnd := event.getAvailability()
		if !exists {
			t.Error("stored hold not found")
		}
		if storedHold != expectedHold {
			t.Errorf("expected stored hold be %v, but it was %v", expectedHold, storedHold)
		}
		if availabilityStart != availabilityEnd {
			t.Errorf("expected availability to be %v, but it was %v", availabilityStart, availabilityEnd)
		}
		if len(event.operations) != 1 {
			t.Errorf("expected only one operation with identical, concurrent transition requests, instead got %v", len(event.operations))
		}
		if operation := event.operations["1"]; operation != expectedHold {
			t.Errorf("expected operation to match expected hold, instead contains %v", operation)
		}
		if slices.Contains(event.expirableHolds, storedHold.id) {
			t.Errorf("expected expirable holds to not contain hold, but contains %v", event.expirableHolds)
		}
	})

	t.Run("confirm two concurrent repository hold requests", func(t *testing.T) {
		responseChan := make(chan *httptest.ResponseRecorder, 2)
		eventChan := make(chan AcquireEvent, 2)
		now := time.Now().Truncate(time.Microsecond)
		deadline := now.Add(time.Minute * 30)
		clock := MockClock{
			StaticTime: now,
		}
		event := &Event{
			id:                 "1",
			capacity:           2,
			onsaleOpening:      now.Add(time.Minute),
			holdCreationCutoff: now.Add(time.Hour),
			confirmationCutoff: now.Add(time.Hour * 2),
		}
		hold := Hold{
			id:       "1",
			status:   HoldStatusActive,
			quantity: 1,
			deadline: deadline,
		}
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*30))
		defer cancel()
		connStr, _ := setupContainer(t, ctx)
		repo := setupRepositoryWithObserver(t, ctx, connStr, eventChan)
		_, err := repo.CreateEvent(ctx, event)
		require.NoError(t, err)
		_, err = repo.CreateHold(ctx, hold, event.id)
		require.NoError(t, err)
		server, err := initServer(&clock, repo)
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc("/events/{eventId}/holds/{holdId}/confirm", server.ConfirmHoldHandler)

		tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		err = tx.QueryRow(ctx,
			"SELECT id FROM events WHERE id = $1 FOR UPDATE",
			event.id).Scan(nil)
		require.NoError(t, err)

		aCtx := context.WithValue(ctx, workerKey, "A")
		bCtx := context.WithValue(ctx, workerKey, "B")

		for i := range 2 {
			var ctx context.Context
			if i == 1 {
				ctx = aCtx
			} else {
				ctx = bCtx
			}
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/events/1/holds/1/confirm", nil)
				req = req.WithContext(ctx)
				req.Header.Set("operation-key", "1")
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				responseChan <- rr
			}()
		}

		var pidOne uint32
		var pidTwo uint32
		for range 2 {
			select {
			case data := <-eventChan:
				require.NoError(t, data.Err)
				switch data.Worker {
				case "A":
					if pidOne != 0 {
						require.Fail(t, "received duplicate worker A")
					}
					pidOne = data.PID
				case "B":
					if pidTwo != 0 {
						require.Fail(t, "received duplicate worker B")
					}
					pidTwo = data.PID
				default:
					require.Fail(t, "unknown worker label")
				}
			case <-ctx.Done():
				require.Fail(t, "listening for events timed out")

			}
		}

		if pidOne == 0 {
			require.Fail(t, "pidOne is 0")
		}
		if pidTwo == 0 {
			require.Fail(t, "pidTwo is 0")
		}
		if pidOne == pidTwo {
			require.Fail(t, "pidOne matches pidTwo")
		}

		ticker := time.NewTicker(time.Millisecond * 10)
		defer ticker.Stop()
		for range ticker.C {
			var blockingPidsOne []int64
			var blockingPidsTwo []int64
			err = tx.QueryRow(ctx, "SELECT pg_blocking_pids($1)", pidOne).Scan(&blockingPidsOne)
			require.NoError(t, err)
			err = tx.QueryRow(ctx, "SELECT pg_blocking_pids($1)", pidTwo).Scan(&blockingPidsTwo)
			require.NoError(t, err)
			if len(blockingPidsOne) != 0 && len(blockingPidsTwo) != 0 {
				break
			}
		}
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		for range 2 {
			select {
			case rr := <-responseChan:
				var body SuccessResponse
				err = json.NewDecoder(rr.Body).Decode(&body)
				require.NoError(t, err)
				status := rr.Code
				contentTypeHeader := rr.Header().Get("Content-Type")
				assert.Equal(t, http.StatusOK, status)
				assert.Equal(t, "application/json; charset=utf-8", contentTypeHeader)

				assert.Equal(t, hold.id, body.Hold.ID)
				assert.Equal(t, "confirmed", body.Hold.Status)
				assert.Equal(t, hold.quantity, body.Hold.Quantity)
				assert.Equal(t, hold.deadline, body.Hold.Deadline)
			case <-aCtx.Done():
				require.Fail(t, "waiting for request responses timed out")
			}
		}

		storedHold, err := repo.ReadHold(ctx, hold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, hold.id, storedHold.id)
		assert.Equal(t, HoldStatusConfirmed, storedHold.status)
		assert.Equal(t, hold.quantity, storedHold.quantity)
		assert.Equal(t, hold.deadline, storedHold.deadline)

		operation, err := repo.ReadOperation(ctx, "1", storedHold.id, event.id)
		assert.NoError(t, err)
		assert.Equal(t, storedHold, operation)

		var operationCount int
		err = repo.pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM operations").Scan(&operationCount)
		assert.NoError(t, err)
		assert.Equal(t, 1, operationCount)
	})
}
