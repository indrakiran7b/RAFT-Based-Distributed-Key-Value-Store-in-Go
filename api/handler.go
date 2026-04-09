package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"raft-kv/model"
	"raft-kv/node"

	"github.com/hashicorp/raft"
)

type Server struct {
	Node *node.Node
}

func raftToHTTP(addr string) string {
	switch addr {
	case "localhost:9001", "127.0.0.1:9001":
		return "localhost:8001"
	case "localhost:9002", "127.0.0.1:9002":
		return "localhost:8002"
	case "localhost:9003", "127.0.0.1:9003":
		return "localhost:8003"
	default:
		return ""
	}
}

func (s *Server) forward(leader string, r *http.Request, w http.ResponseWriter) {
	leaderHTTP := raftToHTTP(leader)
	if leaderHTTP == "" {
		http.Error(w, "Leader not found", 500)
		return
	}

	body, _ := io.ReadAll(r.Body)

	resp, err := http.Post(
		"http://"+leaderHTTP+"/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}

// applyWithRetry submits a command to Raft and retries up to 3 times on
// ErrEnqueueTimeout. This covers burst scenarios where the Raft apply queue
// is momentarily full — the queue drains quickly (ms) so a short backoff
// is enough to absorb the spike without failing the request.
func (s *Server) applyWithRetry(data []byte) error {
	var err error
	backoff := 20 * time.Millisecond
	for i := 0; i < 3; i++ {
		f := s.Node.Raft.Apply(data, 5*time.Second)
		err = f.Error()
		if err == nil {
			return nil
		}
		if err != raft.ErrEnqueueTimeout {
			return err // non-retryable (e.g. not leader, shutdown)
		}
		time.Sleep(backoff)
		backoff *= 2 // 20ms → 40ms → 80ms
	}
	return err
}

func (s *Server) Put(w http.ResponseWriter, r *http.Request) {
	if s.Node.Raft.State() != raft.Leader {
		s.forward(string(s.Node.Raft.Leader()), r, w)
		return
	}

	var cmd model.Command
	json.NewDecoder(r.Body).Decode(&cmd)

	data, _ := json.Marshal(cmd)

	if err := s.applyWithRetry(data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}

func (s *Server) Get(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	val := s.Node.FSM.Get(key)
	w.Write([]byte(val))
}

func (s *Server) Status(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{
		"role":   strings.ToLower(s.Node.Raft.State().String()),
		"leader": string(s.Node.Raft.Leader()),
	})
}

func (s *Server) Join(w http.ResponseWriter, r *http.Request) {
	if s.Node.Raft.State() != raft.Leader {
		leader := string(s.Node.Raft.Leader())
		leaderHTTP := raftToHTTP(leader)
		if leaderHTTP == "" {
			http.Error(w, "no leader available", 503)
			return
		}
		body, _ := io.ReadAll(r.Body)
		resp, err := http.Post("http://"+leaderHTTP+"/join", "application/json", bytes.NewBuffer(body))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer resp.Body.Close()
		io.Copy(w, resp.Body)
		return
	}

	var req struct {
		ID   string `json:"id"`
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if err := s.Node.Join(req.ID, req.Addr); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}
