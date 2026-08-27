package mediafetch

import "testing"

func TestValidateURLRejectsSSRFVectors(t *testing.T) {
	bad := []string{"http://example.com/a.mp3", "https://127.0.0.1/a", "https://[::1]/a", "https://user:pass@example.com/a", "https://example.com/a#secret"}
	for _, raw := range bad {
		if ValidateURL(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := ValidateURL("https://media.example.com/call.mp3"); err != nil {
		t.Fatal(err)
	}
}
