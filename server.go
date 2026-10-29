package rushline

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// TODO: Temporary struct, will be replaced by a db
type Server struct {
	events map[string]*Event
	clock  Clock
}

type HoldResponse struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	Quantity int       `json:"quantity"`
	Deadline time.Time `json:"deadline"`
}

type SuccessResponse struct {
	Hold HoldResponse `json:"hold"`
}

type ErrorResponse struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func initServer(clock Clock) (*Server, error) {
	newServer := &Server{
		events: make(map[string]*Event),
		clock:  clock,
	}

	return newServer, nil
}

// TODO: Temporary method, will be replaced by an HTTP handler
func (s *Server) addEvent(event *Event) {
	s.events[event.id] = event
}

func writeJSONSuccess(w http.ResponseWriter, hold Hold) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	var status string
	switch hold.status {
	case 1:
		status = "active"
	case 2:
		status = "confirmed"
	case 3:
		status = "cancelled"
	case 4:
		status = "expired"
	}

	payload := SuccessResponse{
		Hold: HoldResponse{
			ID:       hold.id,
			Status:   status,
			Quantity: hold.quantity,
			Deadline: hold.deadline,
		},
	}
	return json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, message string, code string, status int) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status == 405 {
		w.Header().Set("Allow", "POST")
	}
	w.WriteHeader(status)

	if status == 500 {
		message = "Internal Server Error"
	}

	payload := ErrorResponse{
		Message: message,
		Code:    code,
	}
	return json.NewEncoder(w).Encode(payload)
}

func (s *Server) ConfirmHoldHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		if err := writeJSONError(w, "method not allowed", "method_not_allowed", http.StatusMethodNotAllowed); err != nil {
			slog.Error("failed to write JSON response", "error", err, "path", r.URL.Path)
		}
		return
	}

	eventId := r.PathValue("eventId")
	holdId := r.PathValue("holdId")
	operationKey := r.Header.Get("operation-key")

	if len(operationKey) == 0 {
		if err := writeJSONError(w, ErrOperationKeyRequired.Error(), "operation_key_required", 400); err != nil {
			slog.Error("failed to write JSON response", "error", err, "path", r.URL.Path)
		}
		return
	}

	event, ok := s.events[eventId]

	if !ok {
		if err := writeJSONError(w, "event does not exist", "event_not_found", http.StatusNotFound); err != nil {
			slog.Error("failed to write JSON response", "error", err, "path", r.URL.Path)
		}
		return
	}

	hold, err := event.confirmHold(holdId, s.clock.Now(), operationKey)
	if err != nil {
		var code string
		var status int
		if errors.Is(err, ErrHoldNotFound) {
			code = "hold_not_found"
			status = 404
		} else if errors.Is(err, ErrHoldAlreadyConfirmed) {
			code = "hold_already_confirmed"
			status = 409
		} else if errors.Is(err, ErrHoldAlreadyCancelled) {
			code = "hold_cancelled"
			status = 409
		} else if errors.Is(err, ErrHoldConfirmExpired) {
			code = "hold_expired"
			status = 409
		} else if errors.Is(err, ErrOperationKeyConflict) {
			code = "operation_key_conflict"
			status = 409
		} else if errors.Is(err, ErrOperationKeyRequired) {
			code = "operation_key_required"
			status = 400
		} else if errors.Is(err, ErrAfterConfirmationDeadline) {
			code = "after_confirmation_deadline"
			status = 409
		} else {
			code = "internal_error"
			status = 500
			slog.Error("hold confirmation failed", "error", err, "path", r.URL.Path)
		}
		if err := writeJSONError(w, err.Error(), code, status); err != nil {
			slog.Error("failed to write JSON response", "error", err, "path", r.URL.Path)
		}
		return
	}

	if err := writeJSONSuccess(w, hold); err != nil {
		slog.Error("failed to write JSON response", "error", err, "path", r.URL.Path)
	}
}
