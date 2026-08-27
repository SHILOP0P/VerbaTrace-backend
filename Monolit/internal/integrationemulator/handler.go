package integrationemulator

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Event struct {
	Headers    http.Header     `json:"headers"`
	Body       json.RawMessage `json:"body"`
	ReceivedAt time.Time       `json:"received_at"`
}
type Emulator struct {
	mu     sync.Mutex
	events []Event
}

func New() *Emulator { return &Emulator{} }
func (e *Emulator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /media/test.wav", func(w http.ResponseWriter, r *http.Request) {
		applyDelay(r)
		if code := status(r); code != 200 {
			w.WriteHeader(code)
			return
		}
		payload := silentWAV()
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("POST /webhooks", func(w http.ResponseWriter, r *http.Request) {
		applyDelay(r)
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		e.mu.Lock()
		e.events = append(e.events, Event{Headers: r.Header.Clone(), Body: append([]byte(nil), raw...), ReceivedAt: time.Now().UTC()})
		e.mu.Unlock()
		w.WriteHeader(status(r))
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, _ *http.Request) {
		e.mu.Lock()
		events := append([]Event(nil), e.events...)
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
	})
	mux.HandleFunc("DELETE /events", func(w http.ResponseWriter, _ *http.Request) {
		e.mu.Lock()
		e.events = nil
		e.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
func status(r *http.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("status"))
	if value < 100 || value > 599 {
		return 200
	}
	return value
}
func applyDelay(r *http.Request) {
	value, _ := strconv.Atoi(r.URL.Query().Get("delay_ms"))
	if value > 0 && value <= 5000 {
		time.Sleep(time.Duration(value) * time.Millisecond)
	}
}
func silentWAV() []byte {
	const samples = 8000
	data := make([]byte, 44+samples*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], samples*2)
	return data
}
