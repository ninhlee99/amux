package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"amux-accounts/pkg/browser"
	"amux-accounts/pkg/muse"
	"amux-accounts/pkg/tools"
	"amux-accounts/pkg/types"
)

// museReplyTimeout bounds one Muse turn (it can run long agentic tasks).
const museReplyTimeout = 5 * time.Minute

// MuseWebAdapter serves Meta Muse (muse.ai) by driving its web app in a
// dedicated, logged-in browser profile over CDP — the same login is shared
// with `amux mcp`'s muse_* tools. Text-only: client tools[] go through the
// web tool-call emulation (<tool_call> blocks) like the other web backends.
type MuseWebAdapter struct {
	AdapterID   string
	PriorityLvl int
	TargetModel string
	Profile     string // browser profile name (default "muse")
	Endpoint    string // optional DevTools endpoint of an already-running Chrome

	mu      sync.Mutex
	convMgr *ProjectConversationManager
	// chat is the driver call; replaced in tests.
	chat func(ctx context.Context, prompt string, o muse.ChatOptions) (*muse.ChatResult, error)
}

func (a *MuseWebAdapter) ID() string          { return a.AdapterID }
func (a *MuseWebAdapter) Priority() int       { return a.PriorityLvl }
func (a *MuseWebAdapter) Plan() string        { return "free" }
func (a *MuseWebAdapter) SupportsTools() bool { return false }

// Model is the model id reported back to clients.
func (a *MuseWebAdapter) Model() string {
	if a.TargetModel != "" {
		return a.TargetModel
	}
	return muse.DefaultModel
}

func (a *MuseWebAdapter) driverConfig() muse.Config {
	c := muse.ConfigFromEnv()
	if a.Profile != "" {
		c.Profile = a.Profile
	}
	if a.Endpoint != "" {
		c.Endpoint = a.Endpoint
	}
	return c
}

func (a *MuseWebAdapter) convs() *ProjectConversationManager {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.convMgr == nil {
		a.convMgr = NewProjectConversationManager(a.AdapterID, DefaultMaxTurnsPerConversation, DefaultConversationTTL)
	}
	return a.convMgr
}

func (a *MuseWebAdapter) send(ctx context.Context, prompt string, o muse.ChatOptions) (*muse.ChatResult, error) {
	if a.chat != nil {
		return a.chat(ctx, prompt, o)
	}
	return muse.Shared(a.driverConfig()).Chat(ctx, prompt, o)
}

// musePrompt flattens the request for Muse. Muse refuses prompts framed as a
// role-play transcript, so the trailing "Assistant:" cue the shared web
// prompt builder appends is dropped.
func musePrompt(req *types.ChatRequest, continuing bool) string {
	p := strings.TrimSpace(WebBackendPrompt(req, continuing))
	p = strings.TrimSpace(strings.TrimSuffix(p, "Assistant:"))
	return p
}

func (a *MuseWebAdapter) SendMessageStream(ctx context.Context, req *types.ChatRequest) (<-chan types.StreamChunk, error) {
	project := req.Project()
	cm := a.convs()
	opts := muse.ChatOptions{Timeout: museReplyTimeout}
	continuing := false
	if req.FullContext {
		// Coding agents resend the full transcript each turn: run it stateless
		// on a fresh side chat so Muse's own thread memory cannot drift.
		opts.NewThread = true
	} else if conv, ok := cm.GetActive(project); ok && conv != nil && conv.ID != "" {
		opts.Chat = conv.ID
		continuing = true
	} else {
		opts.NewThread = true
	}
	prompt := musePrompt(req, continuing)
	if prompt == "" {
		return nil, fmt.Errorf("%s: empty prompt", a.AdapterID)
	}

	deltas := make(chan string, 64)
	type outcome struct {
		res *muse.ChatResult
		err error
	}
	doneCh := make(chan outcome, 1)
	opts.OnDelta = func(full string) {
		select {
		case deltas <- full:
		default: // client is slow; the final reply still carries everything
		}
	}
	go func() {
		res, err := a.send(ctx, prompt, opts)
		doneCh <- outcome{res, err}
	}()

	// Hold the response until the first text (or the outcome) so failures
	// before any output surface as errors the pool can fail over on.
	var first string
	var early *outcome
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case first = <-deltas:
	case o := <-doneCh:
		early = &o
	}
	if early != nil && early.err != nil {
		return nil, fmt.Errorf("%s: %w", a.AdapterID, classifyMuseErr(early.err))
	}

	out := make(chan types.StreamChunk, 16)
	go func() {
		defer close(out)
		sent := ""
		emit := func(full string) bool {
			if !strings.HasPrefix(full, sent) || len(full) == len(sent) {
				return true
			}
			delta := full[len(sent):]
			sent = full
			return sendChunk(ctx, out, types.StreamChunk{ID: a.AdapterID, Content: delta})
		}
		finish := func(o outcome) {
			if o.err != nil {
				sendChunk(ctx, out, types.StreamChunk{ID: a.AdapterID, Error: classifyMuseErr(o.err), Done: true})
				return
			}
			if o.res != nil {
				emit(o.res.Reply)
				if !req.FullContext && o.res.ThreadURL != "" {
					cm.Register(project, req.SessionID, o.res.ThreadURL, "", nil)
				}
			}
			sendChunk(ctx, out, types.StreamChunk{ID: a.AdapterID, FinishReason: "stop", Done: true, LogText: sent})
		}
		if early != nil {
			finish(*early)
			return
		}
		if !emit(first) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case full := <-deltas:
				if !emit(full) {
					return
				}
			case o := <-doneCh:
				// Drain deltas that raced the outcome, then finish.
				for {
					select {
					case full := <-deltas:
						emit(full)
						continue
					default:
					}
					break
				}
				finish(o)
				return
			}
		}
	}()
	return tools.MaybeWrapWebStream(a.AdapterID, req, out), nil
}

// classifyMuseErr maps driver failures onto the pool's sentinel errors.
func classifyMuseErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, types.ErrAuthentication) || errors.Is(err, types.ErrRateLimitReached) {
		return err
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "amux login muse"), strings.Contains(msg, "not signed in"), strings.Contains(msg, "log in"):
		return fmt.Errorf("%w: %v", types.ErrAuthentication, err)
	case strings.Contains(msg, "limit"), strings.Contains(msg, "too many"), strings.Contains(msg, "try again later"):
		return fmt.Errorf("%w: %v", types.ErrRateLimitReached, err)
	}
	return err
}

// MuseProfileReady reports whether a Muse browser profile has been created by
// `amux login muse` (the login itself is verified lazily in the page).
func MuseProfileReady(profile string) bool {
	if profile == "" {
		profile = "muse"
	}
	return browser.ProfileExists(profile)
}
