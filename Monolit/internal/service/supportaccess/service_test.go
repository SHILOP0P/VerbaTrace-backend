package supportaccess

import "testing"

func TestOnlyAllowedRejectsUnknownScope(t *testing.T) {
	if !onlyAllowed([]string{"calls", " actions "}, allowedResources) {
		t.Fatal("known resources must be accepted")
	}
	if onlyAllowed([]string{"calls", "all_customer_data"}, allowedResources) {
		t.Fatal("unknown resource must be rejected")
	}
	if onlyAllowed([]string{"diagnose", ""}, allowedCommands) {
		t.Fatal("empty command must be rejected")
	}
}
