package renderer

import (
	"context"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// URLValidator validates whether a URL is safe to fetch.
type URLValidator interface {
	ValidateURL(ctx context.Context, rawURL string) error
}

type requestGuard struct {
	validator URLValidator
}

func newRequestGuard(validator URLValidator) *requestGuard {
	return &requestGuard{validator: validator}
}

func (g *requestGuard) attach(page *rod.Page) (*guardSession, error) {
	session := &guardSession{
		router:  page.HijackRequests(),
		blocked: &blockedRequestState{},
	}

	if err := session.router.Add("*", "", func(h *rod.Hijack) {
		requestURL := h.Request.URL().String()
		if err := g.validator.ValidateURL(h.Request.Req().Context(), requestURL); err != nil {
			session.blocked.set(err)
			h.Response.Fail(proto.NetworkErrorReasonAccessDenied)
			return
		}
		h.ContinueRequest(&proto.FetchContinueRequest{})
	}); err != nil {
		return nil, err
	}

	go session.router.Run()
	return session, nil
}

type guardSession struct {
	router  *rod.HijackRouter
	blocked *blockedRequestState
}

func (s *guardSession) blockedErr() error {
	return s.blocked.get()
}

func (s *guardSession) close() error {
	if s == nil || s.router == nil {
		return nil
	}
	return s.router.Stop()
}

type blockedRequestState struct {
	mu  sync.Mutex
	err error
}

func (s *blockedRequestState) set(err error) {
	if err == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *blockedRequestState) get() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
