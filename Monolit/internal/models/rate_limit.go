package models

import "time"

// RateLimitRule is one counter: how many failures inside a window, and for how
// long the subject is locked out once it crosses that.
type RateLimitRule struct {
	Scope     string
	Threshold int
	Window    time.Duration
	Block     time.Duration
}

// The thresholds are deliberately mild: they stop guessing without locking out
// a person who mistyped their password twice.
var (
	LoginAccountRateLimit = RateLimitRule{Scope: "login_account", Threshold: 5, Window: 15 * time.Minute, Block: 15 * time.Minute}
	LoginIPRateLimit      = RateLimitRule{Scope: "login_ip", Threshold: 20, Window: 15 * time.Minute, Block: 15 * time.Minute}
	SignupIPRateLimit     = RateLimitRule{Scope: "signup_ip", Threshold: 5, Window: time.Hour, Block: time.Hour}
)
