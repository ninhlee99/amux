package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
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
	defer listener.Close()

	ch := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		errParam := q.Get("error")
		errDesc := q.Get("error_description")
		code := q.Get("code")
		state := q.Get("state")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if errParam != "" {
			errMsg := errParam
			if errDesc != "" {
				errMsg += ": " + errDesc
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorHTML, errMsg)
			ch <- callbackResult{Error: errMsg}
			return
		}

		if expectedState != "" && state != expectedState {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorHTML, "State parameter mismatch (CSRF token error)")
			ch <- callbackResult{Error: "state mismatch"}
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorHTML, "Missing authorization code in callback")
			ch <- callbackResult{Error: "missing code"}
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, successHTML)
		ch <- callbackResult{Code: code, State: state}
	})

	srv := &http.Server{Handler: mux}

	go func() {
		_ = srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		return "", ctx.Err()
	case res := <-ch:
		// Give the browser a moment to receive the HTML response
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = srv.Shutdown(context.Background())
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
      max-width: 420px;
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
    <p>Your credentials have been securely received.<br>You can now close this tab and return to <strong>amux</strong> in your terminal.</p>
  </div>
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
