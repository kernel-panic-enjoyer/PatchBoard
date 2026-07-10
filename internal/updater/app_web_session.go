package updater

import "sync"

// webSession owns the local WebUI's bootstrap credential, cookie session, and
// bound loopback endpoint. The credentials and endpoint are immutable after
// server startup; bootstrapMu protects only the one-time URL-to-cookie
// transition so it cannot contend with inventory or job state.
type webSession struct {
	bootstrapToken string
	sessionToken   string
	listenHost     string
	listenPort     int

	bootstrapMu   sync.Mutex
	bootstrapUsed bool
}

func (session *webSession) consumeBootstrapToken(token string) bool {
	if !tokensEqualConstantTime(token, session.bootstrapToken) {
		return false
	}
	session.bootstrapMu.Lock()
	defer session.bootstrapMu.Unlock()
	if session.bootstrapUsed {
		return false
	}
	session.bootstrapUsed = true
	return true
}
