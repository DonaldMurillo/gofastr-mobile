package nativeui

// lockConnection retires protected instances without replaying pending writes.
// Application storage must retain a recoverable identity before starting OS work.
func (runtime *Runtime) lockConnection(connection string) {
	runtime.mu.Lock()
	runtime.privacyGeneration++
	if runtime.options.OnLock != nil {
		runtime.options.OnLock()
	}
	sessions := make([]*runtimeSession, 0, len(runtime.sessions))
	for _, session := range runtime.sessions {
		sessions = append(sessions, session)
	}
	runtime.mu.Unlock()
	for _, session := range sessions {
		session.mu.Lock()
		// OnLock revokes application-wide grants, so every protected session
		// using those grants must lose its cached replies too.
		if (session.connection == connection || runtime.options.OnLock != nil) && session.screen.Protection != nil {
			session.privacyRevoked, session.closed = true, true
			if !session.active {
				runtime.scrubProtected(session)
			}
		}
		session.mu.Unlock()
	}
}

func (runtime *Runtime) scrubProtected(session *runtimeSession) {
	runtime.cancelWatch(session)
	runtime.releasePresentation(session)
	runtime.releaseFiles(session)
	runtime.releaseService(session)
	runtime.releaseDraft(session)
	route := session.screen.Route
	if session.screen.Protection != nil {
		route = session.screen.Protection.LockRoute
	}
	session.screen = Screen{Route: route, Title: "Locked"}
	session.editor = nil
	session.actions = make(map[string]*runtimeAction)
	session.effects = make(map[string]EffectReceipt)
}

func (runtime *Runtime) finishRevoked(session *runtimeSession, reply *Reply) bool {
	if !session.privacyRevoked {
		return false
	}
	runtime.scrubProtected(session)
	reply.Response, reply.Editor = nil, nil
	reply.Error = commandFailure("expired", "This protected screen was locked. Open it again to authenticate.")
	return true
}
