package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var nodes = []string{
	"http://localhost:8001",
	"http://localhost:8002",
	"http://localhost:8003",
}

var cmds []*exec.Cmd

func binaryPath() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	name := "raft-kv-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func startNode(id, httpAddr, raftAddr, joinAddr string) {
	args := []string{
		"--id=" + id,
		"--http=" + httpAddr,
		"--raft=" + raftAddr,
	}
	if joinAddr != "" {
		args = append(args, "--join="+joinAddr)
	}

	cmd := exec.Command(binaryPath(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		panic(err)
	}
	cmds = append(cmds, cmd)
}

func waitForNode(url string) {
	for i := 0; i < 40; i++ {
		resp, err := http.Get(url + "/status")
		if err == nil && resp.StatusCode == 200 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	panic("Node not starting: " + url)
}

func stopNodes() {
	for _, cmd := range cmds {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
}

func waitForLeader(t *testing.T) {
	timeout := time.After(25 * time.Second)
	tick := time.Tick(500 * time.Millisecond)

	for {
		select {
		case <-timeout:
			t.Fatal("Leader not elected within timeout")

		case <-tick:
			leaderCount := 0
			for _, node := range nodes {
				resp, err := http.Get(node + "/status")
				if err != nil {
					continue
				}
				var data map[string]string
				json.NewDecoder(resp.Body).Decode(&data)
				if data["role"] == "leader" {
					leaderCount++
				}
			}
			if leaderCount == 1 {
				return
			}
		}
	}
}

func TestMain(m *testing.M) {
	bin := binaryPath()

	fmt.Println("Building binary...")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("Build failed: " + err.Error())
	}
	fmt.Println("Build done:", bin)

	// Clean old raft state
	os.RemoveAll("node1")
	os.RemoveAll("node2")
	os.RemoveAll("node3")
	os.Remove("node1.db")
	os.Remove("node2.db")
	os.Remove("node3.db")

	// node1 bootstraps as a single-node cluster and elects itself leader
	startNode("node1", "localhost:8001", "localhost:9001", "")
	waitForNode("http://localhost:8001")
	time.Sleep(3 * time.Second)

	// node2 and node3 join via node1
	startNode("node2", "localhost:8002", "localhost:9002", "localhost:8001")
	waitForNode("http://localhost:8002")

	startNode("node3", "localhost:8003", "localhost:9003", "localhost:8001")
	waitForNode("http://localhost:8003")

	time.Sleep(3 * time.Second)

	code := m.Run()
	stopNodes()
	os.Remove(bin)
	os.Exit(code)
}

func TestLeaderElection(t *testing.T) {
	waitForLeader(t)
}

func TestWriteForwarding(t *testing.T) {
	waitForLeader(t)

	payload := map[string]string{
		"op":              "PUT",
		"key":             "testkey",
		"value":           "testvalue",
		"idempotency_key": "forward1",
	}
	body, _ := json.Marshal(payload)

	http.Post(nodes[1]+"/put", "application/json", bytes.NewBuffer(body))
	time.Sleep(2 * time.Second)

	for _, node := range nodes {
		resp, err := http.Get(node + "/get?key=testkey")
		if err != nil {
			t.Fatal(err)
		}
		var result bytes.Buffer
		result.ReadFrom(resp.Body)
		if result.String() != "testvalue" {
			t.Errorf("Replication failed on %s: got %q", node, result.String())
		}
	}
}

func TestIdempotency(t *testing.T) {
	waitForLeader(t)

	payload := map[string]string{
		"op":              "PUT",
		"key":             "dupkey",
		"value":           "value",
		"idempotency_key": "same123",
	}
	body, _ := json.Marshal(payload)

	http.Post(nodes[0]+"/put", "application/json", bytes.NewBuffer(body))
	http.Post(nodes[0]+"/put", "application/json", bytes.NewBuffer(body))
	time.Sleep(2 * time.Second)

	resp, _ := http.Get(nodes[0] + "/get?key=dupkey")
	var result bytes.Buffer
	result.ReadFrom(resp.Body)
	if result.String() != "value" {
		t.Errorf("Idempotency failed: got %q", result.String())
	}
}
