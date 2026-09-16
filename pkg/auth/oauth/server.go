package oauth

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type callbackResult struct {
	Code  string
	State string
	Error string
}

// listenForCallback starts a single-use local HTTP server listening on the specified
// port and path, waiting for the OAuth redirect callback.
func listenForCallback(ctx context.Context, port int, path string, expectedState string) (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("could not listen on %s: %w (is another process using this port?)", addr, err)
	}

	ch := make(chan callbackResult, 1)
	// sendResult drops duplicate incoming results (e.g. browser reload or favicon requests)
	// without blocking the handler goroutine.
	sendResult := func(res callbackResult) {
		select {
		case ch <- res:
		default:
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		errParam := q.Get("error")
		errDesc := q.Get("error_description")
		code := q.Get("code")
		state := q.Get("state")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("Clear-Site-Data", `"cookies", "storage"`)

		if errParam != "" {
			errMsg := errParam
			if errDesc != "" {
				errMsg += ": " + errDesc
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorHTML, errMsg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			sendResult(callbackResult{Error: errMsg})
			return
		}

		if expectedState != "" {
			hExpected := sha256.Sum256([]byte(expectedState))
			hActual := sha256.Sum256([]byte(state))
			if state == "" || subtle.ConstantTimeCompare(hExpected[:], hActual[:]) != 1 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, errorHTML, "State parameter mismatch or missing (potential CSRF attempt)")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				sendResult(callbackResult{Error: "state mismatch"})
				return
			}
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorHTML, "Missing authorization code in callback")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			sendResult(callbackResult{Error: "missing code"})
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, successHTML)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		sendResult(callbackResult{Code: code, State: state})
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Transfer listener ownership to srv.Serve (closes listener on shutdown)
	go func() {
		_ = srv.Serve(listener)
	}()

	// Background stdin listener for remote / headless / SSH environments
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			// Case 1: Full callback URL
			if strings.Contains(text, "code=") {
				if u, err := url.Parse(text); err == nil {
					q := u.Query()
					if errParam := q.Get("error"); errParam != "" {
						errDesc := q.Get("error_description")
						if errDesc != "" {
							errParam += ": " + errDesc
						}
						sendResult(callbackResult{Error: errParam})
						return
					}
					c := q.Get("code")
					st := q.Get("state")
					if expectedState != "" && st != "" && st != expectedState {
						sendResult(callbackResult{Error: "state mismatch"})
						return
					}
					if c != "" {
						sendResult(callbackResult{Code: c, State: st})
						return
					}
				}
			}
			// Case 2: code#state or code:state
			if strings.Contains(text, "#") {
				parts := strings.SplitN(text, "#", 2)
				c := strings.TrimSpace(parts[0])
				st := strings.TrimSpace(parts[1])
				if expectedState != "" && st != "" && st != expectedState {
					sendResult(callbackResult{Error: "state mismatch"})
					return
				}
				if c != "" {
					sendResult(callbackResult{Code: c, State: st})
					return
				}
			}
			// Case 3: Raw authorization code pasted directly
			sendResult(callbackResult{Code: text, State: expectedState})
			return
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(shutdownCtx)
		cancel()
		return "", ctx.Err()
	case res := <-ch:
		// Drain loopback connection in background after allowing brief moment (100ms)
		// for TCP buffers to flush rendered HTML to browser without socket reset.
		go func() {
			time.Sleep(100 * time.Millisecond)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
		if res.Error != "" {
			return "", fmt.Errorf("oauth error from provider: %s", res.Error)
		}
		return res.Code, nil
	}
}

const successHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>amux - Authentication Successful</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      display: flex;
      justify-content: center;
      align-items: center;
      height: 100vh;
      margin: 0;
      background: #0f172a;
      color: #f8fafc;
      text-align: center;
    }
    .card {
      background: #1e293b;
      padding: 2.5rem;
      border-radius: 1rem;
      border: 1px solid #334155;
      box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5);
      max-width: 440px;
    }
    .icon {
      font-size: 3rem;
      margin-bottom: 1rem;
    }
    h1 {
      color: #38bdf8;
      font-size: 1.5rem;
      margin-top: 0;
      margin-bottom: 0.5rem;
    }
    p {
      color: #94a3b8;
      font-size: 0.95rem;
      line-height: 1.5;
      margin: 0;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">✨</div>
    <h1>Authentication Successful</h1>
    <p>Your credentials have been securely received by <strong>amux</strong>.<br>You can now close this tab and return to your terminal.</p>
  </div>
  <script>
    setTimeout(function() {
      window.close();
    }, 2000);
  </script>
</body>
</html>`

const errorHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>amux - Authentication Failed</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      display: flex;
      justify-content: center;
      align-items: center;
      height: 100vh;
      margin: 0;
      background: #0f172a;
      color: #f8fafc;
      text-align: center;
    }
    .card {
      background: #1e293b;
      padding: 2.5rem;
      border-radius: 1rem;
      border: 1px solid #ef4444;
      box-shadow: 0 10px 25px -5px rgba(0,0,0,0.5);
      max-width: 420px;
    }
    .icon {
      font-size: 3rem;
      margin-bottom: 1rem;
    }
    h1 {
      color: #f87171;
      font-size: 1.5rem;
      margin-top: 0;
      margin-bottom: 0.5rem;
    }
    p {
      color: #94a3b8;
      font-size: 0.95rem;
      line-height: 1.5;
      margin: 0;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">⚠️</div>
    <h1>Authentication Failed</h1>
    <p>%s</p>
  </div>
</body>
</html>`
