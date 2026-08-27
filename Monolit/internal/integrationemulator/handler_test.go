package integrationemulator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmulatorMediaFaultAndWebhookJournal(t *testing.T) {
	e := New()
	server := httptest.NewServer(e.Handler())
	defer server.Close()
	resp, err := http.Get(server.URL + "/media/test.wav")
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("media status=%v err=%v", resp, err)
	}
	_ = resp.Body.Close()
	resp, err = http.Get(server.URL + "/media/test.wav?status=503")
	if err != nil || resp.StatusCode != 503 {
		t.Fatalf("fault status=%v err=%v", resp, err)
	}
	_ = resp.Body.Close()
	resp, err = http.Post(server.URL+"/webhooks", "application/json", bytes.NewBufferString(`{"id":"evt"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	resp, err = http.Get(server.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload struct {
		Events []Event `json:"events"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Events) != 1 {
		t.Fatalf("events=%d err=%v", len(payload.Events), err)
	}
}
